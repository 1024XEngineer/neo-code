package provider_test

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	"neo-code/internal/provider/gemini"
	"neo-code/internal/provider/openai"
	"neo-code/internal/provider/openll"
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
			BaseURL:   openai.DefaultBaseURL,
			Model:     openai.DefaultModel,
			APIKeyEnv: openai.DefaultAPIKeyEnv,
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

func TestServiceListProvidersUsesConfiguredMetadata(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			Name:      "unsupported",
			Driver:    "custom",
			BaseURL:   "https://example.com",
			Model:     "custom-model",
			Models:    []string{"custom-model"},
			APIKeyEnv: "CUSTOM_API_KEY",
		})
		return nil
	}); err != nil {
		t.Fatalf("append provider: %v", err)
	}

	service := provider.NewService(manager, newTestRegistry(t))
	items, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	expectedModels := map[string]int{
		openai.Name: len(openai.BuiltinConfig().Models),
		gemini.Name: len(gemini.BuiltinConfig().Models),
		openll.Name: len(openll.BuiltinConfig().Models),
	}
	if len(items) != len(expectedModels) {
		t.Fatalf("expected only supported providers, got %d", len(items))
	}

	for _, item := range items {
		wantModels, ok := expectedModels[item.ID]
		if !ok {
			t.Fatalf("unexpected supported provider %q", item.ID)
		}
		if item.Description != "" {
			t.Fatalf("expected provider description to stay empty for hidden metadata, got %q", item.Description)
		}
		if len(item.Models) != wantModels {
			t.Fatalf("expected provider models to come from config, got %+v", item.Models)
		}
		delete(expectedModels, item.ID)
	}
	if len(expectedModels) != 0 {
		t.Fatalf("missing supported providers from catalog: %+v", expectedModels)
	}
}

func TestServiceSelectProviderAndSetCurrentModel(t *testing.T) {
	defaults := builtin.DefaultConfig()
	defaults.Providers = append(defaults.Providers, config.ProviderConfig{
		Name:      "custom-main",
		Driver:    "custom",
		BaseURL:   "https://example.com",
		Model:     "custom-model",
		Models:    []string{"custom-model", "custom-alt"},
		APIKeyEnv: "CUSTOM_API_KEY",
	})
	manager := newTestManagerWithDefaults(t, defaults)
	registry := newTestRegistry(t)
	if err := registry.Register(stubDriver("custom")); err != nil {
		t.Fatalf("register stub driver: %v", err)
	}

	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	service := provider.NewService(manager, registry)

	selection, err := service.SelectProvider(context.Background(), "custom-main")
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != "custom-main" || selection.ModelID != "custom-model" {
		t.Fatalf("unexpected selection after switch: %+v", selection)
	}

	if _, err := service.SetCurrentModel(context.Background(), "missing-model"); !errors.Is(err, provider.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}

	selection, err = service.SetCurrentModel(context.Background(), "custom-alt")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != "custom-alt" {
		t.Fatalf("expected selected model %q, got %+v", "custom-alt", selection)
	}

	cfg := manager.Get()
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if selected.Name != "custom-main" {
		t.Fatalf("expected selected provider %q, got %+v", "custom-main", selected)
	}
	if selected.Model != "custom-model" || cfg.CurrentModel != "custom-alt" {
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
	if selected.Model != "custom-model" || reloaded.CurrentModel != "custom-alt" {
		t.Fatalf("expected current model persistence without overriding provider default, got provider=%q current=%q", selected.Model, reloaded.CurrentModel)
	}
}

func TestServiceModelOperationsUseProviderConfigEvenWithoutDriver(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			Name:      "broken-provider",
			Driver:    "missing-driver",
			BaseURL:   "https://example.com",
			Model:     "broken-model",
			Models:    []string{"broken-model", "broken-alt"},
			APIKeyEnv: "BROKEN_API_KEY",
		})
		cfg.SelectedProvider = "broken-provider"
		cfg.CurrentModel = "broken-model"
		return nil
	}); err != nil {
		t.Fatalf("append broken provider: %v", err)
	}

	service := provider.NewService(manager, newTestRegistry(t))

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 || models[1].ID != "broken-alt" {
		t.Fatalf("expected models from provider config, got %+v", models)
	}

	selection, err := service.SetCurrentModel(context.Background(), "broken-alt")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != "broken-alt" {
		t.Fatalf("expected current model to update from provider config, got %+v", selection)
	}
}

func TestServiceSelectProviderRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Providers = append(cfg.Providers, config.ProviderConfig{
			Name:      "broken-provider",
			Driver:    "missing-driver",
			BaseURL:   "https://example.com",
			Model:     "broken-model",
			Models:    []string{"broken-model"},
			APIKeyEnv: "BROKEN_API_KEY",
		})
		return nil
	}); err != nil {
		t.Fatalf("append broken provider: %v", err)
	}

	service := provider.NewService(manager, newTestRegistry(t))
	if _, err := service.SelectProvider(context.Background(), "broken-provider"); !errors.Is(err, provider.ErrDriverNotFound) {
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
			BaseURL:   openai.DefaultBaseURL,
			Model:     openai.DefaultModel,
			APIKeyEnv: openai.DefaultAPIKeyEnv,
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
	service := provider.NewService(manager, nil)

	_, err := service.Build(context.Background(), config.ResolvedProviderConfig{})
	if err == nil {
		t.Fatalf("expected error for nil registry")
	}
}
