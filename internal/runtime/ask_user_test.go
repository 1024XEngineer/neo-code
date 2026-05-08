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

	// Allow goroutine to enter Open and register the request (first ID = ask-1).
	time.Sleep(50 * time.Millisecond)

	resolveErr := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: "ask-1",
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

	// Allow goroutine to enter Open and register the request (first ID = ask-1).
	time.Sleep(50 * time.Millisecond)

	resolveErr := service.ResolveUserQuestion(context.Background(), UserQuestionResolutionInput{
		RequestID: "ask-1",
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

	// First Open in a fresh broker always gets ask-1.
	resolveErr := service.askUserBroker.Resolve("ask-1", askuser.Result{
		Status: askuser.StatusAnswered,
		Values: []string{"Yes"},
	})
	if resolveErr != nil {
		t.Fatalf("resolve via broker: %v", resolveErr)
	}

	wg.Wait()
}
