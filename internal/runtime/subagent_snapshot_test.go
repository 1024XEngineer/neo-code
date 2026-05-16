package runtime

import (
	"context"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

func TestSubAgentSnapshotStateApplySpawnResult(t *testing.T) {
	t.Parallel()

	var state subAgentSnapshotState
	if changed := state.applySpawnResult(tools.ToolResult{}); changed {
		t.Fatal("empty result should not update snapshot state")
	}

	completed := tools.ToolResult{Metadata: map[string]any{
		"task_id": "task-1",
		"role":    "coder",
		"state":   string(subagent.StateSucceeded),
	}}
	if changed := state.applySpawnResult(completed); !changed {
		t.Fatal("first completed result should update snapshot state")
	}
	if changed := state.applySpawnResult(completed); changed {
		t.Fatal("duplicate completed result should be deduplicated")
	}

	failed := tools.ToolResult{Metadata: map[string]any{
		"task_id":     "task-2",
		"role":        "reviewer",
		"state":       string(subagent.StateFailed),
		"stop_reason": "error",
	}}
	if changed := state.applySpawnResult(failed); !changed {
		t.Fatal("failed result should update snapshot state")
	}

	got := state.snapshot()
	if got.StartedCount != 2 || got.CompletedCount != 1 || got.FailedCount != 1 {
		t.Fatalf("snapshot = %+v, want started=2 completed=1 failed=1", got)
	}
}

func TestEmitSubAgentSnapshotEventsUpdatesSnapshotAndEmitsEvents(t *testing.T) {
	t.Parallel()

	service := &Service{
		events:           make(chan RuntimeEvent, 4),
		runtimeSnapshots: make(map[string]RuntimeSnapshot),
	}
	state := newRunState("run-subagent", newRuntimeSession("session-subagent"))
	result := tools.ToolResult{Metadata: map[string]any{
		"task_id": "task-1",
		"role":    "coder",
		"state":   string(subagent.StateSucceeded),
	}}

	service.emitSubAgentSnapshotEvents(context.Background(), &state, providertypes.ToolCall{
		Name: tools.ToolNameSpawnSubAgent,
	}, result)

	snapshot := buildRuntimeSnapshot(&state)
	if snapshot.SubAgents.StartedCount != 1 || snapshot.SubAgents.CompletedCount != 1 || snapshot.SubAgents.FailedCount != 0 {
		t.Fatalf("runtime snapshot subagents = %+v", snapshot.SubAgents)
	}

	first := <-service.events
	second := <-service.events
	if first.Type != EventSubAgentSnapshotUpdated || second.Type != EventRuntimeSnapshotUpdated {
		t.Fatalf("event sequence = [%s %s], want [%s %s]", first.Type, second.Type, EventSubAgentSnapshotUpdated, EventRuntimeSnapshotUpdated)
	}
}
