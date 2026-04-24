package config

import "testing"

func TestRuntimeConfigClone(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{MaxNoProgressStreak: 7, MaxRepeatCycleStreak: 4, MaxTurns: 21}
	cloned := cfg.Clone()
	if cloned.MaxNoProgressStreak != 7 {
		t.Fatalf("expected cloned MaxNoProgressStreak=7, got %d", cloned.MaxNoProgressStreak)
	}
	if cloned.MaxRepeatCycleStreak != 4 {
		t.Fatalf("expected cloned MaxRepeatCycleStreak=4, got %d", cloned.MaxRepeatCycleStreak)
	}
	if cloned.MaxTurns != 21 {
		t.Fatalf("expected cloned MaxTurns=21, got %d", cloned.MaxTurns)
	}
	if cloned.Assets.MaxSessionAssetBytes != cfg.Assets.MaxSessionAssetBytes {
		t.Fatalf("expected cloned MaxSessionAssetBytes=%d, got %d", cfg.Assets.MaxSessionAssetBytes, cloned.Assets.MaxSessionAssetBytes)
	}
	if cloned.Assets.MaxSessionAssetsTotalBytes != cfg.Assets.MaxSessionAssetsTotalBytes {
		t.Fatalf(
			"expected cloned MaxSessionAssetsTotalBytes=%d, got %d",
			cfg.Assets.MaxSessionAssetsTotalBytes,
			cloned.Assets.MaxSessionAssetsTotalBytes,
		)
	}
}

func TestRuntimeConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	defaults := RuntimeConfig{
		MaxNoProgressStreak:  3,
		MaxRepeatCycleStreak: 5,
		MaxTurns:             7,
		Assets: RuntimeAssetsConfig{
			MaxSessionAssetBytes:       11,
			MaxSessionAssetsTotalBytes: 22,
		},
	}

	cfg := RuntimeConfig{MaxNoProgressStreak: 0, MaxRepeatCycleStreak: 0, MaxTurns: 0}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 3 {
		t.Fatalf("expected defaulted MaxNoProgressStreak=3, got %d", cfg.MaxNoProgressStreak)
	}
	if cfg.MaxRepeatCycleStreak != 5 {
		t.Fatalf("expected defaulted MaxRepeatCycleStreak=5, got %d", cfg.MaxRepeatCycleStreak)
	}
	if cfg.MaxTurns != 7 {
		t.Fatalf("expected defaulted MaxTurns=7, got %d", cfg.MaxTurns)
	}
	if cfg.Assets.MaxSessionAssetBytes != 11 {
		t.Fatalf("expected defaulted MaxSessionAssetBytes=11, got %d", cfg.Assets.MaxSessionAssetBytes)
	}
	if cfg.Assets.MaxSessionAssetsTotalBytes != 22 {
		t.Fatalf("expected defaulted MaxSessionAssetsTotalBytes=22, got %d", cfg.Assets.MaxSessionAssetsTotalBytes)
	}

	cfg = RuntimeConfig{MaxNoProgressStreak: 5, MaxRepeatCycleStreak: 8, MaxTurns: 9}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 5 {
		t.Fatalf("expected existing MaxNoProgressStreak=5 to be preserved, got %d", cfg.MaxNoProgressStreak)
	}
	if cfg.MaxRepeatCycleStreak != 8 {
		t.Fatalf("expected existing MaxRepeatCycleStreak=8 to be preserved, got %d", cfg.MaxRepeatCycleStreak)
	}
	if cfg.MaxTurns != 9 {
		t.Fatalf("expected existing MaxTurns=9 to be preserved, got %d", cfg.MaxTurns)
	}

	cfg = RuntimeConfig{MaxNoProgressStreak: 2, MaxRepeatCycleStreak: -1, MaxTurns: -1}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxRepeatCycleStreak != 5 {
		t.Fatalf("expected negative MaxRepeatCycleStreak=-1 to be replaced by default=5, got %d", cfg.MaxRepeatCycleStreak)
	}
	if cfg.MaxTurns != 7 {
		t.Fatalf("expected negative MaxTurns=-1 to be replaced by default=7, got %d", cfg.MaxTurns)
	}

	var nilCfg *RuntimeConfig
	nilCfg.ApplyDefaults(defaults)
}

func TestRuntimeConfigValidate(t *testing.T) {
	t.Parallel()

	if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: 1, MaxTurns: 1}).Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	for _, bad := range []int{0, -1, -99} {
		if err := (RuntimeConfig{MaxNoProgressStreak: bad, MaxRepeatCycleStreak: 1, MaxTurns: 1}).Validate(); err == nil {
			t.Fatalf("expected validation error for MaxNoProgressStreak=%d", bad)
		}
	}

	for _, bad := range []int{0, -1, -99} {
		if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: bad, MaxTurns: 1}).Validate(); err == nil {
			t.Fatalf("expected validation error for MaxRepeatCycleStreak=%d", bad)
		}
	}
	for _, bad := range []int{-1, -99} {
		if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: 1, MaxTurns: bad}).Validate(); err == nil {
			t.Fatalf("expected validation error for MaxTurns=%d", bad)
		}
	}
	if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: 1, MaxTurns: 0}).Validate(); err != nil {
		t.Fatalf("expected MaxTurns=0 to be valid (use default), got %v", err)
	}

	if err := (RuntimeConfig{
		MaxNoProgressStreak:  1,
		MaxRepeatCycleStreak: 1,
		MaxTurns:             1,
		Assets: RuntimeAssetsConfig{
			MaxSessionAssetBytes:       1,
			MaxSessionAssetsTotalBytes: 1,
		},
	}).Validate(); err != nil {
		t.Fatalf("expected valid assets config, got %v", err)
	}
	if err := (RuntimeConfig{
		MaxNoProgressStreak:  1,
		MaxRepeatCycleStreak: 1,
		MaxTurns:             1,
		Assets: RuntimeAssetsConfig{
			MaxSessionAssetBytes:       -1,
			MaxSessionAssetsTotalBytes: 1,
		},
	}).Validate(); err == nil {
		t.Fatal("expected validation error for assets.max_session_asset_bytes=-1")
	}
	if err := (RuntimeConfig{
		MaxNoProgressStreak:  1,
		MaxRepeatCycleStreak: 1,
		MaxTurns:             1,
		Assets: RuntimeAssetsConfig{
			MaxSessionAssetBytes:       1,
			MaxSessionAssetsTotalBytes: -1,
		},
	}).Validate(); err == nil {
		t.Fatal("expected validation error for assets.max_session_assets_total_bytes=-1")
	}

	if err := (RuntimeConfig{
		MaxNoProgressStreak:  1,
		MaxRepeatCycleStreak: 1,
		MaxTurns:             1,
		Verification: VerificationConfig{
			DefaultTaskPolicy: "unknown",
			MaxNoProgress:     1,
			MaxRetries:        0,
			Verifiers: map[string]VerifierConfig{
				"todo_convergence": {FailOpen: true, FailClosed: true},
			},
			ExecutionPolicy: VerificationExecutionPolicyConfig{
				Mode:             "non_interactive",
				DefaultTimeout:   1,
				DefaultOutputCap: 1,
			},
		},
	}).Validate(); err == nil {
		t.Fatal("expected validation error for invalid verification config")
	}
}

func TestRuntimeAssetsConfigZeroValuesResolveToDefaults(t *testing.T) {
	t.Parallel()

	cfg := RuntimeAssetsConfig{
		MaxSessionAssetBytes:       0,
		MaxSessionAssetsTotalBytes: 0,
	}
	resolvedPolicy := cfg.ResolveSessionAssetPolicy()
	resolvedBudget := cfg.ResolveRequestAssetBudget()
	defaults := defaultRuntimeAssetsConfig()
	if resolvedPolicy.MaxSessionAssetBytes != defaults.MaxSessionAssetBytes {
		t.Fatalf(
			"expected MaxSessionAssetBytes to fallback to default=%d, got %d",
			defaults.MaxSessionAssetBytes,
			resolvedPolicy.MaxSessionAssetBytes,
		)
	}
	if resolvedBudget.MaxSessionAssetsTotalBytes != defaults.MaxSessionAssetsTotalBytes {
		t.Fatalf(
			"expected MaxSessionAssetsTotalBytes to fallback to default=%d, got %d",
			defaults.MaxSessionAssetsTotalBytes,
			resolvedBudget.MaxSessionAssetsTotalBytes,
		)
	}
}

func TestRuntimeConfigVerificationDefaultsApplied(t *testing.T) {
	t.Parallel()

	defaults := defaultRuntimeConfig()
	cfg := RuntimeConfig{}
	cfg.ApplyDefaults(defaults)
	if !cfg.Verification.EnabledValue() {
		t.Fatalf("expected verification enabled by default")
	}
	if !cfg.Verification.FinalInterceptValue() {
		t.Fatalf("expected verification final intercept enabled by default")
	}
	if cfg.Verification.MaxNoProgress <= 0 {
		t.Fatalf("expected max_no_progress > 0, got %d", cfg.Verification.MaxNoProgress)
	}
	if len(cfg.Verification.Verifiers) == 0 {
		t.Fatal("expected default verifiers to be populated")
	}
}

func TestRuntimeConfigVerificationExplicitFalsePreserved(t *testing.T) {
	t.Parallel()

	defaults := defaultRuntimeConfig()
	cfg := RuntimeConfig{
		Verification: VerificationConfig{
			Enabled:        boolPtrTest(false),
			FinalIntercept: boolPtrTest(false),
		},
	}
	cfg.ApplyDefaults(defaults)
	if cfg.Verification.EnabledValue() {
		t.Fatalf("expected explicit verification.enabled=false to be preserved")
	}
	if cfg.Verification.FinalInterceptValue() {
		t.Fatalf("expected explicit verification.final_intercept=false to be preserved")
	}
}

func boolPtrTest(value bool) *bool {
	v := value
	return &v
}
