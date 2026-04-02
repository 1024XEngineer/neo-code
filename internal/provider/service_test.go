package provider

import (
	"context"
	"sync"
	"testing"
	"time"

	"neo-code/internal/config"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	registry := NewRegistry()
	store := newMemoryCatalogStore()

	service := NewService(manager, registry, store)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.catalogs != store {
		t.Fatal("expected explicit catalog store to be used")
	}
}

func TestServiceListProvidersReturnsBuiltinCatalog(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, nil)); err != nil {
		t.Fatalf("register test driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	items, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	expected := config.DefaultProviders()
	if len(items) != len(expected) {
		t.Fatalf("expected %d builtin providers, got %d", len(expected), len(items))
	}
	expectedNames := make(map[string]struct{}, len(expected))
	for _, providerCfg := range expected {
		expectedNames[providerCfg.Name] = struct{}{}
	}
	for _, item := range items {
		if _, ok := expectedNames[item.ID]; !ok {
			t.Fatalf("unexpected provider in catalog: %+v", item)
		}
		if len(item.Models) != 1 {
			t.Fatalf("expected default fallback model for %q, got %+v", item.ID, item.Models)
		}
	}
}

func TestServiceSelectProviderFallsBackToProviderDefault(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, nil)); err != nil {
		t.Fatalf("register openai driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	selection, err := service.SelectProvider(context.Background(), config.QiniuName)
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != config.QiniuName || selection.ModelID != config.QiniuDefaultModel {
		t.Fatalf("unexpected selection: %+v", selection)
	}
}

func TestServiceEnsureSelectionRepairsInvalidCurrentModel(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("seed invalid current model: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, nil)); err != nil {
		t.Fatalf("register openai driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != config.OpenAIName || selection.ModelID != config.OpenAIDefaultModel {
		t.Fatalf("unexpected normalized selection: %+v", selection)
	}

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != config.OpenAIDefaultModel {
		t.Fatalf("expected rewritten current model %q, got %q", config.OpenAIDefaultModel, reloaded.CurrentModel)
	}
}

func TestServiceListModelsFallsBackToDefaultModelWithoutDiscovery(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	service := NewService(manager, NewRegistry(), newMemoryCatalogStore())
	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected default model fallback, got %+v", models)
	}
}

func TestServiceSetCurrentModelUsesDiscoveredModels(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
		return []ModelDescriptor{
			{ID: cfg.Model, Name: cfg.Model},
			{ID: "gpt-4o", Name: "GPT-4o"},
		}, nil
	})); err != nil {
		t.Fatalf("register discovery driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	selection, err := service.SetCurrentModel(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != "gpt-4o" {
		t.Fatalf("expected selected model %q, got %+v", "gpt-4o", selection)
	}
}

func TestServiceListModelsDiscoversAndCachesOnMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	store := newMemoryCatalogStore()
	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
		return []ModelDescriptor{{
			ID:              "server-model",
			Name:            "Server Model",
			ContextWindow:   32000,
			MaxOutputTokens: 4096,
		}}, nil
	})); err != nil {
		t.Fatalf("register discovery driver: %v", err)
	}

	service := NewService(manager, registry, store)
	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if !containsModelDescriptorID(models, "server-model") {
		t.Fatalf("expected discovered model in result, got %+v", models)
	}

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	catalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() cached catalog error = %v", err)
	}
	if !containsModelDescriptorID(catalog.Models, "server-model") {
		t.Fatalf("expected cached discovered model, got %+v", catalog.Models)
	}
}

func TestServiceListModelsReturnsStaleCacheAndRefreshesInBackground(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}

	store := newMemoryCatalogStore()
	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: modelCatalogSchemaVersion,
		Identity:      identity,
		FetchedAt:     now.Add(-48 * time.Hour),
		ExpiresAt:     now.Add(-24 * time.Hour),
		Models: []ModelDescriptor{{
			ID:   "stale-model",
			Name: "Stale Model",
		}},
	}); err != nil {
		t.Fatalf("seed stale catalog: %v", err)
	}

	refreshed := make(chan struct{}, 1)
	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []ModelDescriptor{{ID: "fresh-model", Name: "Fresh Model"}}, nil
	})); err != nil {
		t.Fatalf("register custom driver: %v", err)
	}

	service := NewService(manager, registry, store)
	service.now = func() time.Time { return now }
	service.backgroundTimeout = time.Second

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
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
		catalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(catalog.Models, "fresh-model") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	catalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain fresh-model, got %+v", catalog.Models)
}

func TestServiceBuildAndValidate(t *testing.T) {
	t.Parallel()

	t.Run("build delegates to registry", func(t *testing.T) {
		t.Parallel()

		manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
		registry := NewRegistry()
		if err := registry.Register(testDriverDefinition("custom", nil)); err != nil {
			t.Fatalf("register driver: %v", err)
		}

		service := NewService(manager, registry, newMemoryCatalogStore())
		providerInstance, err := service.Build(context.Background(), config.ResolvedProviderConfig{
			ProviderConfig: config.ProviderConfig{
				Name:      "custom",
				Driver:    "custom",
				BaseURL:   "https://example.com/v1",
				Model:     "model",
				APIKeyEnv: "CUSTOM_API_KEY",
			},
			APIKey: "test-key",
		})
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		if _, ok := providerInstance.(serviceTestProvider); !ok {
			t.Fatalf("expected serviceTestProvider, got %T", providerInstance)
		}
	})

	t.Run("nil service fails validate", func(t *testing.T) {
		t.Parallel()

		var service *Service
		if err := service.validate(); err == nil {
			t.Fatal("expected validate error for nil service")
		}
	})
}

func TestResolveCurrentModelHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currentModel string
		models       []ModelDescriptor
		fallback     string
		expected     string
		changed      bool
	}{
		{
			name:         "current model in list",
			currentModel: "gpt-4o",
			models: []ModelDescriptor{
				{ID: "gpt-4.1"},
				{ID: "gpt-4o"},
				{ID: "gpt-5.4"},
			},
			fallback: "gpt-4.1",
			expected: "gpt-4o",
			changed:  false,
		},
		{
			name:         "current model falls back to provider default",
			currentModel: "unknown-model",
			models: []ModelDescriptor{
				{ID: "gpt-4.1"},
				{ID: "gpt-4o"},
				{ID: "gpt-5.4"},
			},
			fallback: "gpt-4.1",
			expected: "gpt-4.1",
			changed:  true,
		},
		{
			name:         "missing fallback keeps current model unchanged",
			currentModel: "unknown-model",
			models: []ModelDescriptor{
				{ID: "gpt-4o"},
			},
			fallback: "gpt-4.1",
			expected: "unknown-model",
			changed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, changed := resolveCurrentModel(tt.currentModel, tt.models, tt.fallback)
			if got != tt.expected || changed != tt.changed {
				t.Fatalf(
					"resolveCurrentModel() = (%q, %v), want (%q, %v)",
					got,
					changed,
					tt.expected,
					tt.changed,
				)
			}
		})
	}
}

func TestModelDescriptorsFromIDsHelper(t *testing.T) {
	t.Parallel()

	models := modelDescriptorsFromIDs([]string{"gpt-4.1", "", "gpt-4o"})
	if len(models) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(models))
	}
	if models[0].ID != "gpt-4.1" || models[1].ID != "gpt-4o" {
		t.Fatalf("unexpected descriptors: %+v", models)
	}
}

func testDefaultConfig() *config.Config {
	cfg := config.Default()
	providers := config.DefaultProviders()

	cfg.Providers = providers
	cfg.SelectedProvider = providers[0].Name
	cfg.CurrentModel = providers[0].Model

	return cfg
}

func testDriverDefinition(name string, discover DiscoveryFunc) DriverDefinition {
	return DriverDefinition{
		Name:     name,
		Discover: discover,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
			return serviceTestProvider{}, nil
		},
	}
}

type serviceTestProvider struct{}

func (serviceTestProvider) Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error) {
	return ChatResponse{}, nil
}

type memoryCatalogStore struct {
	mu       sync.Mutex
	catalogs map[string]ModelCatalog
}

func newMemoryCatalogStore() *memoryCatalogStore {
	return &memoryCatalogStore{
		catalogs: map[string]ModelCatalog{},
	}
}

func (s *memoryCatalogStore) Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return ModelCatalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	catalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return ModelCatalog{}, ErrModelCatalogNotFound
	}
	return catalog, nil
}

func (s *memoryCatalogStore) Save(ctx context.Context, catalog ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.catalogs[catalog.Identity.Key()] = catalog
	return nil
}

const (
	testProviderName = "openai"
	testAPIKeyEnv    = "OPENAI_API_KEY"
)
