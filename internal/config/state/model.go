package state

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// Selection 记录当前激活的 provider 和 model。
type Selection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

// ProviderOption 表示当前运行时可供选择的 provider 及其模型候选。
type ProviderOption struct {
	ID     string                          `json:"id"`
	Name   string                          `json:"name"`
	Models []providertypes.ModelDescriptor `json:"models,omitempty"`
}

// 选择状态领域错误。
var (
	ErrProviderNotFound  = errors.New("provider not found")
	ErrModelNotFound     = errors.New("model not found")
	ErrNoModelsAvailable = errors.New("provider has no available models")
	ErrDriverUnsupported = errors.New("provider driver not supported by current runtime")
	errSelectionDrifted  = errors.New("selection drifted during update")
)

// DriverSupporter 用于检查给定 driver 是否受当前运行时支持。
type DriverSupporter interface {
	Supports(driverType string) bool
}

// ModelCatalog 定义模型目录查询接口，用于获取 provider 的可用模型列表。
type ModelCatalog interface {
	ListProviderModels(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error)
	ListProviderModelsSnapshot(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error)
	ListProviderModelsCached(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error)
}

// selectionFromConfig 将配置快照映射为当前选择结果。
func selectionFromConfig(cfg config.Config) Selection {
	return Selection{
		ProviderID: cfg.SelectedProvider,
		ModelID:    cfg.CurrentModel,
	}
}

// resolveCurrentModel 依据候选模型列表修正当前模型，并返回是否发生变更。
func resolveCurrentModel(currentModel string, models []providertypes.ModelDescriptor, fallback string) (string, bool) {
	currentModel = strings.TrimSpace(currentModel)
	if containsModelDescriptorID(models, currentModel) {
		return currentModel, false
	}

	fallback = strings.TrimSpace(fallback)
	if fallback != "" && containsModelDescriptorID(models, fallback) {
		return fallback, currentModel != fallback
	}

	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id != "" {
			return id, currentModel != id
		}
	}

	return currentModel, false
}

// providerOption 将 provider 配置与模型列表整理为选择服务返回的候选项。
func providerOption(cfg config.ProviderConfig, models []providertypes.ModelDescriptor) ProviderOption {
	return ProviderOption{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: providertypes.MergeModelDescriptors(models),
	}
}

// catalogInputFromProvider 将配置层 provider 定义转换为 catalog 查询输入。
func catalogInputFromProvider(cfg config.ProviderConfig) (provider.CatalogInput, error) {
	cloned := cfg
	cloned.Models = providertypes.CloneModelDescriptors(cfg.Models)

	identity, err := cfg.Identity()
	if err != nil {
		return provider.CatalogInput{}, err
	}

	input := provider.CatalogInput{
		Identity:         identity,
		ConfiguredModels: providertypes.CloneModelDescriptors(cloned.Models),
		ResolveDiscoveryConfig: func() (provider.RuntimeConfig, error) {
			resolved, err := cloned.Resolve()
			if err != nil {
				return provider.RuntimeConfig{}, err
			}
			return resolved.ToRuntimeConfig(), nil
		},
	}
	if cloned.Source != config.ProviderSourceCustom {
		input.DefaultModels = providertypes.DescriptorsFromIDs([]string{cloned.Model})
	}
	return input, nil
}

// containsModelDescriptorID 判断模型列表中是否包含目标 ID。
func containsModelDescriptorID(models []providertypes.ModelDescriptor, modelID string) bool {
	target := provider.NormalizeKey(modelID)
	if target == "" {
		return false
	}

	for _, model := range models {
		if provider.NormalizeKey(model.ID) == target {
			return true
		}
	}
	return false
}

// selectedProviderConfig 解析当前配置中记录的选中 provider。
func selectedProviderConfig(cfg config.Config) (config.ProviderConfig, error) {
	name := strings.TrimSpace(cfg.SelectedProvider)
	if name == "" {
		return config.ProviderConfig{}, ErrProviderNotFound
	}
	providerCfg, err := cfg.ProviderByName(name)
	if err != nil {
		return config.ProviderConfig{}, ErrProviderNotFound
	}
	return providerCfg, nil
}

// ensureSupportedProvider 统一校验当前运行时是否支持指定 provider。
func ensureSupportedProvider(supporters DriverSupporter, cfg config.ProviderConfig) error {
	if supporters.Supports(cfg.Driver) {
		return nil
	}
	return fmt.Errorf(
		"selection: provider %q driver %q: %w",
		cfg.Name,
		cfg.Driver,
		ErrDriverUnsupported,
	)
}

// sameProviderIdentity 比较两个 provider 配置是否仍然指向同一个底层连接身份。
func sameProviderIdentity(left config.ProviderConfig, right config.ProviderConfig) (bool, error) {
	leftIdentity, err := left.Identity()
	if err != nil {
		return false, err
	}
	rightIdentity, err := right.Identity()
	if err != nil {
		return false, err
	}
	return leftIdentity == rightIdentity, nil
}

// sameSelectionSnapshot 判断最新配置是否仍与旧快照指向同一 provider 且保持相同的当前模型。
func sameSelectionSnapshot(latest config.Config, snapshot config.Config, expected config.ProviderConfig) (bool, error) {
	latestSelected, err := selectedProviderConfig(latest)
	if err != nil {
		return false, nil
	}
	sameIdentity, err := sameProviderIdentity(latestSelected, expected)
	if err != nil {
		return false, err
	}
	if !sameIdentity {
		return false, nil
	}
	return strings.TrimSpace(latest.CurrentModel) == strings.TrimSpace(snapshot.CurrentModel), nil
}
