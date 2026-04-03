package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

const (
	defaultTTL               = 24 * time.Hour
	defaultBackgroundTimeout = 30 * time.Second
)

type Service struct {
	registry          *provider.Registry
	store             Store
	catalogTTL        time.Duration
	backgroundTimeout time.Duration
	now               func() time.Time

	refreshMu    sync.Mutex
	inFlightByID map[string]struct{}
}

func NewService(baseDir string, registry *provider.Registry, store Store) *Service {
	if store == nil && strings.TrimSpace(baseDir) != "" {
		store = NewJSONStore(baseDir)
	}

	return &Service{
		registry:          registry,
		store:             store,
		catalogTTL:        defaultTTL,
		backgroundTimeout: defaultBackgroundTimeout,
		now:               time.Now,
		inFlightByID:      map[string]struct{}{},
	}
}

func (s *Service) ListProviderModels(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, providerCfg, queryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	}), nil
}

func (s *Service) ListProviderModelsSnapshot(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, providerCfg, queryOptions{
		queueRefresh: true,
	}), nil
}

func (s *Service) ListProviderModelsCached(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, providerCfg, queryOptions{}), nil
}

func (s *Service) validate() error {
	if s == nil {
		return errors.New("provider catalog: service is nil")
	}
	if s.registry == nil {
		return errors.New("provider catalog: registry is nil")
	}
	return nil
}

type queryOptions struct {
	allowSyncRefresh bool
	queueRefresh     bool
}

func (s *Service) modelsForProvider(ctx context.Context, providerCfg config.ProviderConfig, options queryOptions) []provider.ModelDescriptor {
	defaultModels := descriptorsFromIDs([]string{providerCfg.Model})

	cached, cachedOK := s.loadCatalogModels(ctx, providerCfg)
	if !cachedOK && options.allowSyncRefresh {
		discovered, ok := s.discoverAndPersist(ctx, providerCfg)
		if ok {
			cached = discovered
			cachedOK = true
		}
	}

	if options.queueRefresh && (!cachedOK || s.catalogExpired(ctx, providerCfg)) {
		s.queueRefresh(providerCfg)
	}

	return provider.MergeModelDescriptors(cached, defaultModels)
}

func (s *Service) catalogExpired(ctx context.Context, providerCfg config.ProviderConfig) bool {
	modelCatalog, err := s.loadCatalog(ctx, providerCfg)
	if err != nil {
		return false
	}
	return modelCatalog.Expired(s.now())
}

func (s *Service) loadCatalogModels(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, bool) {
	modelCatalog, err := s.loadCatalog(ctx, providerCfg)
	if err != nil {
		return nil, false
	}
	return modelCatalog.Models, true
}

func (s *Service) loadCatalog(ctx context.Context, providerCfg config.ProviderConfig) (ModelCatalog, error) {
	if s.store == nil {
		return ModelCatalog{}, ErrCatalogNotFound
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return ModelCatalog{}, err
	}
	return s.store.Load(ctx, identity)
}

func (s *Service) discoverAndPersist(ctx context.Context, providerCfg config.ProviderConfig) ([]provider.ModelDescriptor, bool) {
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

	discovered = provider.MergeModelDescriptors(discovered)
	if s.store == nil {
		return discovered, true
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return discovered, true
	}

	now := s.now()
	_ = s.store.Save(ctx, ModelCatalog{
		SchemaVersion: SchemaVersion,
		Identity:      identity,
		FetchedAt:     now,
		ExpiresAt:     now.Add(s.catalogTTL),
		Models:        discovered,
	})
	return discovered, true
}

func (s *Service) queueRefresh(providerCfg config.ProviderConfig) {
	if s.store == nil || !s.registry.Supports(providerCfg.Driver) {
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

func descriptorsFromIDs(modelIDs []string) []provider.ModelDescriptor {
	if len(modelIDs) == 0 {
		return nil
	}

	descriptors := make([]provider.ModelDescriptor, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		id := strings.TrimSpace(modelID)
		if id == "" {
			continue
		}
		descriptors = append(descriptors, provider.ModelDescriptor{
			ID:   id,
			Name: id,
		})
	}
	if len(descriptors) == 0 {
		return nil
	}
	return descriptors
}
