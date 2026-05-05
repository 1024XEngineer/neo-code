package session

import (
	"encoding/json"
	"testing"
)

func TestTodoCompatibilityDefaultsForLegacyFields(t *testing.T) {
	t.Parallel()

	var todos []TodoItem
	if err := json.Unmarshal([]byte(`[
		{"id":"todo-1","content":"legacy","status":"blocked"},
		{"id":"todo-2","content":"legacy2","status":"pending"}
	]`), &todos); err != nil {
		t.Fatalf("unmarshal todos: %v", err)
	}

	normalized, err := normalizeAndValidateTodos(todos)
	if err != nil {
		t.Fatalf("normalizeAndValidateTodos() error = %v", err)
	}
	if len(normalized) != 2 {
		t.Fatalf("normalized len = %d, want 2", len(normalized))
	}
	if !normalized[0].RequiredValue() || !normalized[1].RequiredValue() {
		t.Fatalf("legacy missing required should default to true, got %+v", normalized)
	}
	// 旧数据缺失 blocked_reason 时,blocked 状态下应保持空(由 LLM/工具后续填写),非 blocked 状态下也应为空。
	if normalized[0].BlockedReason != "" {
		t.Fatalf("legacy blocked todo without reason should keep empty, got %q", normalized[0].BlockedReason)
	}
	if normalized[1].BlockedReason != "" {
		t.Fatalf("legacy pending todo should not carry blocked_reason, got %q", normalized[1].BlockedReason)
	}
}

func TestTodoOptionalAndBlockedReasonPatch(t *testing.T) {
	t.Parallel()

	session := New("compat")
	if err := session.AddTodo(TodoItem{
		ID:      "todo-1",
		Content: "optional task",
	}); err != nil {
		t.Fatalf("AddTodo() error = %v", err)
	}
	item, ok := session.FindTodo("todo-1")
	if !ok {
		t.Fatalf("FindTodo() not found")
	}

	required := false
	blocked := TodoBlockedReasonUserInputWait
	status := TodoStatusBlocked
	if err := session.UpdateTodo("todo-1", TodoPatch{
		Required:      &required,
		BlockedReason: &blocked,
		Status:        &status,
	}, item.Revision); err != nil {
		t.Fatalf("UpdateTodo() error = %v", err)
	}

	updated, _ := session.FindTodo("todo-1")
	if updated.RequiredValue() {
		t.Fatalf("expected optional todo (required=false), got %+v", updated)
	}
	if updated.BlockedReason != TodoBlockedReasonUserInputWait {
		t.Fatalf("blocked_reason = %q, want %q", updated.BlockedReason, TodoBlockedReasonUserInputWait)
	}
}
