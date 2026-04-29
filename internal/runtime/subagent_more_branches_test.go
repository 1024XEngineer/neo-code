package runtime

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/subagent"
)

type markerFactory struct{}

func (markerFactory) Create(role subagent.Role) (subagent.WorkerRuntime, error) {
	_ = role
	return nil, errors.New("unused")
}

func TestSubAgentFactoryRegistryBranches(t *testing.T) {
	t.Parallel()

	registry := &subAgentFactoryRegistry{
		factory: make(map[*Service]subagent.Factory),
		tracked: make(map[*Service]struct{}),
	}
	service := &Service{}

	registry.ensureTracked(nil)
	registry.ensureTracked(service)
	registry.ensureTracked(service)
	if len(registry.tracked) != 1 {
		t.Fatalf("tracked size = %d, want 1", len(registry.tracked))
	}

	if _, ok := registry.get(nil); ok {
		t.Fatalf("get(nil) should return not found")
	}
	registry.set(nil, markerFactory{})
	registry.set(service, markerFactory{})
	if _, ok := registry.get(service); !ok {
		t.Fatalf("expected factory set/get to succeed")
	}
}

func TestRunSubAgentTaskInputValidationBranches(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(nil, nil, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.RunSubAgentTask(ctx, SubAgentTaskInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	if _, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		Role: subagent.RoleCoder,
		Task: subagent.Task{ID: "t", Goal: "g"},
	}); err == nil {
		t.Fatalf("expected run id required error")
	}

	if _, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID: "run-invalid-role",
		Role:  subagent.Role("invalid"),
		Task:  subagent.Task{ID: "t", Goal: "g"},
	}); err == nil {
		t.Fatalf("expected invalid role error")
	}

	if _, err := service.RunSubAgentTask(context.Background(), SubAgentTaskInput{
		RunID: "run-invalid-task",
		Role:  subagent.RoleCoder,
		Task:  subagent.Task{ID: "", Goal: ""},
	}); err == nil {
		t.Fatalf("expected invalid task error")
	}
}

func TestSubAgentResultErrorBranches(t *testing.T) {
	t.Parallel()

	if err := subAgentResultError(subagent.Result{Error: "explicit error"}); err == nil || err.Error() != "explicit error" {
		t.Fatalf("expected explicit error passthrough, got %v", err)
	}
	if err := subAgentResultError(subagent.Result{State: subagent.StateFailed, StopReason: subagent.StopReasonError}); err == nil {
		t.Fatalf("expected synthesized fallback error")
	}
}

func TestSubAgentFactoryNilReceiverBranches(t *testing.T) {
	t.Parallel()

	var nilService *Service
	nilService.SetSubAgentFactory(markerFactory{})
	if nilService.SubAgentFactory() == nil {
		t.Fatalf("nil receiver SubAgentFactory should return default factory")
	}
}

func TestEmitSubAgentFailedNilServiceNoPanic(t *testing.T) {
	t.Parallel()

	emitSubAgentFailed(nil, context.Background(), "run", "session", subagent.RoleCoder, "task", errors.New("boom"))
}

func TestSubAgentRuntimeToolExecutorListToolSpecsError(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{listErr: errors.New("list failed")},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	executor := newSubAgentRuntimeToolExecutor(service)
	if _, err := executor.ListToolSpecs(context.Background(), subagent.ToolSpecListInput{
		SessionID:    "session",
		Role:         subagent.RoleCoder,
		AllowedTools: []string{"bash"},
	}); err == nil {
		t.Fatalf("expected list specs error")
	}
}

func TestSubAgentRuntimeToolExecutorEmitNilService(t *testing.T) {
	t.Parallel()

	executor := &subAgentRuntimeToolExecutor{}
	executor.emit(context.Background(), "run", "session", EventSubAgentToolCallStarted, SubAgentToolCallEventPayload{
		Role:     subagent.RoleCoder,
		TaskID:   "task",
		ToolName: "bash",
	})
}

func TestRuntimeSubAgentResolveSettingsModelFallbackAndEmptyModel(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = ""
		for i := range cfg.Providers {
			cfg.Providers[i].Model = "model-from-provider"
			if cfg.Providers[i].Source == config.ProviderSourceBuiltin {
				cfg.Providers[i].Models = append(cfg.Providers[i].Models, providertypes.ModelDescriptor{
					ID:   "model-from-provider",
					Name: "model-from-provider",
				})
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("manager.Update() error = %v", err)
	}

	engine := runtimeSubAgentEngine{
		service: &Service{
			configManager:   manager,
			providerFactory: &scriptedProviderFactory{provider: &scriptedProvider{}},
		},
	}
	_, model, _, err := engine.resolveSettings()
	if err != nil {
		t.Fatalf("resolveSettings() error = %v", err)
	}
	if model != "model-from-provider" {
		t.Fatalf("model = %q, want model-from-provider", model)
	}
}

func TestSubAgentToolResultToMessageFallbackName(t *testing.T) {
	t.Parallel()

	msg := subAgentToolResultToMessage(providertypes.ToolCall{ID: "call-1", Name: "bash"}, subagent.ToolExecutionResult{
		Name:     "",
		Content:  "ok",
		Decision: permissionDecisionAllow,
	})
	if msg.ToolCallID != "call-1" {
		t.Fatalf("tool call id = %q, want call-1", msg.ToolCallID)
	}
	toolName := msg.ToolMetadata["tool_name"]
	if toolName != "bash" {
		t.Fatalf("tool_name metadata = %q, want bash", toolName)
	}
}
