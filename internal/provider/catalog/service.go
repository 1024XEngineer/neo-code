package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
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
		store = newJSONStore(baseDir)
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

func (s *Service) ListProviderModels(ctx context.Context, providerCfg config.ProviderConfig) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	})
}

func (s *Service) ListProviderModelsSnapshot(ctx context.Context, providerCfg config.ProviderConfig) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{
		queueRefresh: true,
	})
}

func (s *Service) ListProviderModelsCached(ctx context.Context, providerCfg config.ProviderConfig) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, providerCfg, queryOptions{})
}

func (s *Service) listProviderModels(
	ctx context.Context,
	providerCfg config.ProviderConfig,
	options queryOptions,
) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, providerCfg, options)
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
	models   []providertypes.ModelDescriptor
	ok       bool
	expired  bool
	identity config.ProviderIdentity
}

func (s *Service) modelsForProvider(ctx context.Context, providerCfg config.ProviderConfig, options queryOptions) ([]providertypes.ModelDescriptor, error) {
	configuredModels := providertypes.MergeModelDescriptors(providerCfg.Models)
	defaultModels := providerDefaultModels(providerCfg)
	snapshot := s.catalogSnapshot(ctx, providerCfg)

	models := snapshot.models
	catalogOK := snapshot.ok
	performedSyncRefresh := false
	if !catalogOK && options.allowSyncRefresh {
		discovered, err := s.discoverAndPersist(ctx, providerCfg)
		if err != nil {
			if len(defaultModels) == 0 {
				return nil, err
			}
		} else {
			models = discovered
			catalogOK = true
			performedSyncRefresh = true
		}
	}

	if options.queueRefresh && snapshot.expired {
		s.queueRefresh(providerCfg, snapshot.identity)
	}
	if options.queueRefresh && !snapshot.ok && !performedSyncRefresh {
		s.queueRefresh(providerCfg, snapshot.identity)
	}

	if !catalogOK {
		if len(defaultModels) == 0 {
			return nil, nil
		}
		return providertypes.MergeModelDescriptors(configuredModels, defaultModels), nil
	}
	return providertypes.MergeModelDescriptors(configuredModels, models, defaultModels), nil
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

func (s *Service) discoverAndPersist(ctx context.Context, providerCfg config.ProviderConfig) ([]providertypes.ModelDescriptor, error) {
	if !s.registry.Supports(providerCfg.Driver) {
		return nil, nil
	}

	resolved, err := providerCfg.Resolve()
	if err != nil {
		return nil, err
	}

	discovered, err := s.registry.DiscoverModels(ctx, resolved.ToRuntimeConfig())
	if err != nil {
		return nil, err
	}

	discovered = providertypes.MergeModelDescriptors(discovered)
	if s.store == nil {
		return discovered, nil
	}

	identity, err := providerCfg.Identity()
	if err != nil {
		return discovered, nil
	}

	now := s.now()
	_ = s.store.Save(ctx, ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     now,
		ExpiresAt:     now.Add(s.catalogTTL),
		Models:        discovered,
	})
	return discovered, nil
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

// providerDefaultModels 仅为内建 provider 暴露代码定义的默认模型，自定义 provider 必须完全依赖远程发现结果。
func providerDefaultModels(providerCfg config.ProviderConfig) []providertypes.ModelDescriptor {
	if providerCfg.Source == config.ProviderSourceCustom {
		return nil
	}
	return providertypes.DescriptorsFromIDs([]string{providerCfg.Model})
}
