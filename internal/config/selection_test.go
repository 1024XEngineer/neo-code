package config

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSelectionServiceListProvidersFallsBackToProviderDefaultModel(t *testing.T) {
	t.Parallel()

	manager := newSelectionTestManager(t, DefaultConfig())
	service := NewSelectionService(manager, newDriverSupporterStub(), nil, newCatalogStub())

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

func TestSelectionServiceSelectProviderAndSetCurrentModel(t *testing.T) {
	manager := newSelectionTestManager(t, DefaultConfig())

	if err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.CurrentModel = "gpt-5.4"
		return nil
	}); err != nil {
		t.Fatalf("seed current model: %v", err)
	}

	service := NewSelectionService(manager, newDriverSupporterStub(), nil, newCatalogStub())

	selection, err := service.SelectProvider(context.Background(), QiniuName)
	if err != nil {
		t.Fatalf("SelectProvider() error = %v", err)
	}
	if selection.ProviderID != QiniuName || selection.ModelID != QiniuDefaultModel {
		t.Fatalf("unexpected selection after switch: %+v", selection)
	}

	if _, err := service.SetCurrentModel(context.Background(), "missing-model"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}

	selection, err = service.SetCurrentModel(context.Background(), QiniuDefaultModel+"-alt")
	if err != nil {
		t.Fatalf("SetCurrentModel() error = %v", err)
	}
	if selection.ModelID != QiniuDefaultModel+"-alt" {
		t.Fatalf("expected selected model %q, got %+v", QiniuDefaultModel+"-alt", selection)
	}

	cfg := manager.Get()
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if selected.Name != QiniuName {
		t.Fatalf("expected selected provider %q, got %+v", QiniuName, selected)
	}
	if selected.Model != QiniuDefaultModel || cfg.CurrentModel != QiniuDefaultModel+"-alt" {
		t.Fatalf("expected provider default and current model to diverge safely, got provider=%q current=%q", selected.Model, cfg.CurrentModel)
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

	service := NewSelectionService(manager, newDriverSupporterStub(), nil, newCatalogStub())

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

func TestSelectionServiceSelectProviderRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	defaults := DefaultConfig()
	defaults.Providers = []ProviderConfig{{
		Name:      OpenAIName,
		Driver:    "missing-driver",
		BaseURL:   OpenAIDefaultBaseURL,
		Model:     OpenAIDefaultModel,
		APIKeyEnv: OpenAIDefaultAPIKeyEnv,
	}}
	defaults.SelectedProvider = OpenAIName
	defaults.CurrentModel = OpenAIDefaultModel

	manager := newSelectionTestManager(t, defaults)
	supporters := &selectiveDriverSupporter{supported: map[string]bool{"openai": true}}
	service := NewSelectionService(manager, supporters, nil, newCatalogStub())

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
	service := NewSelectionService(manager, newDriverSupporterStub(), nil, newCatalogStub())

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
	service := NewSelectionService(manager, newDriverSupporterStub(), nil, newCatalogStub())

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
	return s.supported[NormalizeKey(driverType)]
}

type catalogStub struct{}

func (catalogStub) ListProviderModels(_ context.Context, cfg ProviderConfig) ([]ModelDescriptor, error) {
	return defaultModelsForProvider(cfg), nil
}

func (catalogStub) ListProviderModelsSnapshot(_ context.Context, cfg ProviderConfig) ([]ModelDescriptor, error) {
	return defaultModelsForProvider(cfg), nil
}

func (catalogStub) ListProviderModelsCached(_ context.Context, cfg ProviderConfig) ([]ModelDescriptor, error) {
	return defaultModelsForProvider(cfg), nil
}

// defaultModelsForProvider 为给定 provider 返回包含默认模型和额外测试模型的列表。
func defaultModelsForProvider(cfg ProviderConfig) []ModelDescriptor {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil
	}
	return []ModelDescriptor{
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
