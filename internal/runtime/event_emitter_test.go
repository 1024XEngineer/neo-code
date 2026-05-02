package runtime

import (
	"testing"

	"neo-code/internal/runtime/controlplane"
)

func TestEmitRunScopedPriorityReplacesOldEventWhenChannelFull(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 1)}
	service.events <- RuntimeEvent{Type: EventAgentChunk}

	state := newRunState("run-priority", newRuntimeSession("session-priority"))
	state.turn = 3
	state.lifecycle = controlplane.RunStateExecute

	service.emitRunScopedPriority(EventDecisionMade, &state, map[string]any{"k": "v"})

	select {
	case evt := <-service.events:
		if evt.Type != EventDecisionMade {
			t.Fatalf("event type = %q, want %q", evt.Type, EventDecisionMade)
		}
		if evt.RunID != "run-priority" || evt.SessionID != "session-priority" {
			t.Fatalf("unexpected run scope: %+v", evt)
		}
		if evt.Turn != 3 || evt.Phase == "" {
			t.Fatalf("unexpected turn/phase: %+v", evt)
		}
	default:
		t.Fatal("expected priority event to be delivered")
	}
}

func TestEmitRunScopedPriorityNilGuards(t *testing.T) {
	t.Parallel()

	var service *Service
	service.emitRunScopedPriority(EventDecisionMade, nil, nil)

	service = &Service{}
	service.emitRunScopedPriority(EventDecisionMade, nil, nil)
}
