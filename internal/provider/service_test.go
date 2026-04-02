package provider

import (
	"context"
	"errors"
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

func TestServiceSetCurrentModelRetriesWhenSelectedProviderChangesDuringUpdate(t *testing.T) {
	t.Parallel()

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, nil)); err != nil {
		t.Fatalf("register openai driver: %v", err)
	}

	store := newBlockingCatalogStore()
	openAIIdentity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("openai identity: %v", err)
	}
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: modelCatalogSchemaVersion,
		Identity:      openAIIdentity,
		Models: []ModelDescriptor{
			{ID: config.OpenAIDefaultModel, Name: config.OpenAIDefaultModel},
			{ID: "gpt-4o", Name: "gpt-4o"},
		},
	}); err != nil {
		t.Fatalf("seed openai model catalog: %v", err)
	}

	service := NewService(manager, registry, store)

	type result struct {
		selection ProviderSelection
		err       error
	}
	resultCh := make(chan result, 1)
	go func() {
		selection, err := service.SetCurrentModel(context.Background(), "gpt-4o")
		resultCh <- result{selection: selection, err: err}
	}()

	<-store.loadStarted
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = config.QiniuName
		cfg.CurrentModel = config.QiniuDefaultModel
		return nil
	}); err != nil {
		t.Fatalf("switch selected provider: %v", err)
	}
	close(store.releaseLoad)

	outcome := <-resultCh
	if !errors.Is(outcome.err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound after provider switch, got selection=%+v err=%v", outcome.selection, outcome.err)
	}

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.SelectedProvider != config.QiniuName || reloaded.CurrentModel != config.QiniuDefaultModel {
		t.Fatalf("expected qiniu selection to stay intact, got provider=%q model=%q", reloaded.SelectedProvider, reloaded.CurrentModel)
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

func TestServiceListModelsSnapshotReturnsDefaultAndRefreshesInBackgroundOnMiss(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	store := newMemoryCatalogStore()
	refreshed := make(chan struct{}, 1)
	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return []ModelDescriptor{{ID: "gpt-4o", Name: "GPT-4o"}}, nil
	})); err != nil {
		t.Fatalf("register discovery driver: %v", err)
	}

	service := NewService(manager, registry, store)
	service.backgroundTimeout = time.Second

	models, err := service.ListModelsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ListModelsSnapshot() error = %v", err)
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
		catalog, err := store.Load(context.Background(), identity)
		if err == nil && containsModelDescriptorID(catalog.Models, "gpt-4o") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	catalog, err := store.Load(context.Background(), identity)
	if err != nil {
		t.Fatalf("Load() refreshed catalog error = %v", err)
	}
	t.Fatalf("expected refreshed catalog to contain gpt-4o, got %+v", catalog.Models)
}

func TestServiceSetCurrentModelUsesCachedDiscoveredModels(t *testing.T) {
	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	store := newMemoryCatalogStore()
	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if err := store.Save(context.Background(), ModelCatalog{
		SchemaVersion: modelCatalogSchemaVersion,
		Identity:      identity,
		Models: []ModelDescriptor{
			{ID: config.OpenAIDefaultModel, Name: config.OpenAIDefaultModel},
			{ID: "gpt-4o", Name: "GPT-4o"},
		},
	}); err != nil {
		t.Fatalf("seed model catalog: %v", err)
	}

	service := NewService(manager, NewRegistry(), store)
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

func TestServiceQueueRefreshReplacesStaleInFlightTask(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "test-key")

	manager := config.NewManager(config.NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	refreshed := make(chan struct{}, 1)
	registry := NewRegistry()
	if err := registry.Register(testDriverDefinition(config.OpenAIName, func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return nil, nil
	})); err != nil {
		t.Fatalf("register discovery driver: %v", err)
	}

	service := NewService(manager, registry, newMemoryCatalogStore())
	service.backgroundTimeout = 100 * time.Millisecond
	service.refreshLease = 10 * time.Millisecond

	identity, err := config.OpenAIProvider().Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	service.inFlightByID[identity.Key()] = refreshTask{
		token:     41,
		startedAt: service.now().Add(-time.Minute),
	}

	service.queueRefresh(config.OpenAIProvider())

	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("expected stale in-flight refresh entry to be replaced")
	}
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
			name:         "missing fallback uses first discovered model",
			currentModel: "unknown-model",
			models: []ModelDescriptor{
				{ID: "gpt-4o"},
			},
			fallback: "gpt-4.1",
			expected: "gpt-4o",
			changed:  true,
		},
		{
			name:         "empty models fall back to provider default",
			currentModel: "unknown-model",
			fallback:     "gpt-4.1",
			expected:     "gpt-4.1",
			changed:      true,
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

func testDriverDefinition(name string, discover func(context.Context, config.ResolvedProviderConfig) ([]ModelDescriptor, error)) DriverDefinition {
	return DriverDefinition{
		Name: name,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
			return serviceTestProvider{
				discover: func(discoverCtx context.Context) ([]ModelDescriptor, error) {
					if discover == nil {
						return nil, nil
					}
					return discover(discoverCtx, cfg)
				},
			}, nil
		},
	}
}

type serviceTestProvider struct {
	discover func(context.Context) ([]ModelDescriptor, error)
}

func (serviceTestProvider) Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error) {
	return ChatResponse{}, nil
}

func (p serviceTestProvider) DiscoverModels(ctx context.Context) ([]ModelDescriptor, error) {
	if p.discover == nil {
		return nil, nil
	}
	return p.discover(ctx)
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

type blockingCatalogStore struct {
	base        *memoryCatalogStore
	loadStarted chan struct{}
	releaseLoad chan struct{}
	once        sync.Once
}

func newBlockingCatalogStore() *blockingCatalogStore {
	return &blockingCatalogStore{
		base:        newMemoryCatalogStore(),
		loadStarted: make(chan struct{}),
		releaseLoad: make(chan struct{}),
	}
}

func (s *blockingCatalogStore) Load(ctx context.Context, identity config.ProviderIdentity) (ModelCatalog, error) {
	s.once.Do(func() {
		close(s.loadStarted)
		<-s.releaseLoad
	})
	return s.base.Load(ctx, identity)
}

func (s *blockingCatalogStore) Save(ctx context.Context, catalog ModelCatalog) error {
	return s.base.Save(ctx, catalog)
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
