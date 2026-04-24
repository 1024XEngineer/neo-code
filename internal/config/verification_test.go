package config

import (
	"strings"
	"testing"
)

func TestVerificationConfigApplyDefaultsAndAccessors(t *testing.T) {
	t.Parallel()

	defaults := defaultVerificationConfig()
	cfg := VerificationConfig{}
	cfg.ApplyDefaults(defaults)

	if !cfg.EnabledValue() {
		t.Fatalf("EnabledValue() = false, want true")
	}
	if !cfg.FinalInterceptValue() {
		t.Fatalf("FinalInterceptValue() = false, want true")
	}
	if cfg.DefaultTaskPolicy == "" || cfg.MaxNoProgress <= 0 || cfg.MaxRetries < 0 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if len(cfg.Verifiers) != len(defaults.Verifiers) {
		t.Fatalf("verifier count = %d, want %d", len(cfg.Verifiers), len(defaults.Verifiers))
	}

	cfg.Enabled = nil
	if cfg.EnabledValue() {
		t.Fatalf("EnabledValue() should be false when pointer is nil")
	}
	cfg.FinalIntercept = nil
	if cfg.FinalInterceptValue() {
		t.Fatalf("FinalInterceptValue() should be false when pointer is nil")
	}

	var nilCfg *VerificationConfig
	nilCfg.ApplyDefaults(defaults)
}

func TestVerificationConfigApplyDefaultsPreservesExistingVerifierAndFillsMissing(t *testing.T) {
	t.Parallel()

	defaults := defaultVerificationConfig()
	cfg := VerificationConfig{
		Enabled:           boolPtrConfig(false),
		FinalIntercept:    boolPtrConfig(false),
		DefaultTaskPolicy: "custom",
		MaxNoProgress:     8,
		MaxRetries:        -3,
		Verifiers: map[string]VerifierConfig{
			verifierGitDiff: {
				Enabled:        true,
				TimeoutSec:     0,
				OutputCapBytes: 0,
				Scope:          "",
			},
		},
		ExecutionPolicy: VerificationExecutionPolicyConfig{},
	}
	cfg.ApplyDefaults(defaults)

	if cfg.EnabledValue() {
		t.Fatalf("enabled should keep explicit false")
	}
	if cfg.FinalInterceptValue() {
		t.Fatalf("final_intercept should keep explicit false")
	}
	if cfg.DefaultTaskPolicy != "custom" || cfg.MaxNoProgress != 8 || cfg.MaxRetries != defaults.MaxRetries {
		t.Fatalf("expected explicit values preserved, got %+v", cfg)
	}
	gitDiff := cfg.Verifiers[verifierGitDiff]
	if gitDiff.TimeoutSec <= 0 || gitDiff.OutputCapBytes <= 0 || strings.TrimSpace(gitDiff.Scope) == "" {
		t.Fatalf("expected verifier defaults merged, got %+v", gitDiff)
	}
	if _, ok := cfg.Verifiers[verifierTodoConvergence]; !ok {
		t.Fatalf("missing default verifier %q", verifierTodoConvergence)
	}
}

func TestVerificationConfigValidate(t *testing.T) {
	t.Parallel()

	valid := defaultVerificationConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	t.Run("default task policy required", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.DefaultTaskPolicy = "  "
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected default_task_policy validation error")
		}
	})

	t.Run("max no progress must be positive", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.MaxNoProgress = 0
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected max_no_progress validation error")
		}
	})

	t.Run("max retries cannot be negative", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.MaxRetries = -1
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected max_retries validation error")
		}
	})

	t.Run("empty verifier name rejected", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.Verifiers[" "] = VerifierConfig{}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected empty verifier name validation error")
		}
	})

	t.Run("verifier validation wrapped", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.Verifiers[verifierTodoConvergence] = VerifierConfig{FailOpen: true, FailClosed: true}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), verifierTodoConvergence) {
			t.Fatalf("expected wrapped verifier error, got %v", err)
		}
	})

	t.Run("execution policy validation", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.ExecutionPolicy.Mode = ""
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected execution policy validation error")
		}
	})

	t.Run("hooks validation", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.Hooks.BeforeVerification.FailurePolicy = "bad"
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), verificationHookBeforeVerification) {
			t.Fatalf("expected hook validation error, got %v", err)
		}
	})

	t.Run("hooks after_verification validation", func(t *testing.T) {
		cfg := defaultVerificationConfig()
		cfg.Hooks.AfterVerification.FailurePolicy = "bad"
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), verificationHookAfterVerification) {
			t.Fatalf("expected hook validation error, got %v", err)
		}
	})
}

func TestVerifierConfigApplyDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	defaults := VerifierConfig{TimeoutSec: 5, OutputCapBytes: 9, Scope: verificationScopeProject, FailClosed: true}
	cfg := VerifierConfig{}
	cfg.ApplyDefaults(defaults)
	if cfg.TimeoutSec != 5 || cfg.OutputCapBytes != 9 || cfg.Scope != verificationScopeProject || !cfg.FailClosed {
		t.Fatalf("ApplyDefaults() mismatch: %+v", cfg)
	}

	cfg = VerifierConfig{FailOpen: true}
	cfg.ApplyDefaults(defaults)
	if !cfg.FailOpen || cfg.FailClosed {
		t.Fatalf("existing fail policy should be preserved: %+v", cfg)
	}

	var nilCfg *VerifierConfig
	nilCfg.ApplyDefaults(defaults)

	if err := (VerifierConfig{}).Validate(); err != nil {
		t.Fatalf("VerifierConfig.Validate() unexpected error: %v", err)
	}
	if err := (VerifierConfig{FailOpen: true, FailClosed: true}).Validate(); err == nil {
		t.Fatalf("expected mutually exclusive fail policy validation error")
	}
	if err := (VerifierConfig{TimeoutSec: -1}).Validate(); err == nil {
		t.Fatalf("expected timeout validation error")
	}
	if err := (VerifierConfig{OutputCapBytes: -1}).Validate(); err == nil {
		t.Fatalf("expected output cap validation error")
	}
}

func TestHookSpecAndHookConfig(t *testing.T) {
	t.Parallel()

	hookDefaults := HookSpec{Enabled: true, TimeoutSec: 2, FailurePolicy: verificationHookFailurePolicyFailClosed}
	hook := HookSpec{}
	hook.ApplyDefaults(hookDefaults)
	if hook.TimeoutSec != 2 || hook.FailurePolicy != verificationHookFailurePolicyFailClosed {
		t.Fatalf("HookSpec.ApplyDefaults() mismatch: %+v", hook)
	}

	var nilHook *HookSpec
	nilHook.ApplyDefaults(hookDefaults)

	if err := (HookSpec{TimeoutSec: -1, FailurePolicy: verificationHookFailurePolicyFailClosed}).Validate(); err == nil {
		t.Fatalf("expected timeout validation error")
	}
	if err := (HookSpec{TimeoutSec: 1, FailurePolicy: "bad"}).Validate(); err == nil {
		t.Fatalf("expected failure_policy validation error")
	}
	if err := (HookSpec{TimeoutSec: 1, FailurePolicy: verificationHookFailurePolicyFailOpen}).Validate(); err != nil {
		t.Fatalf("expected fail_open to be valid, got %v", err)
	}

	hooks := VerificationHookConfig{}
	hooks.ApplyDefaults(defaultVerificationConfig().Hooks)
	if err := hooks.Validate(); err != nil {
		t.Fatalf("VerificationHookConfig.Validate() error = %v", err)
	}

	hooks.BeforeCompletionDecision.FailurePolicy = "bad"
	err := hooks.Validate()
	if err == nil || !strings.Contains(err.Error(), verificationHookBeforeCompletionDecision) {
		t.Fatalf("expected before_completion_decision error, got %v", err)
	}

	var nilHooks *VerificationHookConfig
	nilHooks.ApplyDefaults(defaultVerificationConfig().Hooks)
}

func TestVerificationExecutionPolicyConfig(t *testing.T) {
	t.Parallel()

	defaults := defaultVerificationExecutionPolicyConfig()
	cfg := VerificationExecutionPolicyConfig{}
	cfg.ApplyDefaults(defaults)
	if cfg.Mode != verificationExecModeNonInteractive || cfg.DefaultTimeout <= 0 || cfg.DefaultOutputCap <= 0 {
		t.Fatalf("ApplyDefaults() mismatch: %+v", cfg)
	}
	if len(cfg.AllowedCommands) == 0 || len(cfg.DeniedCommands) == 0 {
		t.Fatalf("command lists should be defaulted")
	}

	cfg.AllowedCommands[0] = "changed"
	if defaults.AllowedCommands[0] == "changed" {
		t.Fatalf("ApplyDefaults() should copy allowed commands")
	}
	cfg.DeniedCommands[0] = "changed"
	if defaults.DeniedCommands[0] == "changed" {
		t.Fatalf("ApplyDefaults() should copy denied commands")
	}

	var nilCfg *VerificationExecutionPolicyConfig
	nilCfg.ApplyDefaults(defaults)

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if err := (VerificationExecutionPolicyConfig{DefaultTimeout: 1, DefaultOutputCap: 1}).Validate(); err == nil {
		t.Fatalf("expected missing mode validation error")
	}
	if err := (VerificationExecutionPolicyConfig{Mode: verificationExecModeNonInteractive, DefaultTimeout: 0, DefaultOutputCap: 1}).Validate(); err == nil {
		t.Fatalf("expected default timeout validation error")
	}
	if err := (VerificationExecutionPolicyConfig{Mode: verificationExecModeNonInteractive, DefaultTimeout: 1, DefaultOutputCap: 0}).Validate(); err == nil {
		t.Fatalf("expected default output cap validation error")
	}
}

func TestVerificationCloneHelpers(t *testing.T) {
	t.Parallel()

	cfg := defaultVerificationConfig()
	cloned := cfg.Clone()
	if cloned.Enabled == cfg.Enabled || cloned.FinalIntercept == cfg.FinalIntercept {
		t.Fatalf("Clone() should deep copy bool pointers")
	}
	if cloned.Verifiers[verifierTodoConvergence].TimeoutSec != cfg.Verifiers[verifierTodoConvergence].TimeoutSec {
		t.Fatalf("Clone() should preserve verifier content")
	}
	cloned.ExecutionPolicy.AllowedCommands[0] = "mutated"
	if cfg.ExecutionPolicy.AllowedCommands[0] == "mutated" {
		t.Fatalf("Clone() should copy execution policy slices")
	}

	if got := cloneBoolPtr(nil); got != nil {
		t.Fatalf("cloneBoolPtr(nil) = %v, want nil", got)
	}
}
