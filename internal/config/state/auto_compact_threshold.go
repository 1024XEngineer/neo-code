package state

import (
	"context"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

// AutoCompactThresholdSource 标识自动压缩阈值最终采用的来源。
type AutoCompactThresholdSource string

const (
	AutoCompactThresholdSourceDisabled AutoCompactThresholdSource = "disabled"
	AutoCompactThresholdSourceExplicit AutoCompactThresholdSource = "explicit"
	AutoCompactThresholdSourceDerived  AutoCompactThresholdSource = "derived"
	AutoCompactThresholdSourceFallback AutoCompactThresholdSource = "fallback"
)

// AutoCompactThresholdResolution 描述自动压缩阈值的解析结果，供 runtime 直接消费。
type AutoCompactThresholdResolution struct {
	Threshold     int
	Source        AutoCompactThresholdSource
	ContextWindow int
	ModelID       string
}

// fallbackAutoCompactThresholdResolution 构造自动推导失败时使用的保底阈值结果。
func fallbackAutoCompactThresholdResolution(cfg config.Config) AutoCompactThresholdResolution {
	return AutoCompactThresholdResolution{
		Threshold: cfg.Context.AutoCompact.FallbackInputTokenThreshold,
		Source:    AutoCompactThresholdSourceFallback,
		ModelID:   strings.TrimSpace(cfg.CurrentModel),
	}
}

// ResolveAutoCompactThreshold 基于当前选择的 provider/model 和模型目录快照解析最终阈值。
func ResolveAutoCompactThreshold(
	ctx context.Context,
	cfg config.Config,
	catalogs ModelCatalog,
) (AutoCompactThresholdResolution, error) {
	autoCompact := cfg.Context.AutoCompact
	if !autoCompact.Enabled {
		return AutoCompactThresholdResolution{Source: AutoCompactThresholdSourceDisabled}, nil
	}

	if autoCompact.InputTokenThreshold > 0 {
		return AutoCompactThresholdResolution{
			Threshold: autoCompact.InputTokenThreshold,
			Source:    AutoCompactThresholdSourceExplicit,
			ModelID:   strings.TrimSpace(cfg.CurrentModel),
		}, nil
	}

	resolution := fallbackAutoCompactThresholdResolution(cfg)
	providerCfg, err := selectedProviderConfig(cfg)
	if err != nil {
		return resolution, nil
	}
	if catalogs == nil {
		return resolution, nil
	}

	input, err := catalogInputFromProvider(providerCfg)
	if err != nil {
		return resolution, nil
	}

	models, err := catalogs.ListProviderModelsSnapshot(ctx, input)
	if err != nil {
		return resolution, err
	}

	modelID := provider.NormalizeKey(cfg.CurrentModel)
	for _, model := range models {
		if provider.NormalizeKey(model.ID) != modelID {
			continue
		}
		resolution.ContextWindow = model.ContextWindow
		if model.ContextWindow > autoCompact.ReserveTokens {
			resolution.Threshold = model.ContextWindow - autoCompact.ReserveTokens
			resolution.Source = AutoCompactThresholdSourceDerived
		}
		return resolution, nil
	}

	return resolution, nil
}
