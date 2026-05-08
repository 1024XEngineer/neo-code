package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

var errCatalogPersist = errors.New("provider catalog: persist discovered models")

type Service struct {
	registry *provider.Registry
	store    Store
	now      func() time.Time
}

func NewService(baseDir string, registry *provider.Registry, store Store) *Service {
	if store == nil && strings.TrimSpace(baseDir) != "" {
		store = newJSONStore(baseDir)
	}

	return &Service{
		registry: registry,
		store:    store,
		now:      time.Now,
	}
}

func (s *Service) ListProviderModels(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{
		allowSyncRefresh: true,
	})
}

func (s *Service) ListProviderModelsSnapshot(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{})
}

func (s *Service) ListProviderModelsCached(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return s.listProviderModels(ctx, input, queryOptions{})
}

// RefreshProviderModels 强制重新执行一次远端 discovery，并在成功后覆盖本地缓存。
func (s *Service) RefreshProviderModels(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.DisableDiscovery {
		configuredModels := providertypes.MergeModelDescriptors(input.ConfiguredModels)
		defaultModels := providertypes.MergeModelDescriptors(input.DefaultModels)
		return providertypes.MergeModelDescriptors(configuredModels, defaultModels), nil
	}

	discovered, err := s.discoverAndPersist(ctx, input)
	if err != nil {
		return nil, err
	}
	configuredModels := providertypes.MergeModelDescriptors(input.ConfiguredModels)
	defaultModels := providertypes.MergeModelDescriptors(input.DefaultModels)
	return mergeResolvedModels(true, configuredModels, discovered, defaultModels), nil
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
}

type catalogSnapshot struct {
	models []providertypes.ModelDescriptor
	ok     bool
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
	if !catalogOK && options.allowSyncRefresh {
		discovered, err := s.discoverAndPersist(ctx, input)
		if err != nil {
			if len(defaultModels) == 0 || provider.IsDiscoveryConfigError(err) || errors.Is(err, errCatalogPersist) {
				return nil, err
			}
		} else {
			models = discovered
			catalogOK = true
		}
	}

	return mergeResolvedModels(catalogOK, configuredModels, models, defaultModels), nil
}

func (s *Service) catalogSnapshot(ctx context.Context, input provider.CatalogInput) catalogSnapshot {
	modelCatalog, err := s.loadCatalog(ctx, input.Identity)
	if err != nil {
		return catalogSnapshot{}
	}
	return catalogSnapshot{
		models: modelCatalog.Models,
		ok:     true,
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
	if !s.registry.SupportsDiscovery(input.Identity.Driver) {
		return nil, provider.NewDiscoveryConfigError(fmt.Sprintf("driver %q does not support model discovery", input.Identity.Driver))
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

	if err := s.store.Save(ctx, ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     s.now(),
		Models:        discovered,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", errCatalogPersist, err)
	}
	return discovered, nil
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
