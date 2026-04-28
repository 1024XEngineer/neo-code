package catalog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat"
	providertypes "neo-code/internal/provider/types"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	store := newMemoryStore()

	service := NewService("", registry, store)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.store != store {
		t.Fatal("expected explicit store to be used")
	}
}

func TestListProviderModelsReturnsConfiguredModelsWhenDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	service := NewService("", newRegistry(t, openaicompat.DriverName, nil), newMemoryStore())
	providerCfg := customGatewayProvider()
	providerCfg.ModelSource = config.ModelSourceManual
	providerCfg.Models = []providertypes.ModelDescriptor{{ID: "manual-model", Name: "Manual Model"}}
	models, err := service.ListProviderModels(context.Background(), mustCatalogInput(t, providerCfg))
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "manual-model" {
		t.Fatalf("expected configured models without discovery, got %+v", models)
	}
}

func TestListProviderModelsCustomProviderDoesNotFallbackWithoutDiscovery(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	service := NewService("", newRegistry(t, openaicompat.DriverName, nil), newMemoryStore())
	models, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected no models for custom provider without cache or discovery, got %+v", models)
	}
}

func TestListProviderModelsMergesConfiguredMetadataAfterDiscovery(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return []providertypes.ModelDescriptor{{
			ID:              "deepseek-coder",
			Name:            "Server DeepSeek",
			ContextWindow:   32768,
			MaxOutputTokens: 4096,
		}}, nil
	})

	service := NewService("", registry, newMemoryStore())
	providerCfg := customGatewayProvider()
	providerCfg.Models = []providertypes.ModelDescriptor{{
		ID:              "deepseek-coder",
		Name:            "DeepSeek Coder",
		ContextWindow:   131072,
		MaxOutputTokens: 8192,
		CapabilityHints: providertypes.ModelCapabilityHints{
			ToolCalling: providertypes.ModelCapabilityStateSupported,
		},
	}}
	providerCfg.Model = "deepseek-coder"

	input := mustCatalogInput(t, providerCfg)
	models, err := service.ListProviderModels(context.Background(), input)
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected merged configured/discovered result, got %+v", models)
	}
	if models[0].Name != "DeepSeek Coder" {
		t.Fatalf("expected configured model name to win, got %+v", models[0])
	}
	if models[0].ContextWindow != 131072 {
		t.Fatalf("expected configured context window to win, got %+v", models[0])
	}
	if models[0].MaxOutputTokens != 8192 {
		t.Fatalf("expected configured max output tokens to win, got %+v", models[0])
	}
	if models[0].CapabilityHints.ToolCalling != providertypes.ModelCapabilityStateSupported {
		t.Fatalf("expected configured capability hints to win, got %+v", models[0].CapabilityHints)
	}
}

func TestListProviderModelsUsesConfiguredContextWindowWhenDiscoveryMissesIt(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return []providertypes.ModelDescriptor{{
			ID:            "deepseek-coder",
			Name:          "Server DeepSeek",
			ContextWindow: 0,
		}}, nil
	})

	service := NewService("", registry, newMemoryStore())
	providerCfg := customGatewayProvider()
	providerCfg.Models = []providertypes.ModelDescriptor{{
		ID:            "deepseek-coder",
		Name:          "DeepSeek Coder",
		ContextWindow: 131072,
	}}

	models, err := service.ListProviderModels(context.Background(), mustCatalogInput(t, providerCfg))
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ContextWindow != 131072 {
		t.Fatalf("expected configured context window to fill discovery gap, got %+v", models)
	}
}

func TestListProviderModelsSnapshotReturnsBuiltinStaticModelsWithoutRefresh(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	refreshed := make(chan struct{}, 1)
	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []providertypes.ModelDescriptor{{ID: "gpt-4o", Name: "GPT-4o"}}, nil
	})

	store := newMemoryStore()
	service := NewService("", registry, store)

	models, err := service.ListProviderModelsSnapshot(context.Background(), openAIProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModelsSnapshot() error = %v", err)
	}
	if !containsModelDescriptorID(models, config.OpenAIDefaultModel) {
		t.Fatalf("expected builtin static models on snapshot path, got %+v", models)
	}

	select {
	case <-refreshed:
		t.Fatal("expected snapshot path to avoid refresh")
	case <-time.After(150 * time.Millisecond):
	}

	identity, err := customGatewayProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	if _, err := store.Load(context.Background(), identity); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("expected snapshot path to avoid cache writes, got %v", err)
	}
}

func TestListProviderModelsReturnsDiscoveryErrorOnCacheMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "")

	service := NewService("", newRegistry(t, "openaicompat", func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		if _, err := cfg.ResolveAPIKeyValue(); err != nil {
			return nil, err
		}
		return nil, nil
	}), newMemoryStore())

	_, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
	if err == nil || !strings.Contains(err.Error(), testAPIKeyEnv) {
		t.Fatalf("expected discovery-time api key error, got %v", err)
	}
}

func TestListProviderModelsDiscoversAndCachesOnMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return []providertypes.ModelDescriptor{{
			ID:              "server-model",
			Name:            "Server Model",
			ContextWindow:   32000,
			MaxOutputTokens: 4096,
		}}, nil
	})

	store := newMemoryStore()
	service := NewService("", registry, store)

	models, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "server-model") {
		t.Fatalf("expected discovered model in result, got %+v", models)
	}

	identity, err := customGatewayProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() cached catalog error = %v", err)
	}
	if !containsModelDescriptorID(modelCatalog.Models, "server-model") {
		t.Fatalf("expected cached discovered model, got %+v", modelCatalog.Models)
	}
}

func TestListProviderModelsReturnsCachedCatalogWithoutAutomaticRefresh(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	identity, err := customGatewayProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryStore()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-48 * time.Hour),
		Models: []providertypes.ModelDescriptor{
			{ID: "stale-model", Name: "Stale Model"},
		},
	}); err != nil {
		t.Fatalf("seed stale catalog: %v", err)
	}

	refreshed := make(chan struct{}, 1)
	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []providertypes.ModelDescriptor{{ID: "fresh-model", Name: "Fresh Model"}}, nil
	})

	service := NewService("", registry, store)
	service.now = func() time.Time { return now }

	models, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "stale-model") {
		t.Fatalf("expected stale cached model to be returned immediately, got %+v", models)
	}

	select {
	case <-refreshed:
		t.Fatal("expected cached catalog path to avoid automatic refresh")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestDescriptorsFromIDsHelper(t *testing.T) {
	t.Parallel()

	models := providertypes.DescriptorsFromIDs([]string{"gpt-4.1", "", "gpt-4o"})
	if len(models) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(models))
	}
	if models[0].ID != "gpt-4.1" || models[1].ID != "gpt-4o" {
		t.Fatalf("unexpected descriptors: %+v", models)
	}
}

func TestServiceValidateErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()

		var service *Service
		_, err := service.ListProviderModels(context.Background(), openAIProviderSource())
		if err == nil || err.Error() != "provider catalog: service is nil" {
			t.Fatalf("expected nil service error, got %v", err)
		}
	})

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()

		service := NewService("", nil, newMemoryStore())
		_, err := service.ListProviderModels(context.Background(), openAIProviderSource())
		if err == nil || err.Error() != "provider catalog: registry is nil" {
			t.Fatalf("expected nil registry error, got %v", err)
		}
	})
}

func TestListProviderModelsHonorsContextError(t *testing.T) {
	t.Parallel()

	service := NewService("", provider.NewRegistry(), newMemoryStore())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.ListProviderModels(ctx, openAIProviderSource())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestListProviderModelsCachedUsesFreshCatalogWithoutDiscovery(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	identity, err := customGatewayProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryStore()
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-time.Hour),
		Models: []providertypes.ModelDescriptor{
			{ID: "cached-model", Name: "Cached Model"},
		},
	}); err != nil {
		t.Fatalf("seed fresh catalog: %v", err)
	}

	var discoverCalls int32
	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		atomic.AddInt32(&discoverCalls, 1)
		return []providertypes.ModelDescriptor{{ID: "fresh-model", Name: "Fresh Model"}}, nil
	})

	service := NewService("", registry, store)
	service.now = func() time.Time { return now }

	models, err := service.ListProviderModelsCached(context.Background(), customGatewayProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModelsCached() error = %v", err)
	}
	if !containsModelDescriptorID(models, "cached-model") {
		t.Fatalf("expected cached model to be returned, got %+v", models)
	}
	if atomic.LoadInt32(&discoverCalls) != 0 {
		t.Fatalf("expected no discovery for fresh cache, got %d", discoverCalls)
	}
}

func TestListProviderModelsRefreshesWhenCatalogSnapshotIsEmpty(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	store := newMemoryStore()
	input := customGatewayProviderSource()
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      input.Identity,
		FetchedAt:     time.Now().Add(-time.Minute),
		Models:        nil,
	}); err != nil {
		t.Fatalf("seed empty catalog: %v", err)
	}

	var discoverCalls int32
	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		atomic.AddInt32(&discoverCalls, 1)
		return []providertypes.ModelDescriptor{{ID: "qwen-plus", Name: "Qwen Plus"}}, nil
	})

	service := NewService("", registry, store)
	models, err := service.ListProviderModels(context.Background(), input)
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "qwen-plus" {
		t.Fatalf("expected sync refresh result from discovery, got %+v", models)
	}
	if atomic.LoadInt32(&discoverCalls) == 0 {
		t.Fatal("expected discovery to be called when snapshot models are empty")
	}
}

func TestRefreshProviderModelsReturnsConfiguredModelsWhenDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	service := NewService("", newRegistry(t, openaicompat.DriverName, nil), newMemoryStore())
	providerCfg := customGatewayProvider()
	providerCfg.ModelSource = config.ModelSourceManual
	providerCfg.Models = []providertypes.ModelDescriptor{{ID: "manual-model", Name: "Manual Model"}}

	models, err := service.RefreshProviderModels(context.Background(), mustCatalogInput(t, providerCfg))
	if err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "manual-model" {
		t.Fatalf("expected configured models without discovery, got %+v", models)
	}
}

func TestRefreshProviderModelsDiscoversAndPersists(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return []providertypes.ModelDescriptor{{ID: "fresh-model", Name: "Fresh Model"}}, nil
	})
	store := newMemoryStore()
	service := NewService("", registry, store)

	input := customGatewayProviderSource()
	models, err := service.RefreshProviderModels(context.Background(), input)
	if err != nil {
		t.Fatalf("RefreshProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "fresh-model" {
		t.Fatalf("expected discovered models, got %+v", models)
	}

	cached, err := store.Load(context.Background(), input.Identity)
	if err != nil {
		t.Fatalf("load cached catalog: %v", err)
	}
	if len(cached.Models) != 1 || cached.Models[0].ID != "fresh-model" {
		t.Fatalf("expected refreshed models to be persisted, got %+v", cached.Models)
	}
}

func TestDiscoverAndPersistFailurePaths(t *testing.T) {
	t.Run("unsupported driver", func(t *testing.T) {
		service := NewService("", provider.NewRegistry(), newMemoryStore())
		discovered, err := service.discoverAndPersist(context.Background(), customGatewayProviderSource())
		if err != nil || discovered != nil {
			t.Fatalf("expected unsupported driver to skip discovery, got err=%v models=%+v", err, discovered)
		}
	})

	t.Run("resolve provider config failure", func(t *testing.T) {
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			if _, err := cfg.ResolveAPIKeyValue(); err != nil {
				return nil, err
			}
			return nil, nil
		}), newMemoryStore())

		providerCfg := config.ProviderConfig{
			Name:      "broken-openai",
			Driver:    openaicompat.DriverName,
			BaseURL:   config.OpenAIDefaultBaseURL,
			Model:     config.OpenAIDefaultModel,
			APIKeyEnv: "",
		}

		input := mustCatalogInput(t, providerCfg)
		discovered, err := service.discoverAndPersist(context.Background(), input)
		if err == nil || discovered != nil {
			t.Fatalf("expected resolve failure to surface as error, got err=%v models=%+v", err, discovered)
		}
	})

	t.Run("discovery error", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return nil, errors.New("discover failed")
		}), newMemoryStore())

		discovered, err := service.discoverAndPersist(context.Background(), customGatewayProviderSource())
		if err == nil || discovered != nil {
			t.Fatalf("expected discovery error to skip persistence, got err=%v models=%+v", err, discovered)
		}
	})

	t.Run("store nil still returns discovered models", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return []providertypes.ModelDescriptor{{ID: "gpt-4.1", Name: "GPT-4.1"}}, nil
		}), nil)

		discovered, err := service.discoverAndPersist(context.Background(), customGatewayProviderSource())
		if err != nil {
			t.Fatalf("expected discovery without store to succeed, got %v", err)
		}
		if !containsModelDescriptorID(discovered, "gpt-4.1") {
			t.Fatalf("expected discovered model to be returned, got %+v", discovered)
		}
	})

	t.Run("empty discovery result is not persisted", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")
		store := newMemoryStore()
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return nil, nil
		}), store)

		discovered, err := service.discoverAndPersist(context.Background(), customGatewayProviderSource())
		if err != nil {
			t.Fatalf("discoverAndPersist() error = %v", err)
		}
		if len(discovered) != 0 {
			t.Fatalf("expected empty discovery result, got %+v", discovered)
		}

		identity, identityErr := customGatewayProvider().Identity()
		if identityErr != nil {
			t.Fatalf("Identity() error = %v", identityErr)
		}
		if _, loadErr := store.Load(context.Background(), identity); !errors.Is(loadErr, ErrCatalogNotFound) {
			t.Fatalf("expected empty discovery not to be cached, got %v", loadErr)
		}
	})

	t.Run("persist failure returns error even when default models exist", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")
		registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return []providertypes.ModelDescriptor{{ID: "gpt-4.1", Name: "GPT-4.1"}}, nil
		})
		store := &failSaveStore{
			memoryStore: newMemoryStore(),
			saveErr:     errors.New("disk full"),
		}
		service := NewService("", registry, store)

		models, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
		if err == nil {
			t.Fatal("expected persist failure to be returned")
		}
		if models != nil {
			t.Fatalf("expected nil models on persist failure, got %+v", models)
		}
		if !errors.Is(err, errCatalogPersist) {
			t.Fatalf("expected persist sentinel error, got %v", err)
		}
	})
}

func newRegistry(t *testing.T, name string, discover provider.DiscoveryFunc) *provider.Registry {
	t.Helper()

	if discover == nil {
		discover = func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return nil, nil
		}
	}

	registry := provider.NewRegistry()
	if err := registry.Register(provider.DriverDefinition{
		Name:     name,
		Discover: discover,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return catalogTestProvider{}, nil
		},
	}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	return registry
}

func openAIProviderSource() provider.CatalogInput {
	providerCfg := config.OpenAIProvider()
	providerCfg.APIKeyEnv = testAPIKeyEnv
	input := mustCatalogInput(nil, providerCfg)
	if len(input.ConfiguredModels) == 0 {
		input.ConfiguredModels = providertypes.DescriptorsFromIDs([]string{
			config.OpenAIDefaultModel,
			"gpt-5.4-mini",
			"gpt-5.3-codex",
			"gpt-4.1",
			"gpt-4o",
			"gpt-4o-mini",
		})
	}
	if len(input.DefaultModels) == 0 {
		input.DefaultModels = providertypes.CloneModelDescriptors(input.ConfiguredModels)
	}
	input.DisableDiscovery = true
	return input
}

func customGatewayProviderSource() provider.CatalogInput {
	return mustCatalogInput(nil, customGatewayProvider())
}

func mustCatalogInput(t *testing.T, cfg config.ProviderConfig) provider.CatalogInput {
	cloned := cfg
	cloned.Models = providertypes.CloneModelDescriptors(cfg.Models)

	identity, err := cloned.Identity()
	if err != nil {
		if t != nil {
			t.Helper()
			t.Fatalf("Identity() error = %v", err)
		}
		panic(err)
	}

	input := provider.CatalogInput{
		Identity:         identity,
		ConfiguredModels: providertypes.CloneModelDescriptors(cloned.Models),
		DisableDiscovery: cloned.Source == config.ProviderSourceBuiltin ||
			(cloned.Source == config.ProviderSourceCustom && config.NormalizeModelSource(cloned.ModelSource) == config.ModelSourceManual),
		ResolveDiscoveryConfig: func() (provider.RuntimeConfig, error) {
			resolved, err := cloned.Resolve()
			if err != nil {
				return provider.RuntimeConfig{}, err
			}
			return resolved.ToRuntimeConfig()
		},
	}
	if cloned.Source != config.ProviderSourceCustom {
		input.DefaultModels = providertypes.CloneModelDescriptors(cloned.Models)
		if len(input.DefaultModels) == 0 {
			input.DefaultModels = providertypes.DescriptorsFromIDs([]string{cloned.Model})
		}
	}
	return input
}

func customGatewayProvider() config.ProviderConfig {
	return config.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: testAPIKeyEnv,
		Source:    config.ProviderSourceCustom,
	}
}

func containsModelDescriptorID(models []providertypes.ModelDescriptor, modelID string) bool {
	target := provider.NormalizeKey(modelID)
	if target == "" {
		return false
	}

	for _, model := range models {
		if provider.NormalizeKey(model.ID) == target {
			return true
		}
	}
	return false
}

type catalogTestProvider struct{}

func (catalogTestProvider) EstimateInputTokens(
	ctx context.Context,
	req providertypes.GenerateRequest,
) (providertypes.BudgetEstimate, error) {
	_ = ctx
	_ = req
	return providertypes.BudgetEstimate{
		EstimateSource: provider.EstimateSourceLocal,
	}, nil
}

func (catalogTestProvider) Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
	return nil
}

type memoryStore struct {
	mu       sync.Mutex
	catalogs map[string]ModelCatalog
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		catalogs: map[string]ModelCatalog{},
	}
}

func (s *memoryStore) Load(ctx context.Context, identity provider.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	modelCatalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return ModelCatalog{}, ErrCatalogNotFound
	}
	return modelCatalog, nil
}

func (s *memoryStore) Save(ctx context.Context, modelCatalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.catalogs[modelCatalog.Identity.Key()] = modelCatalog
	return nil
}

const testAPIKeyEnv = "NEOCODE_TEST_CATALOG_API_KEY"

type failSaveStore struct {
	*memoryStore
	saveErr error
}

func (s *failSaveStore) Save(ctx context.Context, modelCatalog ModelCatalog) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	return s.memoryStore.Save(ctx, modelCatalog)
}
