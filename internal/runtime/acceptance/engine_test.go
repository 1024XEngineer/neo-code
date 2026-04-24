package acceptance

import (
	"context"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

type staticPolicy struct {
	verifiers []verify.FinalVerifier
}

func (p staticPolicy) ResolveVerifiers(input verify.FinalVerifyInput) []verify.FinalVerifier {
	_ = input
	return p.verifiers
}

type staticVerifier struct {
	name   string
	result verify.VerificationResult
}

func (v staticVerifier) Name() string { return v.name }
func (v staticVerifier) VerifyFinal(ctx context.Context, input verify.FinalVerifyInput) (verify.VerificationResult, error) {
	_ = ctx
	_ = input
	return v.result, nil
}

func TestEngineEvaluateFinalAggregation(t *testing.T) {
	t.Parallel()

	makeInput := func() FinalAcceptanceInput {
		return FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: verifyEnabledConfig(),
			},
		}
	}

	t.Run("any fail -> failed", func(t *testing.T) {
		t.Parallel()
		engine := NewEngine(staticPolicy{
			verifiers: []verify.FinalVerifier{
				staticVerifier{name: "test", result: verify.VerificationResult{Name: "test", Status: verify.VerificationFail, ErrorClass: verify.ErrorClassTestFailure}},
			},
		})
		decision, err := engine.EvaluateFinal(context.Background(), makeInput())
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceFailed {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceFailed)
		}
		if decision.StopReason != controlplane.StopReasonVerificationFailed {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonVerificationFailed)
		}
	})

	t.Run("fail keeps detailed stop reason from gate", func(t *testing.T) {
		t.Parallel()
		engine := NewEngine(staticPolicy{
			verifiers: []verify.FinalVerifier{
				staticVerifier{
					name: "build",
					result: verify.VerificationResult{
						Name:       "build",
						Status:     verify.VerificationFail,
						Reason:     "missing verifier command configuration",
						ErrorClass: verify.ErrorClassEnvMissing,
					},
				},
			},
		})
		decision, err := engine.EvaluateFinal(context.Background(), makeInput())
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.StopReason != controlplane.StopReasonVerificationConfigMissing {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonVerificationConfigMissing)
		}
	})

	t.Run("any hard_block -> incomplete", func(t *testing.T) {
		t.Parallel()
		engine := NewEngine(staticPolicy{
			verifiers: []verify.FinalVerifier{
				staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationHardBlock, WaitingExternal: true}},
			},
		})
		decision, err := engine.EvaluateFinal(context.Background(), makeInput())
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceIncomplete {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceIncomplete)
		}
		if decision.StopReason != controlplane.StopReasonTodoWaitingExternal {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonTodoWaitingExternal)
		}
	})

	t.Run("any soft_block -> continue", func(t *testing.T) {
		t.Parallel()
		engine := NewEngine(staticPolicy{
			verifiers: []verify.FinalVerifier{
				staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationSoftBlock}},
			},
		})
		decision, err := engine.EvaluateFinal(context.Background(), makeInput())
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceContinue {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceContinue)
		}
	})

	t.Run("all pass -> accepted", func(t *testing.T) {
		t.Parallel()
		engine := NewEngine(staticPolicy{
			verifiers: []verify.FinalVerifier{
				staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationPass}},
			},
		})
		decision, err := engine.EvaluateFinal(context.Background(), makeInput())
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceAccepted {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceAccepted)
		}
		if decision.StopReason != controlplane.StopReasonAccepted {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonAccepted)
		}
	})
}

func TestEngineEvaluateFinalCompletionGateAndRetry(t *testing.T) {
	t.Parallel()

	engine := NewEngine(staticPolicy{
		verifiers: []verify.FinalVerifier{
			staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationPass}},
		},
	})

	t.Run("completion gate false -> continue", func(t *testing.T) {
		t.Parallel()
		decision, err := engine.EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: false},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: verifyEnabledConfig(),
			},
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceContinue {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceContinue)
		}
	})

	t.Run("retry exhausted overrides", func(t *testing.T) {
		t.Parallel()
		decision, err := engine.EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: verifyEnabledConfig(),
				Todos: []verify.TodoSnapshot{
					{ID: "todo-1", Required: true, RetryCount: 2, RetryLimit: 2},
				},
			},
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceFailed {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceFailed)
		}
		if decision.StopReason != controlplane.StopReasonRetryExhausted {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonRetryExhausted)
		}
	})
}

func TestEngineEvaluateFinalHookFailurePolicy(t *testing.T) {
	t.Parallel()

	engine := NewEngine(staticPolicy{
		verifiers: []verify.FinalVerifier{
			staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationPass}},
		},
	})

	t.Run("fail_closed returns failed decision", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		cfg.Hooks.BeforeCompletionDecision.Enabled = true
		cfg.Hooks.BeforeCompletionDecision.FailurePolicy = "fail_closed"
		decision, err := engine.EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: cfg,
			},
			MaxTurnsReached: true,
			MaxTurnsLimit:   0,
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceFailed {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceFailed)
		}
		if decision.StopReason != controlplane.StopReasonVerificationExecutionError {
			t.Fatalf("stop_reason = %q, want %q", decision.StopReason, controlplane.StopReasonVerificationExecutionError)
		}
	})

	t.Run("fail_open ignores hook error", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		cfg.Hooks.BeforeCompletionDecision.Enabled = true
		cfg.Hooks.BeforeCompletionDecision.FailurePolicy = "fail_open"
		decision, err := engine.EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: cfg,
			},
			MaxTurnsReached: true,
			MaxTurnsLimit:   0,
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceAccepted {
			t.Fatalf("status = %q, want %q", decision.Status, AcceptanceAccepted)
		}
	})
}

func verifyEnabledConfig() (cfg config.VerificationConfig) {
	cfg = config.StaticDefaults().Runtime.Verification
	cfg.Enabled = boolPtr(true)
	return cfg
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}
