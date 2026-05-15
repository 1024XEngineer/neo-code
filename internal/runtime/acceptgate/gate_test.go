package acceptgate

import (
	"context"
	"testing"

	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

func TestEvaluateAcceptedWhenOutputAndTodosConverged(t *testing.T) {
	t.Parallel()

	report := Evaluate(context.Background(), Input{LastAssistantText: "done"})
	if report.Outcome != OutcomeAccepted || report.StopReason != controlplane.StopReasonAccepted {
		t.Fatalf("report = %+v, want accepted", report)
	}
}

func TestEvaluateContinueWhenOutputEmpty(t *testing.T) {
	t.Parallel()

	report := Evaluate(context.Background(), Input{})
	if report.Outcome != OutcomeContinue || report.StopReason != controlplane.StopReasonAcceptContinue {
		t.Fatalf("report = %+v, want continue", report)
	}
	if report.ContinueHint == "" {
		t.Fatalf("continue hint should not be empty: %+v", report)
	}
}

func TestEvaluateTodoOutcomes(t *testing.T) {
	t.Parallel()

	required := true
	input := Input{
		LastAssistantText: "done",
		Todos: []agentsession.TodoItem{
			{ID: "todo-1", Status: agentsession.TodoStatusFailed, Required: &required},
		},
	}
	report := Evaluate(context.Background(), input)
	if report.Outcome != OutcomeFailed || report.StopReason != controlplane.StopReasonRequiredTodoFailed {
		t.Fatalf("failed todo report = %+v, want required_todo_failed", report)
	}

	input.Todos[0].Status = agentsession.TodoStatusPending
	report = Evaluate(context.Background(), input)
	if report.Outcome != OutcomeContinue || report.StopReason != controlplane.StopReasonAcceptContinue {
		t.Fatalf("pending todo report = %+v, want continue", report)
	}
}
