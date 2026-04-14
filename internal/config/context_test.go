package config

import (
	"strings"
	"testing"
)

func TestContextConfigCloneIndependence(t *testing.T) {
	t.Parallel()

	original := ContextConfig{
		Compact: CompactConfig{
			ManualStrategy:           CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
			ReadTimeMaxMessageSpans:  24,
		},
		AutoCompact: AutoCompactConfig{
			Enabled:             true,
			InputTokenThreshold: 50000,
		},
	}
	cloned := original.Clone()

	cloned.Compact.ManualStrategy = CompactManualStrategyFullReplace
	cloned.Compact.ManualKeepRecentMessages = 5
	cloned.AutoCompact.Enabled = false
	cloned.AutoCompact.InputTokenThreshold = 100000

	if original.Compact.ManualStrategy == cloned.Compact.ManualStrategy {
		t.Fatal("expected Compact Clone to be independent")
	}
	if original.Compact.ManualKeepRecentMessages == cloned.Compact.ManualKeepRecentMessages {
		t.Fatal("expected ManualKeepRecentMessages Clone to be independent")
	}
	if original.AutoCompact.Enabled == cloned.AutoCompact.Enabled {
		t.Fatal("expected AutoCompact Enabled clone to be independent")
	}
	if original.AutoCompact.InputTokenThreshold == cloned.AutoCompact.InputTokenThreshold {
		t.Fatal("expected AutoCompact InputTokenThreshold clone to be independent")
	}
}

func TestCompactConfigCloneValueSemantics(t *testing.T) {
	t.Parallel()

	original := CompactConfig{
		ManualStrategy:           CompactManualStrategyFullReplace,
		ManualKeepRecentMessages: 5,
		MaxSummaryChars:          800,
		MicroCompactDisabled:     true,
		ReadTimeMaxMessageSpans:  24,
	}
	cloned := original.Clone()
	if original != cloned {
		t.Fatalf("expected equal configs, got %+v vs %+v", original, cloned)
	}
}

func TestAutoCompactConfigCloneValueSemantics(t *testing.T) {
	t.Parallel()

	original := AutoCompactConfig{Enabled: true, InputTokenThreshold: 75000}
	cloned := original.Clone()
	if original != cloned {
		t.Fatalf("expected equal configs, got %+v vs %+v", original, cloned)
	}
}

func TestContextConfigValidatePropagatesCompactError(t *testing.T) {
	t.Parallel()

	cfg := ContextConfig{
		Compact: CompactConfig{
			ManualKeepRecentMessages: 0,
			MaxSummaryChars:          0,
		},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "compact") {
		t.Fatalf("expected compact validation error, got %v", err)
	}
}

func TestContextConfigApplyDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var ctxCfg *ContextConfig
	ctxCfg.ApplyDefaults(ContextConfig{
		Compact:     CompactConfig{ManualStrategy: CompactManualStrategyFullReplace},
		AutoCompact: AutoCompactConfig{InputTokenThreshold: 50000},
	})
}

func TestCompactConfigApplyDefaultsAllZeroValues(t *testing.T) {
	t.Parallel()

	cfg := CompactConfig{}
	defaults := CompactConfig{
		ManualStrategy:           CompactManualStrategyFullReplace,
		ManualKeepRecentMessages: 15,
		MaxSummaryChars:          2000,
		ReadTimeMaxMessageSpans:  24,
	}
	cfg.ApplyDefaults(defaults)
	if cfg.ManualStrategy != CompactManualStrategyFullReplace {
		t.Fatalf("expected strategy %q, got %q", CompactManualStrategyFullReplace, cfg.ManualStrategy)
	}
	if cfg.ManualKeepRecentMessages != 15 {
		t.Fatalf("expected messages=15, got %d", cfg.ManualKeepRecentMessages)
	}
	if cfg.MaxSummaryChars != 2000 {
		t.Fatalf("expected chars=2000, got %d", cfg.MaxSummaryChars)
	}
	if cfg.ReadTimeMaxMessageSpans != 24 {
		t.Fatalf("expected read-time spans=24, got %d", cfg.ReadTimeMaxMessageSpans)
	}
}
