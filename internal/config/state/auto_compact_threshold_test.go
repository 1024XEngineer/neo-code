package state

import (
	"context"
	"errors"
	"testing"

	configpkg "neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
)

func assertAutoCompactResolution(t *testing.T, got AutoCompactThresholdResolution, wantThreshold int, wantSource AutoCompactThresholdSource) {
	t.Helper()

	if got.Threshold != wantThreshold || got.Source != wantSource {
		t.Fatalf("expected threshold=%d source=%s, got %+v", wantThreshold, wantSource, got)
	}
}

func TestResolveAutoCompactThresholdDisabled(t *testing.T) {
	t.Parallel()

	cfg := configpkg.StaticDefaults().Clone()
	cfg.Context.AutoCompact.Enabled = false

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 0, AutoCompactThresholdSourceDisabled)
}

func TestResolveAutoCompactThresholdExplicitWins(t *testing.T) {
	t.Parallel()

	cfg := configpkg.StaticDefaults().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 42000

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 42000, AutoCompactThresholdSourceExplicit)
}

func TestResolveAutoCompactThresholdDerivedFromContextWindow(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.ReserveTokens = 13000
	cfg.CurrentModel = "deepseek-coder"
	cfg.Providers[0].Model = "deepseek-coder"
	cfg.Providers[0].Models = []providertypes.ModelDescriptor{{
		ID:            "deepseek-coder",
		ContextWindow: 131072,
	}}

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{
		snapshotModels: cfg.Providers[0].Models,
	})
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 118072, AutoCompactThresholdSourceDerived)
}

func TestResolveAutoCompactThresholdFallsBackWhenWindowTooSmall(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.ReserveTokens = 13000
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000
	cfg.CurrentModel = "small-model"
	cfg.Providers[0].Model = "small-model"

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{
		snapshotModels: []providertypes.ModelDescriptor{{
			ID:            "small-model",
			ContextWindow: 8000,
		}},
	})
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 88000, AutoCompactThresholdSourceFallback)
}

func TestResolveAutoCompactThresholdFallsBackWhenModelMissing(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000
	cfg.CurrentModel = "missing-model"

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{
		snapshotModels: []providertypes.ModelDescriptor{{ID: "other-model", ContextWindow: 131072}},
	})
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 88000, AutoCompactThresholdSourceFallback)
}

func TestResolveAutoCompactThresholdFallsBackWhenSelectedProviderInvalid(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000
	cfg.SelectedProvider = "missing-provider"

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{})
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 88000, AutoCompactThresholdSourceFallback)
}

func TestResolveAutoCompactThresholdFallsBackWhenCatalogInputResolutionFails(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000
	cfg.Providers[0].BaseURL = ""

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{})
	if err != nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = %v", err)
	}
	assertAutoCompactResolution(t, resolution, 88000, AutoCompactThresholdSourceFallback)
}

func TestResolveAutoCompactThresholdFallsBackWhenSnapshotLookupFails(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000

	resolution, err := ResolveAutoCompactThreshold(context.Background(), cfg, catalogMethodsStub{
		snapshotErr: errors.New("snapshot failed"),
	})
	if err == nil {
		t.Fatalf("ResolveAutoCompactThreshold() error = nil, want non-nil")
	}
	assertAutoCompactResolution(t, resolution, 88000, AutoCompactThresholdSourceFallback)
}
