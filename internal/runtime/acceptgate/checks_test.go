package acceptgate

import (
	"testing"

	agentsession "neo-code/internal/session"
)

func TestCheckOutputOnly(t *testing.T) {
	t.Parallel()

	if got := checkOutputOnly("done"); !got.Passed {
		t.Fatalf("checkOutputOnly(done) = %+v, want pass", got)
	}
	if got := checkOutputOnly("  "); got.Passed {
		t.Fatalf("checkOutputOnly(blank) = %+v, want fail", got)
	}
}

func TestCheckRequiredTodoConvergence(t *testing.T) {
	t.Parallel()

	required := true
	result := checkRequiredTodoConvergence([]agentsession.TodoItem{
		{ID: "todo-1", Required: &required, Status: agentsession.TodoStatusPending},
	})
	if result.Passed {
		t.Fatalf("result = %+v, want failed", result)
	}
}
