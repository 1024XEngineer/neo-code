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

func TestServiceListProvidersFiltersUnsupportedDrivers(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			Name:      "unsupported",
			Driver:    "custom",
			BaseURL:   "https://example.com/v1",
			Model:     "custom-model",
			APIKeyEnv: "CUSTOM_API_KEY",
		})
		return nil
	}); err != nil {
		t.Fatalf("append provider: %v", err)
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
	if len(items) != 4 {
		t.Fatalf("expected 4 supported builtin providers, got %d", len(items))
	}
	for _, item := range items {
		if len(item.Models) != 1 {
			t.Fatalf("expected default fallback model for %q, got %+v", item.ID, item.Models)
		}
	}
}

func TestServiceSelectProviderFallsBackToProviderDefault(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, config.ProviderConfig{
		Name:      "custom-main",
		Driver:    "custom",
		BaseURL:   "https://example.com/v1",
		Model:     "custom-model",
		Models:    []string{"custom-model", "custom-alt"},
		APIKeyEnv: "CUSTOM_API_KEY",
	})

	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
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
	if err := registry.Register(testDriverDefinition("custom", nil)); err != nil {
		t.Fatalf("register custom driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	selection, err := service.SelectProvider(context.Background(), "custom-main")
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != "custom-main" || selection.ModelID != "custom-model" {
		t.Fatalf("unexpected selection: %+v", selection)
	}
}

func TestServiceListModelsUsesConfiguredModelsWithoutDiscovery(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			Name:      "broken-provider",
			Driver:    "missing-driver",
			BaseURL:   "https://example.com/v1",
			Model:     "broken-model",
			Models:    []string{"broken-model", "broken-alt"},
			APIKeyEnv: "BROKEN_API_KEY",
		})
		cfg.SelectedProvider = "broken-provider"
		cfg.CurrentModel = "broken-model"
		return nil
	}); err != nil {
		t.Fatalf("append provider: %v", err)
	}

	service := NewService(manager, NewRegistry(), newMemoryCatalogStore())
	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 || models[1].ID != "broken-alt" {
		t.Fatalf("expected configured models fallback, got %+v", models)
	}
}

func TestServiceSetCurrentModelUsesConfiguredModels(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers[0].Models = []string{cfg.Providers[0].Model, "gpt-4o"}
		return nil
	}); err != nil {
		t.Fatalf("seed provider models: %v", err)
	}

	service := NewService(manager, NewRegistry(), newMemoryCatalogStore())
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
			Metadata: map[string]any{
				"id":                "server-model",
				"context_window":    float64(32000),
				"max_output_tokens": float64(4096),
			},
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
	t.Setenv("CUSTOM_API_KEY", "test-key")

	defaults := testDefaultConfig()
	defaults.Providers = []config.ProviderConfig{{
		Name:      "custom",
		Driver:    "custom",
		BaseURL:   "https://example.com/v1",
		Model:     "fallback-model",
		APIKeyEnv: "CUSTOM_API_KEY",
	}}
	defaults.SelectedProvider = "custom"
	defaults.CurrentModel = "fallback-model"

	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	identity, err := defaults.Providers[0].Identity()
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
	if err := registry.Register(testDriverDefinition("custom", func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
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

func TestSelectModelHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currentModel string
		models       []string
		fallback     string
		expected     string
	}{
		{
			name:         "current model in list",
			currentModel: "gpt-4o",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4o",
		},
		{
			name:         "current model not in list",
			currentModel: "unknown-model",
			models:       []string{"gpt-4.1", "gpt-4o", "gpt-5.4"},
			fallback:     "gpt-4.1",
			expected:     "gpt-4.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := selectModel(tt.currentModel, tt.models, tt.fallback); got != tt.expected {
				t.Fatalf("selectModel() = %q, want %q", got, tt.expected)
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
