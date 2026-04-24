package acceptance

import (
	"context"
	"errors"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

func TestNewEngineWithNilPolicy(t *testing.T) {
	t.Parallel()

	engine := NewEngine(nil)
	cfg := verifyEnabledConfig()
	disableAllHooks(&cfg)
	decision, err := engine.EvaluateFinal(context.Background(), FinalAcceptanceInput{
		CompletionGate: CompletionGateDecision{Passed: true},
		VerificationInput: verify.FinalVerifyInput{
			VerificationConfig: cfg,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateFinal() error = %v", err)
	}
	if decision.Status != AcceptanceAccepted {
		t.Fatalf("status = %q, want %q", decision.Status, AcceptanceAccepted)
	}
}

func TestEngineEvaluateFinalAdditionalBranches(t *testing.T) {
	t.Parallel()

	t.Run("verification disabled compatibility fallback", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		cfg.Enabled = boolPtr(false)
		disableAllHooks(&cfg)
		decision, err := NewEngine(staticPolicy{}).EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate: CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{
				VerificationConfig: cfg,
			},
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceAccepted || decision.StopReason != controlplane.StopReasonCompatibilityFallback {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("hook fail closed before verification", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		cfg.Hooks.BeforeVerification.Enabled = true
		cfg.Hooks.BeforeVerification.FailurePolicy = "fail_closed"
		decision, err := NewEngine(staticPolicy{}).EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate:    CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{VerificationConfig: cfg},
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceFailed {
			t.Fatalf("status = %q, want failed", decision.Status)
		}
	})

	t.Run("no progress breaker", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		disableAllHooks(&cfg)
		decision, err := NewEngine(staticPolicy{verifiers: []verify.FinalVerifier{
			staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationSoftBlock}},
		}}).EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate:     CompletionGateDecision{Passed: true},
			VerificationInput:  verify.FinalVerifyInput{VerificationConfig: cfg},
			NoProgressExceeded: true,
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.Status != AcceptanceIncomplete || decision.StopReason != controlplane.StopReasonNoProgressAfterFinalIntercept {
			t.Fatalf("unexpected decision: %+v", decision)
		}
	})

	t.Run("max turns with unconverged todos", func(t *testing.T) {
		t.Parallel()
		cfg := verifyEnabledConfig()
		disableAllHooks(&cfg)
		decision, err := NewEngine(staticPolicy{verifiers: []verify.FinalVerifier{
			staticVerifier{name: "todo", result: verify.VerificationResult{Name: "todo", Status: verify.VerificationSoftBlock}},
		}}).EvaluateFinal(context.Background(), FinalAcceptanceInput{
			CompletionGate:    CompletionGateDecision{Passed: true},
			VerificationInput: verify.FinalVerifyInput{VerificationConfig: cfg},
			MaxTurnsReached:   true,
			MaxTurnsLimit:     9,
		})
		if err != nil {
			t.Fatalf("EvaluateFinal() error = %v", err)
		}
		if decision.StopReason != controlplane.StopReasonMaxTurnExceededWithUnconvergedTodos {
			t.Fatalf("stop reason = %q", decision.StopReason)
		}
	})

	t.Run("retry exhausted helper", func(t *testing.T) {
		t.Parallel()
		if hasRetryExhausted(nil) {
			t.Fatalf("nil todos should not exhaust retry")
		}
		if hasRetryExhausted([]verify.TodoSnapshot{{RetryLimit: 0, RetryCount: 99}}) {
			t.Fatalf("retry limit <=0 should be ignored")
		}
		if !hasRetryExhausted([]verify.TodoSnapshot{{Required: true, RetryLimit: 1, RetryCount: 1}}) {
			t.Fatalf("expected retry exhausted")
		}
	})

	t.Run("firstResultByStatus no match", func(t *testing.T) {
		t.Parallel()
		if got := firstResultByStatus([]verify.VerificationResult{{Status: verify.VerificationPass}}, verify.VerificationFail); got != nil {
			t.Fatalf("unexpected matched result")
		}
	})
}

func TestRunHookWithTimeout(t *testing.T) {
	t.Parallel()

	if err := runHookWithTimeout(context.Background(), 0, nil, FinalAcceptanceInput{}); err != nil {
		t.Fatalf("nil hook should return nil: %v", err)
	}

	hook := namedHook{name: "sleep", run: func(ctx context.Context, _ FinalAcceptanceInput) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	err := runHookWithTimeout(context.Background(), 1, hook, FinalAcceptanceInput{})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	quick := namedHook{name: "ok", run: func(context.Context, FinalAcceptanceInput) error { return nil }}
	if err := runHookWithTimeout(context.Background(), int((2*time.Second)/time.Second), quick, FinalAcceptanceInput{}); err != nil {
		t.Fatalf("quick hook error = %v", err)
	}
}

func TestDefaultPolicyBuildVerifierCoversAllNames(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy{Executor: verify.PolicyCommandExecutor{}}
	for _, name := range []string{"file_exists", "content_match", "command_success", "git_diff", "build", "test", "lint", "typecheck"} {
		if v := policy.buildVerifier(name); v == nil {
			t.Fatalf("buildVerifier(%q) returned nil", name)
		}
	}
}

func TestDecideStopReasonAdditionalBranches(t *testing.T) {
	t.Parallel()

	reason, detail := controlplane.DecideStopReason(controlplane.StopInput{
		PreDecidedReason: controlplane.StopReasonCompatibilityFallback,
		PreDecidedDetail: "  keep  ",
	})
	if reason != controlplane.StopReasonCompatibilityFallback || detail != "keep" {
		t.Fatalf("pre-decided mismatch: %q %q", reason, detail)
	}

	reason, _ = controlplane.DecideStopReason(controlplane.StopInput{MaxTurnsReached: true})
	if reason != controlplane.StopReasonMaxTurnExceeded {
		t.Fatalf("reason = %q", reason)
	}
	reason, _ = controlplane.DecideStopReason(controlplane.StopInput{RetryExhausted: true})
	if reason != controlplane.StopReasonRetryExhausted {
		t.Fatalf("reason = %q", reason)
	}
	reason, _ = controlplane.DecideStopReason(controlplane.StopInput{VerificationFailed: true})
	if reason != controlplane.StopReasonVerificationFailed {
		t.Fatalf("reason = %q", reason)
	}
}

func disableAllHooks(cfg *config.VerificationConfig) {
	cfg.Hooks.BeforeVerification.Enabled = false
	cfg.Hooks.AfterVerification.Enabled = false
	cfg.Hooks.BeforeCompletionDecision.Enabled = false
}
