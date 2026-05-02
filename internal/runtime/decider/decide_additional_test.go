package decider

import (
	"testing"

	runtimefacts "neo-code/internal/runtime/facts"
)

func TestContinueWithCompletionReasonBranches(t *testing.T) {
	t.Parallel()

	t.Run("pending_todo without open ids requires input", func(t *testing.T) {
		t.Parallel()
		decision := continueWithCompletionReason(DecisionInput{
			CompletionReason: "pending_todo",
			Todos: TodoSnapshot{
				Items: []TodoViewItem{{ID: "x", Required: true, Status: "completed"}},
			},
		})
		if decision.RequiredInput == nil || decision.RequiredInput.Kind != "missing_required_todo_id" {
			t.Fatalf("required input = %+v", decision.RequiredInput)
		}
	})

	t.Run("unverified_write without target requires input", func(t *testing.T) {
		t.Parallel()
		decision := continueWithCompletionReason(DecisionInput{
			CompletionReason: "unverified_write",
		})
		if decision.RequiredInput == nil || decision.RequiredInput.Kind != "missing_file_target_or_content" {
			t.Fatalf("required input = %+v", decision.RequiredInput)
		}
	})

	t.Run("post_execute_closure_required maps to closure missing fact", func(t *testing.T) {
		t.Parallel()
		decision := continueWithCompletionReason(DecisionInput{
			CompletionReason: "post_execute_closure_required",
		})
		if len(decision.MissingFacts) == 0 || decision.MissingFacts[0].Kind != "post_execute_closure" {
			t.Fatalf("missing facts = %+v", decision.MissingFacts)
		}
	})
}

func TestDecideTaskSpecificBranches(t *testing.T) {
	t.Parallel()

	t.Run("todo_state without creation facts requests todo_write", func(t *testing.T) {
		t.Parallel()
		decision := decideTodoState(DecisionInput{})
		if decision.RequiredInput == nil || decision.RequiredInput.Kind != "missing_todo_content" {
			t.Fatalf("required input = %+v", decision.RequiredInput)
		}
	})

	t.Run("mixed accepts when verification passed and required todos closed", func(t *testing.T) {
		t.Parallel()
		decision := decideMixed(DecisionInput{
			Facts: runtimefacts.RuntimeFacts{
				Verification: runtimefacts.VerificationFacts{
					Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_read_file", Scope: "artifact:a.txt"}},
				},
			},
			Todos: TodoSnapshot{Summary: TodoSummary{RequiredOpen: 0}},
		})
		if decision.Status != DecisionAccepted {
			t.Fatalf("status = %q, want accepted", decision.Status)
		}
	})

	t.Run("subagent failed without completion returns failed", func(t *testing.T) {
		t.Parallel()
		decision := decideSubAgent(DecisionInput{
			Facts: runtimefacts.RuntimeFacts{
				SubAgents: runtimefacts.SubAgentFacts{
					Started: []runtimefacts.SubAgentFact{{TaskID: "sa-1"}},
					Failed:  []runtimefacts.SubAgentFact{{TaskID: "sa-1"}},
				},
			},
		})
		if decision.Status != DecisionFailed {
			t.Fatalf("status = %q, want failed", decision.Status)
		}
	})
}

func TestHelperFunctionsBranches(t *testing.T) {
	t.Parallel()

	if got := latestToolErrorDetail([]runtimefacts.ToolErrorFact{{Tool: "filesystem_write_file", ErrorClass: "timeout"}}, "filesystem_write_file"); got != "timeout" {
		t.Fatalf("latestToolErrorDetail = %q, want timeout", got)
	}

	if !hasVerificationForTarget(runtimefacts.RuntimeFacts{
		Verification: runtimefacts.VerificationFacts{
			Passed: []runtimefacts.VerificationFact{{Scope: "artifact:./docs/../docs/readme.md"}},
		},
	}, "docs/readme.md") {
		t.Fatal("expected normalized artifact scope match")
	}

	if got := extractGoalPaths(`edit "A.TXT", then update a.txt and b.md`); len(got) != 2 {
		t.Fatalf("extractGoalPaths len = %d, want 2", len(got))
	}
}

func TestHelperFunctionsAdditionalBranches(t *testing.T) {
	t.Parallel()

	if got := firstOrEmpty(nil); got != "" {
		t.Fatalf("firstOrEmpty(nil) = %q, want empty", got)
	}
	if got := firstOrEmpty([]string{"a", "b"}); got != "a" {
		t.Fatalf("firstOrEmpty = %q, want a", got)
	}

	open := collectOpenRequiredTodos([]TodoViewItem{
		{ID: "todo-1", Required: true, Status: "pending"},
		{ID: "todo-2", Required: true, Status: "in_progress"},
		{ID: "todo-3", Required: true, Status: "completed"},
		{ID: "", Required: true, Status: "pending"},
	})
	if len(open) != 2 || open[0] != "todo-1" || open[1] != "todo-2" {
		t.Fatalf("collectOpenRequiredTodos = %#v, want [todo-1 todo-2]", open)
	}

	if got := latestToolErrorDetail([]runtimefacts.ToolErrorFact{
		{Tool: "filesystem_write_file", Content: " last content "},
	}, "filesystem_write_file"); got != "last content" {
		t.Fatalf("latestToolErrorDetail(content fallback) = %q, want trimmed content", got)
	}

	if hasWorkspaceWriteHardFailure([]runtimefacts.ToolErrorFact{
		{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for /tmp/other.txt"},
		{Tool: "filesystem_write_file", ErrorClass: "permission_denied", Content: "permission denied for /tmp/other2.txt"},
	}, "/tmp/target.txt") {
		t.Fatal("hasWorkspaceWriteHardFailure should require target correlation")
	}

	target, expected, ok := selectVerificationTarget(DecisionInput{
		UserGoal: "please edit ./docs/readme.md and include hello",
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Written: []runtimefacts.FileWriteFact{
					{Path: "a.md", WorkspaceWrite: true},
					{Path: "docs/readme.md", WorkspaceWrite: true, ExpectedContent: "hello"},
				},
			},
		},
	})
	if !ok || target != "docs/readme.md" || expected != "hello" {
		t.Fatalf("selectVerificationTarget = target=%q expected=%q ok=%v", target, expected, ok)
	}
}
