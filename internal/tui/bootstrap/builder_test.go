package bootstrap

import (
	"context"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
)

type testRuntime struct{}

func (r *testRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r *testRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r *testRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return nil
}

func (r *testRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r *testRuntime) CancelActiveRun() bool {
	return false
}

func (r *testRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (r *testRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (r *testRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

type testProviderService struct{}

func (s *testProviderService) ListProviders(ctx context.Context) ([]config.ProviderCatalogItem, error) {
	return nil, nil
}

func (s *testProviderService) SelectProvider(ctx context.Context, providerID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

func (s *testProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) SetCurrentModel(ctx context.Context, modelID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

func TestBuild(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		manager := &config.Manager{}
		runtime := &testRuntime{}
		providerSvc := &testProviderService{}

		container, err := Build(Options{
			ConfigManager:   manager,
			Runtime:         runtime,
			ProviderService: providerSvc,
		})
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		if container.ConfigManager != manager {
			t.Error("expected ConfigManager to be set")
		}
	})

	t.Run("nil config manager", func(t *testing.T) {
		_, err := Build(Options{
			ConfigManager:   nil,
			Runtime:         &testRuntime{},
			ProviderService: &testProviderService{},
		})
		if err == nil {
			t.Fatal("expected error for nil config manager")
		}
	})

	t.Run("nil runtime", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:   manager,
			Runtime:         nil,
			ProviderService: &testProviderService{},
		})
		if err == nil {
			t.Fatal("expected error for nil runtime")
		}
	})

	t.Run("nil provider service", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:   manager,
			Runtime:         &testRuntime{},
			ProviderService: nil,
		})
		if err == nil {
			t.Fatal("expected error for nil provider service")
		}
	})
}

func TestResolveConfigSnapshot(t *testing.T) {
	t.Run("nil config returns manager get", func(t *testing.T) {
		manager := &config.Manager{}
		cfg := resolveConfigSnapshot(nil, manager)
		if cfg.Workdir == "" && cfg.Shell == "" {
			t.Log("config returned from manager")
		}
	})

	t.Run("config provided returns clone", func(t *testing.T) {
		manager := &config.Manager{}
		inputCfg := &config.Config{
			Workdir: "/test",
		}
		cfg := resolveConfigSnapshot(inputCfg, manager)
		if cfg.Workdir != "/test" {
			t.Errorf("expected Workdir /test, got %s", cfg.Workdir)
		}
	})
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		name  string
		input Mode
		want  Mode
	}{
		{"empty becomes live", "", ModeLive},
		{"live stays live", ModeLive, ModeLive},
		{"offline stays offline", ModeOffline, ModeOffline},
		{"mock stays mock", ModeMock, ModeMock},
		{"unknown becomes live", Mode("unknown"), ModeLive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeMode(tt.input); got != tt.want {
				t.Errorf("NormalizeMode(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
