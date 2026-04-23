package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

func TestEmitRunTerminationEmitsStopReasonOnce(t *testing.T) {
	t.Parallel()

	s := NewWithFactory(newRuntimeConfigManager(t), &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{
		provider: &scriptedProvider{},
	}, nil)

	state := newRunState("run-t", agentsessionFixture(t))
	errSample := errors.New("boom")

	s.emitRunTermination(context.Background(), UserInput{RunID: "run-t", SessionID: state.session.ID}, &state, errSample)
	s.emitRunTermination(context.Background(), UserInput{RunID: "run-t", SessionID: state.session.ID}, &state, errSample)

	events := collectRuntimeEvents(s.Events())
	var stops int
	for _, e := range events {
		if e.Type == EventStopReasonDecided {
			stops++
			p, ok := e.Payload.(StopReasonDecidedPayload)
			if !ok {
				t.Fatalf("expected StopReasonDecidedPayload, got %#v", e.Payload)
			}
			if p.Reason != controlplane.StopReasonFatalError {
				t.Fatalf("reason = %q, want fatal error", p.Reason)
			}
		}
	}
	if stops != 1 {
		t.Fatalf("expected exactly one stop_reason_decided, got %d", stops)
	}
}

func TestEmitRunTerminationUsesFallbackContextWhenCanceled(t *testing.T) {
	t.Parallel()

	s := NewWithFactory(newRuntimeConfigManager(t), &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{
		provider: &scriptedProvider{},
	}, nil)
	s.events = make(chan RuntimeEvent, 1)
	s.events <- RuntimeEvent{Type: EventAgentChunk}

	state := newRunState("run-cancel-fallback", agentsessionFixture(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.emitRunTermination(ctx, UserInput{RunID: "run-cancel-fallback", SessionID: state.session.ID}, &state, errors.New("boom"))
		close(done)
	}()

	select {
	case <-done:
		t.Fatalf("expected emitRunTermination to wait for channel availability")
	case <-time.After(30 * time.Millisecond):
	}

	<-s.events

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("emitRunTermination did not finish after freeing channel")
	}

	events := collectRuntimeEvents(s.Events())
	assertEventContains(t, events, EventStopReasonDecided)
}

func TestEmitRunTerminationMapsMaxTurnsToDedicatedStopReason(t *testing.T) {
	t.Parallel()

	s := NewWithFactory(newRuntimeConfigManager(t), &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{
		provider: &scriptedProvider{},
	}, nil)

	state := newRunState("run-max-turns", agentsessionFixture(t))
	state.maxTurnsReached = true
	state.maxTurnsLimit = 40

	s.emitRunTermination(context.Background(), UserInput{RunID: "run-max-turns", SessionID: state.session.ID}, &state, nil)

	events := collectRuntimeEvents(s.Events())
	assertEventContains(t, events, EventStopReasonDecided)
	for _, event := range events {
		if event.Type != EventStopReasonDecided {
			continue
		}
		payload, ok := event.Payload.(StopReasonDecidedPayload)
		if !ok {
			t.Fatalf("expected StopReasonDecidedPayload, got %T", event.Payload)
		}
		if payload.Reason != controlplane.StopReasonMaxTurnsReached {
			t.Fatalf("stop reason = %q, want %q", payload.Reason, controlplane.StopReasonMaxTurnsReached)
		}
		if payload.Detail != "runtime: max turn limit reached (40)" {
			t.Fatalf("stop detail = %q, want max turn detail", payload.Detail)
		}
	}
}

func agentsessionFixture(t *testing.T) agentsession.Session {
	t.Helper()
	s := agentsession.New("t")
	s.ID = "sess-t"
	return s
}
