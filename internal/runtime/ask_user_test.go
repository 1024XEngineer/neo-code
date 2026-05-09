package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/runtime/askuser"
	"neo-code/internal/tools"
)

func TestResolveUserQuestionValidation(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	err := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{})
	if err == nil {
		t.Fatalf("expected empty request id error")
	}
	if !strings.Contains(err.Error(), "request id is empty") {
		t.Fatalf("expected request id empty message, got %v", err)
	}

	err = service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: "ask-not-found",
		Status:    askuser.StatusAnswered,
	})
	if err == nil {
		t.Fatalf("expected request not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found message, got %v", err)
	}
}

func TestResolveUserQuestionInvalidStatus(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	err := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: "ask-1",
		Status:    "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported user question status") {
		t.Fatalf("expected unsupported status error, got %v", err)
	}
}

func TestResolveUserQuestionSuccess(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var openResult askuser.Result
	var openErr error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, openResult, openErr = service.askUserBroker.Open(ctx, askuser.Request{
			QuestionID: "q1",
			TimeoutSec: 10,
		})
	}()

	// Allow goroutine to enter Open and register the request.
	time.Sleep(50 * time.Millisecond)

	ids := service.askUserBroker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}

	resolveErr := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: ids[0],
		Status:    askuser.StatusAnswered,
		Values:    []string{"hello", "world"},
	})
	if resolveErr != nil {
		t.Fatalf("ResolveUserQuestion error: %v", resolveErr)
	}

	wg.Wait()

	if openErr != nil {
		t.Fatalf("broker Open error: %v", openErr)
	}
	if openResult.Status != askuser.StatusAnswered {
		t.Fatalf("expected answered, got %q", openResult.Status)
	}
	if len(openResult.Values) != 2 || openResult.Values[0] != "hello" || openResult.Values[1] != "world" {
		t.Fatalf("unexpected values: %v", openResult.Values)
	}
}

func TestResolveUserQuestionSkip(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var openResult askuser.Result
	var openErr error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, openResult, openErr = service.askUserBroker.Open(ctx, askuser.Request{
			QuestionID: "q1",
			TimeoutSec: 10,
		})
	}()

	// Allow goroutine to enter Open and register the request.
	time.Sleep(50 * time.Millisecond)

	ids := service.askUserBroker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}

	resolveErr := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: ids[0],
		Status:    askuser.StatusSkipped,
	})
	if resolveErr != nil {
		t.Fatalf("ResolveUserQuestion skip error: %v", resolveErr)
	}

	wg.Wait()

	if openErr != nil {
		t.Fatalf("broker Open error: %v", openErr)
	}
	if openResult.Status != askuser.StatusSkipped {
		t.Fatalf("expected skipped, got %q", openResult.Status)
	}
}

func TestResolveUserQuestionDefaultsStatusAndTrimsMessage(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var openResult askuser.Result
	var openErr error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, openResult, openErr = service.askUserBroker.Open(ctx, askuser.Request{
			QuestionID: "q1",
			TimeoutSec: 10,
		})
	}()

	time.Sleep(50 * time.Millisecond)

	ids := service.askUserBroker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}

	resolveErr := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: ids[0],
		Message:   "  keep this trimmed  ",
	})
	if resolveErr != nil {
		t.Fatalf("ResolveUserQuestion default status error: %v", resolveErr)
	}

	wg.Wait()

	if openErr != nil {
		t.Fatalf("broker Open error: %v", openErr)
	}
	if openResult.Status != askuser.StatusAnswered {
		t.Fatalf("expected default answered status, got %q", openResult.Status)
	}
	if openResult.Message != "keep this trimmed" {
		t.Fatalf("expected trimmed message, got %q", openResult.Message)
	}
}

func TestResolveUserQuestionContextCanceled(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.ResolveUserQuestion(ctx, UserQuestionResolutionInput{
		RequestID: "ask-1",
		Status:    askuser.StatusAnswered,
	})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestEventTypeFromAskUserEvent(t *testing.T) {
	t.Parallel()

	tests := map[string]EventType{
		"user_question_requested": EventUserQuestionRequested,
		"user_question_answered":  EventUserQuestionAnswered,
		"user_question_skipped":   EventUserQuestionSkipped,
		"user_question_timeout":   EventUserQuestionTimeout,
		"unknown":                 EventError,
	}

	for name, want := range tests {
		if got := eventTypeFromAskUserEvent(name); got != want {
			t.Fatalf("eventTypeFromAskUserEvent(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestAskUserBrokerAdapterConversion(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)

	adapter := service.AskUserBrokerAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Open via adapter in a goroutine; resolve via the broker directly using first generated ID.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, toolsResult, err := adapter.Open(ctx, tools.AskUserRequest{
			QuestionID: "adapter-q1",
			Title:      "Test?",
			Kind:       "text",
			TimeoutSec: 10,
			Options:    []tools.AskUserOption{{Label: "Yes"}, {Label: "No"}},
		})
		if err != nil {
			t.Errorf("adapter Open error: %v", err)
			return
		}
		if toolsResult.Status != askuser.StatusAnswered {
			t.Errorf("expected answered, got %q", toolsResult.Status)
		}
		if len(toolsResult.Values) != 1 || toolsResult.Values[0] != "Yes" {
			t.Errorf("unexpected values: %v", toolsResult.Values)
		}
	}()

	// Give the goroutine time to register the request.
	time.Sleep(50 * time.Millisecond)

	ids := service.askUserBroker.PendingIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(ids))
	}
	resolveErr := service.askUserBroker.Resolve(ids[0], askuser.Result{
		Status: askuser.StatusAnswered,
		Values: []string{"Yes"},
	})
	if resolveErr != nil {
		t.Fatalf("resolve via broker: %v", resolveErr)
	}

	wg.Wait()
}

func TestAskUserBrokerAdapterErrorAndOptionConversion(t *testing.T) {
	t.Parallel()

	broker := askuser.NewBroker()
	adapter := newAskUserBrokerAdapter(broker)

	if got := convertAskUserOptions(nil); got != nil {
		t.Fatalf("expected nil options conversion, got %#v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	requestID, result, err := adapter.Open(ctx, tools.AskUserRequest{
		QuestionID: "adapter-q2",
		Title:      "Choose",
		Kind:       "single_choice",
		Options: []tools.AskUserOption{
			{Label: "A", Description: "first"},
		},
	})
	if err == nil {
		t.Fatal("expected adapter Open error when context is canceled")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("expected canceled error, got %v", err)
	}
	if requestID == "" {
		t.Fatal("expected generated request_id on canceled adapter Open")
	}
	if result.Status != askuser.StatusTimeout {
		t.Fatalf("expected timeout status on canceled adapter Open, got %q", result.Status)
	}
}
