package config

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestSelectionServiceListProvidersUsesCatalogModels(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), newCatalogStub())

	items, err := service.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	expected := map[string]int{
		OpenAIName: 2,
		GeminiName: 2,
		OpenLLName: 2,
		QiniuName:  2,
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

func TestSelectionServiceListModelsUsesCurrentSelectedProvider(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"

	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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

	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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

	defaults := DefaultConfig()
	defaults.Providers = []ProviderConfig{{
		Name:      OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
		Source:    ProviderSourceBuiltin,
	}}
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = OpenAIDefaultModel

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}
	service := NewSelectionService(manager, supporters, newCatalogStub())

	_, err := service.ListModelsSnapshot(context.Background())
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}
}

func TestSelectionServiceSelectProviderAndSetCurrentModel(t *testing.T) {
	manager := newSelectionTestManager(t, DefaultConfig())

	if err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	service := NewSelectionService(manager, newDriverSupporterStub(), newCatalogStub())

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
	if selection != (ProviderSelection{}) {
		t.Fatalf("expected failed switch to return empty selection, got %+v", selection)
	}

	cfg := manager.Get()
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
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

	defaults := DefaultConfig()
	defaults.Providers = []ProviderConfig{{
		Name:      OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
		Source:    ProviderSourceBuiltin,
	}}
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = OpenAIDefaultModel

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}
	service := NewSelectionService(manager, supporters, newCatalogStub())

	_, err := service.SetCurrentModel(context.Background(), OpenAIDefaultModel)
	if !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}
}

func TestSelectionServiceSelectProviderRequiresDiscoveryOnCacheMiss(t *testing.T) {
	t.Parallel()

	defaults := DefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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
	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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

	manager := newSelectionTestManager(t, DefaultConfig())
	if err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("seed invalid current model: %v", err)
	}

	service := NewSelectionService(manager, newDriverSupporterStub(), newCatalogStub())

	selection, err := service.EnsureSelection(context.Background())
	if err != nil {
		t.Fatalf("EnsureSelection() error = %v", err)
	}
	if selection.ProviderID != OpenAIName || selection.ModelID != OpenAIDefaultModel {
		t.Fatalf("unexpected normalized selection: %+v", selection)
	}

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected rewritten current model %q, got %q", OpenAIDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionRejectsUnsupportedSelectedProvider(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "anthropic",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "deepseek-coder"

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}
	service := NewSelectionService(manager, supporters, newCatalogStub())

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
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "unknown-model"

	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{tracker: tracker})

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

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != OpenAIDefaultModel {
		t.Fatalf("expected builtin fallback to persist %q, got %q", OpenAIDefaultModel, reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionKeepsCustomSelectionWhenSnapshotMissing(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = "unknown-model"

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{tracker: tracker})

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

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != "unknown-model" {
		t.Fatalf("expected custom current model to stay unchanged, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionBackfillsEmptyCustomModelFromSynchronousDiscovery(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = ""

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != "server-coder" {
		t.Fatalf("expected discovered current model to persist, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceEnsureSelectionKeepsEmptyCustomModelWhenSynchronousDiscoveryFails(t *testing.T) {
	t.Parallel()

	defaults := testDefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})
	defaults.SelectedProvider = "company-gateway"
	defaults.CurrentModel = ""

	tracker := &catalogMethodCalls{}
	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), catalogMethodsStub{
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

	reloaded, err := manager.Reload(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.CurrentModel != "" {
		t.Fatalf("expected empty current model to stay unchanged after failed discovery, got %q", reloaded.CurrentModel)
	}
}

func TestSelectionServiceSelectCustomProviderDoesNotPersistWhenDiscoveryFails(t *testing.T) {
	t.Parallel()

	defaults := DefaultConfig()
	defaults.Providers = append(defaults.Providers, ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
		Source:    ProviderSourceCustom,
	})

	manager := newSelectionTestManager(t, defaults)
	service := NewSelectionService(manager, newDriverSupporterStub(), errorCatalogStub{err: errors.New("discover failed")})

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

	defaults := DefaultConfig()
	defaults.Providers = []ProviderConfig{{
		Name:      OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
		Source:    ProviderSourceBuiltin,
	}}
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = OpenAIDefaultModel

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openaicompat": true}}
	service := NewSelectionService(manager, supporters, newCatalogStub())

	if _, err := service.SelectProvider(context.Background(), OpenAIName); !errors.Is(err, ErrDriverUnsupported) {
		t.Fatalf("expected SelectProvider() to preserve driver error, got %v", err)
	}
}

func TestSelectionServiceValidateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		service *SelectionService
		errMsg  string
	}{
		{
			name:    "nil service",
			service: nil,
			errMsg:  "selection: service is nil",
		},
		{
			name: "nil manager",
			service: &SelectionService{
				manager:    nil,
				supporters: &driverSupporterAll{},
				catalogs:   newCatalogStub(),
			},
			errMsg: "selection: config manager is nil",
		},
		{
			name: "nil supporters",
			service: &SelectionService{
				manager:    NewManager(NewLoader(t.TempDir(), DefaultConfig())),
				supporters: nil,
				catalogs:   newCatalogStub(),
			},
			errMsg: "selection: driver supporter is nil",
		},
		{
			name: "nil catalogs",
			service: &SelectionService{
				manager:    NewManager(NewLoader(t.TempDir(), DefaultConfig())),
				supporters: &driverSupporterAll{},
				catalogs:   nil,
			},
			errMsg: "selection: catalog service is nil",
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

func TestSelectionServiceOperationsWithCanceledContext(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), newCatalogStub())

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

func TestSelectionServiceSetCurrentModelEmptyModelID(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), newCatalogStub())

	_, err := service.SetCurrentModel(context.Background(), "")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for empty model ID, got %v", err)
	}

	_, err = service.SetCurrentModel(context.Background(), "   ")
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound for whitespace model ID, got %v", err)
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

// ---- 测试辅助 ----

type driverSupporterAll struct{}

func (*driverSupporterAll) Supports(_ string) bool { return true }

type selectiveDriverSupporter struct {
	supported map[string]bool
}

func (s *selectiveDriverSupporter) Supports(driverType string) bool {
	return s.supported[provider.NormalizeKey(driverType)]
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

type catalogMethodCalls struct {
	listCalls     int
	snapshotCalls int
	cachedCalls   int
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

// defaultModelsForInput 为给定 catalog 输入返回默认模型及其变体，便于选择逻辑测试复用。
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

func newSelectionTestManager(t *testing.T, defaults *Config) *Manager {
	t.Helper()

	manager := NewManager(NewLoader(t.TempDir(), defaults))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}
