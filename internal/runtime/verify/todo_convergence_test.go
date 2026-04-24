package verify

import (
	"context"
	"testing"
)

func TestTodoConvergenceVerifierStates(t *testing.T) {
	t.Parallel()

	verifier := TodoConvergenceVerifier{}

	t.Run("all terminal required todos pass", func(t *testing.T) {
		t.Parallel()
		result, err := verifier.VerifyFinal(context.Background(), FinalVerifyInput{
			Todos: []TodoSnapshot{
				{ID: "t1", Status: "completed", Required: true},
				{ID: "t2", Status: "failed", Required: true},
				{ID: "t3", Status: "canceled", Required: true},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationPass {
			t.Fatalf("status = %q, want %q", result.Status, VerificationPass)
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
		if result.Status != VerificationSoftBlock {
			t.Fatalf("status = %q, want %q", result.Status, VerificationSoftBlock)
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
		if result.Status != VerificationHardBlock {
			t.Fatalf("status = %q, want %q", result.Status, VerificationHardBlock)
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
