package state

import (
	"context"
	"errors"
	"strings"
	"testing"

	configpkg "neo-code/internal/config"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// ---- state/model.go 扩展边界测试 ----

func TestSelectionFromConfig(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{
		SelectedProvider: "test-provider",
		CurrentModel:     "test-model",
	}
	sel := selectionFromConfig(cfg)
	if sel.ProviderID != "test-provider" {
		t.Fatalf("expected ProviderID=test-provider, got %q", sel.ProviderID)
	}
	if sel.ModelID != "test-model" {
		t.Fatalf("expected ModelID=test-model, got %q", sel.ModelID)
	}
}

func TestSelectionFromConfigEmptyFields(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{}
	sel := selectionFromConfig(cfg)
	if sel.ProviderID != "" || sel.ModelID != "" {
		t.Fatalf("expected empty selection for empty config, got %+v", sel)
	}
}

func TestResolveCurrentModelAdditionalEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currentModel string
		models       []providertypes.ModelDescriptor
		fallback     string
		expected     string
		changed      bool
	}{
		{
			name:         "empty current returns fallback",
			currentModel: "",
			models:       []providertypes.ModelDescriptor{{ID: "gpt-4o"}, {ID: "gpt-5.4"}},
			fallback:     "gpt-5.4",
			expected:     "gpt-5.4",
			changed:      true,
		},
		{
			name:         "whitespace current trimmed",
			currentModel: "   gpt-4o   ",
			models:       []providertypes.ModelDescriptor{{ID: "gpt-4o"}},
			fallback:     "fallback",
			expected:     "gpt-4o",
			changed:      false,
		},
		{
			name:         "empty models list returns current unchanged",
			currentModel: "gpt-4o",
			models:       []providertypes.ModelDescriptor{},
			fallback:     "fallback",
			expected:     "gpt-4o",
			changed:      false,
		},
		{
			name:         "nil models list returns current unchanged",
			currentModel: "gpt-4o",
			models:       nil,
			fallback:     "fallback",
			expected:     "gpt-4o",
			changed:      false,
		},
		{
			name:         "current not found, fallback also not found, uses first available",
			currentModel: "unknown",
			models:       []providertypes.ModelDescriptor{{ID: "first"}, {ID: "second"}},
			fallback:     "also-unknown",
			expected:     "first",
			changed:      true,
		},
		{
			name:         "current not found, empty fallback, uses first available",
			currentModel: "unknown",
			models:       []providertypes.ModelDescriptor{{ID: "first"}, {ID: "second"}},
			fallback:     "",
			expected:     "first",
			changed:      true,
		},
		{
			name:         "model with whitespace ID matches after trim",
			currentModel: "gpt-4o",
			models:       []providertypes.ModelDescriptor{{ID: "  gpt-4o  "}},
			fallback:     "fallback",
			expected:     "gpt-4o",
			changed:      false,
		},
		{
			name:         "empty current and empty fallback, use first non-empty model",
			currentModel: "",
			models:       []providertypes.ModelDescriptor{{ID: ""}, {ID: "real-model"}},
			fallback:     "",
			expected:     "real-model",
			changed:      true,
		},
		{
			name:         "all models have empty IDs, return current unchanged",
			currentModel: "some-model",
			models:       []providertypes.ModelDescriptor{{ID: ""}, {ID: "  "}},
			fallback:     "",
			expected:     "some-model",
			changed:      false,
		},
		{
			name:         "whitespace fallback is treated as empty",
			currentModel: "missing",
			models:       []providertypes.ModelDescriptor{{ID: "found"}},
			fallback:     "   ",
			expected:     "found",
			changed:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, changed := resolveCurrentModel(tt.currentModel, tt.models, tt.fallback)
			if got != tt.expected || changed != tt.changed {
				t.Fatalf("resolveCurrentModel() = (%q, %v), want (%q, %v)",
					got, changed, tt.expected, tt.changed)
			}
		})
	}
}

func TestContainsModelDescriptorID(t *testing.T) {
	t.Parallel()

	models := []providertypes.ModelDescriptor{
		{ID: "gpt-4o"},
		{ID: "gemini-pro"},
	}
	if !containsModelDescriptorID(models, "GPT-4O") {
		t.Fatal("expected case-insensitive match")
	}
	if containsModelDescriptorID(models, "claude") {
		t.Fatal("expected no match")
	}
	if containsModelDescriptorID(nil, "gpt-4o") {
		t.Fatal("expected nil models to return false")
	}
	if containsModelDescriptorID(models, "") {
		t.Fatal("expected empty ID to return false")
	}
	if containsModelDescriptorID(models, "  ") {
		t.Fatal("expected whitespace ID to return false")
	}
}

func TestProviderOptionNormalizesFields(t *testing.T) {
	t.Parallel()

	option := providerOption(configpkg.ProviderConfig{Name: "  OpenAI  "}, []providertypes.ModelDescriptor{
		{ID: "gpt-4o", Name: "GPT-4o"},
	})
	if option.ID != "OpenAI" {
		t.Fatalf("expected trimmed ID, got %q", option.ID)
	}
	if option.Name != "OpenAI" {
		t.Fatalf("expected trimmed Name, got %q", option.Name)
	}
	if len(option.Models) == 0 {
		t.Fatal("expected models to be populated")
	}
}

func TestSelectedProviderConfigEmptySelection(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{}
	_, err := selectedProviderConfig(cfg)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound for empty selection, got %v", err)
	}
}

func TestSelectedProviderConfigNotFound(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{
		SelectedProvider: "nonexistent",
		Providers: []configpkg.ProviderConfig{
			{Name: "openai"},
		},
	}
	_, err := selectedProviderConfig(cfg)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestEnsureSupportedProvider(t *testing.T) {
	t.Parallel()

	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}

	err := ensureSupportedProvider(supporters, configpkg.ProviderConfig{
		Name:   "test",
		Driver: "anthropic",
	})
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported for anthropic driver, got %v", err)
	}
	if !strings.Contains(err.Error(), `"test"`) {
		t.Fatalf("expected error to contain provider name, got %v", err)
	}
	if !strings.Contains(err.Error(), `"anthropic"`) {
		t.Fatalf("expected error to contain driver name, got %v", err)
	}

	// 支持的 driver 不应返回错误。
	err = ensureSupportedProvider(supporters, configpkg.ProviderConfig{
		Name:   "test",
		Driver: "openaicompat",
	})
	if err != nil {
		t.Fatalf("expected no error for supported driver, got %v", err)
	}
}

// ---- state/service.go 扩展测试 ----

type additionalCatalogStub struct{}

func (additionalCatalogStub) ListProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (additionalCatalogStub) ListProviderModelsSnapshot(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (additionalCatalogStub) ListProviderModelsCached(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (additionalCatalogStub) RefreshProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

type denyAllDriverSupporter struct{}

func (*denyAllDriverSupporter) Supports(_ string) bool { return false }

func TestSelectionServiceSelectProviderNoModelsAvailable(t *testing.T) {
	t.Parallel()

	// 使用自定义 provider，避免 builtin provider 在 snapshot 为空时回退默认模型。
	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "empty-model-provider",
		Driver:                "openaicompat",
		BaseURL:               "https://example.com/v1",
		APIKeyEnv:             "EMPTY_PROVIDER_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), additionalCatalogStub{})

	_, err := service.SelectProvider(context.Background(), "empty-model-provider")
	if !errors.Is(err, ErrNoModelsAvailable) {
		t.Fatalf("expected ErrNoModelsAvailable, got %v", err)
	}
}

func TestSelectionServiceSetCurrentModelNoModelsAvailable(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), additionalCatalogStub{})

	_, err := service.SetCurrentModel(context.Background(), "some-model")
	if !errors.Is(err, ErrNoModelsAvailable) {
		t.Fatalf("expected ErrNoModelsAvailable, got %v", err)
	}
}

func TestSelectionServiceListModelsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	supporters := &denyAllDriverSupporter{}
	service := NewService(manager, supporters, newCatalogStub())

	_, err := service.ListModels(context.Background())
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}
}

func TestSelectionServiceEnsureSelectionBootstrapInitialWhenNoSelection(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.SelectedProvider = ""
	defaults.CurrentModel = ""

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() bootstrap error = %v", err)
	}
	if selection.ProviderID == "" {
		t.Fatal("expected non-empty ProviderID after bootstrap")
	}

	reloaded, _ := manager.Load(context.Background())
	if reloaded.SelectedProvider == "" {
		t.Fatal("expected persisted selection after bootstrap")
	}
}

func TestSelectionServiceEnsureSelectionNoSupportedProviders(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.SelectedProvider = ""

	manager := newSelectionTestManager(t, defaults)
	supporters := &denyAllDriverSupporter{}
	service := NewService(manager, supporters, newCatalogStub())

	_, err := service.EnsureSelection(context.Background())
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound when no supported providers, got %v", err)
	}
}

func TestSelectionServiceListProviderOptionsSkipsUnsupportedDrivers(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	supporters := &denyAllDriverSupporter{}
	service := NewService(manager, supporters, newCatalogStub())

	options, err := service.ListProviderOptions(context.Background())
	if err != nil {
		t.Fatalf("ListProviderOptions() error = %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("expected zero options when all drivers unsupported, got %d", len(options))
	}
}

// ---- catalogInputFromProvider 相关测试 ----

func TestCatalogInputFromProviderCustomWithoutModelField(t *testing.T) {
	t.Setenv("CUSTOM_NO_MODEL_KEY", "secret-key")

	input, err := catalogInputFromProvider(configpkg.ProviderConfig{
		Name:      "custom-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "CUSTOM_NO_MODEL_KEY",
		Source:    configpkg.ProviderSourceCustom,
		// custom provider 可以不声明默认 model。
	})
	if err != nil {
		t.Fatalf("catalogInputFromProvider() error = %v", err)
	}
	if input.DefaultModels != nil {
		t.Fatalf("expected custom provider without model to have nil DefaultModels, got %+v", input.DefaultModels)
	}
}

// ---- EnsureSelection 在自定义 provider 场景下的回退测试 ----

func TestEnsureSelectionCustomProviderWithSnapshotModels(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "custom-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://example.com/v1",
		APIKeyEnv:             "CUSTOM_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "custom-gateway"
	defaults.CurrentModel = "unknown-model"

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStubForAdditional{
		snapshotModels: []providertypes.ModelDescriptor{
			{ID: "server-model-1"},
		},
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ModelID != "server-model-1" {
		t.Fatalf("expected fallback to snapshot model, got %q", selection.ModelID)
	}
}

// ---- SelectProvider 失败路径测试 ----

func TestSelectProviderUpdateFailsPreservesState(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	// 先加载初始配置。
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, &failingDriverSupporter{}, newCatalogStub())

	_, err := service.SelectProvider(context.Background(), "company-gateway")
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}

	cfg := manager.Get()
	if cfg.SelectedProvider == "company-gateway" {
		t.Fatal("expected selected provider to stay unchanged after failed select")
	}
}

// ---- SetCurrentModel 閺囧瓨鏌婇崘鍛村劥閺嶏繝鐛欐径杈Е ----

func TestSetCurrentModelInternalValidationFails(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())

	// 通过 manager 注入一个无效的当前 provider。
	err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = "nonexistent-provider"
		return nil
	})
	if err != nil {
		t.Fatalf("setup update failed: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())
	// SetCurrentModel 会先读取当前 provider，随后在 Update 期间再次校验并失败。
	_, err = service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
	if err == nil {
		t.Fatal("expected error when current selection is invalid")
	}
}

// ---- 测试桩定义 ----

type failingDriverSupporter struct{}

func (*failingDriverSupporter) Supports(_ string) bool { return false }

type catalogMethodsStubForAdditional struct {
	listModels     []providertypes.ModelDescriptor
	snapshotModels []providertypes.ModelDescriptor
	cachedModels   []providertypes.ModelDescriptor
	listErr        error
	snapshotErr    error
	cachedErr      error
	tracker        *catalogMethodCallsForAdditional
}

func (s catalogMethodsStubForAdditional) ListProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.listCalls++
	}
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listModels, nil
}

func (s catalogMethodsStubForAdditional) ListProviderModelsSnapshot(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.snapshotCalls++
	}
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	return s.snapshotModels, nil
}

func (s catalogMethodsStubForAdditional) ListProviderModelsCached(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.cachedCalls++
	}
	if s.cachedErr != nil {
		return nil, s.cachedErr
	}
	return s.cachedModels, nil
}

func (s catalogMethodsStubForAdditional) RefreshProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listModels, nil
}

type catalogMethodCallsForAdditional struct {
	listCalls     int
	snapshotCalls int
	cachedCalls   int
}
