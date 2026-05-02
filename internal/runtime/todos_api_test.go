package runtime

import (
	"context"
	"testing"

	agentsession "neo-code/internal/session"
)

func TestServiceListTodosReturnsSnapshot(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	session := agentsession.New("session-todos-list")
	required := true
	if err := session.AddTodo(agentsession.TodoItem{
		ID:       "todo-1",
		Content:  "create todo snapshot",
		Required: &required,
	}); err != nil {
		t.Fatalf("add todo-1: %v", err)
	}
	if err := session.AddTodo(agentsession.TodoItem{
		ID:      "todo-2",
		Content: "already done",
		Status:  agentsession.TodoStatusCompleted,
		Required: func() *bool {
			v := false
			return &v
		}(),
	}); err != nil {
		t.Fatalf("add todo-2: %v", err)
	}
	store.sessions[session.ID] = session

	service := Service{sessionStore: store}
	snapshot, err := service.ListTodos(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListTodos() error = %v", err)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(snapshot.Items))
	}
	if snapshot.Summary.Total != 2 {
		t.Fatalf("summary.total = %d, want 2", snapshot.Summary.Total)
	}
	if snapshot.Summary.RequiredTotal != 1 || snapshot.Summary.RequiredOpen != 1 {
		t.Fatalf("unexpected required summary: %+v", snapshot.Summary)
	}
}
