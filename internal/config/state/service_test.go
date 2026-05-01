package state

import (
	"context"
	"errors"
	"strings"
	"testing"

	configpkg "neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/catalog"
	"neo-code/internal/provider/openaicompat"
	providertypes "neo-code/internal/provider/types"
)

const (
	OpenAIName           = configpkg.OpenAIName
	GeminiName           = configpkg.GeminiName
	QiniuName            = configpkg.QiniuName
	ModelScopeName       = configpkg.ModelScopeName
	QiniuDefaultModel    = configpkg.QiniuDefaultModel
	OpenAIDefaultModel   = configpkg.OpenAIDefaultModel
	ProviderSourceCustom = configpkg.ProviderSourceCustom
)

func testDefaultConfig() *configpkg.Config {
	cfg := configpkg.StaticDefaults()
	cfg.Providers = configpkg.DefaultProviders()
	if len(cfg.Providers) > 0 {
		cfg.SelectedProvider = cfg.Providers[0].Name
		cfg.CurrentModel = cfg.Providers[0].Model
	}
	return cfg
}

func openAIProviderForTest() configpkg.ProviderConfig { return configpkg.OpenAIProvider() }

func selectedProviderConfigForTest(cfg configpkg.Config) (configpkg.ProviderConfig, error) {
	if strings.TrimSpace(cfg.SelectedProvider) == "" {
		return configpkg.ProviderConfig{}, errors.New("selected provider is empty")
	}
	return cfg.ProviderByName(cfg.SelectedProvider)
}

func TestSelectionServiceListProviderOptionsUsesCatalogModels(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	items, err := service.ListProviderOptions(context.Background())
	if err != nil {
		t.Fatalf("ListProviderOptions() error = %v", err)
	}
	expected := map[string]int{
		OpenAIName:     2,
		GeminiName:     2,
		QiniuName:      2,
		ModelScopeName: 2,
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

func TestSelectionServiceBuiltinUnsupportedAPIStyleNoLongerFailsAcrossSnapshotPaths(t *testing.T) {
	t.Parallel()

	providerCfg := openAIProviderForTest()

	defaults := testDefaultConfig()
	defaults.Providers = []configpkg.ProviderConfig{providerCfg}
	defaults.SelectedProvider = providerCfg.Name
	defaults.CurrentModel = providerCfg.Model

	manager := newSelectionTestManager(t, defaults)
	registry := provider.NewRegistry()
	if err := registry.Register(openaicompat.Driver()); err != nil {
		t.Fatalf("register openaicompat driver: %v", err)
	}
	service := NewService(manager, newDriverSupporterStub(), catalog.NewService("", registry, nil))

	if _, err := service.ListProviderOptions(context.Background()); err != nil {
		t.Fatalf("expected ListProviderOptions() to remain available, got %v", err)
	}
	if _, err := service.SelectProvider(context.Background(), OpenAIName); err != nil {
		t.Fatalf("expected SelectProvider() to remain available, got %v", err)
	}
	if _, err := service.EnsureSelection(context.Background()); err != nil {
		t.Fatalf("expected EnsureSelection() to remain available, got %v", err)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.SelectedProvider != providerCfg.Name {
		t.Fatalf("expected selected provider to stay on %q, got %q", providerCfg.Name, reloaded.SelectedProvider)
	}
	if reloaded.CurrentModel != providerCfg.Model {
		t.Fatalf("expected current model to stay on %q, got %q", providerCfg.Model, reloaded.CurrentModel)
	}
}

func TestSelectionServiceListModelsUsesCurrentSelectedProvider(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "server-coder", Name: "Server Coder"},
		},
	})

	models, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "server-coder" {
		t.Fatalf("expected selected provider models, got %+v", models)
	}
}

func TestSelectionServiceListModelsSnapshotUsesSnapshotCatalog(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "sync-model", Name: "Sync Model"},
		},
		snapshotModels: []providertypes.ModelDescriptor{
			{ID: "snapshot-model", Name: "Snapshot Model"},
		},
	})

	models, err := service.ListModelsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("ListModelsSnapshot() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "snapshot-model" {
		t.Fatalf("expected snapshot models, got %+v", models)
	}
}

func TestSelectionServiceListModelsSnapshotRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"anthropic": true}}
	service := NewService(manager, supporters, newCatalogStub())

	_, err := service.ListModelsSnapshot(context.Background())
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), `provider "openai" driver "openaicompat"`) {
		t.Fatalf("expected contextual unsupported-driver error, got %v", err)
	}
}

func TestSelectionServiceRefreshModelsUsesRefreshPathAndRepairsCurrentModel(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.CurrentModel = "removed-model"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: OpenAIDefaultModel, Name: "GPT Default"},
			{ID: "gpt-5.4-mini", Name: "GPT Mini"},
		},
	})

	models, err := service.RefreshModels(context.Background())
	if err != nil {
		t.Fatalf("RefreshModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected refreshed models, got %+v", models)
	}

	cfg := manager.Get()
	if cfg.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected current model repaired to provider default, got %q", cfg.CurrentModel)
	}
}

func TestSelectionServiceRefreshModelsReturnsNoModelsAvailable(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: nil,
	})

	_, err := service.RefreshModels(context.Background())
	if !errors.Is(err, ErrNoModelsAvailable) {
		t.Fatalf("expected ErrNoModelsAvailable, got %v", err)
	}
}

func TestSelectionServiceSelectProviderAndSetCurrentModel(t *testing.T) {
	manager := newSelectionTestManager(t, testDefaultConfig())

	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	selection, err := service.SelectProvider(context.Background(), QiniuName)
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != QiniuName || selection.ModelID != QiniuDefaultModel {
		t.Fatalf("unexpected selection after switch: %+v", selection)
	}

	selection, err = service.SetCurrentModel(context.Background(), QiniuDefaultModel+"-alt")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != QiniuDefaultModel+"-alt" {
		t.Fatalf("expected selected model %q, got %+v", QiniuDefaultModel+"-alt", selection)
	}

	selection, err = service.SetCurrentModel(context.Background(), "missing-model")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
	if selection != (Selection{}) {
		t.Fatalf("expected failed switch to return empty selection, got %+v", selection)
	}

	cfg := manager.Get()
	selected, err := selectedProviderConfigForTest(cfg)
	if err != nil {
		t.Fatalf("selectedProviderConfig() error = %v", err)
	}
	if selected.Name != QiniuName {
		t.Fatalf("expected selected provider %q, got %+v", QiniuName, selected)
	}
	if cfg.CurrentModel != QiniuDefaultModel+"-alt" {
		t.Fatalf("expected failed switch to preserve current model %q, got %q", QiniuDefaultModel+"-alt", cfg.CurrentModel)
	}
}

func TestSelectionServiceSetCurrentModelRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"anthropic": true}}
	service := NewService(manager, supporters, newCatalogStub())

	_, err := service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}
}

func TestSelectionServiceSelectProviderRequiresDiscoveryOnCacheMiss(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "server-coder", Name: "Server Coder"},
			{ID: "server-chat", Name: "Server Chat"},
		},
		tracker: tracker,
	})

	selection, err := service.SelectProvider(context.Background(), "company-gateway")
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "server-coder" {
		t.Fatalf("expected sync discovery-backed selection, got %+v", selection)
	}
	if tracker.listCalls != 1 || tracker.snapshotCalls != 0 {
		t.Fatalf("expected custom provider selection to use sync catalog only, got %+v", *tracker)
	}
}

func TestSelectionServiceSelectBuiltinProviderUsesSnapshotCatalog(t *testing.T) {
	t.Parallel()

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listErr: errors.New("unexpected sync discovery"),
		snapshotModels: []providertypes.ModelDescriptor{
			{ID: QiniuDefaultModel, Name: QiniuDefaultModel},
			{ID: QiniuDefaultModel + "-alt", Name: QiniuDefaultModel + "-alt"},
		},
		tracker: tracker,
	})

	selection, err := service.SelectProvider(context.Background(), QiniuName)
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != QiniuName || selection.ModelID != QiniuDefaultModel {
		t.Fatalf("expected snapshot-backed builtin selection, got %+v", selection)
	}
	if tracker.listCalls != 0 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected builtin provider selection to use snapshot catalog only, got %+v", *tracker)
	}
}

func TestSelectionServiceEnsureSelectionRepairsInvalidCurrentModel(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("seed invalid current model: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != OpenAIName || selection.ModelID != OpenAIDefaultModel {
		t.Fatalf("unexpected normalized selection: %+v", selection)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected rewritten current model %q, got %q", OpenAIDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionRejectsUnsupportedSelectedProvider(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "anthropic",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "deepseek-coder"

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}
	service := NewService(manager, supporters, newCatalogStub())

	_, err := service.EnsureSelection(context.Background())
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}

	cfg := manager.Get()
	if cfg.SelectedProvider != "company-gateway" {
		t.Fatalf("expected selected provider to stay on company-gateway, got %q", cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "deepseek-coder" {
		t.Fatalf("expected current model to stay unchanged, got %q", cfg.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionFallsBackToFirstDiscoveredModel(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "unknown-model"

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		snapshotModels: []providertypes.ModelDescriptor{
			{ID: "server-coder", Name: "Server Coder"},
			{ID: "server-chat", Name: "Server Chat"},
		},
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "server-coder" {
		t.Fatalf("expected first discovered model fallback, got %+v", selection)
	}
}

func TestSelectionServiceEnsureSelectionFallsBackToBuiltinDefaultModelWhenSnapshotMissing(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.CurrentModel = "unsupported-current"

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{tracker: tracker})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != OpenAIName || selection.ModelID != OpenAIDefaultModel {
		t.Fatalf("expected builtin fallback to default model, got %+v", selection)
	}
	if tracker.listCalls != 0 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected builtin ensure to use snapshot catalog only, got %+v", *tracker)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected builtin fallback to persist %q, got %q", OpenAIDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionKeepsCustomSelectionWhenSnapshotMissing(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "unknown-model"

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{tracker: tracker})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "unknown-model" {
		t.Fatalf("expected custom selection to stay unchanged without snapshot, got %+v", selection)
	}
	if tracker.listCalls != 0 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected custom ensure to use snapshot catalog only, got %+v", *tracker)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.CurrentModel != "unknown-model" {
		t.Fatalf("expected custom current model to stay unchanged, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionBackfillsEmptyCustomModelFromSynchronousDiscovery(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = ""

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listModels: []providertypes.ModelDescriptor{
			{ID: "server-coder", Name: "Server Coder"},
			{ID: "server-chat", Name: "Server Chat"},
		},
		tracker: tracker,
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "server-coder" {
		t.Fatalf("expected synchronous discovery to backfill first discovered model, got %+v", selection)
	}
	if tracker.listCalls != 1 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected custom ensure to try snapshot first and then sync discovery, got %+v", *tracker)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.CurrentModel != "server-coder" {
		t.Fatalf("expected discovered current model to persist, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionKeepsEmptyCustomModelWhenSynchronousDiscoveryFails(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = ""

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listErr: errors.New("discover failed"),
		tracker: tracker,
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() should preserve the empty model when discovery fails, got %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "" {
		t.Fatalf("expected empty custom model to stay unchanged after failed discovery, got %+v", selection)
	}
	if tracker.listCalls != 1 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected custom ensure to attempt sync discovery once, got %+v", *tracker)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.CurrentModel != "" {
		t.Fatalf("expected empty current model to stay unchanged after failed discovery, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionReturnsBootstrappedSelectionWhenCustomDiscoveryFails(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = []configpkg.ProviderConfig{
		{
			Name:                  "company-gateway",
			Driver:                "openaicompat",
			BaseURL:               "https://llm.example.com/v1",
			APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
			DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
			Source:                ProviderSourceCustom,
		},
	}
	defaults.SelectedProvider = ""
	defaults.CurrentModel = ""

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), catalogMethodsStub{
		listErr: errors.New("discover failed"),
		tracker: tracker,
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() should preserve the bootstrapped selection when discovery fails, got %v", err)
	}
	if selection.ProviderID != "company-gateway" || selection.ModelID != "" {
		t.Fatalf("expected bootstrapped custom selection to be returned, got %+v", selection)
	}
	if tracker.listCalls != 1 || tracker.snapshotCalls != 1 {
		t.Fatalf("expected custom ensure to attempt snapshot and one sync discovery, got %+v", *tracker)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.SelectedProvider != "company-gateway" {
		t.Fatalf("expected selected provider to persist as company-gateway, got %q", reloaded.SelectedProvider)
	}
	if reloaded.CurrentModel != "" {
		t.Fatalf("expected current model to remain empty after failed discovery, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionRetriesWhenProviderDriftsDuringUpdate(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = "invalid-openai-model"

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), &driftingSnapshotCatalog{
		t:       t,
		manager: manager,
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != QiniuName || selection.ModelID != QiniuDefaultModel {
		t.Fatalf("expected retried selection to use drifted provider snapshot, got %+v", selection)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.SelectedProvider != QiniuName {
		t.Fatalf("expected selected provider to persist as %q, got %q", QiniuName, reloaded.SelectedProvider)
	}
	if reloaded.CurrentModel != QiniuDefaultModel {
		t.Fatalf("expected current model to be repaired to %q, got %q", QiniuDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionRetriesWhenProviderDriftsBeforeEarlyReturn(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = OpenAIDefaultModel

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), &driftingSnapshotCatalog{
		t:       t,
		manager: manager,
	})

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != QiniuName || selection.ModelID != QiniuDefaultModel {
		t.Fatalf("expected drifted provider selection after retry, got %+v", selection)
	}

	reloaded, err := manager.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.SelectedProvider != QiniuName {
		t.Fatalf("expected selected provider to persist as %q, got %q", QiniuName, reloaded.SelectedProvider)
	}
	if reloaded.CurrentModel != QiniuDefaultModel {
		t.Fatalf("expected current model to be repaired to %q, got %q", QiniuDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceSelectCustomProviderDoesNotPersistWhenDiscoveryFails(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, configpkg.ProviderConfig{
		Name:                  "company-gateway",
		Driver:                "openaicompat",
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "COMPANY_GATEWAY_API_KEY",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceCustom,
	})

	manager := newSelectionTestManager(t, defaults)
	service := NewService(manager, newDriverSupporterStub(), errorCatalogStub{err: errors.New("discover failed")})

	_, err := service.SelectProvider(context.Background(), "company-gateway")
	if err == nil || !strings.Contains(err.Error(), "discover failed") {
		t.Fatalf("expected discovery failure, got %v", err)
	}

	cfg := manager.Get()
	if cfg.SelectedProvider != OpenAIName {
		t.Fatalf("expected selected provider to stay on %q, got %q", OpenAIName, cfg.SelectedProvider)
	}
	if cfg.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected current model to stay on %q, got %q", OpenAIDefaultModel, cfg.CurrentModel)
	}
}

func TestSelectionServiceSelectProviderRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"anthropic": true}}
	service := NewService(manager, supporters, newCatalogStub())

	if _, err := service.SelectProvider(context.Background(), OpenAIName); !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected SelectProvider() to preserve driver error, got %v", err)
	} else if !strings.Contains(err.Error(), `provider "openai" driver "openaicompat"`) {
		t.Fatalf("expected contextual unsupported-driver error, got %v", err)
	}
}

func TestSelectionServiceValidateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		service *Service
		errMsg  string
	}{
		{
			name:    "nil service",
			service: nil,
			errMsg:  "selection: service is nil",
		},
		{
			name: "nil manager",
			service: &Service{
				manager:    nil,
				supporters: &driverSupporterAll{},
				catalogs:   newCatalogStub(),
			},
			errMsg: "selection: config manager is nil",
		},
		{
			name: "nil supporters",
			service: &Service{
				manager:    configpkg.NewManager(configpkg.NewLoader(t.TempDir(), testDefaultConfig())),
				supporters: nil,
				catalogs:   newCatalogStub(),
			},
			errMsg: "selection: driver supporter is nil",
		},
		{
			name: "nil catalogs",
			service: &Service{
				manager:    configpkg.NewManager(configpkg.NewLoader(t.TempDir(), testDefaultConfig())),
				supporters: &driverSupporterAll{},
				catalogs:   nil,
			},
			errMsg: "selection: catalog service is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.service.ListProviderOptions(context.Background())
			if err == nil || err.Error() != tt.errMsg {
				t.Fatalf("expected error %q, got %v", tt.errMsg, err)
			}
		})
	}
}

func TestSelectionServiceOperationsWithCanceledContext(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		fn   func(context.Context) error
	}{
		{
			name: "ListProviderOptions",
			fn: func(ctx context.Context) error {
				_, err := service.ListProviderOptions(ctx)
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

func TestSelectionServiceSetCurrentModelEmptyModelID(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	_, err := service.SetCurrentModel(context.Background(), "")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for empty model ID, got %v", err)
	}

	_, err = service.SetCurrentModel(context.Background(), "   ")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for whitespace model ID, got %v", err)
	}
}

func TestSelectionServiceNilReceiverValidationAcrossMethods(t *testing.T) {
	t.Parallel()

	var service *Service

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "SelectProvider",
			fn: func() error {
				_, err := service.SelectProvider(context.Background(), OpenAIName)
				return err
			},
		},
		{
			name: "ListModels",
			fn: func() error {
				_, err := service.ListModels(context.Background())
				return err
			},
		},
		{
			name: "ListModelsSnapshot",
			fn: func() error {
				_, err := service.ListModelsSnapshot(context.Background())
				return err
			},
		},
		{
			name: "SetCurrentModel",
			fn: func() error {
				_, err := service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
				return err
			},
		},
		{
			name: "EnsureSelection",
			fn: func() error {
				_, err := service.EnsureSelection(context.Background())
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.fn()
			if err == nil || err.Error() != "selection: service is nil" {
				t.Fatalf("expected nil-service validation error, got %v", err)
			}
		})
	}
}

func TestSelectionServiceListProviderOptionsHonorsContextCanceledMidIteration(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	service := NewService(manager, newDriverSupporterStub(), &cancelAfterFirstCachedCatalog{cancel: cancel})

	_, err := service.ListProviderOptions(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled when canceled mid-iteration, got %v", err)
	}
}

func TestSelectionServiceSelectProviderNotFound(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())

	_, err := service.SelectProvider(context.Background(), "missing-provider")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got %v", err)
	}
}

func TestSelectionServiceSelectProviderFailsWhenDriverBecomesUnsupportedDuringUpdate(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, &flappingDriverSupporter{}, newCatalogStub())

	_, err := service.SelectProvider(context.Background(), OpenAIName)
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported during update closure, got %v", err)
	}
}

func TestSelectionServiceSelectProviderFailsWhenTargetProviderRemovedBeforeUpdate(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), &mutatingCatalog{
		snapshotModels: []providertypes.ModelDescriptor{
			{ID: QiniuDefaultModel, Name: QiniuDefaultModel},
		},
		onSnapshot: func(ctx context.Context) error {
			return manager.Update(ctx, func(cfg *configpkg.Config) error {
				kept := make([]configpkg.ProviderConfig, 0, len(cfg.Providers))
				for _, providerCfg := range cfg.Providers {
					if providerCfg.Name == QiniuName {
						continue
					}
					kept = append(kept, providerCfg)
				}
				cfg.Providers = kept
				if cfg.SelectedProvider == QiniuName {
					cfg.SelectedProvider = OpenAIName
					cfg.CurrentModel = OpenAIDefaultModel
				}
				return nil
			})
		},
	})

	_, err := service.SelectProvider(context.Background(), QiniuName)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound after provider removal, got %v", err)
	}
}

func TestSelectionServiceListModelsFailsWithoutSelection(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = ""
		cfg.CurrentModel = ""
		return nil
	}); err != nil {
		t.Fatalf("clear selection: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())
	_, err := service.ListModels(context.Background())
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound for empty selection, got %v", err)
	}
}

func TestSelectionServiceListModelsSnapshotFailsWithoutSelection(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *configpkg.Config) error {
		cfg.SelectedProvider = ""
		cfg.CurrentModel = ""
		return nil
	}); err != nil {
		t.Fatalf("clear selection: %v", err)
	}

	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())
	_, err := service.ListModelsSnapshot(context.Background())
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound for empty selection, got %v", err)
	}
}

func TestSelectionServiceSetCurrentModelPropagatesCatalogError(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	backendErr := errors.New("model discovery failed")
	service := NewService(manager, newDriverSupporterStub(), errorCatalogStub{err: backendErr})

	_, err := service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
	if !errors.Is(err, backendErr) {
		t.Fatalf("expected catalog error propagation, got %v", err)
	}
}

func TestSelectionServiceSetCurrentModelFailsWhenSelectionDriftsBeforeUpdate(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), &mutatingCatalog{
		listModels: []providertypes.ModelDescriptor{
			{ID: OpenAIDefaultModel, Name: OpenAIDefaultModel},
		},
		onList: func(ctx context.Context) error {
			return manager.Update(ctx, func(cfg *configpkg.Config) error {
				cfg.SelectedProvider = ""
				cfg.CurrentModel = ""
				return nil
			})
		},
	})

	_, err := service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound when selection drifts, got %v", err)
	}
}

func TestSelectionServiceBootstrapInitialSelectionPropagatesUpdateError(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, testDefaultConfig())
	service := NewService(manager, newDriverSupporterStub(), newCatalogStub())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.bootstrapInitialSelection(ctx, manager.Get())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from bootstrap update, got %v", err)
	}
}

func TestResolveCurrentModelHelper(t *testing.T) {
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
			name:         "current model in list",
			currentModel: "gpt-4o",
			models: []providertypes.ModelDescriptor{
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
			models: []providertypes.ModelDescriptor{
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
			models: []providertypes.ModelDescriptor{
				{ID: "gpt-4o"},
			},
			fallback: "gpt-4.1",
			expected: "gpt-4o",
			changed:  true,
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

// ---- 婵炴潙顑堥惁顖涙綇閸涱厼袠 ----

type driverSupporterAll struct{}

func (*driverSupporterAll) Supports(_ string) bool { return true }

type selectiveDriverSupporter struct {
	supported map[string]bool
}

func (s *selectiveDriverSupporter) Supports(driverType string) bool {
	return s.supported[provider.NormalizeKey(driverType)]
}

type flappingDriverSupporter struct {
	calls int
}

func (s *flappingDriverSupporter) Supports(_ string) bool {
	s.calls++
	return s.calls == 1
}

type catalogStub struct{}

func (catalogStub) ListProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

func (catalogStub) ListProviderModelsSnapshot(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

func (catalogStub) ListProviderModelsCached(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

func (catalogStub) RefreshProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

type catalogMethodsStub struct {
	listModels     []providertypes.ModelDescriptor
	snapshotModels []providertypes.ModelDescriptor
	cachedModels   []providertypes.ModelDescriptor
	listErr        error
	snapshotErr    error
	cachedErr      error
	tracker        *catalogMethodCalls
}

func (s catalogMethodsStub) ListProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.listCalls++
	}
	if s.listErr != nil {
		return nil, s.listErr
	}
	return providertypes.MergeModelDescriptors(s.listModels), nil
}

func (s catalogMethodsStub) ListProviderModelsSnapshot(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.snapshotCalls++
	}
	if s.snapshotErr != nil {
		return nil, s.snapshotErr
	}
	return providertypes.MergeModelDescriptors(s.snapshotModels), nil
}

func (s catalogMethodsStub) ListProviderModelsCached(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.tracker != nil {
		s.tracker.cachedCalls++
	}
	if s.cachedErr != nil {
		return nil, s.cachedErr
	}
	return providertypes.MergeModelDescriptors(s.cachedModels), nil
}

func (s catalogMethodsStub) RefreshProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return providertypes.MergeModelDescriptors(s.listModels), nil
}

type catalogMethodCalls struct {
	listCalls     int
	snapshotCalls int
	cachedCalls   int
}

type cancelAfterFirstCachedCatalog struct {
	cancel context.CancelFunc
	calls  int
}

func (c *cancelAfterFirstCachedCatalog) ListProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

func (c *cancelAfterFirstCachedCatalog) ListProviderModelsSnapshot(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

func (c *cancelAfterFirstCachedCatalog) ListProviderModelsCached(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	c.calls++
	if c.calls == 1 && c.cancel != nil {
		c.cancel()
	}
	return defaultModelsForInput(input), nil
}

func (c *cancelAfterFirstCachedCatalog) RefreshProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return defaultModelsForInput(input), nil
}

type mutatingCatalog struct {
	listModels     []providertypes.ModelDescriptor
	snapshotModels []providertypes.ModelDescriptor
	onList         func(context.Context) error
	onSnapshot     func(context.Context) error
}

func (m *mutatingCatalog) ListProviderModels(ctx context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if m.onList != nil {
		if err := m.onList(ctx); err != nil {
			return nil, err
		}
		m.onList = nil
	}
	return providertypes.MergeModelDescriptors(m.listModels), nil
}

func (m *mutatingCatalog) ListProviderModelsSnapshot(ctx context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if m.onSnapshot != nil {
		if err := m.onSnapshot(ctx); err != nil {
			return nil, err
		}
		m.onSnapshot = nil
	}
	return providertypes.MergeModelDescriptors(m.snapshotModels), nil
}

func (m *mutatingCatalog) ListProviderModelsCached(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (m *mutatingCatalog) RefreshProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return providertypes.MergeModelDescriptors(m.listModels), nil
}

type errorCatalogStub struct {
	err error
}

func (s errorCatalogStub) ListProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

func (s errorCatalogStub) ListProviderModelsSnapshot(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

func (s errorCatalogStub) ListProviderModelsCached(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

func (s errorCatalogStub) RefreshProviderModels(_ context.Context, _ provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return nil, s.err
}

type driftingSnapshotCatalog struct {
	t        *testing.T
	manager  *configpkg.Manager
	switched bool
}

func (c *driftingSnapshotCatalog) ListProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return c.modelsFor(input), nil
}

func (c *driftingSnapshotCatalog) ListProviderModelsSnapshot(ctx context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	if !c.switched && strings.EqualFold(input.Identity.Key(), mustCatalogIdentity(c.t, configpkg.OpenAIProvider()).Key()) {
		c.switched = true
		if err := c.manager.Update(ctx, func(cfg *configpkg.Config) error {
			cfg.SelectedProvider = QiniuName
			cfg.CurrentModel = "stale-openai-model"
			return nil
		}); err != nil {
			c.t.Fatalf("seed provider drift: %v", err)
		}
	}
	return c.modelsFor(input), nil
}

func (c *driftingSnapshotCatalog) ListProviderModelsCached(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return c.modelsFor(input), nil
}

func (c *driftingSnapshotCatalog) RefreshProviderModels(_ context.Context, input provider.CatalogInput) ([]providertypes.ModelDescriptor, error) {
	return c.modelsFor(input), nil
}

func (c *driftingSnapshotCatalog) modelsFor(input provider.CatalogInput) []providertypes.ModelDescriptor {
	switch input.Identity.Key() {
	case mustCatalogIdentity(c.t, configpkg.OpenAIProvider()).Key():
		return []providertypes.ModelDescriptor{{ID: OpenAIDefaultModel, Name: OpenAIDefaultModel}}
	case mustCatalogIdentity(c.t, configpkg.QiniuProvider()).Key():
		return []providertypes.ModelDescriptor{{ID: QiniuDefaultModel, Name: QiniuDefaultModel}}
	default:
		return nil
	}
}

func mustCatalogIdentity(t *testing.T, cfg configpkg.ProviderConfig) provider.ProviderIdentity {
	t.Helper()

	identity, err := cfg.Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	return identity
}

// defaultModelsForInput 基于 catalog 输入构造稳定的默认模型集合。
func defaultModelsForInput(input provider.CatalogInput) []providertypes.ModelDescriptor {
	defaults := providertypes.MergeModelDescriptors(input.DefaultModels)
	if len(defaults) == 0 {
		return nil
	}
	model := strings.TrimSpace(defaults[0].ID)
	if model == "" {
		return nil
	}
	return []providertypes.ModelDescriptor{
		{ID: model, Name: model},
		{ID: model + "-alt", Name: model + "-alt"},
	}
}

func newCatalogStub() ModelCatalog            { return catalogStub{} }
func newDriverSupporterStub() DriverSupporter { return &driverSupporterAll{} }

func newSelectionTestManager(t *testing.T, defaults *configpkg.Config) *configpkg.Manager {
	t.Helper()

	manager := configpkg.NewManager(configpkg.NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}
