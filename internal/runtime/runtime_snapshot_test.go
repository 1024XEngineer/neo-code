package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	agentsession "neo-code/internal/session"
)

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
	if got.Todos.Summary.Total != 0 {
		t.Fatalf("unexpected todos snapshot: %+v", got.Todos)
	}
}

func TestBuildRuntimeSnapshotCarriesIndependentSubAgentCounts(t *testing.T) {
	t.Parallel()

	state := newRunState("run-subagent", newRuntimeSession("session-subagent"))
	state.subAgentSnapshot.started = map[string]struct{}{"task-1:coder": {}}
	state.subAgentSnapshot.completed = map[string]struct{}{"task-1": {}}

	got := buildRuntimeSnapshot(&state)
	if got.SubAgents.StartedCount != 1 || got.SubAgents.CompletedCount != 1 || got.SubAgents.FailedCount != 0 {
		t.Fatalf("subagent snapshot = %+v", got.SubAgents)
	}
}
