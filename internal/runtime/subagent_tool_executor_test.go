package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

func TestSubAgentRuntimeToolExecutorListToolSpecs(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{
			specs: []providertypes.ToolSpec{
				{Name: "filesystem_read_file"},
				{Name: "bash"},
			},
		},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	executor := newSubAgentRuntimeToolExecutor(service)

	tests := []struct {
		name     string
		allow    []string
		wantSize int
	}{
		{name: "no allowlist", allow: nil, wantSize: 0},
		{name: "single allowlist", allow: []string{"bash"}, wantSize: 1},
		{name: "case-insensitive allowlist", allow: []string{"FILESYSTEM_READ_FILE"}, wantSize: 1},
		{name: "empty allowlist denies all", allow: []string{""}, wantSize: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			specs, err := executor.ListToolSpecs(context.Background(), subagent.ToolSpecListInput{
				SessionID:    "session-list-tools",
				Role:         subagent.RoleCoder,
				AllowedTools: tt.allow,
			})
			if err != nil {
				t.Fatalf("ListToolSpecs() error = %v", err)
			}
			if len(specs) != tt.wantSize {
				t.Fatalf("len(specs) = %d, want %d", len(specs), tt.wantSize)
			}
		})
	}
}

func TestSubAgentRuntimeToolExecutorExecuteToolEvents(t *testing.T) {
	t.Parallel()

	t.Run("allow should emit started and result", func(t *testing.T) {
		t.Parallel()

		toolManager := &stubToolManager{
			executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
				return tools.ToolResult{
					ToolCallID: input.ID,
					Name:       input.Name,
					Content:    "ok",
					Metadata:   map[string]any{"truncated": true},
				}, nil
			},
		}
		service := NewWithFactory(
			newRuntimeConfigManager(t),
			toolManager,
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service)

		result, err := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-tool-allow",
			SessionID: "session-subagent-tool-allow",
			TaskID:    "task-subagent-tool-allow",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:allow",
			Workdir:   t.TempDir(),
			Timeout:   2 * time.Second,
			Call: providertypes.ToolCall{
				ID:        "call-allow",
				Name:      "filesystem_read_file",
				Arguments: `{"path":"README.md"}`,
			},
		})
		if err != nil {
			t.Fatalf("ExecuteTool() error = %v", err)
		}
		if result.Decision != permissionDecisionAllow {
			t.Fatalf("decision = %q, want %q", result.Decision, permissionDecisionAllow)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallResult})
		assertSubAgentToolEventPayload(t, events, EventSubAgentToolCallResult, "filesystem_read_file", permissionDecisionAllow, true)
	})

	t.Run("permission deny should emit denied", func(t *testing.T) {
		t.Parallel()

		registry := tools.NewRegistry()
		registry.Register(&stubTool{name: "bash", content: "ok"})
		gateway, err := security.NewStaticGateway(security.DecisionDeny, nil)
		if err != nil {
			t.Fatalf("NewStaticGateway() error = %v", err)
		}
		manager, err := tools.NewManager(registry, gateway, nil)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			manager,
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service)

		result, execErr := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-tool-deny",
			SessionID: "session-subagent-tool-deny",
			TaskID:    "task-subagent-tool-deny",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:deny",
			Workdir:   t.TempDir(),
			Timeout:   2 * time.Second,
			Call: providertypes.ToolCall{
				ID:        "call-deny",
				Name:      "bash",
				Arguments: `{"command":"echo hi"}`,
			},
		})
		if execErr == nil {
			t.Fatalf("expected permission error")
		}
		if !errors.Is(execErr, tools.ErrPermissionDenied) {
			t.Fatalf("expected ErrPermissionDenied, got %v", execErr)
		}
		if result.Decision != string(security.DecisionDeny) {
			t.Fatalf("decision = %q, want %q", result.Decision, security.DecisionDeny)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallDenied})
		assertSubAgentToolEventPayload(t, events, EventSubAgentToolCallDenied, "bash", string(security.DecisionDeny), false)
	})

	t.Run("non-permission error should include elapsed and error payload", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			&stubToolManager{
				executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
					_ = ctx
					_ = input
					return tools.ToolResult{}, errors.New("tool manager down")
				},
			},
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service)
		_, execErr := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-tool-error",
			SessionID: "session-subagent-tool-error",
			TaskID:    "task-subagent-tool-error",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:error",
			Workdir:   t.TempDir(),
			Timeout:   2 * time.Second,
			Call: providertypes.ToolCall{
				ID:        "call-error",
				Name:      "filesystem_read_file",
				Arguments: `{"path":"README.md"}`,
			},
		})
		if execErr == nil {
			t.Fatalf("expected execution error")
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallResult})
		for _, evt := range events {
			if evt.Type != EventSubAgentToolCallResult {
				continue
			}
			payload, ok := evt.Payload.(SubAgentToolCallEventPayload)
			if !ok {
				t.Fatalf("payload type = %T, want SubAgentToolCallEventPayload", evt.Payload)
			}
			if payload.ElapsedMS < 0 {
				t.Fatalf("elapsed_ms = %d, want >= 0", payload.ElapsedMS)
			}
			if !strings.Contains(payload.Error, "tool manager down") {
				t.Fatalf("error = %q, want contain tool manager down", payload.Error)
			}
			return
		}
		t.Fatalf("result event not found")
	})
}

func TestSubAgentToolEventEmitRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	service := NewWithFactory(
		newRuntimeConfigManager(t),
		&stubToolManager{
			executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
				_ = input
				if err := ctx.Err(); err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{
					ToolCallID: input.ID,
					Name:       input.Name,
					Content:    "ok",
				}, nil
			},
		},
		newMemoryStore(),
		&scriptedProviderFactory{provider: &scriptedProvider{}},
		nil,
	)
	service.events = make(chan RuntimeEvent, 1)
	service.events <- RuntimeEvent{Type: EventSubAgentProgress}
	executor := newSubAgentRuntimeToolExecutor(service)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := executor.ExecuteTool(ctx, subagent.ToolExecutionInput{
			RunID:     "run-subagent-tool-canceled",
			SessionID: "session-subagent-tool-canceled",
			TaskID:    "task-subagent-tool-canceled",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:canceled",
			Workdir:   t.TempDir(),
			Timeout:   2 * time.Second,
			Call: providertypes.ToolCall{
				ID:        "call-canceled",
				Name:      "filesystem_read_file",
				Arguments: `{"path":"README.md"}`,
			},
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ExecuteTool() blocked when event channel is full and context canceled")
	}
}
