package bootstrap

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
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

func (s *testProviderService) ListModels(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) ListModelsSnapshot(ctx context.Context) ([]config.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) SetCurrentModel(ctx context.Context, modelID string) (config.ProviderSelection, error) {
	return config.ProviderSelection{}, nil
}

type testWorkspaceSwitcher struct{}

func (s *testWorkspaceSwitcher) SwitchWorkspace(ctx context.Context, workdir string) error {
	return nil
}

type testFactory struct {
	useRuntime              bool
	runtimeResult           agentruntime.Runtime
	runtimeErr              error
	useProvider             bool
	providerResult          ProviderService
	providerErr             error
	useWorkspaceSwitcher    bool
	workspaceSwitcherResult WorkspaceSwitcher
	workspaceSwitcherErr    error
}

func (f testFactory) BuildRuntime(mode Mode, current agentruntime.Runtime) (agentruntime.Runtime, error) {
	if !f.useRuntime {
		return current, nil
	}
	return f.runtimeResult, f.runtimeErr
}

func (f testFactory) BuildProvider(mode Mode, current ProviderService) (ProviderService, error) {
	if !f.useProvider {
		return current, nil
	}
	return f.providerResult, f.providerErr
}

func (f testFactory) BuildWorkspaceSwitcher(mode Mode, current WorkspaceSwitcher) (WorkspaceSwitcher, error) {
	if !f.useWorkspaceSwitcher {
		return current, nil
	}
	return f.workspaceSwitcherResult, f.workspaceSwitcherErr
}

func TestBuild(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		manager := &config.Manager{}
		runtime := &testRuntime{}
		providerSvc := &testProviderService{}

		container, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           runtime,
			ProviderService:   providerSvc,
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
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
			ConfigManager:     nil,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
		})
		if err == nil {
			t.Fatal("expected error for nil config manager")
		}
	})

	t.Run("nil runtime", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           nil,
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
		})
		if err == nil {
			t.Fatal("expected error for nil runtime")
		}
	})

	t.Run("nil provider service", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   nil,
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
		})
		if err == nil {
			t.Fatal("expected error for nil provider service")
		}
	})

	t.Run("nil workspace switcher", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: nil,
		})
		if err == nil {
			t.Fatal("expected error for nil workspace switcher")
		}
	})

	t.Run("runtime factory error", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useRuntime: true,
				runtimeErr: errors.New("runtime factory failed"),
			},
		})
		if err == nil || err.Error() != "tui bootstrap: build runtime: runtime factory failed" {
			t.Fatalf("expected runtime factory error, got %v", err)
		}
	})

	t.Run("runtime factory returns nil", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useRuntime:              true,
				runtimeResult:           nil,
				useProvider:             true,
				providerResult:          &testProviderService{},
				useWorkspaceSwitcher:    true,
				workspaceSwitcherResult: &testWorkspaceSwitcher{},
			},
		})
		if err == nil || err.Error() != "tui bootstrap: runtime factory returned nil" {
			t.Fatalf("expected nil runtime error, got %v", err)
		}
	})

	t.Run("provider factory error", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useProvider: true,
				providerErr: errors.New("provider factory failed"),
			},
		})
		if err == nil || err.Error() != "tui bootstrap: build provider service: provider factory failed" {
			t.Fatalf("expected provider factory error, got %v", err)
		}
	})

	t.Run("provider factory returns nil", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useRuntime:              true,
				runtimeResult:           &testRuntime{},
				useProvider:             true,
				providerResult:          nil,
				useWorkspaceSwitcher:    true,
				workspaceSwitcherResult: &testWorkspaceSwitcher{},
			},
		})
		if err == nil || err.Error() != "tui bootstrap: provider factory returned nil" {
			t.Fatalf("expected nil provider error, got %v", err)
		}
	})

	t.Run("workspace switcher factory error", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useWorkspaceSwitcher: true,
				workspaceSwitcherErr: errors.New("workspace switcher factory failed"),
			},
		})
		if err == nil || err.Error() != "tui bootstrap: build workspace switcher: workspace switcher factory failed" {
			t.Fatalf("expected workspace switcher factory error, got %v", err)
		}
	})

	t.Run("workspace switcher factory returns nil", func(t *testing.T) {
		manager := &config.Manager{}
		_, err := Build(Options{
			ConfigManager:     manager,
			Runtime:           &testRuntime{},
			ProviderService:   &testProviderService{},
			WorkspaceSwitcher: &testWorkspaceSwitcher{},
			Factory: testFactory{
				useRuntime:              true,
				runtimeResult:           &testRuntime{},
				useProvider:             true,
				providerResult:          &testProviderService{},
				useWorkspaceSwitcher:    true,
				workspaceSwitcherResult: nil,
			},
		})
		if err == nil || err.Error() != "tui bootstrap: workspace switcher factory returned nil" {
			t.Fatalf("expected nil workspace switcher error, got %v", err)
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
		cfg.Workdir = "/mutated"
		if inputCfg.Workdir != "/test" {
			t.Fatalf("expected resolveConfigSnapshot to return clone, got source %q", inputCfg.Workdir)
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
