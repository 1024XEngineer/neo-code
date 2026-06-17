package config

import "testing"

func TestRuntimeConfigCloneAndDefaults(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{MaxRepeatCycleStreak: 4, MaxTurns: 21}
	cloned := cfg.Clone()
	if cloned.MaxRepeatCycleStreak != 4 || cloned.MaxTurns != 21 {
		t.Fatalf("Clone() mismatch: %+v", cloned)
	}

	defaults := defaultRuntimeConfig()
	var zero RuntimeConfig
	zero.ApplyDefaults(defaults)
	if len(zero.Verification.Verifiers) == 0 {
		t.Fatal("expected default verifiers to be populated")
	}
}

// TestRuntimeAssetsConfigApplyDefaultsNilAsTrue 验证当 defaults.TextAssetEnabled 为 nil 时，
// ApplyDefaults 将 TextAssetEnabled 设为 true，与 IsTextAssetEnabled() 的 nil-as-true 语义对齐。
func TestRuntimeAssetsConfigApplyDefaultsNilAsTrue(t *testing.T) {
	t.Parallel()

	var zero RuntimeAssetsConfig
	zero.ApplyDefaults(RuntimeAssetsConfig{})
	if zero.TextAssetEnabled == nil {
		t.Fatal("expected TextAssetEnabled to be non-nil after ApplyDefaults")
	}
	if !*zero.TextAssetEnabled {
		t.Fatalf("expected TextAssetEnabled default true, got false")
	}
	if !zero.IsTextAssetEnabled() {
		t.Fatalf("expected IsTextAssetEnabled() true, got false")
	}
}

func TestRuntimeConfigValidate(t *testing.T) {
	t.Parallel()

	if err := (RuntimeConfig{MaxRepeatCycleStreak: 1, MaxTurns: 1}).Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if err := (RuntimeConfig{MaxRepeatCycleStreak: 0, MaxTurns: 1}).Validate(); err == nil {
		t.Fatal("expected max_repeat_cycle_streak validation error")
	}
	if err := (RuntimeConfig{MaxRepeatCycleStreak: 1, MaxTurns: -1}).Validate(); err == nil {
		t.Fatal("expected max_turns validation error")
	}

	err := (RuntimeConfig{
		MaxRepeatCycleStreak: 1,
		MaxTurns:             1,
		Verification: VerificationConfig{
			Verifiers: map[string]VerifierConfig{
				"": {},
			},
			ExecutionPolicy: VerificationExecutionPolicyConfig{
				Mode:             verificationExecModeNonInteractive,
				DefaultTimeout:   1,
				DefaultOutputCap: 1,
			},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected invalid verification config")
	}
}
