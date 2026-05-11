package config

import "testing"

func TestVerificationConfigApplyDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	defaults := defaultVerificationConfig()
	cfg := VerificationConfig{}
	cfg.ApplyDefaults(defaults)

	if len(cfg.Verifiers) != len(defaults.Verifiers) {
		t.Fatalf("verifier count = %d, want %d", len(cfg.Verifiers), len(defaults.Verifiers))
	}
	if _, ok := cfg.Verifiers["git_diff"]; ok {
		t.Fatal("git_diff verifier should not be configured by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestVerificationConfigValidateRejectsBadFields(t *testing.T) {
	t.Parallel()

	cfg := defaultVerificationConfig()
	cfg.Verifiers[" "] = VerifierConfig{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty verifier name validation error")
	}

	cfg = defaultVerificationConfig()
	cfg.Verifiers[verifierTodoConvergence] = VerifierConfig{TimeoutSec: -1}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected verifier timeout validation error")
	}
}

func TestVerifierConfigCloneAndDefaults(t *testing.T) {
	t.Parallel()

	defaults := VerifierConfig{
		Command:        []string{"go", "test", "./..."},
		TimeoutSec:     5,
		OutputCapBytes: 9,
		Scope:          verificationScopeProject,
	}
	cfg := VerifierConfig{}
	cfg.ApplyDefaults(defaults)
	if len(cfg.Command) != 3 || cfg.Command[0] != "go" {
		t.Fatalf("ApplyDefaults() command = %#v", cfg.Command)
	}
	if cfg.TimeoutSec != 5 || cfg.OutputCapBytes != 9 || cfg.Scope != verificationScopeProject {
		t.Fatalf("ApplyDefaults() mismatch: %+v", cfg)
	}

	cloned := cfg.Clone()
	cloned.Command[0] = "git"
	if cfg.Command[0] != "go" {
		t.Fatalf("Clone() should deep copy command slice")
	}
}

func TestVerificationExecutionPolicyConfig(t *testing.T) {
	t.Parallel()

	defaults := defaultVerificationExecutionPolicyConfig()
	cfg := VerificationExecutionPolicyConfig{}
	cfg.ApplyDefaults(defaults)
	if cfg.Mode != verificationExecModeNonInteractive || cfg.DefaultTimeout <= 0 || cfg.DefaultOutputCap <= 0 {
		t.Fatalf("ApplyDefaults() mismatch: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
