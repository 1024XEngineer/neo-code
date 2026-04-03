package selection

import (
	"context"
	"errors"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/catalog"
)

type modelCatalog interface {
	ListProviderModels(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error)
	ListProviderModelsSnapshot(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error)
	ListProviderModelsCached(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error)
}

type Service struct {
	manager  *config.Manager
	registry *provider.Registry
	catalogs modelCatalog
}

func NewService(manager *config.Manager, registry *provider.Registry, catalogs *catalog.Service) *Service {
	return &Service{
		manager:  manager,
		registry: registry,
		catalogs: catalogs,
	}
}

func (s *Service) ListProviders(ctx context.Context) ([]provider.ProviderCatalogItem, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := s.manager.Get()
	items := make([]provider.ProviderCatalogItem, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !s.registry.Supports(providerCfg.Driver) {
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

func (s *Service) SelectProvider(ctx context.Context, providerName string) (provider.ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return provider.ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	providerCfg, err := cfgSnapshot.ProviderByName(providerName)
	if err != nil {
		return provider.ProviderSelection{}, provider.ErrProviderNotFound
	}
	if !s.registry.Supports(providerCfg.Driver) {
		return provider.ProviderSelection{}, provider.ErrDriverNotFound
	}

	models, err := s.catalogs.ListProviderModelsCached(ctx, providerCfg)
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	var selection provider.ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		selected, err := cfg.ProviderByName(providerName)
		if err != nil {
			return provider.ErrProviderNotFound
		}
		if !s.registry.Supports(selected.Driver) {
			return provider.ErrDriverNotFound
		}

		cfg.SelectedProvider = selected.Name
		nextModel, _ := resolveCurrentModel(cfg.CurrentModel, models, selected.Model)
		cfg.CurrentModel = nextModel
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	return selection, nil
}

func (s *Service) ListModels(ctx context.Context) ([]provider.ModelDescriptor, error) {
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

func (s *Service) ListModelsSnapshot(ctx context.Context) ([]provider.ModelDescriptor, error) {
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

func (s *Service) SetCurrentModel(ctx context.Context, modelID string) (provider.ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return provider.ProviderSelection{}, err
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return provider.ProviderSelection{}, provider.ErrModelNotFound
	}

	selected, err := s.selectedProviderConfig()
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	models, err := s.catalogs.ListProviderModelsSnapshot(ctx, selected)
	if err != nil {
		return provider.ProviderSelection{}, err
	}
	if !containsModelDescriptorID(models, modelID) {
		return provider.ProviderSelection{}, provider.ErrModelNotFound
	}

	var selection provider.ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		if _, err := cfg.SelectedProviderConfig(); err != nil {
			return err
		}
		cfg.CurrentModel = modelID
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	return selection, nil
}

func (s *Service) EnsureSelection(ctx context.Context) (provider.ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return provider.ProviderSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return provider.ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	selected, err := cfgSnapshot.SelectedProviderConfig()
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	models, err := s.catalogs.ListProviderModelsSnapshot(ctx, selected)
	if err != nil {
		return provider.ProviderSelection{}, err
	}
	nextModel, changed := resolveCurrentModel(cfgSnapshot.CurrentModel, models, selected.Model)
	if !changed {
		return selectionFromConfig(cfgSnapshot), nil
	}

	var selection provider.ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		if _, err := cfg.SelectedProviderConfig(); err != nil {
			return err
		}
		cfg.CurrentModel = nextModel
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return provider.ProviderSelection{}, err
	}

	return selection, nil
}

func (s *Service) validate() error {
	if s == nil {
		return errors.New("provider selection: service is nil")
	}
	if s.manager == nil {
		return errors.New("provider selection: config manager is nil")
	}
	if s.registry == nil {
		return errors.New("provider selection: registry is nil")
	}
	if s.catalogs == nil {
		return errors.New("provider selection: catalog service is nil")
	}
	return nil
}

func (s *Service) selectedProviderConfig() (config.ProviderConfig, error) {
	cfg := s.manager.Get()
	return cfg.SelectedProviderConfig()
}

func selectionFromConfig(cfg config.Config) provider.ProviderSelection {
	return provider.ProviderSelection{
		ProviderID: cfg.SelectedProvider,
		ModelID:    cfg.CurrentModel,
	}
}

func resolveCurrentModel(currentModel string, models []provider.ModelDescriptor, fallback string) (string, bool) {
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

func providerCatalogItem(cfg config.ProviderConfig, models []provider.ModelDescriptor) provider.ProviderCatalogItem {
	return provider.ProviderCatalogItem{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: provider.MergeModelDescriptors(models),
	}
}

func containsModelDescriptorID(models []provider.ModelDescriptor, modelID string) bool {
	target := config.NormalizeKey(modelID)
	if target == "" {
		return false
	}

	for _, model := range models {
		if config.NormalizeKey(model.ID) == target {
			return true
		}
	}
	return false
}
