package config

import "testing"

func TestRuntimeConfigClone(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{MaxNoProgressStreak: 7}
	cloned := cfg.Clone()
	if cloned.MaxNoProgressStreak != 7 {
		t.Fatalf("expected cloned MaxNoProgressStreak=7, got %d", cloned.MaxNoProgressStreak)
	}
}

func TestRuntimeConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	defaults := RuntimeConfig{MaxNoProgressStreak: 3}

	cfg := RuntimeConfig{MaxNoProgressStreak: 0}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 3 {
		t.Fatalf("expected defaulted MaxNoProgressStreak=3, got %d", cfg.MaxNoProgressStreak)
	}

	cfg = RuntimeConfig{MaxNoProgressStreak: 5}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 5 {
		t.Fatalf("expected existing MaxNoProgressStreak=5 to be preserved, got %d", cfg.MaxNoProgressStreak)
	}

	var nilCfg *RuntimeConfig
	nilCfg.ApplyDefaults(defaults)
}

func TestRuntimeConfigValidate(t *testing.T) {
	t.Parallel()

	if err := (RuntimeConfig{MaxNoProgressStreak: 1}).Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	if err := (RuntimeConfig{MaxNoProgressStreak: 0}).Validate(); err == nil {
		t.Fatal("expected validation error for zero MaxNoProgressStreak")
	}
}
