package verify

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/runtime/controlplane"
)

type stubFinalVerifier struct {
	name   string
	result VerificationResult
	err    error
}

func (s stubFinalVerifier) Name() string { return s.name }
func (s stubFinalVerifier) VerifyFinal(ctx context.Context, input FinalVerifyInput) (VerificationResult, error) {
	_ = ctx
	_ = input
	if s.err != nil {
		return VerificationResult{}, s.err
	}
	return s.result, nil
}

func TestOrchestratorRunFinalVerification(t *testing.T) {
	t.Parallel()

	t.Run("runs all verifiers, aggregates all results", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationFail}},
			stubFinalVerifier{name: "build", result: VerificationResult{Name: "build", Status: VerificationFail}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed {
			t.Fatalf("unexpected decision: %+v", decision)
		}
		if len(decision.Results) != 2 {
			t.Fatalf("results len = %d, want 2 (all verifiers run)", len(decision.Results))
		}
	})

	t.Run("verifier error becomes fail", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", err: errors.New("boom")},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonVerificationFailed {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("fail with waiting external maps to verification_failed", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationFail, WaitingExternal: true}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Reason != controlplane.StopReasonVerificationFailed {
			t.Fatalf("reason = %q, want %q", decision.Reason, controlplane.StopReasonTodoWaitingExternal)
		}
	})

	t.Run("fail stop reason uses error class only", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "build", result: VerificationResult{Name: "build", Status: VerificationFail, ErrorClass: ErrorClassEnvMissing}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Reason != controlplane.StopReasonVerificationConfigMissing {
			t.Fatalf("reason = %q, want %q", decision.Reason, controlplane.StopReasonVerificationConfigMissing)
		}
	})
}
