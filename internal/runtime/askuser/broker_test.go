package askuser

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBrokerNilReceiverBranches(t *testing.T) {
	t.Parallel()

	var b *Broker

	_, _, err := b.Open(context.Background(), Request{QuestionID: "q1"})
	if err == nil || !strings.Contains(err.Error(), "broker is nil") {
		t.Fatalf("expected broker nil error from Open, got %v", err)
	}

	err = b.Resolve("ask-1", Result{Status: StatusAnswered})
	if err == nil || !strings.Contains(err.Error(), "broker is nil") {
		t.Fatalf("expected broker nil error from Resolve, got %v", err)
	}

	// Close on nil broker should not panic
	b.Close("ask-1")
}

func TestBrokerOpenResolveCloseFlow(t *testing.T) {
	t.Parallel()

	broker := NewBroker()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	var openResult Result
	var openErr error

	go func() {
		defer wg.Done()
		_, openResult, openErr = broker.Open(ctx, Request{QuestionID: "q1", TimeoutSec: 10})
	}()

	// Allow goroutine to enter Open and register the request.
	time.Sleep(50 * time.Millisecond)

	ids := broker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}
	requestID := ids[0]

	resolveErr := broker.Resolve(requestID, Result{
		Status: StatusAnswered,
		Values: []string{"opt-a", "opt-b"},
	})
	if resolveErr != nil {
		t.Fatalf("Resolve error: %v", resolveErr)
	}

	wg.Wait()

	if openErr != nil {
		t.Fatalf("Open error: %v", openErr)
	}
	if openResult.Status != StatusAnswered {
		t.Fatalf("expected answered status, got %q", openResult.Status)
	}
	if openResult.QuestionID != "q1" {
		t.Fatalf("expected question_id q1, got %q", openResult.QuestionID)
	}
	if len(openResult.Values) != 2 || openResult.Values[0] != "opt-a" || openResult.Values[1] != "opt-b" {
		t.Fatalf("unexpected values: %v", openResult.Values)
	}

	// Request should be cleaned up after Open returns
	err := broker.Resolve(requestID, Result{Status: StatusSkipped})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error after close, got %v", err)
	}
}

func TestBrokerResolveUnknownRequest(t *testing.T) {
	t.Parallel()

	broker := NewBroker()

	err := broker.Resolve("unknown-ask-1", Result{Status: StatusAnswered})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestBrokerOpenUsesProvidedRequestID(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	var returnedID string
	go func() {
		defer wg.Done()
		requestID, _, _ := broker.Open(ctx, Request{
			RequestID:  "ask-provided",
			QuestionID: "q1",
			TimeoutSec: 10,
		})
		returnedID = requestID
	}()

	time.Sleep(50 * time.Millisecond)
	if err := broker.Resolve("ask-provided", Result{Status: StatusAnswered}); err != nil {
		t.Fatalf("resolve provided id: %v", err)
	}
	wg.Wait()
	if returnedID != "ask-provided" {
		t.Fatalf("expected returned id ask-provided, got %q", returnedID)
	}
}

func TestBrokerOpenRejectsDuplicateProvidedRequestID(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var firstOpen sync.WaitGroup
	firstOpen.Add(1)
	go func() {
		defer firstOpen.Done()
		_, _, _ = broker.Open(ctx, Request{
			RequestID:  "ask-dup",
			QuestionID: "q1",
			TimeoutSec: 10,
		})
	}()

	time.Sleep(50 * time.Millisecond)

	dupCtx, dupCancel := context.WithTimeout(context.Background(), time.Second)
	defer dupCancel()
	_, _, err := broker.Open(dupCtx, Request{
		RequestID:  "ask-dup",
		QuestionID: "q2",
		TimeoutSec: 10,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate request id error, got %v", err)
	}

	if resolveErr := broker.Resolve("ask-dup", Result{Status: StatusAnswered}); resolveErr != nil {
		t.Fatalf("resolve original request: %v", resolveErr)
	}
	firstOpen.Wait()
}

func TestBrokerResolveDuplicateIsIdempotent(t *testing.T) {
	t.Parallel()

	broker := NewBroker()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		broker.Open(ctx, Request{QuestionID: "q1", TimeoutSec: 10})
	}()

	time.Sleep(50 * time.Millisecond)

	ids := broker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}
	requestID := ids[0]

	// First resolve.
	if err := broker.Resolve(requestID, Result{Status: StatusAnswered}); err != nil {
		t.Fatalf("first Resolve error: %v", err)
	}
	// Duplicate resolve — should not block or error
	if err := broker.Resolve(requestID, Result{Status: StatusSkipped}); err != nil {
		t.Fatalf("duplicate Resolve error: %v", err)
	}

	wg.Wait()
}

func TestBrokerOpenTimeout(t *testing.T) {
	t.Parallel()

	broker := NewBroker()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	requestID, result, err := broker.Open(ctx, Request{
		QuestionID: "q1",
		TimeoutSec: 3600, // longer than ctx timeout
	})
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if result.Status != StatusTimeout {
		t.Fatalf("expected timeout status, got %q", result.Status)
	}
	if requestID == "" {
		t.Fatal("expected non-empty request_id even on timeout")
	}

	// Request should be cleaned up
	resolveErr := broker.Resolve(requestID, Result{Status: StatusAnswered})
	if resolveErr == nil || !strings.Contains(resolveErr.Error(), "not found") {
		t.Fatalf("expected not found after timeout cleanup, got %v", resolveErr)
	}
}

func TestBrokerOpenCancel(t *testing.T) {
	t.Parallel()

	broker := NewBroker()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)

	var requestID string
	var result Result
	var openErr error

	go func() {
		defer wg.Done()
		requestID, result, openErr = broker.Open(ctx, Request{QuestionID: "q1", TimeoutSec: 3600})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	wg.Wait()

	if openErr == nil {
		t.Fatalf("expected cancel error, got nil")
	}
	if !errors.Is(openErr, context.Canceled) {
		t.Fatalf("expected Canceled, got %v", openErr)
	}
	if result.Status != StatusTimeout {
		t.Fatalf("expected timeout status on cancel, got %q", result.Status)
	}

	// Request should be cleaned up
	resolveErr := broker.Resolve(requestID, Result{Status: StatusAnswered})
	if resolveErr == nil || !strings.Contains(resolveErr.Error(), "not found") {
		t.Fatalf("expected not found after cancel cleanup, got %v", resolveErr)
	}
}

func TestTimeoutForRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     Request
		wantMin time.Duration
		wantMax time.Duration
	}{
		{
			name:    "default timeout",
			req:     Request{},
			wantMin: defaultRequestTimeout,
			wantMax: defaultRequestTimeout,
		},
		{
			name:    "explicit timeout",
			req:     Request{TimeoutSec: 60},
			wantMin: 60 * time.Second,
			wantMax: 60 * time.Second,
		},
		{
			name:    "capped at max",
			req:     Request{TimeoutSec: 7200},
			wantMin: maxRequestTimeout,
			wantMax: maxRequestTimeout,
		},
		{
			name:    "negative is treated as 0",
			req:     Request{TimeoutSec: -1},
			wantMin: defaultRequestTimeout,
			wantMax: defaultRequestTimeout,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TimeoutForRequest(tt.req)
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("expected timeout in [%v, %v], got %v", tt.wantMin, tt.wantMax, got)
			}
		})
	}
}

func TestBrokerConcurrentOpenResolve(t *testing.T) {
	t.Parallel()

	broker := NewBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const numRequests = 5
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := Request{
				QuestionID: "q" + string(rune('0'+i)),
				TimeoutSec: 10,
			}
			_, result, err := broker.Open(ctx, req)
			if err != nil {
				t.Errorf("request %d Open error: %v", i, err)
				return
			}
			if result.Status != StatusAnswered {
				t.Errorf("request %d expected answered, got %q", i, result.Status)
			}
		}()
	}

	// Allow all goroutines to register their requests.
	time.Sleep(100 * time.Millisecond)

	// Resolve all requests using the current pending IDs.
	ids := broker.PendingIDs()
	if len(ids) != numRequests {
		t.Fatalf("expected %d pending requests, got %d", numRequests, len(ids))
	}
	for _, rid := range ids {
		if err := broker.Resolve(rid, Result{Status: StatusAnswered, Values: []string{"yes"}}); err != nil {
			t.Errorf("resolve %s error: %v", rid, err)
		}
	}

	wg.Wait()
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()

	if StatusAnswered != "answered" {
		t.Fatalf("expected StatusAnswered='answered', got %q", StatusAnswered)
	}
	if StatusSkipped != "skipped" {
		t.Fatalf("expected StatusSkipped='skipped', got %q", StatusSkipped)
	}
	if StatusTimeout != "timeout" {
		t.Fatalf("expected StatusTimeout='timeout', got %q", StatusTimeout)
	}
}
