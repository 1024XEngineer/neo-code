package selection

import (
	"context"
	"errors"
	"sync"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	providercatalog "neo-code/internal/provider/catalog"
)

func TestServiceListProvidersFallsBackToProviderDefaultModel(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.DefaultConfig())
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	items, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	expected := map[string]int{
		config.OpenAIName: 1,
		config.GeminiName: 1,
		config.OpenLLName: 1,
		config.QiniuName:  1,
	}
	if len(items) != len(expected) {
		t.Fatalf("expected only builtin providers, got %d", len(items))
	}

	for _, item := range items {
		wantModels, ok := expected[item.ID]
		if !ok {
			t.Fatalf("unexpected builtin provider %q", item.ID)
		}
		if len(item.Models) != wantModels {
			t.Fatalf("expected provider models to fall back to the default model, got %+v", item.Models)
		}
		delete(expected, item.ID)
	}
	if len(expected) != 0 {
		t.Fatalf("missing builtin providers from catalog: %+v", expected)
	}
}

func TestServiceSelectProviderAndSetCurrentModel(t *testing.T) {
	manager := newTestManager(t, config.DefaultConfig())
	registry := newRegistry(t, config.OpenAIName, nil)

	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	store := newCatalogStoreStub()
	identity, err := config.QiniuProvider().Identity()
	if err != nil {
		t.Fatalf("qiniu provider identity: %v", err)
	}
	if err := store.Save(context.Background(), providercatalog.ModelCatalog{
		SchemaVersion: providercatalog.SchemaVersion,
		Identity:      identity,
		Models: []provider.ModelDescriptor{
			{ID: "qiniu-alt", Name: "qiniu-alt"},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	service := NewService(manager, registry, providercatalog.NewService("", registry, store))

	selection, err := service.SelectProvider(context.Background(), config.QiniuName)
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != config.QiniuName || selection.ModelID != config.QiniuDefaultModel {
		t.Fatalf("unexpected selection after switch: %+v", selection)
	}

	if _, err := service.SetCurrentModel(context.Background(), "missing-model"); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}

	selection, err = service.SetCurrentModel(context.Background(), "qiniu-alt")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != "qiniu-alt" {
		t.Fatalf("expected selected model %q, got %+v", "qiniu-alt", selection)
	}

	cfg := manager.Get()
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if selected.Name != config.QiniuName {
		t.Fatalf("expected selected provider %q, got %+v", config.QiniuName, selected)
	}
	if selected.Model != config.QiniuDefaultModel || cfg.CurrentModel != "qiniu-alt" {
		t.Fatalf("expected provider default and current model to diverge safely, got provider=%q current=%q", selected.Model, cfg.CurrentModel)
	}
}

func TestServiceEnsureSelectionRepairsInvalidCurrentModel(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.DefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("seed invalid current model: %v", err)
	}

	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

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

func TestServiceModelOperationsUseBuiltinConfigEvenWithoutDriver(t *testing.T) {
	t.Parallel()

	defaults := config.DefaultConfig()
	defaults.Providers = []config.ProviderConfig{{
		Name:      config.OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   config.OpenAIDefaultBaseURL,
		Model:     "broken-model",
		APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
	}}
	defaults.SelectedProvider = config.OpenAIName
	defaults.CurrentModel = "broken-model"

	manager := newTestManager(t, defaults)
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "broken-model" {
		t.Fatalf("expected default model fallback, got %+v", models)
	}

	if _, err := service.SetCurrentModel(context.Background(), "broken-alt"); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("expected SetCurrentModel() to reject undiscovered model, got %v", err)
	}
}

func TestServiceSelectProviderRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	defaults := config.DefaultConfig()
	defaults.Providers = []config.ProviderConfig{{
		Name:      config.OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   config.OpenAIDefaultBaseURL,
		Model:     config.OpenAIDefaultModel,
		APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
	}}
	defaults.SelectedProvider = config.OpenAIName
	defaults.CurrentModel = config.OpenAIDefaultModel

	manager := newTestManager(t, defaults)
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	if _, err := service.SelectProvider(context.Background(), config.OpenAIName); !errors.Is(err, provider.ErrDriverNotFound) {
		t.Fatalf("expected SelectProvider() to preserve ErrDriverNotFound, got %v", err)
	}
}

func TestServiceListModelsSnapshot(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.DefaultConfig())
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	models, err := service.ListModelsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ListModelsSnapshot() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != config.OpenAIDefaultModel {
		t.Fatalf("expected default model fallback, got %+v", models)
	}
}

func TestServiceValidateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		service *Service
		errMsg  string
	}{
		{
			name:    "nil service",
			service: nil,
			errMsg:  "provider selection: service is nil",
		},
		{
			name: "nil manager",
			service: &Service{
				manager:  nil,
				registry: provider.NewRegistry(),
				catalogs: providercatalog.NewService("", provider.NewRegistry(), newCatalogStoreStub()),
			},
			errMsg: "provider selection: config manager is nil",
		},
		{
			name: "nil registry",
			service: &Service{
				manager:  config.NewManager(config.NewLoader(t.TempDir(), config.DefaultConfig())),
				registry: nil,
				catalogs: providercatalog.NewService("", provider.NewRegistry(), newCatalogStoreStub()),
			},
			errMsg: "provider selection: registry is nil",
		},
		{
			name: "nil catalogs",
			service: &Service{
				manager:  config.NewManager(config.NewLoader(t.TempDir(), config.DefaultConfig())),
				registry: provider.NewRegistry(),
				catalogs: nil,
			},
			errMsg: "provider selection: catalog service is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.service.ListProviders(context.Background())
			if err == nil || err.Error() != tt.errMsg {
				t.Fatalf("expected error %q, got %v", tt.errMsg, err)
			}
		})
	}
}

func TestServiceOperationsWithCanceledContext(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.DefaultConfig())
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		fn   func(context.Context) error
	}{
		{
			name: "ListProviders",
			fn: func(ctx context.Context) error {
				_, err := service.ListProviders(ctx)
				return err
			},
		},
		{
			name: "ListModels",
			fn: func(ctx context.Context) error {
				_, err := service.ListModels(ctx)
				return err
			},
		},
		{
			name: "ListModelsSnapshot",
			fn: func(ctx context.Context) error {
				_, err := service.ListModelsSnapshot(ctx)
				return err
			},
		},
		{
			name: "EnsureSelection",
			fn: func(ctx context.Context) error {
				_, err := service.EnsureSelection(ctx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.fn(ctx)
			if err == nil {
				t.Fatalf("expected error for canceled context")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", err)
			}
		})
	}
}

func TestServiceSetCurrentModelEmptyModelID(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.DefaultConfig())
	registry := newRegistry(t, config.OpenAIName, nil)
	service := NewService(manager, registry, providercatalog.NewService("", registry, newCatalogStoreStub()))

	_, err := service.SetCurrentModel(context.Background(), "")
	if !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for empty model ID, got %v", err)
	}

	_, err = service.SetCurrentModel(context.Background(), "   ")
	if !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for whitespace model ID, got %v", err)
	}
}

func TestResolveCurrentModelHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currentModel string
		models       []provider.ModelDescriptor
		fallback     string
		expected     string
		changed      bool
	}{
		{
			name:         "current model in list",
			currentModel: "gpt-4o",
			models: []provider.ModelDescriptor{
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
			models: []provider.ModelDescriptor{
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
			models: []provider.ModelDescriptor{
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
				t.Fatalf("resolveCurrentModel() = (%q, %v), want (%q, %v)", got, changed, tt.expected, tt.changed)
			}
		})
	}
}

func newTestManager(t *testing.T, defaults *config.Config) *config.Manager {
	t.Helper()

	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}

func newRegistry(t *testing.T, name string, discover provider.DiscoveryFunc) *provider.Registry {
	t.Helper()

	registry := provider.NewRegistry()
	if err := registry.Register(provider.DriverDefinition{
		Name:     name,
		Discover: discover,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return selectionTestProvider{}, nil
		},
	}); err != nil {
		t.Fatalf("register driver: %v", err)
	}
	return registry
}

type selectionTestProvider struct{}

func (selectionTestProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, nil
}

type catalogStoreStub struct {
	mu       sync.Mutex
	catalogs map[string]providercatalog.ModelCatalog
}

func newCatalogStoreStub() *catalogStoreStub {
	return &catalogStoreStub{
		catalogs: map[string]providercatalog.ModelCatalog{},
	}
}

func (s *catalogStoreStub) Load(ctx context.Context, identity config.ProviderIdentity) (providercatalog.ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return providercatalog.ModelCatalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	catalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return providercatalog.ModelCatalog{}, providercatalog.ErrCatalogNotFound
	}
	return catalog, nil
}

func (s *catalogStoreStub) Save(ctx context.Context, catalog providercatalog.ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogs[catalog.Identity.Key()] = catalog
	return nil
}
