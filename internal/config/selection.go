package config

import (
	"context"
	"errors"
	"strings"
)

// Selection 领域错误。
var (
	ErrProviderNotFound  = errors.New("provider not found")
	ErrModelNotFound     = errors.New("model not found")
	ErrDriverUnsupported = errors.New("provider driver not supported by current runtime")
)

// ---- 数据类型定义 ----

// ModelDescriptor 表示模型元数据描述符。
type ModelDescriptor struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	ContextWindow   int             `json:"context_window,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Capabilities    map[string]bool `json:"capabilities,omitempty"`
}

// ProviderCatalogItem 表示一个已配置的 provider 及其可用模型列表（用于 UI 展示）。
type ProviderCatalogItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Models      []ModelDescriptor `json:"models,omitempty"`
}

// ProviderSelection 表示当前选中的 provider 和 model。
type ProviderSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

// ---- 接口定义（避免循环依赖）----

// DriverSupporter 检查给定驱动类型是否被支持。
type DriverSupporter interface {
	Supports(driverType string) bool
}

// ModelDiscovery 定义模型发现能力，由 provider.Registry 提供。
type ModelDiscovery interface {
	DiscoverModels(ctx context.Context, cfg ResolvedProviderConfig) ([]ModelDescriptor, error)
}

// ---- 模型描述符工具函数 ----

// DescriptorFromRawModel 将原始 provider 模型对象标准化为 ModelDescriptor。
func DescriptorFromRawModel(raw map[string]any) (ModelDescriptor, bool) {
	id := firstNonEmptyString(
		stringValue(raw["id"]),
		stringValue(raw["model"]),
		stringValue(raw["name"]),
	)
	if id == "" {
		return ModelDescriptor{}, false
	}

	descriptor := ModelDescriptor{
		ID:              id,
		Name:            firstNonEmptyString(stringValue(raw["name"]), stringValue(raw["display_name"]), id),
		Description:     stringValue(raw["description"]),
		ContextWindow:   firstPositiveInt(raw["context_window"], raw["contextLength"], raw["input_token_limit"], raw["max_context_tokens"]),
		MaxOutputTokens: firstPositiveInt(raw["max_output_tokens"], raw["output_token_limit"], raw["max_tokens"]),
		Capabilities:    boolMapValue(raw["capabilities"]),
	}
	return normalizeModelDescriptor(descriptor), true
}

// MergeModelDescriptors 合并多个 ModelDescriptor 来源，按 ID 去重，
// 优先保留较早来源的字段值，后续来源用于回填空字段。
func MergeModelDescriptors(sources ...[]ModelDescriptor) []ModelDescriptor {
	if len(sources) == 0 {
		return nil
	}

	merged := make([]ModelDescriptor, 0)
	indexByID := make(map[string]int)

	for _, source := range sources {
		for _, candidate := range source {
			normalized := normalizeModelDescriptor(candidate)
			key := NormalizeKey(normalized.ID)
			if key == "" {
				continue
			}

			if index, exists := indexByID[key]; exists {
				merged[index] = mergeModelDescriptor(merged[index], normalized)
				continue
			}

			indexByID[key] = len(merged)
			merged = append(merged, normalized)
		}
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}

// DescriptorsFromIDs 从模型 ID 字符串列表构建最小化的 ModelDescriptor 列表。
func DescriptorsFromIDs(modelIDs []string) []ModelDescriptor {
	if len(modelIDs) == 0 {
		return nil
	}

	descriptors := make([]ModelDescriptor, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		id := strings.TrimSpace(modelID)
		if id == "" {
			continue
		}
		descriptors = append(descriptors, ModelDescriptor{
			ID:   id,
			Name: id,
		})
	}
	if len(descriptors) == 0 {
		return nil
	}
	return descriptors
}

// ---- 选择服务 ----

// ModelCatalog 定义模型目录查询接口，用于获取 provider 可用模型列表。
type ModelCatalog interface {
	ListProviderModels(ctx context.Context, providerCfg ProviderConfig) ([]ModelDescriptor, error)
	ListProviderModelsSnapshot(ctx context.Context, providerCfg ProviderConfig) ([]ModelDescriptor, error)
	ListProviderModelsCached(ctx context.Context, providerCfg ProviderConfig) ([]ModelDescriptor, error)
}

// SelectionService 管理 provider/模型选择状态，负责切换当前 provider、切换当前模型、
// 校验和修复无效的 selection 等。所有变更通过 ConfigManager 持久化到配置中。
//
// 通过 DriverSupporter 和 ModelDiscovery 接口注入 provider 能力，避免 config -> provider 循环依赖。
type SelectionService struct {
	manager    *Manager
	supporters DriverSupporter
	discovery  ModelDiscovery
	catalogs   ModelCatalog
}

// NewSelectionService 创建选择服务实例。
func NewSelectionService(manager *Manager, supporters DriverSupporter, discovery ModelDiscovery, catalogs ModelCatalog) *SelectionService {
	return &SelectionService{
		manager:    manager,
		supporters: supporters,
		discovery:  discovery,
		catalogs:   catalogs,
	}
}

// ListProviders 枚举所有已配置且驱动支持的 provider 及其可用模型。
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

// SelectProvider 切换当前选中 provider，同时将 current_model 解析为有效值。
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

	models, err := s.catalogs.ListProviderModelsCached(ctx, providerCfg)
	if err != nil {
		return ProviderSelection{}, err
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

// ListModels 获取当前选中 provider 的模型列表（可触发远程刷新）。
func (s *SelectionService) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
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

// ListModelsSnapshot 获取当前选中 provider 的缓存模型列表。
func (s *SelectionService) ListModelsSnapshot(ctx context.Context) ([]ModelDescriptor, error) {
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

// SetCurrentModel 切换当前使用的模型（必须在当前 provider 的可用模型列表中）。
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

	models, err := s.catalogs.ListProviderModelsSnapshot(ctx, selected)
	if err != nil {
		return ProviderSelection{}, err
	}
	if !containsModelDescriptorID(models, modelID) {
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

// EnsureSelection 确保当前 selection 有效：如果 current_model 不在可用模型列表中则自动修正。
func (s *SelectionService) EnsureSelection(ctx context.Context) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	selected, err := cfgSnapshot.SelectedProviderConfig()
	if err != nil {
		return ProviderSelection{}, err
	}

	models, err := s.catalogs.ListProviderModelsSnapshot(ctx, selected)
	if err != nil {
		return ProviderSelection{}, err
	}
	nextModel, changed := resolveCurrentModel(cfgSnapshot.CurrentModel, models, selected.Model)
	if !changed {
		return selectionFromConfig(cfgSnapshot), nil
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *Config) error {
		if _, err := cfg.SelectedProviderConfig(); err != nil {
			return err
		}
		cfg.CurrentModel = nextModel
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}

	return selection, nil
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

// ---- 内部辅助函数 ----

func selectionFromConfig(cfg Config) ProviderSelection {
	return ProviderSelection{
		ProviderID: cfg.SelectedProvider,
		ModelID:    cfg.CurrentModel,
	}
}

func resolveCurrentModel(currentModel string, models []ModelDescriptor, fallback string) (string, bool) {
	currentModel = strings.TrimSpace(currentModel)
	if containsModelDescriptorID(models, currentModel) {
		return currentModel, false
	}

	fallback = strings.TrimSpace(fallback)
	if fallback != "" && containsModelDescriptorID(models, fallback) {
		return fallback, currentModel != fallback
	}

	return currentModel, false
}

func providerCatalogItem(cfg ProviderConfig, models []ModelDescriptor) ProviderCatalogItem {
	return ProviderCatalogItem{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: MergeModelDescriptors(models),
	}
}

func containsModelDescriptorID(models []ModelDescriptor, modelID string) bool {
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

func normalizeModelDescriptor(descriptor ModelDescriptor) ModelDescriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Name = strings.TrimSpace(descriptor.Name)
	descriptor.Description = strings.TrimSpace(descriptor.Description)
	if descriptor.Name == "" {
		descriptor.Name = descriptor.ID
	}
	descriptor.Capabilities = cloneStringBoolMap(descriptor.Capabilities)
	return descriptor
}

func mergeModelDescriptor(primary ModelDescriptor, secondary ModelDescriptor) ModelDescriptor {
	if strings.TrimSpace(primary.Name) == "" {
		primary.Name = secondary.Name
	}
	if strings.TrimSpace(primary.Description) == "" {
		primary.Description = secondary.Description
	}
	if primary.ContextWindow <= 0 {
		primary.ContextWindow = secondary.ContextWindow
	}
	if primary.MaxOutputTokens <= 0 {
		primary.MaxOutputTokens = secondary.MaxOutputTokens
	}
	primary.Capabilities = mergeStringBoolMaps(primary.Capabilities, secondary.Capabilities)
	return normalizeModelDescriptor(primary)
}

func mergeStringBoolMaps(primary map[string]bool, secondary map[string]bool) map[string]bool {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}

	merged := cloneStringBoolMap(primary)
	if merged == nil {
		merged = make(map[string]bool, len(secondary))
	}
	for key, value := range secondary {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
}

func cloneStringBoolMap(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// ---- 通用工具函数 ----

func boolMapValue(value any) map[string]bool {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}

	normalized := make(map[string]bool, len(raw))
	for key, value := range raw {
		boolValue, ok := value.(bool)
		if !ok {
			continue
		}
		normalized[strings.TrimSpace(key)] = boolValue
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			if typed > 0 {
				return typed
			}
		case int32:
			if typed > 0 {
				return int(typed)
			}
		case int64:
			if typed > 0 {
				return int(typed)
			}
		case float32:
			if typed > 0 {
				return int(typed)
			}
		case float64:
			if typed > 0 {
				return int(typed)
			}
		}
	}
	return 0
}
