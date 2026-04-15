package subagent

import (
	"context"
	"testing"
)

func TestDefaultEngineRunStep(t *testing.T) {
	t.Parallel()

	engine := defaultEngine{}

	t.Run("uses expected output as summary", func(t *testing.T) {
		t.Parallel()

		out, err := engine.RunStep(context.Background(), StepInput{
			Task: Task{
				Goal:           "goal",
				ExpectedOutput: "expected",
			},
		})
		if err != nil {
			t.Fatalf("RunStep() error = %v", err)
		}
		if !out.Done {
			t.Fatalf("expected done output")
		}
		if out.Output.Summary != "expected" {
			t.Fatalf("summary = %q, want %q", out.Output.Summary, "expected")
		}
		if len(out.Output.Findings) == 0 || len(out.Output.Patches) == 0 || len(out.Output.Risks) == 0 {
			t.Fatalf("default engine should populate required list sections, got %+v", out.Output)
		}
		if len(out.Output.NextActions) == 0 || len(out.Output.Artifacts) == 0 {
			t.Fatalf("default engine should populate required sections, got %+v", out.Output)
		}
	})

	t.Run("falls back to goal", func(t *testing.T) {
		t.Parallel()

		out, err := engine.RunStep(context.Background(), StepInput{
			Task: Task{
				Goal:           "goal-value",
				ExpectedOutput: " ",
			},
		})
		if err != nil {
			t.Fatalf("RunStep() error = %v", err)
		}
		if out.Output.Summary != "goal-value" {
			t.Fatalf("summary = %q, want %q", out.Output.Summary, "goal-value")
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := engine.RunStep(ctx, StepInput{Task: Task{Goal: "g"}}); err == nil {
			t.Fatalf("expected context error")
		}
	})

	t.Run("satisfies default role contract", func(t *testing.T) {
		t.Parallel()

		out, err := engine.RunStep(context.Background(), StepInput{
			Task: Task{
				Goal:           "goal",
				ExpectedOutput: "summary",
			},
		})
		if err != nil {
			t.Fatalf("RunStep() error = %v", err)
		}

		for _, role := range []Role{RoleResearcher, RoleCoder, RoleReviewer} {
			policy, err := DefaultRolePolicy(role)
			if err != nil {
				t.Fatalf("DefaultRolePolicy(%q) error = %v", role, err)
			}
			if err := validateOutputContract(policy, out.Output); err != nil {
				t.Fatalf("validateOutputContract(%q) error = %v", role, err)
			}
		}
	})
}
