package bootstrap

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
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

func (r *testRuntime) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r *testRuntime) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r *testRuntime) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return []agentruntime.SessionSkillState{{SkillID: "test", Descriptor: &skills.Descriptor{ID: "test"}}}, nil
}

type testProviderService struct{}

func (s *testProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
	return nil, nil
}

func (s *testProviderService) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	return configstate.Selection{}, nil
}

func (s *testProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s *testProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	return configstate.Selection{}, nil
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

type errorFactory struct {
	runtimeErr  error
	providerErr error
	runtimeNil  bool
	providerNil bool
}

func (f errorFactory) BuildRuntime(mode Mode, current agentruntime.Runtime) (agentruntime.Runtime, error) {
	if f.runtimeErr != nil {
		return nil, f.runtimeErr
	}
	if f.runtimeNil {
		return nil, nil
	}
	return current, nil
}

func (f errorFactory) BuildProvider(mode Mode, current ProviderService) (ProviderService, error) {
	if f.providerErr != nil {
		return nil, f.providerErr
	}
	if f.providerNil {
		return nil, nil
	}
	return current, nil
}

type noopRuntime struct{}

func (r noopRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	return nil
}

func (r noopRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return agentruntime.CompactResult{}, nil
}

func (r noopRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return nil
}

func (r noopRuntime) Events() <-chan agentruntime.RuntimeEvent {
	ch := make(chan agentruntime.RuntimeEvent)
	close(ch)
	return ch
}

func (r noopRuntime) CancelActiveRun() bool {
	return false
}

func (r noopRuntime) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return nil, nil
}

func (r noopRuntime) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return agentsession.Session{}, nil
}

func (r noopRuntime) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r noopRuntime) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return nil
}

func (r noopRuntime) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return nil, nil
}

type noopProviderService struct{}

func (s noopProviderService) ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error) {
	return nil, nil
}

func (s noopProviderService) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	return configstate.Selection{}, nil
}

func (s noopProviderService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s noopProviderService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return nil, nil
}

func (s noopProviderService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	return configstate.Selection{}, nil
}

func TestBuildFactoryErrors(t *testing.T) {
	manager := &config.Manager{}
	runtimeSvc := noopRuntime{}
	providerSvc := noopProviderService{}

	_, err := Build(Options{
		ConfigManager:   manager,
		Runtime:         runtimeSvc,
		ProviderService: providerSvc,
		Factory:         errorFactory{runtimeErr: errors.New("runtime boom")},
	})
	if err == nil {
		t.Fatalf("expected runtime factory error")
	}

	_, err = Build(Options{
		ConfigManager:   manager,
		Runtime:         runtimeSvc,
		ProviderService: providerSvc,
		Factory:         errorFactory{providerErr: errors.New("provider boom")},
	})
	if err == nil {
		t.Fatalf("expected provider factory error")
	}

	_, err = Build(Options{
		ConfigManager:   manager,
		Runtime:         runtimeSvc,
		ProviderService: providerSvc,
		Factory:         errorFactory{runtimeNil: true},
	})
	if err == nil {
		t.Fatalf("expected nil runtime factory error")
	}

	_, err = Build(Options{
		ConfigManager:   manager,
		Runtime:         runtimeSvc,
		ProviderService: providerSvc,
		Factory:         errorFactory{providerNil: true},
	})
	if err == nil {
		t.Fatalf("expected nil provider factory error")
	}
}
