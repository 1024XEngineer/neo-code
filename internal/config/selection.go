package config

import (
	"context"
	"errors"
	"fmt"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// Selection 领域错误。
var (
	ErrProviderNotFound  = errors.New("provider not found")
	ErrModelNotFound     = errors.New("model not found")
	ErrNoModelsAvailable = errors.New("provider has no available models")
	ErrDriverUnsupported = errors.New("provider driver not supported by current runtime")
)

// ProviderCatalogItem 表示一个已配置的 provider 及其可用模型列表，用于 UI 展示。
type ProviderCatalogItem struct {
	ID     string                          `json:"id"`
	Name   string                          `json:"name"`
	Models []providertypes.ModelDescriptor `json:"models,omitempty"`
}

// ProviderSelection 表示当前选中的 provider 和 model。
type ProviderSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

// DriverSupporter 用于检查给定 driver 是否被当前运行时支持。
type DriverSupporter interface {
	Supports(driverType string) bool
}

// ModelCatalog 定义模型目录查询接口，用于获取 provider 的可用模型列表。
type ModelCatalog interface {
	ListProviderModels(ctx context.Context, providerCfg ProviderConfig) ([]providertypes.ModelDescriptor, error)
	ListProviderModelsSnapshot(ctx context.Context, providerCfg ProviderConfig) ([]providertypes.ModelDescriptor, error)
	ListProviderModelsCached(ctx context.Context, providerCfg ProviderConfig) ([]providertypes.ModelDescriptor, error)
}

// SelectionService 管理 provider 和模型选择状态，并通过 ConfigManager 持久化变更。
type SelectionService struct {
	manager    *Manager
	supporters DriverSupporter
	catalogs   ModelCatalog
}

// NewSelectionService 创建选择服务实例。
func NewSelectionService(manager *Manager, supporters DriverSupporter, catalogs ModelCatalog) *SelectionService {
	return &SelectionService{
		manager:    manager,
		supporters: supporters,
		catalogs:   catalogs,
	}
}

// ListProviders 枚举所有已配置且当前运行时支持的 provider 及其缓存模型列表。
func (s *SelectionService) ListProviders(ctx context.Context) ([]ProviderCatalogItem, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := s.manager.Get()
	items := make([]ProviderCatalogItem, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !s.supporters.Supports(providerCfg.Driver) {
			continue
		}

		models, err := s.catalogs.ListProviderModelsCached(ctx, providerCfg)
		if err != nil {
			return nil, err
		}
		items = append(items, providerCatalogItem(providerCfg, models))
	}

	return items, nil
}

// SelectProvider 切换当前 provider，并将 current_model 修正为该 provider 下的有效模型。
func (s *SelectionService) SelectProvider(ctx context.Context, providerName string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	providerCfg, err := cfgSnapshot.ProviderByName(providerName)
	if err != nil {
		return ProviderSelection{}, ErrProviderNotFound
	}
	if !s.supporters.Supports(providerCfg.Driver) {
		return ProviderSelection{}, ErrDriverUnsupported
	}

	models, err := s.catalogs.ListProviderModels(ctx, providerCfg)
	if err != nil {
		return ProviderSelection{}, err
	}
	if len(models) == 0 {
		return ProviderSelection{}, ErrNoModelsAvailable
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *Config) error {
		selected, err := cfg.ProviderByName(providerName)
		if err != nil {
			return ErrProviderNotFound
		}
		if !s.supporters.Supports(selected.Driver) {
			return ErrDriverUnsupported
		}

		cfg.SelectedProvider = selected.Name
		nextModel, _ := resolveCurrentModel(cfg.CurrentModel, models, selected.Model)
		cfg.CurrentModel = nextModel
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}

	return selection, nil
}

// ListModels 获取当前选中 provider 的模型列表，必要时会同步触发远程发现。
func (s *SelectionService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selected, err := s.selectedProviderConfig()
	if err != nil {
		return nil, err
	}
	return s.catalogs.ListProviderModels(ctx, selected)
}

// ListModelsSnapshot 获取当前选中 provider 的快照模型列表，不阻塞等待同步发现。
func (s *SelectionService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selected, err := s.selectedProviderConfig()
	if err != nil {
		return nil, err
	}
	return s.catalogs.ListProviderModelsSnapshot(ctx, selected)
}

// SetCurrentModel 切换当前模型，目标模型必须出现在当前 provider 的可用模型列表中。
func (s *SelectionService) SetCurrentModel(ctx context.Context, modelID string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ProviderSelection{}, ErrModelNotFound
	}

	selected, err := s.selectedProviderConfig()
	if err != nil {
		return ProviderSelection{}, err
	}

	models, err := s.catalogs.ListProviderModels(ctx, selected)
	if err != nil {
		return ProviderSelection{}, err
	}
	if len(models) == 0 {
		return ProviderSelection{}, ErrNoModelsAvailable
	}
	if !containsModelDescriptorID(models, modelID) {
		fallbackID := strings.TrimSpace(selected.Model)
		if fallbackID != "" && containsModelDescriptorID(models, fallbackID) {
			selection, fallbackErr := s.fallbackToProviderDefaultModel(ctx, selected, fallbackID)
			if fallbackErr != nil {
				return ProviderSelection{}, fallbackErr
			}
			return selection, fmt.Errorf(
				"selection: model %q not found; fell back to default model %q: %w",
				modelID,
				fallbackID,
				ErrModelNotFound,
			)
		}
		return ProviderSelection{}, ErrModelNotFound
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *Config) error {
		if _, err := cfg.SelectedProviderConfig(); err != nil {
			return err
		}
		cfg.CurrentModel = modelID
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}

	return selection, nil
}

// fallbackToProviderDefaultModel 在切换模型失败时回退到当前 provider 的默认模型，并持久化修正后的选择状态。
func (s *SelectionService) fallbackToProviderDefaultModel(ctx context.Context, selected ProviderConfig, modelID string) (ProviderSelection, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ProviderSelection{}, ErrModelNotFound
	}

	var selection ProviderSelection
	err := s.manager.Update(ctx, func(cfg *Config) error {
		currentSelected, err := cfg.SelectedProviderConfig()
		if err != nil {
			return err
		}
		if NormalizeProviderName(currentSelected.Name) != NormalizeProviderName(selected.Name) {
			return ErrProviderNotFound
		}
		cfg.CurrentModel = modelID
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}
	return selection, nil
}

// EnsureSelection 确保当前 provider 和 model 仍然有效，必要时自动修正。
func (s *SelectionService) EnsureSelection(ctx context.Context) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	selected, selectionChanged, err := s.resolveSupportedProvider(cfgSnapshot)
	if err != nil {
		return ProviderSelection{}, err
	}

	models, err := s.catalogs.ListProviderModels(ctx, selected)
	if err != nil {
		return ProviderSelection{}, err
	}
	if len(models) == 0 {
		return ProviderSelection{}, ErrNoModelsAvailable
	}
	_, modelChanged := resolveCurrentModel(cfgSnapshot.CurrentModel, models, selected.Model)
	if !selectionChanged && !modelChanged {
		return selectionFromConfig(cfgSnapshot), nil
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *Config) error {
		nextSelected := selected
		if selectionChanged {
			cfg.SelectedProvider = selected.Name
		} else {
			resolved, err := cfg.SelectedProviderConfig()
			if err != nil {
				return err
			}
			nextSelected = resolved
		}
		cfg.CurrentModel, _ = resolveCurrentModel(cfg.CurrentModel, models, nextSelected.Model)
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}

	return selection, nil
}

// resolveSupportedProvider 返回当前可用的 provider；若已选 provider 不受支持，则回退到首个受支持项。
func (s *SelectionService) resolveSupportedProvider(cfg Config) (ProviderConfig, bool, error) {
	selected, err := cfg.SelectedProviderConfig()
	if err == nil && s.supporters.Supports(selected.Driver) {
		return selected, false, nil
	}

	for _, providerCfg := range cfg.Providers {
		if s.supporters.Supports(providerCfg.Driver) {
			return providerCfg, true, nil
		}
	}

	if err != nil {
		return ProviderConfig{}, false, err
	}
	return ProviderConfig{}, false, ErrDriverUnsupported
}

func (s *SelectionService) validate() error {
	if s == nil {
		return errors.New("selection: service is nil")
	}
	if s.manager == nil {
		return errors.New("selection: config manager is nil")
	}
	if s.supporters == nil {
		return errors.New("selection: driver supporter is nil")
	}
	if s.catalogs == nil {
		return errors.New("selection: catalog service is nil")
	}
	return nil
}

func (s *SelectionService) selectedProviderConfig() (ProviderConfig, error) {
	cfg := s.manager.Get()
	return cfg.SelectedProviderConfig()
}

func selectionFromConfig(cfg Config) ProviderSelection {
	return ProviderSelection{
		ProviderID: cfg.SelectedProvider,
		ModelID:    cfg.CurrentModel,
	}
}

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

func providerCatalogItem(cfg ProviderConfig, models []providertypes.ModelDescriptor) ProviderCatalogItem {
	return ProviderCatalogItem{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: providertypes.MergeModelDescriptors(models),
	}
}

func containsModelDescriptorID(models []providertypes.ModelDescriptor, modelID string) bool {
	target := NormalizeKey(modelID)
	if target == "" {
		return false
	}

	for _, model := range models {
		if NormalizeKey(model.ID) == target {
			return true
		}
	}
	return false
}
