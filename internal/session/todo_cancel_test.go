package session

import (
	"testing"
	"time"
)

func TestCancelNonTerminalTodos(t *testing.T) {
	t.Parallel()

	todos := []TodoItem{
		{ID: "p", Status: TodoStatusPending},
		{ID: "i", Status: TodoStatusInProgress},
		{ID: "b", Status: TodoStatusBlocked},
		{ID: "c", Status: TodoStatusCompleted},
	}

	CancelNonTerminalTodos(todos)

	for _, id := range []string{"p", "i", "b"} {
		item := todos[indexTodoByID(t, todos, id)]
		if item.Status != TodoStatusCanceled {
			t.Fatalf("todo %q status = %q, want canceled", id, item.Status)
		}
		if item.UpdatedAt.IsZero() || time.Since(item.UpdatedAt) > time.Minute {
			t.Fatalf("todo %q missing updated_at: %+v", id, item)
		}
	}
	if todos[indexTodoByID(t, todos, "c")].Status != TodoStatusCompleted {
		t.Fatalf("terminal todo should stay completed: %+v", todos)
	}
}

func TestCancelTodosByIDs(t *testing.T) {
	t.Parallel()

	todos := []TodoItem{
		{ID: "keep", Status: TodoStatusPending},
		{ID: "cancel", Status: TodoStatusBlocked},
		{ID: "done", Status: TodoStatusCompleted},
	}

	CancelTodosByIDs(todos, []string{" cancel ", "done"})

	if todos[indexTodoByID(t, todos, "cancel")].Status != TodoStatusCanceled {
		t.Fatalf("expected selected non-terminal todo to be canceled: %+v", todos)
	}
	if todos[indexTodoByID(t, todos, "keep")].Status != TodoStatusPending {
		t.Fatalf("expected unmatched todo to stay pending: %+v", todos)
	}
	if todos[indexTodoByID(t, todos, "done")].Status != TodoStatusCompleted {
		t.Fatalf("expected terminal todo to stay completed: %+v", todos)
	}

	CancelTodosByIDs(todos, nil)
}

func indexTodoByID(t *testing.T, todos []TodoItem, id string) int {
	t.Helper()
	for i := range todos {
		if todos[i].ID == id {
			return i
		}
	}
	t.Fatalf("todo %q not found", id)
	return -1
}
