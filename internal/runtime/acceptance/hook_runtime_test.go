package acceptance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/runtime/verify"
)

type namedHook struct {
	name string
	run  func(ctx context.Context, input FinalAcceptanceInput) error
}

func (h namedHook) Name() string { return h.name }

func (h namedHook) Run(ctx context.Context, input FinalAcceptanceInput) error {
	if h.run == nil {
		return nil
	}
	return h.run(ctx, input)
}

func TestRunConfiguredHooks(t *testing.T) {
	t.Parallel()

	makeSpec := func() config.HookSpec {
		return config.HookSpec{Enabled: true, TimeoutSec: 1}
	}

	t.Run("disabled or empty short circuit", func(t *testing.T) {
		t.Parallel()
		spec := makeSpec()
		spec.Enabled = false
		if err := runConfiguredHooks(context.Background(), spec, "before", nil, FinalAcceptanceInput{}); err != nil {
			t.Fatalf("runConfiguredHooks() error = %v", err)
		}
	})

	t.Run("priority filter and stable order", func(t *testing.T) {
		t.Parallel()
		executed := make([]string, 0)
		spec := makeSpec()
		spec.Priority = 10
		hooks := []prioritizedHook{
			{priority: 5, hook: namedHook{name: "skip-low", run: func(context.Context, FinalAcceptanceInput) error { executed = append(executed, "skip-low"); return nil }}},
			{priority: 10, hook: namedHook{name: "b", run: func(context.Context, FinalAcceptanceInput) error { executed = append(executed, "b"); return nil }}},
			{priority: 10, hook: namedHook{name: "a", run: func(context.Context, FinalAcceptanceInput) error { executed = append(executed, "a"); return nil }}},
		}
		if err := runConfiguredHooks(context.Background(), spec, "before", hooks, FinalAcceptanceInput{}); err != nil {
			t.Fatalf("runConfiguredHooks() error = %v", err)
		}
		if strings.Join(executed, ",") != "a,b" {
			t.Fatalf("execution order = %v, want [a b]", executed)
		}
	})

	t.Run("all hooks filtered or nil returns nil", func(t *testing.T) {
		t.Parallel()
		spec := makeSpec()
		spec.Priority = 100
		err := runConfiguredHooks(context.Background(), spec, "before", []prioritizedHook{
			{priority: 10, hook: nil},
			{priority: 10, hook: namedHook{name: "low"}},
		}, FinalAcceptanceInput{})
		if err != nil {
			t.Fatalf("runConfiguredHooks() error = %v", err)
		}
	})

	t.Run("hook error includes stage and hook name", func(t *testing.T) {
		t.Parallel()
		spec := makeSpec()
		err := runConfiguredHooks(context.Background(), spec, "after", []prioritizedHook{
			{priority: 10, hook: namedHook{name: "failing", run: func(context.Context, FinalAcceptanceInput) error { return errors.New("boom") }}},
		}, FinalAcceptanceInput{})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "after hook \"failing\" failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestHookPolicyAndDecision(t *testing.T) {
	t.Parallel()

	if !isFailOpenPolicy(" fail_open ") {
		t.Fatalf("expected fail_open to be true")
	}
	if isFailOpenPolicy("fail_closed") {
		t.Fatalf("expected fail_closed to be false")
	}

	decision := hookFailureDecision("before", errors.New("x"))
	if decision.Status != AcceptanceFailed || decision.StopReason != controlplane.StopReasonVerificationExecutionError {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if !strings.Contains(decision.InternalSummary, "before") {
		t.Fatalf("unexpected internal summary: %q", decision.InternalSummary)
	}

	emptyDecision := hookFailureDecision("after", errors.New(""))
	if !strings.Contains(emptyDecision.InternalSummary, "acceptance hook failed") {
		t.Fatalf("expected default hook failure detail, got %q", emptyDecision.InternalSummary)
	}
}

func TestRuntimeHooksNameAndRun(t *testing.T) {
	t.Parallel()

	t.Run("verifier selection", func(t *testing.T) {
		t.Parallel()
		hook := verifierSelectionHook{verifierCount: 0}
		if hook.Name() != "verifier_selection" {
			t.Fatalf("unexpected name: %q", hook.Name())
		}
		input := FinalAcceptanceInput{VerificationInput: verify.FinalVerifyInput{VerificationConfig: config.StaticDefaults().Runtime.Verification}}
		if err := hook.Run(context.Background(), input); err == nil {
			t.Fatalf("expected verifier selection error")
		}
		hook.verifierCount = 1
		if err := hook.Run(context.Background(), input); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("task type metadata", func(t *testing.T) {
		t.Parallel()
		hook := taskTypeMetadataHook{}
		if hook.Name() != "task_type_metadata" {
			t.Fatalf("unexpected name: %q", hook.Name())
		}
		if err := hook.Run(context.Background(), FinalAcceptanceInput{}); err != nil {
			t.Fatalf("unexpected empty metadata error: %v", err)
		}
		if err := hook.Run(context.Background(), FinalAcceptanceInput{VerificationInput: verify.FinalVerifyInput{Metadata: map[string]any{"task_type": " "}}}); err == nil {
			t.Fatalf("expected empty task_type error")
		}
	})

	t.Run("verification result hook", func(t *testing.T) {
		t.Parallel()
		hook := verificationResultHook{resultCount: 0}
		if hook.Name() != "verification_results" {
			t.Fatalf("unexpected name: %q", hook.Name())
		}
		input := FinalAcceptanceInput{VerificationInput: verify.FinalVerifyInput{VerificationConfig: config.StaticDefaults().Runtime.Verification}}
		if err := hook.Run(context.Background(), input); err == nil {
			t.Fatalf("expected empty results error")
		}
		hook.resultCount = 1
		if err := hook.Run(context.Background(), input); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("max turn consistency hook", func(t *testing.T) {
		t.Parallel()
		hook := maxTurnConsistencyHook{}
		if hook.Name() != "max_turn_consistency" {
			t.Fatalf("unexpected name: %q", hook.Name())
		}
		if err := hook.Run(context.Background(), FinalAcceptanceInput{MaxTurnsReached: true, MaxTurnsLimit: 0}); err == nil {
			t.Fatalf("expected max turn inconsistency error")
		}
		if err := hook.Run(context.Background(), FinalAcceptanceInput{MaxTurnsReached: true, MaxTurnsLimit: 5}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestHookBuilders(t *testing.T) {
	t.Parallel()

	if hooks := beforeVerificationHooks(1); len(hooks) != 2 {
		t.Fatalf("beforeVerificationHooks len = %d, want 2", len(hooks))
	}
	if hooks := afterVerificationHooks(1); len(hooks) != 1 {
		t.Fatalf("afterVerificationHooks len = %d, want 1", len(hooks))
	}
	if hooks := beforeCompletionDecisionHooks(); len(hooks) != 1 {
		t.Fatalf("beforeCompletionDecisionHooks len = %d, want 1", len(hooks))
	}
}
