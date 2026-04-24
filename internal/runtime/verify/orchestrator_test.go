package verify

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
)

type stubFinalVerifier struct {
	name   string
	result VerificationResult
	err    error
}

func (s stubFinalVerifier) Name() string {
	return s.name
}

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

	t.Run("empty verifier list passes", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if !decision.Passed || decision.Reason != controlplane.StopReasonAccepted {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("nil verifier is ignored", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{nil}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if len(decision.Results) != 0 {
			t.Fatalf("results length = %d, want 0", len(decision.Results))
		}
	})

	t.Run("verifier error converts to fail decision", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", err: errors.New("boom")},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonVerificationExecutionError {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("hard block with waiting external maps to waiting_external reason", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationHardBlock, WaitingExternal: true}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonTodoWaitingExternal {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("hard block without waiting external maps to todo_not_converged", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationHardBlock}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonTodoNotConverged {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("soft block keeps todo_not_converged", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationSoftBlock}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonTodoNotConverged {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("normalize empty status to fail", func(t *testing.T) {
		t.Parallel()
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "empty", result: VerificationResult{Name: "empty"}},
		}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonVerificationFailed {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("fail_open downgrades verifier fail", func(t *testing.T) {
		t.Parallel()
		input := FinalVerifyInput{
			VerificationConfig: config.VerificationConfig{
				Verifiers: map[string]config.VerifierConfig{
					"todo": {FailOpen: true},
				},
			},
		}
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{name: "todo", result: VerificationResult{Name: "todo", Status: VerificationFail}},
		}}).RunFinalVerification(context.Background(), input)
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if !decision.Passed || decision.Reason != controlplane.StopReasonAccepted {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("fail_closed escalates soft_block to fail", func(t *testing.T) {
		t.Parallel()
		input := FinalVerifyInput{
			VerificationConfig: config.VerificationConfig{
				Verifiers: map[string]config.VerifierConfig{
					"todo": {FailClosed: true},
				},
			},
		}
		decision, err := (Orchestrator{Verifiers: []FinalVerifier{
			stubFinalVerifier{
				name: "todo",
				result: VerificationResult{
					Name:       "todo",
					Status:     VerificationSoftBlock,
					ErrorClass: ErrorClassEnvMissing,
				},
			},
		}}).RunFinalVerification(context.Background(), input)
		if err != nil {
			t.Fatalf("RunFinalVerification() error = %v", err)
		}
		if decision.Passed || decision.Reason != controlplane.StopReasonVerificationConfigMissing {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("fail reason maps to detailed stop reason", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name   string
			result VerificationResult
			want   controlplane.StopReason
		}{
			{
				name:   "config missing",
				result: VerificationResult{Name: "build", Status: VerificationFail, Reason: "missing verifier command configuration", ErrorClass: ErrorClassEnvMissing},
				want:   controlplane.StopReasonVerificationConfigMissing,
			},
			{
				name:   "execution denied",
				result: VerificationResult{Name: "test", Status: VerificationFail, Reason: "verification command denied by execution policy", ErrorClass: ErrorClassPermissionDenied},
				want:   controlplane.StopReasonVerificationExecutionDenied,
			},
			{
				name:   "execution error",
				result: VerificationResult{Name: "lint", Status: VerificationFail, Reason: "verification command execution failed", ErrorClass: ErrorClassTimeout},
				want:   controlplane.StopReasonVerificationExecutionError,
			},
		}
		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				decision, err := (Orchestrator{Verifiers: []FinalVerifier{
					stubFinalVerifier{name: tc.result.Name, result: tc.result},
				}}).RunFinalVerification(context.Background(), FinalVerifyInput{})
				if err != nil {
					t.Fatalf("RunFinalVerification() error = %v", err)
				}
				if decision.Reason != tc.want {
					t.Fatalf("reason = %q, want %q", decision.Reason, tc.want)
				}
			})
		}
	})
}
