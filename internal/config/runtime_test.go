package config

import (
	"strings"
	"testing"
)

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

func TestRuntimeTextAssetConfigDelegationAndClone(t *testing.T) {
	t.Parallel()

	disabled := false
	cfg := RuntimeConfig{Assets: RuntimeAssetsConfig{
		TextAssetEnabled:  &disabled,
		MaxTextAssetBytes: 123,
		MaxTextAssetChars: 456,
	}}
	if cfg.IsTextAssetEnabled() {
		t.Fatal("expected text assets to be disabled")
	}
	policy := cfg.ResolveTextAssetPolicy()
	if policy.MaxTextAssetBytes != 123 || policy.MaxTextAssetChars != 456 {
		t.Fatalf("unexpected resolved text asset policy: %+v", policy)
	}
	if !policy.Whitelist.LookupByMime("text/markdown") {
		t.Fatal("expected resolved policy to contain the default text whitelist")
	}

	cloned := cfg.Clone()
	if cloned.Assets.TextAssetEnabled == cfg.Assets.TextAssetEnabled {
		t.Fatal("expected Clone() to copy the feature flag pointer")
	}
	*cloned.Assets.TextAssetEnabled = true
	if cfg.IsTextAssetEnabled() {
		t.Fatal("mutating cloned feature flag must not affect the source")
	}
}

func TestRuntimeAssetsConfigTextDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	disabled := false
	defaults := RuntimeAssetsConfig{
		TextAssetEnabled:  &disabled,
		MaxTextAssetBytes: 321,
		MaxTextAssetChars: 654,
	}
	var cfg RuntimeAssetsConfig
	cfg.ApplyDefaults(defaults)
	if cfg.IsTextAssetEnabled() || cfg.MaxTextAssetBytes != 321 || cfg.MaxTextAssetChars != 654 {
		t.Fatalf("unexpected applied text defaults: %+v", cfg)
	}

	tests := []struct {
		name string
		cfg  RuntimeAssetsConfig
		want string
	}{
		{name: "negative bytes", cfg: RuntimeAssetsConfig{MaxTextAssetBytes: -1}, want: "max_text_asset_bytes"},
		{name: "negative chars", cfg: RuntimeAssetsConfig{MaxTextAssetChars: -1}, want: "max_text_asset_chars"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
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
