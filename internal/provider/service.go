package provider

import (
	"context"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
)

const (
	defaultModelCatalogTTL       = 24 * time.Hour
	defaultBackgroundRefreshTime = 30 * time.Second
)

type Service struct {
	manager           *config.Manager
	registry          *Registry
	catalogs          ModelCatalogStore
	catalogTTL        time.Duration
	backgroundTimeout time.Duration
	now               func() time.Time

	refreshMu    sync.Mutex
	inFlightByID map[string]struct{}
}

func NewService(manager *config.Manager, registry *Registry, catalogs ModelCatalogStore) *Service {
	if catalogs == nil && manager != nil {
		catalogs = NewJSONModelCatalogStore(manager.BaseDir())
	}

	return &Service{
		manager:           manager,
		registry:          registry,
		catalogs:          catalogs,
		catalogTTL:        defaultModelCatalogTTL,
		backgroundTimeout: defaultBackgroundRefreshTime,
		now:               time.Now,
		inFlightByID:      map[string]struct{}{},
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

		models := s.modelsForProvider(ctx, providerCfg, modelQueryOptions{})
		items = append(items, catalogItemFromConfig(providerCfg, models))
	}

	return items, nil
}

func (s *Service) SelectProvider(ctx context.Context, providerName string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	cfgSnapshot := s.manager.Get()
	providerCfg, err := cfgSnapshot.ProviderByName(providerName)
	if err != nil {
		return ProviderSelection{}, ErrProviderNotFound
	}
	if !s.registry.Supports(providerCfg.Driver) {
		return ProviderSelection{}, ErrDriverNotFound
	}

	models := s.modelsForProvider(ctx, providerCfg, modelQueryOptions{})

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		selected, err := cfg.ProviderByName(providerName)
		if err != nil {
			return ErrProviderNotFound
		}
		if !s.registry.Supports(selected.Driver) {
			return ErrDriverNotFound
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

	s.queueRefresh(providerCfg)
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

	return s.modelsForProvider(ctx, selected, modelQueryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	}), nil
}

func (s *Service) SetCurrentModel(ctx context.Context, modelID string) (ProviderSelection, error) {
	if err := s.validate(); err != nil {
		return ProviderSelection{}, err
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ProviderSelection{}, ErrModelNotFound
	}

	cfgSnapshot := s.manager.Get()
	selected, err := cfgSnapshot.SelectedProviderConfig()
	if err != nil {
		return ProviderSelection{}, err
	}

	models := s.modelsForProvider(ctx, selected, modelQueryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	})
	if !containsModelDescriptorID(models, modelID) {
		return ProviderSelection{}, ErrModelNotFound
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
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

func (s *Service) Build(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s.registry.Build(ctx, cfg)
}

func (s *Service) EnsureSelection(ctx context.Context) (ProviderSelection, error) {
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

	models := s.modelsForProvider(ctx, selected, modelQueryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	})
	nextModel, changed := resolveCurrentModel(cfgSnapshot.CurrentModel, models, selected.Model)
	if !changed {
		return selectionFromConfig(cfgSnapshot), nil
	}

	var selection ProviderSelection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
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

func (s *Service) validate() error {
	if s == nil || s.manager == nil {
		return ErrServiceManagerNil
	}
	if s.registry == nil {
		return ErrServiceRegistryNil
	}
	return nil
}

type modelQueryOptions struct {
	allowSyncRefresh bool
	queueRefresh     bool
}

func (s *Service) modelsForProvider(ctx context.Context, providerCfg config.ProviderConfig, options modelQueryOptions) []ModelDescriptor {
	defaultModels := modelDescriptorsFromIDs([]string{providerCfg.Model})

	cached, cachedOK := s.loadCatalogModels(ctx, providerCfg)
	if !cachedOK && options.allowSyncRefresh {
		discovered, ok := s.discoverAndPersist(ctx, providerCfg)
		if ok {
			cached = discovered
			cachedOK = true
		}
	}

	if cachedOK && options.queueRefresh && s.catalogExpired(ctx, providerCfg) {
		s.queueRefresh(providerCfg)
	}

	return MergeModelDescriptors(cached, defaultModels)
}

func (s *Service) catalogExpired(ctx context.Context, providerCfg config.ProviderConfig) bool {
	catalog, err := s.loadCatalog(ctx, providerCfg)
	if err != nil {
		return false
	}
	return catalog.Expired(s.now())
}

func (s *Service) loadCatalogModels(ctx context.Context, providerCfg config.ProviderConfig) ([]ModelDescriptor, bool) {
	catalog, err := s.loadCatalog(ctx, providerCfg)
	if err != nil {
		return nil, false
	}
	return catalog.Models, true
}

func (s *Service) loadCatalog(ctx context.Context, providerCfg config.ProviderConfig) (ModelCatalog, error) {
	if s.catalogs == nil {
		return ModelCatalog{}, ErrModelCatalogNotFound
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return ModelCatalog{}, err
	}
	return s.catalogs.Load(ctx, identity)
}

func (s *Service) discoverAndPersist(ctx context.Context, providerCfg config.ProviderConfig) ([]ModelDescriptor, bool) {
	if s.registry == nil {
		return nil, false
	}
	if !s.registry.Supports(providerCfg.Driver) {
		return nil, false
	}

	resolved, err := providerCfg.Resolve()
	if err != nil {
		return nil, false
	}

	discovered, err := s.registry.DiscoverModels(ctx, resolved)
	if err != nil {
		return nil, false
	}

	discovered = MergeModelDescriptors(discovered)
	if s.catalogs == nil {
		return discovered, true
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return discovered, true
	}

	now := s.now()
	_ = s.catalogs.Save(ctx, ModelCatalog{
		SchemaVersion: modelCatalogSchemaVersion,
		Identity:      identity,
		FetchedAt:     now,
		ExpiresAt:     now.Add(s.catalogTTL),
		Models:        discovered,
	})
	return discovered, true
}

func (s *Service) queueRefresh(providerCfg config.ProviderConfig) {
	if s.catalogs == nil || s.registry == nil || !s.registry.Supports(providerCfg.Driver) {
		return
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return
	}

	key := identity.Key()
	s.refreshMu.Lock()
	if _, exists := s.inFlightByID[key]; exists {
		s.refreshMu.Unlock()
		return
	}
	s.inFlightByID[key] = struct{}{}
	s.refreshMu.Unlock()

	go func() {
		defer func() {
			s.refreshMu.Lock()
			delete(s.inFlightByID, key)
			s.refreshMu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), s.backgroundTimeout)
		defer cancel()
		_, _ = s.discoverAndPersist(ctx, providerCfg)
	}()
}

func selectionFromConfig(cfg config.Config) ProviderSelection {
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

func catalogItemFromConfig(cfg config.ProviderConfig, models []ModelDescriptor) ProviderCatalogItem {
	return ProviderCatalogItem{
		ID:     strings.TrimSpace(cfg.Name),
		Name:   strings.TrimSpace(cfg.Name),
		Models: MergeModelDescriptors(models),
	}
}

func containsModelDescriptorID(models []ModelDescriptor, modelID string) bool {
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
