package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/tools"
)

func TestProgressStreakStopsRun(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-progress", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-progress",
		Workdir:          t.TempDir(),
	}

	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_error"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			// Always return error to avoid generating progress
			return tools.ToolResult{Content: "error occurred", IsError: true}, nil
		},
	}

	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				// the model always decides to call the tool
				events <- providertypes.NewToolCallStartStreamEvent(0, "call_err", "tool_error")
				events <- providertypes.NewToolCallDeltaStreamEvent(0, "call_err", "{}")
				events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))

	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	input := UserInput{
		RunID:   "run-progress",
		Content: "trigger error loop",
	}

	err := service.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected error from streak limit, got nil")
	}

	if !errors.Is(err, ErrNoProgressStreakLimit) {
		t.Fatalf("expected ErrNoProgressStreakLimit, got %v", err)
	}

	events := collectRuntimeEvents(service.Events())

	// Verify StopReason is error and specifies the streak limit
	assertEventContains(t, events, EventStopReasonDecided)

	for _, e := range events {
		if e.Type == EventStopReasonDecided {
			payload := e.Payload.(StopReasonDecidedPayload)
			if payload.Reason != controlplane.StopReasonError {
				t.Errorf("expected StopReasonError, got %s", payload.Reason)
			}
			if payload.Detail != ErrNoProgressStreakLimit.Error() {
				t.Errorf("expected detail to be %q, got %q", ErrNoProgressStreakLimit.Error(), payload.Detail)
			}
		}
	}
}

func TestProgressEvidenceResetsNoProgressStreak(t *testing.T) {
	t.Setenv("TEST_KEY", "dummy")

	cfg := config.Config{
		Providers:        []config.ProviderConfig{{Name: "test-progress", Driver: "test", BaseURL: "http://localhost", Model: "test", APIKeyEnv: "TEST_KEY"}},
		SelectedProvider: "test-progress",
		Workdir:          t.TempDir(),
	}

	var executeCalls int32
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{
			{Name: "tool_mixed"},
		},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			call := int(atomic.AddInt32(&executeCalls, 1))
			if call == 3 {
				return tools.ToolResult{Name: input.Name, Content: "ok", IsError: false}, nil
			}
			return tools.ToolResult{Name: input.Name, Content: "error occurred", IsError: true}, nil
		},
	}

	var providerCalls int32
	providerFactory := &scriptedProviderFactory{
		provider: &scriptedProvider{
			chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
				call := int(atomic.AddInt32(&providerCalls, 1))
				if call <= 4 {
					events <- providertypes.NewToolCallStartStreamEvent(0, "call_mixed", "tool_mixed")
					events <- providertypes.NewToolCallDeltaStreamEvent(0, "call_mixed", "{}")
					events <- providertypes.NewMessageDoneStreamEvent("tool_calls", nil)
					return nil
				}
				events <- providertypes.NewTextDeltaStreamEvent("done")
				events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
				return nil
			},
		},
	}

	manager := config.NewManager(config.NewLoader(t.TempDir(), &cfg))
	service := NewWithFactory(
		manager,
		toolManager,
		newMemoryStore(),
		providerFactory,
		nil,
	)

	err := service.Run(context.Background(), UserInput{
		RunID:   "run-progress-reset",
		Content: "trigger mixed progress loop",
	})
	if err != nil {
		t.Fatalf("expected run to finish successfully, got %v", err)
	}

	if executeCalls != 4 {
		t.Fatalf("expected 4 tool executions, got %d", executeCalls)
	}
	if providerCalls != 5 {
		t.Fatalf("expected 5 provider calls (4 tool turns + 1 done), got %d", providerCalls)
	}

	events := collectRuntimeEvents(service.Events())
	for _, e := range events {
		if e.Type == EventStopReasonDecided {
			payload := e.Payload.(StopReasonDecidedPayload)
			if payload.Reason != controlplane.StopReasonSuccess {
				t.Fatalf("expected stop reason success, got %s", payload.Reason)
			}
		}
	}
}
