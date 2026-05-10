package runtime

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestCallProviderRetriesWithThinkingDowngrade(t *testing.T) {
	t.Parallel()

	scripted := &scriptedProvider{requireExplicitCompletion: true}
	scripted.chatFn = func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
		if len(scripted.requests) == 1 {
			return errors.Join(provider.ErrThinkingNotSupported, errors.New("upstream rejected thinking"))
		}
		events <- providertypes.NewTextDeltaStreamEvent("answer")
		events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
		return nil
	}
	service := &Service{events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-thinking-retry", agentsession.Session{ID: "session-thinking-retry"})
	snapshot := TurnBudgetSnapshot{
		Request: providertypes.GenerateRequest{
			Model: "test-model",
			Messages: []providertypes.Message{{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
			}},
			ThinkingConfig: &providertypes.ThinkingConfig{Enabled: true, Effort: "high"},
		},
	}

	output, err := service.callProvider(context.Background(), &state, snapshot, scripted)
	if err != nil {
		t.Fatalf("callProvider() error = %v", err)
	}
	if scripted.callCount != 2 {
		t.Fatalf("provider calls = %d, want 2", scripted.callCount)
	}
	if scripted.requests[0].ThinkingConfig == nil {
		t.Fatal("first request should include thinking config")
	}
	if scripted.requests[1].ThinkingConfig == nil || scripted.requests[1].ThinkingConfig.Enabled {
		t.Fatalf("second request should explicitly disable thinking, got %+v", scripted.requests[1].ThinkingConfig)
	}
	if renderPartsForTest(output.assistant.Parts) != "answer" {
		t.Fatalf("unexpected assistant output: %+v", output.assistant)
	}
}

func TestCallProviderEmitsThinkingDeltaEvent(t *testing.T) {
	t.Parallel()

	scripted := &scriptedProvider{
		requireExplicitCompletion: true,
		chatFn: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			events <- providertypes.NewThinkingDeltaStreamEvent("plan")
			events <- providertypes.NewTextDeltaStreamEvent("answer")
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}
	service := &Service{events: make(chan RuntimeEvent, 8)}
	state := newRunState("run-thinking-event", agentsession.Session{ID: "session-thinking-event"})

	if _, err := service.callProvider(
		context.Background(),
		&state,
		TurnBudgetSnapshot{Request: providertypes.GenerateRequest{Model: "test-model"}},
		scripted,
	); err != nil {
		t.Fatalf("callProvider() error = %v", err)
	}

	events := collectThinkingRuntimeEvents(service.events)
	if !hasRuntimeEvent(events, EventThinkingDelta, "plan") {
		t.Fatalf("expected thinking_delta event, got %+v", events)
	}
}

func TestBuildThinkingRetryRequests(t *testing.T) {
	t.Parallel()

	t.Run("enabled config retries with disable then nil", func(t *testing.T) {
		base := providertypes.GenerateRequest{
			ThinkingConfig: &providertypes.ThinkingConfig{Enabled: true, Effort: "high"},
		}
		retries := buildThinkingRetryRequests(base)
		if len(retries) != 2 {
			t.Fatalf("retry count = %d, want 2", len(retries))
		}
		if retries[0].ThinkingConfig == nil || retries[0].ThinkingConfig.Enabled {
			t.Fatalf("first retry should disable thinking, got %+v", retries[0].ThinkingConfig)
		}
		if retries[1].ThinkingConfig != nil {
			t.Fatalf("second retry should clear thinking config, got %+v", retries[1].ThinkingConfig)
		}
	})

	t.Run("nil config retries with explicit disable", func(t *testing.T) {
		base := providertypes.GenerateRequest{}
		retries := buildThinkingRetryRequests(base)
		if len(retries) != 1 {
			t.Fatalf("retry count = %d, want 1", len(retries))
		}
		if retries[0].ThinkingConfig == nil || retries[0].ThinkingConfig.Enabled {
			t.Fatalf("retry should disable thinking, got %+v", retries[0].ThinkingConfig)
		}
	})

	t.Run("already disabled config retries without config", func(t *testing.T) {
		base := providertypes.GenerateRequest{
			ThinkingConfig: &providertypes.ThinkingConfig{Enabled: false},
		}
		retries := buildThinkingRetryRequests(base)
		if len(retries) != 1 {
			t.Fatalf("retry count = %d, want 1", len(retries))
		}
		if retries[0].ThinkingConfig != nil {
			t.Fatalf("retry should clear thinking config, got %+v", retries[0].ThinkingConfig)
		}
	})
}

func collectThinkingRuntimeEvents(ch <-chan RuntimeEvent) []RuntimeEvent {
	var events []RuntimeEvent
	for {
		select {
		case event := <-ch:
			events = append(events, event)
		default:
			return events
		}
	}
}

func hasRuntimeEvent(events []RuntimeEvent, eventType EventType, payload string) bool {
	for _, event := range events {
		if event.Type == eventType && event.Payload == payload {
			return true
		}
	}
	return false
}
