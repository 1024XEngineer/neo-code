package runtime

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestCallProviderRetriesWithoutThinkingConfig(t *testing.T) {
	t.Parallel()

	scripted := &scriptedProvider{}
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
	if scripted.requests[1].ThinkingConfig != nil {
		t.Fatalf("second request should clear thinking config, got %+v", scripted.requests[1].ThinkingConfig)
	}
	if renderPartsForTest(output.assistant.Parts) != "answer" {
		t.Fatalf("unexpected assistant output: %+v", output.assistant)
	}
}

func TestCallProviderEmitsThinkingDeltaEvent(t *testing.T) {
	t.Parallel()

	scripted := &scriptedProvider{
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
