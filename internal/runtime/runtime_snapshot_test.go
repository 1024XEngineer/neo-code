package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/runtime/decider"
	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

func TestEmitSubAgentSnapshotUpdatedEmitsAggregatedCounts(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 2)}
	state := newRunState("run-subagent-snapshot", newRuntimeSession("session-subagent-snapshot"))
	collector := runtimefacts.NewCollector()
	collector.ApplySubAgentStarted(runtimefacts.SubAgentFact{TaskID: "agent-1", Role: "task"})
	collector.ApplySubAgentFinished(runtimefacts.SubAgentFact{TaskID: "agent-1", Artifacts: []string{"a.txt"}}, true)
	collector.ApplySubAgentStarted(runtimefacts.SubAgentFact{TaskID: "agent-2", Role: "task"})
	collector.ApplySubAgentFinished(runtimefacts.SubAgentFact{TaskID: "agent-2", StopReason: "tool_error"}, false)
	state.factsCollector = collector

	service.emitSubAgentSnapshotUpdated(&state, "tool_result")

	select {
	case evt := <-service.events:
		if evt.Type != EventSubAgentSnapshotUpdated {
			t.Fatalf("event type = %q, want %q", evt.Type, EventSubAgentSnapshotUpdated)
		}
		payload, ok := evt.Payload.(SubAgentSnapshotUpdatedPayload)
		if !ok {
			t.Fatalf("payload type = %T, want SubAgentSnapshotUpdatedPayload", evt.Payload)
		}
		if payload.Reason != "tool_result" {
			t.Fatalf("reason = %q, want tool_result", payload.Reason)
		}
		if payload.SubAgent.StartedCount != 2 || payload.SubAgent.CompletedCount != 1 || payload.SubAgent.FailedCount != 1 {
			t.Fatalf("unexpected subagent counts: %+v", payload.SubAgent)
		}
	default:
		t.Fatal("expected subagent snapshot event")
	}
}

func TestGetRuntimeSnapshotBranches(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.GetRuntimeSnapshot(context.Background(), ""); !errors.Is(err, agentsession.ErrSessionNotFound) {
		t.Fatalf("empty session id error = %v, want ErrSessionNotFound", err)
	}

	cached := RuntimeSnapshot{SessionID: "session-cached", RunID: "run-cached", UpdatedAt: time.Now()}
	service.runtimeSnapshots = map[string]RuntimeSnapshot{cached.SessionID: cached}
	gotCached, err := service.GetRuntimeSnapshot(context.Background(), cached.SessionID)
	if err != nil {
		t.Fatalf("GetRuntimeSnapshot(cached) error = %v", err)
	}
	if gotCached.RunID != cached.RunID {
		t.Fatalf("cached run id = %q, want %q", gotCached.RunID, cached.RunID)
	}

	store := newMemoryStore()
	session := agentsession.New("snapshot-source")
	session.TaskState.VerificationProfile = agentsession.VerificationProfileTaskOnly
	session.UpdatedAt = time.Now().Add(-time.Minute)
	store.sessions[session.ID] = session

	service = &Service{
		sessionStore:     store,
		runtimeSnapshots: map[string]RuntimeSnapshot{},
	}
	got, err := service.GetRuntimeSnapshot(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("GetRuntimeSnapshot(store) error = %v", err)
	}
	if got.SessionID != session.ID {
		t.Fatalf("session id = %q, want %q", got.SessionID, session.ID)
	}
	if got.TaskKind != string(decider.TaskKindMixed) {
		t.Fatalf("task kind = %q, want %q", got.TaskKind, decider.TaskKindMixed)
	}
	if got.Facts.RuntimeFacts.Progress.ObservedFactCount != 0 {
		t.Fatalf("unexpected facts snapshot: %+v", got.Facts.RuntimeFacts)
	}
}
