package provider_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	"neo-code/internal/provider/openai"
)

type stubProvider struct{}

func (stubProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, nil
}

func stubDriver(driverType string) provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: driverType,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return stubProvider{}, nil
		},
	}
}

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()

	registry := provider.NewRegistry()
	if err := registry.Register(openai.Driver()); err != nil {
		t.Fatalf("register openai driver: %v", err)
	}
	return registry
}

func newTestManager(t *testing.T) *config.Manager {
	t.Helper()

	return newTestManagerWithDefaults(t, builtin.DefaultConfig())
}

func newTestManagerWithDefaults(t *testing.T, defaults *config.Config) *config.Manager {
	t.Helper()

	manager := config.NewManager(config.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}

func TestRegistryBuildsRegisteredDriverCaseInsensitively(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)
	got, err := registry.Build(context.Background(), config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{
			Name:      "openai-main",
			Driver:    "OPENAI",
			BaseURL:   config.OpenAIDefaultBaseURL,
			Model:     config.OpenAIDefaultModel,
			APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
		},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := got.(*openai.Provider); !ok {
		t.Fatalf("expected openai.Provider, got %T", got)
	}
}

func TestRegistryUnknownDriverReturnsTypedError(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	_, err := registry.Build(context.Background(), config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{Driver: "missing"},
	})
	if !errors.Is(err, provider.ErrDriverNotFound) {
		t.Fatalf("expected ErrDriverNotFound, got %v", err)
	}
}

func TestRegistryRejectsDuplicateDriverRegistration(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	if err := registry.Register(stubDriver("custom")); err != nil {
		t.Fatalf("initial Register() error = %v", err)
	}
	if err := registry.Register(stubDriver("CUSTOM")); err == nil {
		t.Fatalf("expected duplicate driver registration to fail")
	}
}

func TestServiceListProvidersFallsBackToProviderDefaultModel(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)

	service := provider.NewService(manager, newTestRegistry(t), newCatalogStoreStub())
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
		if item.Description != "" {
			t.Fatalf("expected provider description to stay empty for hidden metadata, got %q", item.Description)
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
	manager := newTestManager(t)
	registry := newTestRegistry(t)

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
	if err := store.Save(context.Background(), provider.ModelCatalog{
		SchemaVersion: 1,
		Identity:      identity,
		Models: []provider.ModelDescriptor{
			{ID: "qiniu-alt", Name: "qiniu-alt"},
		},
	}); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	service := provider.NewService(manager, registry, store)

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

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	selected, err = reloaded.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() after reload error = %v", err)
	}
	if selected.Model != config.QiniuDefaultModel || reloaded.CurrentModel != "qiniu-alt" {
		t.Fatalf("expected current model persistence without overriding provider default, got provider=%q current=%q", selected.Model, reloaded.CurrentModel)
	}
}

func TestServiceModelOperationsUseBuiltinConfigEvenWithoutDriver(t *testing.T) {
	t.Parallel()

	defaults := builtin.DefaultConfig()
	defaults.Providers = []config.ProviderConfig{{
		Name:      config.OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   config.OpenAIDefaultBaseURL,
		Model:     "broken-model",
		APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
	}}
	defaults.SelectedProvider = config.OpenAIName
	defaults.CurrentModel = "broken-model"
	manager := newTestManagerWithDefaults(t, defaults)

	service := provider.NewService(manager, newTestRegistry(t), newCatalogStoreStub())

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

	defaults := builtin.DefaultConfig()
	defaults.Providers = []config.ProviderConfig{{
		Name:      config.OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   config.OpenAIDefaultBaseURL,
		Model:     config.OpenAIDefaultModel,
		APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
	}}
	defaults.SelectedProvider = config.OpenAIName
	defaults.CurrentModel = config.OpenAIDefaultModel
	manager := newTestManagerWithDefaults(t, defaults)

	service := provider.NewService(manager, newTestRegistry(t), newCatalogStoreStub())
	if _, err := service.SelectProvider(context.Background(), config.OpenAIName); !errors.Is(err, provider.ErrDriverNotFound) {
		t.Fatalf("expected SelectProvider() to preserve ErrDriverNotFound, got %v", err)
	}
}

// --- Service.Build 补充测试 ---

func TestServiceBuildDelegatesToRegistry(t *testing.T) {
	t.Parallel()

	registry := newTestRegistry(t)

	resolved := config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{
			Name:      "openai-main",
			Driver:    "openai",
			BaseURL:   config.OpenAIDefaultBaseURL,
			Model:     config.OpenAIDefaultModel,
			APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
		},
		APIKey: "test-key",
	}

	got, err := registry.Build(context.Background(), resolved)
	if err != nil {
		t.Fatalf("Registry.Build() error = %v", err)
	}
	if _, ok := got.(*openai.Provider); !ok {
		t.Fatalf("expected openai.Provider, got %T", got)
	}
}

func TestServiceBuildReturnsErrorOnNilManager(t *testing.T) {
	t.Parallel()

	service := (*provider.Service)(nil)
	_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
	if err == nil {
		t.Fatalf("expected error for nil Service")
	}
}

func TestServiceBuildReturnsErrorOnNilRegistry(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	service := provider.NewService(manager, nil, newCatalogStoreStub())

	_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
	if err == nil {
		t.Fatalf("expected error for nil registry")
	}
}

type catalogStoreStub struct {
	mu       sync.Mutex
	catalogs map[string]provider.ModelCatalog
}

func newCatalogStoreStub() *catalogStoreStub {
	return &catalogStoreStub{
		catalogs: map[string]provider.ModelCatalog{},
	}
}

func (s *catalogStoreStub) Load(ctx context.Context, identity config.ProviderIdentity) (provider.ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return provider.ModelCatalog{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	catalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return provider.ModelCatalog{}, provider.ErrModelCatalogNotFound
	}
	return catalog, nil
}

func (s *catalogStoreStub) Save(ctx context.Context, catalog provider.ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalogs[catalog.Identity.Key()] = catalog
	return nil
}
