package config

import "testing"

func TestRuntimeConfigClone(t *testing.T) {
	t.Parallel()

	cfg := RuntimeConfig{MaxNoProgressStreak: 7, MaxRepeatCycleStreak: 4}
	cloned := cfg.Clone()
	if cloned.MaxNoProgressStreak != 7 {
		t.Fatalf("expected cloned MaxNoProgressStreak=7, got %d", cloned.MaxNoProgressStreak)
	}
	if cloned.MaxRepeatCycleStreak != 4 {
		t.Fatalf("expected cloned MaxRepeatCycleStreak=4, got %d", cloned.MaxRepeatCycleStreak)
	}
}

func TestRuntimeConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	defaults := RuntimeConfig{MaxNoProgressStreak: 3, MaxRepeatCycleStreak: 5}

	cfg := RuntimeConfig{MaxNoProgressStreak: 0, MaxRepeatCycleStreak: 0}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 3 {
		t.Fatalf("expected defaulted MaxNoProgressStreak=3, got %d", cfg.MaxNoProgressStreak)
	}
	if cfg.MaxRepeatCycleStreak != 5 {
		t.Fatalf("expected defaulted MaxRepeatCycleStreak=5, got %d", cfg.MaxRepeatCycleStreak)
	}

	cfg = RuntimeConfig{MaxNoProgressStreak: 5, MaxRepeatCycleStreak: 8}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxNoProgressStreak != 5 {
		t.Fatalf("expected existing MaxNoProgressStreak=5 to be preserved, got %d", cfg.MaxNoProgressStreak)
	}
	if cfg.MaxRepeatCycleStreak != 8 {
		t.Fatalf("expected existing MaxRepeatCycleStreak=8 to be preserved, got %d", cfg.MaxRepeatCycleStreak)
	}

	cfg = RuntimeConfig{MaxNoProgressStreak: 2, MaxRepeatCycleStreak: -1}
	cfg.ApplyDefaults(defaults)
	if cfg.MaxRepeatCycleStreak != 5 {
		t.Fatalf("expected negative MaxRepeatCycleStreak=-1 to be replaced by default=5, got %d", cfg.MaxRepeatCycleStreak)
	}

	var nilCfg *RuntimeConfig
	nilCfg.ApplyDefaults(defaults)
}

func TestRuntimeConfigValidate(t *testing.T) {
	t.Parallel()

	if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: 1}).Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}

	for _, bad := range []int{0, -1, -99} {
		if err := (RuntimeConfig{MaxNoProgressStreak: bad}).Validate(); err == nil {
			t.Fatalf("expected validation error for MaxNoProgressStreak=%d", bad)
		}
	}

	for _, bad := range []int{0, -1, -99} {
		if err := (RuntimeConfig{MaxNoProgressStreak: 1, MaxRepeatCycleStreak: bad}).Validate(); err == nil {
			t.Fatalf("expected validation error for MaxRepeatCycleStreak=%d", bad)
		}
	}
}
