package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

const subAgentDonePayload = `{"summary":"done","findings":["f1"],"patches":["p1"],"risks":["r1"],"next_actions":["n1"],"artifacts":["a1"]}`

func TestRuntimeSubAgentEngineRunStepToolLoopSuccess(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{
						providertypes.NewTextPart(subAgentDonePayload),
					},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "filesystem_read_file", Schema: map[string]any{"type": "object"}}},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{
				ToolCallID: input.ID,
				Name:       input.Name,
				Content:    "file content",
				IsError:    false,
				Metadata:   map[string]any{"truncated": true},
			}, nil
		},
	}
	providerFactory := &scriptedProviderFactory{provider: providerImpl}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), providerFactory, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.ToolUseMode = subagent.ToolUseModeAuto
	policy.MaxToolCallsPerStep = 2
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}

	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{
		ID:   "task-1",
		Goal: "read file and summarize",
	})
	stepInput.Budget = subagent.Budget{MaxSteps: 4}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.RunID = "run-subagent-step-success"
	stepInput.SessionID = "session-subagent-step-success"
	stepInput.AgentID = "subagent:task-1"

	output, err := engine.RunStep(context.Background(), stepInput)
	if err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if !output.Done {
		t.Fatalf("expected step done")
	}
	if output.Output.Summary != "done" {
		t.Fatalf("summary = %q, want %q", output.Output.Summary, "done")
	}
	if toolManager.executeCalls != 1 {
		t.Fatalf("tool execute calls = %d, want 1", toolManager.executeCalls)
	}
	if providerImpl.callCount != 2 {
		t.Fatalf("provider calls = %d, want 2", providerImpl.callCount)
	}
	if providerFactory.calls != 1 {
		t.Fatalf("provider build calls = %d, want 1", providerFactory.calls)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallResult})
	assertSubAgentToolEventPayload(t, events, EventSubAgentToolCallResult, "filesystem_read_file", permissionDecisionAllow, true)
}

func TestRuntimeSubAgentEngineRunStepCapabilityDenied(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-bash", Name: "bash", Arguments: `{"command":"echo hi"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{
						providertypes.NewTextPart(`{"summary":"fallback","findings":["f1"],"patches":["p1"],"risks":["r1"],"next_actions":["n1"],"artifacts":["a1"]}`),
					},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}},
	}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.MaxToolCallsPerStep = 2
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}

	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{
		ID:   "task-cap-deny",
		Goal: "execute bash",
	})
	stepInput.Budget = subagent.Budget{MaxSteps: 4}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.RunID = "run-subagent-cap-deny"
	stepInput.SessionID = "session-subagent-cap-deny"
	stepInput.AgentID = "subagent:task-cap-deny"

	stepOutput, err := engine.RunStep(context.Background(), stepInput)
	if err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if !stepOutput.Done {
		t.Fatalf("expected step done")
	}
	if toolManager.executeCalls != 0 {
		t.Fatalf("tool execute calls = %d, want 0", toolManager.executeCalls)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventSubAgentToolCallDenied})
	assertSubAgentToolEventPayload(t, events, EventSubAgentToolCallDenied, "bash", permissionDecisionDeny, false)
}

func TestRuntimeSubAgentEngineRequiredModeCapabilityDeniedDoesNotSatisfyRequirement(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-bash", Name: "bash", Arguments: `{"command":"echo hi"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{
						providertypes.NewTextPart(subAgentDonePayload),
					},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "bash", Schema: map[string]any{"type": "object"}}},
	}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.ToolUseMode = subagent.ToolUseModeRequired
	policy.MaxToolCallsPerStep = 2
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}

	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{
		ID:   "task-required-denied",
		Goal: "must call allowed tool",
	})
	stepInput.Budget = subagent.Budget{MaxSteps: 4}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.RunID = "run-subagent-required-denied"
	stepInput.SessionID = "session-subagent-required-denied"

	_, err = engine.RunStep(context.Background(), stepInput)
	if err == nil || !strings.Contains(err.Error(), "requires at least one tool call") {
		t.Fatalf("expected required-mode error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineRunStepRequiredModeWithoutToolFails(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{
						providertypes.NewTextPart(subAgentDonePayload),
					},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "filesystem_read_file", Schema: map[string]any{"type": "object"}}},
	}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.ToolUseMode = subagent.ToolUseModeRequired
	policy.MaxToolCallsPerStep = 1
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}

	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{
		ID:   "task-required",
		Goal: "must call tool",
	})
	stepInput.Budget = subagent.Budget{MaxSteps: 2}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.RunID = "run-subagent-required"
	stepInput.SessionID = "session-subagent-required"
	stepInput.AgentID = "subagent:task-required"

	_, err = engine.RunStep(context.Background(), stepInput)
	if err == nil || !strings.Contains(err.Error(), "requires at least one tool call") {
		t.Fatalf("expected required-mode error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineUnavailableDepsShouldFailByDefault(t *testing.T) {
	t.Parallel()

	policy, err := subagent.DefaultRolePolicy(subagent.RoleReviewer)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	engine := runtimeSubAgentEngine{
		service: nil,
		role:    subagent.RoleReviewer,
		policy:  policy,
	}
	step, err := engine.RunStep(context.Background(), subagent.StepInput{
		Role:   subagent.RoleReviewer,
		Policy: policy,
		Task:   subagent.Task{ID: "task-fallback", Goal: "review"},
	})
	if err == nil || !errors.Is(err, errSubAgentRuntimeUnavailable) {
		t.Fatalf("expected runtime unavailable error, got output=%+v err=%v", step, err)
	}
}

func TestRuntimeSubAgentEngineRunStepProviderBuildFailureReturnsError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleReviewer)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	engine := runtimeSubAgentEngine{
		service: NewWithFactory(manager, &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{
			err: errors.New("provider init failed"),
		}, nil),
		role:   subagent.RoleReviewer,
		policy: policy,
	}
	_, err = engine.RunStep(context.Background(), subagent.StepInput{
		Role:     subagent.RoleReviewer,
		Policy:   policy,
		Task:     subagent.Task{ID: "task-provider-fail", Goal: "review"},
		Executor: newSubAgentRuntimeToolExecutor(engine.service),
	})
	if err == nil || !strings.Contains(err.Error(), "build subagent provider") {
		t.Fatalf("expected build provider error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineDisabledModeRejectsAnyToolCall(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}
	service := NewWithFactory(
		manager,
		&stubToolManager{specs: []providertypes.ToolSpec{{Name: "filesystem_read_file"}}},
		newMemoryStore(),
		&scriptedProviderFactory{provider: providerImpl},
		nil,
	)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.ToolUseMode = subagent.ToolUseModeDisabled
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}
	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{ID: "task-disabled", Goal: "no tools"})

	_, err = engine.RunStep(context.Background(), stepInput)
	if err == nil || !strings.Contains(err.Error(), "tool_use_mode is disabled") {
		t.Fatalf("expected disabled mode error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineMaxToolCallsExceeded(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-2", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "filesystem_read_file", Schema: map[string]any{"type": "object"}}},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			return tools.ToolResult{ToolCallID: input.ID, Name: input.Name, Content: "ok"}, nil
		},
	}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	policy.MaxToolCallsPerStep = 1
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}
	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{ID: "task-max-calls", Goal: "call twice"})
	stepInput.Budget = subagent.Budget{MaxSteps: 4}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}

	_, err = engine.RunStep(context.Background(), stepInput)
	if err == nil || !strings.Contains(err.Error(), "max_tool_calls_per_step exceeded") {
		t.Fatalf("expected max tool calls exceeded error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineAbortOnNonRecoverableToolError(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart(subAgentDonePayload)},
				},
				FinishReason: "stop",
			},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}
	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{ID: "task-non-recoverable", Goal: "tool fail"})
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.Executor = failingToolExecutor{err: errors.New("manager unavailable")}

	_, err = engine.RunStep(context.Background(), stepInput)
	if err == nil || !strings.Contains(err.Error(), "manager unavailable") {
		t.Fatalf("expected non-recoverable tool error, got %v", err)
	}
}

func TestRuntimeSubAgentEngineAskRejectIsRecoverable(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart(subAgentDonePayload)},
				},
				FinishReason: "stop",
			},
		},
	}
	service := NewWithFactory(manager, &stubToolManager{}, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}
	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{ID: "task-ask-reject", Goal: "tool reject should continue"})
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}
	stepInput.Executor = failingToolExecutor{
		err:      errors.New(permissionRejectedErrorMessage),
		decision: permissionDecisionDeny,
	}

	output, err := engine.RunStep(context.Background(), stepInput)
	if err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if !output.Done {
		t.Fatalf("expected step done")
	}
	if output.Output.Summary != "done" {
		t.Fatalf("summary = %q, want done", output.Output.Summary)
	}
}

func TestRuntimeSubAgentEngineLowWorkerBudgetStillAllowsToolClosure(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	providerImpl := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					ToolCalls: []providertypes.ToolCall{
						{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
					},
				},
				FinishReason: "tool_calls",
			},
			{
				Message: providertypes.Message{
					Role:  providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart(subAgentDonePayload)},
				},
				FinishReason: "stop",
			},
		},
	}
	toolManager := &stubToolManager{
		specs: []providertypes.ToolSpec{{Name: "filesystem_read_file", Schema: map[string]any{"type": "object"}}},
		executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
			_ = ctx
			return tools.ToolResult{ToolCallID: input.ID, Name: input.Name, Content: "ok"}, nil
		},
	}
	service := NewWithFactory(manager, toolManager, newMemoryStore(), &scriptedProviderFactory{provider: providerImpl}, nil)
	policy, err := subagent.DefaultRolePolicy(subagent.RoleCoder)
	if err != nil {
		t.Fatalf("DefaultRolePolicy() error = %v", err)
	}
	engine := runtimeSubAgentEngine{service: service, role: subagent.RoleCoder, policy: policy}
	stepInput := newRuntimeSubAgentStepInput(t, service, policy, subagent.Task{ID: "task-low-budget", Goal: "tool closure with low outer budget"})
	stepInput.Budget = subagent.Budget{MaxSteps: 1}
	stepInput.Capability = subagent.Capability{AllowedTools: []string{"filesystem_read_file"}}

	output, err := engine.RunStep(context.Background(), stepInput)
	if err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if !output.Done {
		t.Fatalf("expected step done")
	}
	if providerImpl.callCount != 2 {
		t.Fatalf("provider calls = %d, want 2", providerImpl.callCount)
	}
}

func TestParseSubAgentOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "plain json",
			input: `{"summary":"s","findings":["f"],"patches":["p"],"risks":["r"],"next_actions":["n"],"artifacts":["a"]}`,
		},
		{
			name:  "json with prefix and suffix",
			input: "result:\n```json\n{\"summary\":\"s\",\"findings\":[\"f\"],\"patches\":[\"p\"],\"risks\":[\"r\"],\"next_actions\":[\"n\"],\"artifacts\":[\"a\"]}\n```\nend",
		},
		{
			name:    "invalid json",
			input:   `summary only`,
			wantErr: true,
		},
		{
			name: "pick object with output contract keys",
			input: strings.Join([]string{
				`{"example":true}`,
				`{"summary":"s","findings":["f"],"patches":["p"],"risks":["r"],"next_actions":["n"],"artifacts":["a"]}`,
			}, "\n"),
		},
		{
			name:    "single non-contract object should fail",
			input:   `{"example":true}`,
			wantErr: true,
		},
		{
			name:    "contract object with wrong types should fail",
			input:   `{"summary":123,"findings":["f"],"patches":["p"],"risks":["r"],"next_actions":["n"],"artifacts":["a"]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseSubAgentOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSubAgentOutput() error = %v", err)
			}
			if got.Summary != "s" {
				t.Fatalf("summary = %q, want %q", got.Summary, "s")
			}
		})
	}
}

func TestEmitCapabilityDeniedEventRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 1)}
	service.events <- RuntimeEvent{Type: EventSubAgentProgress}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{}, 1)
	go func() {
		emitCapabilityDeniedEvent(ctx, service, subagent.StepInput{
			RunID:     "run-cap-denied-canceled",
			SessionID: "session-cap-denied-canceled",
			Role:      subagent.RoleCoder,
			Task:      subagent.Task{ID: "task-cap-denied-canceled"},
		}, "bash")
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("emitCapabilityDeniedEvent() blocked when context is canceled")
	}
}

func TestEmitCapabilityDeniedEventEmitsPayload(t *testing.T) {
	t.Parallel()

	service := &Service{events: make(chan RuntimeEvent, 1)}
	emitCapabilityDeniedEvent(context.Background(), service, subagent.StepInput{
		RunID:     "run-cap-denied",
		SessionID: "session-cap-denied",
		Role:      subagent.RoleReviewer,
		Task:      subagent.Task{ID: "task-cap-denied"},
	}, "  bash  ")

	select {
	case event := <-service.Events():
		if event.Type != EventSubAgentToolCallDenied {
			t.Fatalf("event type = %q, want %q", event.Type, EventSubAgentToolCallDenied)
		}
		payload, ok := event.Payload.(SubAgentToolCallEventPayload)
		if !ok {
			t.Fatalf("payload type = %T", event.Payload)
		}
		if payload.ToolName != "bash" || payload.Decision != permissionDecisionDeny || payload.Error != "capability denied" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
	default:
		t.Fatal("expected capability denied event to be emitted")
	}
}

func TestParseSubAgentOutputPayloadAndMaxIntBranches(t *testing.T) {
	t.Parallel()

	_, err := parseSubAgentOutputPayload(`{"summary":"x"`)
	if err == nil || !strings.Contains(err.Error(), "parse subagent output json") {
		t.Fatalf("expected invalid json error, got %v", err)
	}

	_, err = parseSubAgentOutputPayload(`{"summary":"s","findings":[],"patches":[],"risks":[],"next_actions":[]}`)
	if err == nil || !strings.Contains(err.Error(), `missing required key "artifacts"`) {
		t.Fatalf("expected missing key error, got %v", err)
	}

	_, err = parseSubAgentOutputPayload(`{"summary":"s","findings":"bad","patches":[],"risks":[],"next_actions":[],"artifacts":[]}`)
	if err == nil || !strings.Contains(err.Error(), `must be []string`) {
		t.Fatalf("expected []string type error, got %v", err)
	}

	out, err := parseSubAgentOutputPayload(`{"summary":" ok ","findings":["f"],"patches":[],"risks":[],"next_actions":[],"artifacts":[]}`)
	if err != nil {
		t.Fatalf("parseSubAgentOutputPayload() unexpected error: %v", err)
	}
	if out.Summary != "ok" {
		t.Fatalf("expected summary to be trimmed, got %q", out.Summary)
	}

	if got := maxInt(4, 9); got != 9 {
		t.Fatalf("maxInt(4,9) = %d", got)
	}
	if got := maxInt(11, 2); got != 11 {
		t.Fatalf("maxInt(11,2) = %d", got)
	}
}

func assertSubAgentToolEventPayload(
	t *testing.T,
	events []RuntimeEvent,
	eventType EventType,
	toolName string,
	decision string,
	truncated bool,
) {
	t.Helper()
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		payload, ok := event.Payload.(SubAgentToolCallEventPayload)
		if !ok {
			t.Fatalf("payload type = %T, want SubAgentToolCallEventPayload", event.Payload)
		}
		if payload.ToolName != toolName {
			t.Fatalf("tool_name = %q, want %q", payload.ToolName, toolName)
		}
		if payload.Decision != decision {
			t.Fatalf("decision = %q, want %q", payload.Decision, decision)
		}
		if payload.Truncated != truncated {
			t.Fatalf("truncated = %v, want %v", payload.Truncated, truncated)
		}
		return
	}
	t.Fatalf("event %q not found", eventType)
}

func newRuntimeSubAgentStepInput(
	t *testing.T,
	service *Service,
	policy subagent.RolePolicy,
	task subagent.Task,
) subagent.StepInput {
	t.Helper()
	return subagent.StepInput{
		Role:     policy.Role,
		Policy:   policy,
		Task:     task,
		Workdir:  t.TempDir(),
		Executor: newSubAgentRuntimeToolExecutor(service),
	}
}

type failingToolExecutor struct {
	err      error
	decision string
}

func (f failingToolExecutor) ListToolSpecs(
	ctx context.Context,
	input subagent.ToolSpecListInput,
) ([]providertypes.ToolSpec, error) {
	_ = ctx
	_ = input
	return []providertypes.ToolSpec{{Name: "filesystem_read_file", Schema: map[string]any{"type": "object"}}}, nil
}

func (f failingToolExecutor) ExecuteTool(
	ctx context.Context,
	input subagent.ToolExecutionInput,
) (subagent.ToolExecutionResult, error) {
	_ = ctx
	_ = input
	decision := strings.TrimSpace(f.decision)
	if decision == "" {
		decision = "error"
	}
	return subagent.ToolExecutionResult{
		Name:     "filesystem_read_file",
		Content:  "",
		Decision: decision,
		IsError:  true,
	}, f.err
}
