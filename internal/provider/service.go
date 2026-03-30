package provider

import (
	"context"
	"errors"
	"strings"

	"neo-code/internal/config"
)

type Service struct {
	manager  *config.Manager
	registry *Registry
}

var (
	errServiceManagerNil  = errors.New("provider: config manager is nil")
	errServiceRegistryNil = errors.New("provider: registry is nil")
)

func NewService(manager *config.Manager, registry *Registry) *Service {
	return &Service{
		manager:  manager,
		registry: registry,
	}
}

func (s *Service) ListProviders(ctx context.Context) ([]ProviderCatalogItem, error) {
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
		if !s.registry.Supports(providerCfg.Driver) {
			continue
		}
		items = append(items, catalogItemFromConfig(providerCfg))
	}

	return items, nil
}

func (s *Service) SelectProvider(ctx context.Context, providerName string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	var selection ProviderSelection

	err := s.manager.Update(ctx, func(cfg *config.Config) error {
		providerCfg, err := cfg.ProviderByName(providerName)
		if err != nil {
			return ErrProviderNotFound
		}
		if !s.registry.Supports(providerCfg.Driver) {
			return ErrDriverNotFound
		}

		cfg.SelectedProvider = providerCfg.Name
		cfg.CurrentModel = selectModel(cfg.CurrentModel, providerCfg.SupportedModels(), providerCfg.Model)
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return ProviderSelection{}, err
	}

	return selection, nil
}

func (s *Service) ListModels(ctx context.Context) ([]ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := s.manager.Get()
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		return nil, err
	}

	return modelDescriptors(selected.SupportedModels()), nil
}

func (s *Service) SetCurrentModel(ctx context.Context, modelID string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ProviderSelection{}, ErrModelNotFound
	}

	var selection ProviderSelection
	err := s.manager.Update(ctx, func(cfg *config.Config) error {
		selected, err := cfg.SelectedProviderConfig()
		if err != nil {
			return err
		}

		if !containsModel(selected.SupportedModels(), modelID) {
			return ErrModelNotFound
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

func (s *Service) SetAPIKeyEnvOverride(ctx context.Context, envName string) error {
	if err := s.validate(); err != nil {
		return err
	}

	return s.manager.Update(ctx, func(cfg *config.Config) error {
		cfg.APIKeyEnvOverride = envName
		return nil
	})
}

func containsModel(models []string, modelID string) bool {
	target := normalizeKey(modelID)
	for _, model := range models {
		if normalizeKey(model) == target {
			return true
		}
	}
	return false
}

func selectionFromConfig(cfg config.Config) ProviderSelection {
	return ProviderSelection{
		ProviderID: cfg.SelectedProvider,
		ModelID:    cfg.CurrentModel,
	}
}

func selectModel(currentModel string, models []string, fallback string) string {
	if containsModel(models, currentModel) {
		return strings.TrimSpace(currentModel)
	}
	return strings.TrimSpace(fallback)
}

func catalogItemFromConfig(cfg config.ProviderConfig) ProviderCatalogItem {
	return ProviderCatalogItem{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: modelDescriptors(cfg.SupportedModels()),
	}
}

func modelDescriptors(models []string) []ModelDescriptor {
	if len(models) == 0 {
		return nil
	}

	descriptors := make([]ModelDescriptor, 0, len(models))
	for _, modelID := range models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		descriptors = append(descriptors, ModelDescriptor{
			ID:   modelID,
			Name: modelID,
		})
	}
	return descriptors
}

func (s *Service) validate() error {
	if s == nil || s.manager == nil {
		return errServiceManagerNil
	}
	if s.registry == nil {
		return errServiceRegistryNil
	}
	return nil
}
