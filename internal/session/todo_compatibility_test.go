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
	if normalized[0].BlockedReasonValue() != TodoBlockedReasonUnknown {
		t.Fatalf("legacy missing blocked_reason should default unknown, got %q", normalized[0].BlockedReasonValue())
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
	if updated.BlockedReasonValue() != TodoBlockedReasonUserInputWait {
		t.Fatalf("blocked_reason = %q, want %q", updated.BlockedReasonValue(), TodoBlockedReasonUserInputWait)
	}
}
