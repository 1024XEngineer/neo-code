package verify

import (
	"context"
	"testing"
)

func TestTodoConvergenceVerifierStates(t *testing.T) {
	t.Parallel()

	verifier := TodoConvergenceVerifier{}

	t.Run("failed required todo returns fail", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "completed", Required: true},
				{ID: "t2", Status: "failed", Required: true},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("canceled todo without replacement returns fail", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "canceled", Required: true},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("canceled todo with replacement passes", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "canceled", Required: true},
				{ID: "t2", Status: "pending", Required: true, Supersedes: []string{"t1"}},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("pending and in_progress are soft_block", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "pending", Required: true},
				{ID: "t2", Status: "in_progress", Required: true},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
	})

	t.Run("blocked waiting external is hard_block", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "blocked", Required: true, BlockedReason: "user_input_wait"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want %q", result.Status, VerificationFail)
		}
		if !result.WaitingExternal {
			t.Fatalf("expected WaitingExternal=true")
		}
	})

	t.Run("optional todo is ignored", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "pending", Required: false},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
		}
	})
}
