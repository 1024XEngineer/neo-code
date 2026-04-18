package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const (
	defaultTTL               = 24 * time.Hour
	defaultBackgroundTimeout = 30 * time.Second
)

var errCatalogPersist = errors.New("provider catalog: persist discovered models")

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

func (s *Service) ListProviderModels(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{
		allowSyncRefresh: true,
		queueRefresh:     true,
	})
}

func (s *Service) ListProviderModelsSnapshot(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{
		queueRefresh: true,
	})
}

func (s *Service) ListProviderModelsCached(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{})
}

func (s *Service) listProviderModels(
	ctx context.Context,
	input provider.CatalogInput,
	options queryOptions,
) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return s.modelsForProvider(ctx, input, options)
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
	models  []providertypes.ModelDescriptor
	ok      bool
	expired bool
}

func (s *Service) modelsForProvider(ctx context.Context, input provider.CatalogInput, options queryOptions) ([]providertypes.ModelDescriptor, error) {
	if err := s.registry.ValidateCatalogIdentity(input.Identity); err != nil {
		return nil, err
	}

	configuredModels := providertypes.MergeModelDescriptors(input.ConfiguredModels)
	defaultModels := providertypes.MergeModelDescriptors(input.DefaultModels)
	if input.DisableDiscovery {
		return providertypes.MergeModelDescriptors(configuredModels, defaultModels), nil
	}

	snapshot := s.catalogSnapshot(ctx, input)

	models := snapshot.models
	catalogOK := snapshot.ok
	if catalogOK && len(models) == 0 {
		// 空 catalog 等价于未命中，避免历史空缓存长期阻断后续 discovery。
		catalogOK = false
	}
	performedSyncRefresh := false
	if !catalogOK && options.allowSyncRefresh {
		discovered, err := s.discoverAndPersist(ctx, input)
		if err != nil {
			if len(defaultModels) == 0 || provider.IsDiscoveryConfigError(err) || errors.Is(err, errCatalogPersist) {
				return nil, err
			}
		} else {
			models = discovered
			catalogOK = true
			performedSyncRefresh = true
		}
	}

	if shouldQueueRefresh(options, snapshot, performedSyncRefresh) {
		s.queueRefresh(input)
	}

	return mergeResolvedModels(catalogOK, configuredModels, models, defaultModels), nil
}

func (s *Service) catalogSnapshot(ctx context.Context, input provider.CatalogInput) catalogSnapshot {
	modelCatalog, err := s.loadCatalog(ctx, input.Identity)
	if err != nil {
		return catalogSnapshot{}
	}
	return catalogSnapshot{
		models:  modelCatalog.Models,
		ok:      true,
		expired: modelCatalog.Expired(s.now()),
	}
}

func (s *Service) loadCatalog(ctx context.Context, identity provider.ProviderIdentity) (ModelCatalog, error) {
	if s.store == nil {
		return ModelCatalog{}, ErrCatalogNotFound
	}
	return s.store.Load(ctx, identity)
}

func (s *Service) discoverAndPersist(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if !s.registry.Supports(input.Identity.Driver) {
		return nil, nil
	}

	if input.ResolveDiscoveryConfig == nil {
		return nil, errors.New("provider catalog: discovery config resolver is nil")
	}

	runtimeCfg, err := input.ResolveDiscoveryConfig()
	if err != nil {
		return nil, err
	}

	discovered, err := s.registry.DiscoverModels(ctx, runtimeCfg)
	if err != nil {
		return nil, err
	}

	discovered = providertypes.MergeModelDescriptors(discovered)
	if len(discovered) == 0 {
		return discovered, nil
	}
	if s.store == nil {
		return discovered, nil
	}

	now := s.now()
	if err := s.store.Save(ctx, ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     now,
		ExpiresAt:     now.Add(s.catalogTTL),
		Models:        discovered,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", errCatalogPersist, err)
	}
	return discovered, nil
}

func (s *Service) queueRefresh(input provider.CatalogInput) {
	if s.store == nil {
		return
	}

	if !s.registry.Supports(input.Identity.Driver) {
		return
	}
	identity := input.Identity
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
		_, _ = s.discoverAndPersist(ctx, input)
	}()
}

func shouldQueueRefresh(options queryOptions, snapshot catalogSnapshot, performedSyncRefresh bool) bool {
	if !options.queueRefresh {
		return false
	}
	if snapshot.expired {
		return true
	}
	return !snapshot.ok && !performedSyncRefresh
}

func mergeResolvedModels(
	catalogOK bool,
	configuredModels []providertypes.ModelDescriptor,
	discoveredModels []providertypes.ModelDescriptor,
	defaultModels []providertypes.ModelDescriptor,
) []providertypes.ModelDescriptor {
	if !catalogOK {
		return providertypes.MergeModelDescriptors(configuredModels, defaultModels)
	}
	return providertypes.MergeModelDescriptors(configuredModels, discoveredModels, defaultModels)
}
