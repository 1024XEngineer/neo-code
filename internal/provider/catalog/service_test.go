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

func TestListProviderModelsFallsBackToDefaultModelWithoutDiscovery(t *testing.T) {
	t.Parallel()

	service := NewService("", newRegistry(t, openaicompat.DriverName, nil), newMemoryStore())
	models, err := service.ListProviderModels(context.Background(), openAIProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected default model fallback, got %+v", models)
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
	providerCfg := config.OpenAIProvider()
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

func TestListProviderModelsSnapshotReturnsDefaultAndRefreshesInBackgroundOnMiss(t *testing.T) {
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
	service.backgroundTimeout = time.Second

	models, err := service.ListProviderModelsSnapshot(context.Background(), openAIProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModelsSnapshot() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected immediate default model fallback, got %+v", models)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background refresh to run")
	}

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		modelCatalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(modelCatalog.Models, "gpt-4o") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain gpt-4o, got %+v", modelCatalog.Models)
}

func TestListProviderModelsReturnsDiscoveryErrorOnCacheMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "")

	service := NewService("", newRegistry(t, "openaicompat", func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		return nil, nil
	}), newMemoryStore())

	_, err := service.ListProviderModels(context.Background(), customGatewayProviderSource())
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
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

	models, err := service.ListProviderModels(context.Background(), openAIProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "server-model") {
		t.Fatalf("expected discovered model in result, got %+v", models)
	}

	identity, err := config.OpenAIProvider().Identity()
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

func TestListProviderModelsReturnsStaleCacheAndRefreshesInBackground(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryStore()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-48 * time.Hour),
		ExpiresAt:     now.Add(-24 * time.Hour),
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
	service.backgroundTimeout = time.Second

	models, err := service.ListProviderModels(context.Background(), openAIProviderSource())
	if err != nil {
		t.Fatalf("ListProviderModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "stale-model") {
		t.Fatalf("expected stale cached model to be returned immediately, got %+v", models)
	}

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected background refresh to run")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		modelCatalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(modelCatalog.Models, "fresh-model") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	modelCatalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain fresh-model, got %+v", modelCatalog.Models)
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

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryStore()
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: schemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-time.Hour),
		ExpiresAt:     now.Add(time.Hour),
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

	models, err := service.ListProviderModelsCached(context.Background(), openAIProviderSource())
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

func TestDiscoverAndPersistFailurePaths(t *testing.T) {
	t.Run("unsupported driver", func(t *testing.T) {
		service := NewService("", provider.NewRegistry(), newMemoryStore())
		discovered, err := service.discoverAndPersist(context.Background(), openAIProviderSource())
		if err != nil || discovered != nil {
			t.Fatalf("expected unsupported driver to skip discovery, got err=%v models=%+v", err, discovered)
		}
	})

	t.Run("resolve provider config failure", func(t *testing.T) {
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
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

		discovered, err := service.discoverAndPersist(context.Background(), openAIProviderSource())
		if err == nil || discovered != nil {
			t.Fatalf("expected discovery error to skip persistence, got err=%v models=%+v", err, discovered)
		}
	})

	t.Run("store nil still returns discovered models", func(t *testing.T) {
		t.Setenv(testAPIKeyEnv, "test-key")
		service := NewService("", newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			return []providertypes.ModelDescriptor{{ID: "gpt-4.1", Name: "GPT-4.1"}}, nil
		}), nil)

		discovered, err := service.discoverAndPersist(context.Background(), openAIProviderSource())
		if err != nil {
			t.Fatalf("expected discovery without store to succeed, got %v", err)
		}
		if !containsModelDescriptorID(discovered, "gpt-4.1") {
			t.Fatalf("expected discovered model to be returned, got %+v", discovered)
		}
	})
}

func TestQueueRefreshDeduplicatesInFlightRequests(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var discoverCalls int32
	registry := newRegistry(t, openaicompat.DriverName, func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
		atomic.AddInt32(&discoverCalls, 1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return []providertypes.ModelDescriptor{{ID: "gpt-4o", Name: "GPT-4o"}}, nil
	})

	service := NewService("", registry, newMemoryStore())
	service.backgroundTimeout = time.Second

	service.queueRefresh(openAIProviderSource())
	<-started
	service.queueRefresh(openAIProviderSource())

	time.Sleep(50 * time.Millisecond)
	if calls := atomic.LoadInt32(&discoverCalls); calls != 1 {
		t.Fatalf("expected exactly one in-flight refresh, got %d", calls)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		service.refreshMu.Lock()
		_, exists := service.inFlightByID[identity.Key()]
		service.refreshMu.Unlock()
		if !exists {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("expected in-flight refresh marker to be cleared")
}

func TestQueueRefreshSkipsWhenDriverUnsupportedOrIdentityIncomplete(t *testing.T) {
	t.Parallel()

	t.Run("unsupported driver", func(t *testing.T) {
		t.Parallel()

		service := NewService("", provider.NewRegistry(), newMemoryStore())
		input := openAIProviderSource()
		input.Identity.Driver = "missing"

		service.queueRefresh(input)

		service.refreshMu.Lock()
		defer service.refreshMu.Unlock()
		if len(service.inFlightByID) != 0 {
			t.Fatalf("expected no refresh to be queued, got %+v", service.inFlightByID)
		}
	})

	t.Run("incomplete identity", func(t *testing.T) {
		t.Parallel()

		service := NewService("", newRegistry(t, openaicompat.DriverName, nil), newMemoryStore())
		input := openAIProviderSource()
		input.Identity.BaseURL = ""

		service.queueRefresh(input)

		service.refreshMu.Lock()
		defer service.refreshMu.Unlock()
		if len(service.inFlightByID) != 0 {
			t.Fatalf("expected no refresh to be queued, got %+v", service.inFlightByID)
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
	return mustCatalogInput(nil, config.OpenAIProvider())
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
		ResolveDiscoveryConfig: func() (provider.RuntimeConfig, error) {
			resolved, err := cloned.Resolve()
			if err != nil {
				return provider.RuntimeConfig{}, err
			}
			return resolved.ToRuntimeConfig(), nil
		},
	}
	if cloned.Source != config.ProviderSourceCustom {
		input.DefaultModels = providertypes.DescriptorsFromIDs([]string{cloned.Model})
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

const testAPIKeyEnv = "OPENAI_API_KEY"
