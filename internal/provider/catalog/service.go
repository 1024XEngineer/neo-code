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

func (s *Service) ListProviderModels(ctx context.Context, providerCfg config.ProviderConfig) ([]config.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	})
}

func (s *Service) ListProviderModelsSnapshot(ctx context.Context, providerCfg config.ProviderConfig) ([]config.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{
		queueRefresh: true,
	})
}

func (s *Service) ListProviderModelsCached(ctx context.Context, providerCfg config.ProviderConfig) ([]config.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{})
}

func (s *Service) listProviderModels(
	ctx context.Context,
	providerCfg config.ProviderConfig,
	options queryOptions,
) ([]config.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, providerCfg, options), nil
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

type catalogSnapshot struct {
	models   []config.ModelDescriptor
	ok       bool
	expired  bool
	identity config.ProviderIdentity
}

func (s *Service) modelsForProvider(ctx context.Context, providerCfg config.ProviderConfig, options queryOptions) []config.ModelDescriptor {
	defaultModels := config.DescriptorsFromIDs([]string{providerCfg.Model})
	snapshot := s.catalogSnapshot(ctx, providerCfg)

	models := snapshot.models
	modelsOK := snapshot.ok
	if !modelsOK && options.allowSyncRefresh {
		discovered, ok := s.discoverAndPersist(ctx, providerCfg)
		if ok {
			models = discovered
			modelsOK = true
		}
	}

	if options.queueRefresh && (!modelsOK || snapshot.expired) {
		s.queueRefresh(providerCfg, snapshot.identity)
	}

	return config.MergeModelDescriptors(models, defaultModels)
}

func (s *Service) catalogSnapshot(ctx context.Context, providerCfg config.ProviderConfig) catalogSnapshot {
	identity, err := providerCfg.Identity()
	if err != nil {
		return catalogSnapshot{}
	}

	modelCatalog, err := s.loadCatalog(ctx, providerCfg)
	if err != nil {
		return catalogSnapshot{identity: identity}
	}
	return catalogSnapshot{
		models:   modelCatalog.Models,
		ok:       true,
		expired:  modelCatalog.Expired(s.now()),
		identity: identity,
	}
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

func (s *Service) discoverAndPersist(ctx context.Context, providerCfg config.ProviderConfig) ([]config.ModelDescriptor, bool) {
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

	discovered = config.MergeModelDescriptors(discovered)
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

func (s *Service) queueRefresh(providerCfg config.ProviderConfig, identity config.ProviderIdentity) {
	if s.store == nil || !s.registry.Supports(providerCfg.Driver) {
		return
	}
	if identity.Driver == "" || identity.BaseURL == "" {
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
