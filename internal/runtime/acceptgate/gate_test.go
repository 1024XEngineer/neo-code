package acceptgate

import (
	"context"
	"testing"

	"neo-code/internal/runtime/controlplane"
	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

func TestEvaluateFallbackOutputOnly(t *testing.T) {
	t.Parallel()

	report := Evaluate(context.Background(), Input{LastAssistantText: "done"})
	if report.Outcome != OutcomeAccepted || report.StopReason != controlplane.StopReasonAccepted {
		t.Fatalf("report = %+v, want accepted", report)
	}

	report = Evaluate(context.Background(), Input{})
	if report.Outcome != OutcomeFailed || report.StopReason != controlplane.StopReasonAcceptCheckFailed {
		t.Fatalf("report = %+v, want accept_check_failed", report)
	}
}

func TestEvaluateCommandSuccess(t *testing.T) {
	t.Parallel()

	input := Input{
		PlanVerify: agentsession.AcceptChecks{{Kind: agentsession.AcceptCheckCommandSuccess, Target: "go test ./..."}},
		Facts: runtimefacts.RuntimeFacts{
			Commands: runtimefacts.CommandFacts{Executed: []runtimefacts.CommandFact{
				{Tool: "bash", Command: "GOFLAGS=-count=1 go test ./...", Succeeded: true},
			}},
		},
		LastAssistantText: "done",
	}
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeAccepted {
		t.Fatalf("report = %+v, want accepted", report)
	}

	input.Facts.Commands.Executed[0].Succeeded = false
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeFailed {
		t.Fatalf("report = %+v, want failed", report)
	}
}

func TestEvaluateWorkspaceChangeUsesRuntimeFactsOnly(t *testing.T) {
	t.Parallel()

	input := Input{
		PlanVerify: agentsession.AcceptChecks{{Kind: agentsession.AcceptCheckWorkspaceChange}},
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{Written: []runtimefacts.FileWriteFact{{Path: "internal/foo.go"}}},
		},
		LastAssistantText: "done",
	}
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeAccepted {
		t.Fatalf("written fact report = %+v, want accepted", report)
	}

	input.Facts.Files.Written = nil
	input.Facts.Files.Exists = []runtimefacts.FileExistFact{{Path: "internal/foo.go", Source: "filesystem_write_file"}}
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeAccepted {
		t.Fatalf("write-source exists report = %+v, want accepted", report)
	}

	input.Facts.Files.Exists = []runtimefacts.FileExistFact{{Path: "internal/foo.go", Source: "filesystem_read_file"}}
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeFailed {
		t.Fatalf("read-only fact report = %+v, want failed", report)
	}
}

func TestEvaluateFileAndContentFacts(t *testing.T) {
	t.Parallel()

	input := Input{
		PlanVerify: agentsession.AcceptChecks{
			{Kind: agentsession.AcceptCheckFileExists, Target: "./README.md"},
			{Kind: agentsession.AcceptCheckContentContains, Target: "README.md", Params: map[string]string{"contains": "NeoCode"}},
		},
		Facts: runtimefacts.RuntimeFacts{
			Files: runtimefacts.FileFacts{
				Exists: []runtimefacts.FileExistFact{{Path: "README.md", Source: "filesystem_read_file"}},
				ContentMatch: []runtimefacts.FileContentMatchFact{{
					Path:               "README.md",
					ExpectedContains:   []string{"NeoCode"},
					VerificationPassed: true,
				}},
			},
		},
		LastAssistantText: "done",
	}
	if report := Evaluate(context.Background(), input); report.Outcome != OutcomeAccepted {
		t.Fatalf("report = %+v, want accepted", report)
	}

	input.Facts.Files.ContentMatch[0].VerificationPassed = false
	report := Evaluate(context.Background(), input)
	if report.Outcome != OutcomeFailed || len(report.Results) != 4 {
		t.Fatalf("report = %+v, want failed with all results", report)
	}
}

func TestEvaluateToolFactAndUnknownKind(t *testing.T) {
	t.Parallel()

	input := Input{
		PlanVerify: agentsession.AcceptChecks{
			{Kind: agentsession.AcceptCheckToolFact, Params: map[string]string{"tool": "bash", "scope": "test"}},
			{Kind: "future_check"},
		},
		Facts: runtimefacts.RuntimeFacts{
			Verification: runtimefacts.VerificationFacts{
				Passed: []runtimefacts.VerificationFact{{Tool: "bash", Scope: "test"}},
			},
		},
		LastAssistantText: "done",
	}
	report := Evaluate(context.Background(), input)
	if report.Outcome != OutcomeFailed || report.StopReason != controlplane.StopReasonAcceptCheckFailed {
		t.Fatalf("report = %+v, want unknown kind failure", report)
	}
	if report.Results[len(report.Results)-1].Reason != "unknown required accept check kind" {
		t.Fatalf("last result = %+v, want unknown kind reason", report.Results[len(report.Results)-1])
	}
}

func TestEvaluateTodoPriority(t *testing.T) {
	t.Parallel()

	required := true
	input := Input{
		PlanVerify:        agentsession.AcceptChecks{{Kind: agentsession.AcceptCheckOutputOnly}},
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
	if report.Outcome != OutcomeFailed || report.StopReason != controlplane.StopReasonTodoNotConverged {
		t.Fatalf("pending todo report = %+v, want todo_not_converged", report)
	}
}
