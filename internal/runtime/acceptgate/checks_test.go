package acceptgate

import (
	"context"
	"testing"

	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

func TestNormalizeCommandKeepsCLIFlags(t *testing.T) {
	t.Parallel()

	got := normalizeCommand("go test ./... -run=TestFoo --filter=a=b -count=1")
	want := "go test ./... -run=testfoo --filter=a=b -count=1"
	if got != want {
		t.Fatalf("normalizeCommand() = %q, want %q", got, want)
	}
}

func TestNormalizeCommandStripsEnvVars(t *testing.T) {
	t.Parallel()

	got := normalizeCommand("CGO_ENABLED=0 GOFLAGS=-count=1 go test ./...")
	if got != "go test ./..." {
		t.Fatalf("normalizeCommand() = %q, want %q", got, "go test ./...")
	}
}

func TestNormalizeCommandKeepsPathAssignments(t *testing.T) {
	t.Parallel()

	got := normalizeCommand("PKG=./cmd/... go test")
	if got != "pkg=./cmd/... go test" {
		t.Fatalf("normalizeCommand() = %q, want %q", got, "pkg=./cmd/... go test")
	}
}

func TestEvaluateCommandSuccessKeepsFlagSpecificity(t *testing.T) {
	t.Parallel()

	report := Evaluate(context.Background(), Input{
		PlanVerify: agentsession.AcceptChecks{
			{Kind: agentsession.AcceptCheckCommandSuccess, Target: "go test ./... -run=TestFoo"},
		},
		Facts: runtimefacts.RuntimeFacts{
			Commands: runtimefacts.CommandFacts{Executed: []runtimefacts.CommandFact{
				{Tool: "bash", Command: "go test ./...", Succeeded: true},
			}},
		},
		LastAssistantText: "done",
	})
	if report.Outcome != OutcomeFailed {
		t.Fatalf("report = %+v, want failed because broad command must not satisfy -run-specific check", report)
	}
}
