package runtime

import (
	"testing"

	agentsession "neo-code/internal/session"
)

// TestBuildTodoSnapshotEmitsBlockedReasonOnlyForBlocked 验证 wire 出口遵守不变量:
// 出口直读 raw 字段,storage invariant(BlockedReason != "" 当且仅当 status == blocked)
// 由 normalize/validate 保证;此测试覆盖出口契约在合规输入下的行为。
func TestBuildTodoSnapshotEmitsBlockedReasonOnlyForBlocked(t *testing.T) {
	t.Parallel()

	required := true
	items := []agentsession.TodoItem{
		{
			ID:            "pending",
			Content:       "pending",
			Status:        agentsession.TodoStatusPending,
			Required:      &required,
			BlockedReason: "",
			Revision:      1,
		},
		{
			ID:            "blocked-real-reason",
			Content:       "blocked",
			Status:        agentsession.TodoStatusBlocked,
			Required:      &required,
			BlockedReason: agentsession.TodoBlockedReasonInternalDependency,
			Revision:      1,
		},
		{
			ID:            "blocked-unknown",
			Content:       "blocked-unknown",
			Status:        agentsession.TodoStatusBlocked,
			Required:      &required,
			BlockedReason: agentsession.TodoBlockedReasonUnknown,
			Revision:      1,
		},
		{
			ID:            "completed",
			Content:       "completed",
			Status:        agentsession.TodoStatusCompleted,
			Required:      &required,
			BlockedReason: "",
			Revision:      1,
		},
	}

	snap := buildTodoSnapshotFromItems(items)
	if len(snap.Items) != 4 {
		t.Fatalf("snapshot len = %d, want 4", len(snap.Items))
	}

	wants := map[string]string{
		"pending":             "",
		"blocked-real-reason": "internal_dependency",
		"blocked-unknown":     "unknown",
		"completed":           "",
	}
	for _, item := range snap.Items {
		want, ok := wants[item.ID]
		if !ok {
			t.Fatalf("unexpected id in snapshot: %q", item.ID)
		}
		if item.BlockedReason != want {
			t.Fatalf("id=%q blocked_reason = %q, want %q", item.ID, item.BlockedReason, want)
		}
	}
}
