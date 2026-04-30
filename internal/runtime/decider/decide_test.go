package decider

import (
	"testing"

	runtimefacts "neo-code/internal/runtime/facts"
)

func TestDecideRequiredTodoFailedStopsImmediately(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind: TaskKindTodoState,
		Todos: TodoSnapshot{
			Summary: TodoSummary{RequiredFailed: 1},
		},
	})

	if decision.Status != DecisionFailed {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionFailed)
	}
	if decision.StopReason != "required_todo_failed" {
		t.Fatalf("stop_reason = %q, want required_todo_failed", decision.StopReason)
	}
}

func TestDecideNoProgressExceededReturnsIncomplete(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:           TaskKindWorkspaceWrite,
		NoProgressExceeded: true,
	})

	if decision.Status != DecisionIncomplete {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionIncomplete)
	}
	if decision.StopReason != "no_progress_after_final_intercept" {
		t.Fatalf("stop_reason = %q, want no_progress_after_final_intercept", decision.StopReason)
	}
}

func TestDecideCompletionBlockedReasonPendingTodo(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindTodoState,
		CompletionPassed: false,
		CompletionReason: "pending_todo",
		Todos: TodoSnapshot{
			Items: []TodoViewItem{
				{ID: "todo-1", Required: true, Status: "pending"},
			},
		},
	})

	if decision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionContinue)
	}
	if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Kind != "required_todo_terminal" {
		t.Fatalf("missing facts = %+v", decision.MissingFacts)
	}
	if len(decision.RequiredNextActions) == 0 || decision.RequiredNextActions[0].Tool != "todo_write" {
		t.Fatalf("required actions = %+v", decision.RequiredNextActions)
	}
}

func TestDecideWorkspaceWriteNeedsVerificationThenAccepts(t *testing.T) {
	continueDecision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
		},
	})
	if continueDecision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", continueDecision.Status, DecisionContinue)
	}

	acceptedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindWorkspaceWrite,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{{Path: "test.txt", Bytes: 1, WorkspaceWrite: true}},
			},
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_read_file", Scope: "artifact:test.txt", Passed: true}},
			},
		},
	})
	if acceptedDecision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", acceptedDecision.Status, DecisionAccepted)
	}
}

func TestDecideSubAgentRequiresCompletedFact(t *testing.T) {
	continueDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
			},
		},
	})
	if continueDecision.Status != DecisionContinue {
		t.Fatalf("status = %q, want %q", continueDecision.Status, DecisionContinue)
	}

	failedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
				Failed:  []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer", State: "failed"}},
			},
		},
	})
	if failedDecision.Status != DecisionFailed {
		t.Fatalf("status = %q, want %q", failedDecision.Status, DecisionFailed)
	}

	acceptedDecision := Decide(DecisionInput{
		TaskKind:         TaskKindSubAgent,
		CompletionPassed: true,
		Facts: runtimefacts.RuntimeFacts{
			SubAgents: runtimefacts.SubAgentFacts{
				Started:   []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer"}},
				Completed: []runtimefacts.SubAgentFact{{TaskID: "sa-1", Role: "reviewer", State: "succeeded"}},
			},
		},
	})
	if acceptedDecision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", acceptedDecision.Status, DecisionAccepted)
	}
}

func TestDecideTodoStateAcceptsWithoutFileVerification(t *testing.T) {
	decision := Decide(DecisionInput{
		TaskKind:         TaskKindTodoState,
		CompletionPassed: true,
		Todos: TodoSnapshot{
			Items: []TodoViewItem{
				{ID: "todo-1", Content: "创建 Todo", Status: "pending", Required: false},
			},
			Summary: TodoSummary{Total: 1, RequiredTotal: 0},
		},
		Facts: runtimefacts.RuntimeFacts{
			Todos: runtimefacts.TodoFacts{CreatedIDs: []string{"todo-1"}},
		},
	})
	if decision.Status != DecisionAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, DecisionAccepted)
	}
}
