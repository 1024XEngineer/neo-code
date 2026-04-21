package runtime

import (
	"context"
	"errors"
	"path/filepath"
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
		{name: "unknown tool allowlist", allow: []string{"webfetch"}, wantSize: 0},
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

	t.Run("permission reject message should emit denied", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			&stubToolManager{
				executeFn: func(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
					_ = ctx
					return tools.ToolResult{
						ToolCallID: input.ID,
						Name:       input.Name,
						Content:    "permission rejected",
						IsError:    true,
					}, errors.New(permissionRejectedErrorMessage)
				},
			},
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service)

		result, execErr := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-tool-reject",
			SessionID: "session-subagent-tool-reject",
			TaskID:    "task-subagent-tool-reject",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:reject",
			Workdir:   t.TempDir(),
			Timeout:   2 * time.Second,
			Call: providertypes.ToolCall{
				ID:        "call-reject",
				Name:      "filesystem_read_file",
				Arguments: `{"path":"README.md"}`,
			},
		})
		if execErr == nil || !strings.Contains(execErr.Error(), "permission rejected by user") {
			t.Fatalf("expected permission rejected error, got %v", execErr)
		}
		if result.Decision != permissionDecisionDeny {
			t.Fatalf("decision = %q, want %q", result.Decision, permissionDecisionDeny)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallDenied})
		assertSubAgentToolEventPayload(t, events, EventSubAgentToolCallDenied, "filesystem_read_file", permissionDecisionDeny, false)
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

	t.Run("capability allowed_paths should deny out-of-scope filesystem access", func(t *testing.T) {
		t.Parallel()

		registry := tools.NewRegistry()
		registry.Register(&stubTool{name: tools.ToolNameFilesystemReadFile, content: "ok"})
		gateway, err := security.NewStaticGateway(security.DecisionAllow, nil)
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

		workdir := t.TempDir()
		allowed := filepath.Join(workdir, "safe")
		denied := filepath.Join(workdir, "unsafe", "note.txt")
		result, execErr := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-cap-path-deny",
			SessionID: "session-subagent-cap-path-deny",
			TaskID:    "task-subagent-cap-path-deny",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:cap-path-deny",
			Workdir:   workdir,
			Timeout:   2 * time.Second,
			Capability: subagent.Capability{
				AllowedTools: []string{tools.ToolNameFilesystemReadFile},
				AllowedPaths: []string{allowed},
			},
			Call: providertypes.ToolCall{
				ID:        "call-cap-path-deny",
				Name:      tools.ToolNameFilesystemReadFile,
				Arguments: `{"path":"` + denied + `"}`,
			},
		})
		if execErr == nil {
			t.Fatalf("expected capability deny error")
		}
		if !errors.Is(execErr, tools.ErrCapabilityDenied) {
			t.Fatalf("expected ErrCapabilityDenied, got %v", execErr)
		}
		if result.Decision != permissionDecisionDeny {
			t.Fatalf("decision = %q, want %q", result.Decision, permissionDecisionDeny)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventSubAgentToolCallStarted, EventSubAgentToolCallDenied})
		assertSubAgentToolEventPayload(
			t,
			events,
			EventSubAgentToolCallDenied,
			tools.ToolNameFilesystemReadFile,
			permissionDecisionDeny,
			false,
		)
	})

	t.Run("capability allowed_paths should allow in-scope filesystem access", func(t *testing.T) {
		t.Parallel()

		registry := tools.NewRegistry()
		registry.Register(&stubTool{name: tools.ToolNameFilesystemReadFile, content: "ok"})
		gateway, err := security.NewStaticGateway(security.DecisionAllow, nil)
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

		workdir := t.TempDir()
		allowed := filepath.Join(workdir, "safe")
		allowedFile := filepath.Join(allowed, "note.txt")
		result, execErr := executor.ExecuteTool(context.Background(), subagent.ToolExecutionInput{
			RunID:     "run-subagent-cap-path-allow",
			SessionID: "session-subagent-cap-path-allow",
			TaskID:    "task-subagent-cap-path-allow",
			Role:      subagent.RoleCoder,
			AgentID:   "subagent:cap-path-allow",
			Workdir:   workdir,
			Timeout:   2 * time.Second,
			Capability: subagent.Capability{
				AllowedTools: []string{tools.ToolNameFilesystemReadFile},
				AllowedPaths: []string{allowed},
			},
			Call: providertypes.ToolCall{
				ID:        "call-cap-path-allow",
				Name:      tools.ToolNameFilesystemReadFile,
				Arguments: `{"path":"` + allowedFile + `"}`,
			},
		})
		if execErr != nil {
			t.Fatalf("ExecuteTool() error = %v", execErr)
		}
		if result.Decision != permissionDecisionAllow {
			t.Fatalf("decision = %q, want %q", result.Decision, permissionDecisionAllow)
		}
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

func TestResolveSubAgentCapabilityToken(t *testing.T) {
	t.Parallel()

	t.Run("explicit capability token should be normalized and reused", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			&stubToolManager{},
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service).(*subAgentRuntimeToolExecutor)
		token := security.CapabilityToken{
			ID:           "token-1",
			TaskID:       "task-1",
			AgentID:      "agent-1",
			IssuedAt:     time.Now().UTC(),
			ExpiresAt:    time.Now().UTC().Add(2 * time.Minute),
			AllowedTools: []string{" filesystem_read_file ", "filesystem_read_file"},
		}
		got := executor.resolveCapabilityToken(subagent.ToolExecutionInput{CapabilityToken: &token})
		if got == nil {
			t.Fatalf("expected token")
		}
		if len(got.AllowedTools) != 1 || got.AllowedTools[0] != tools.ToolNameFilesystemReadFile {
			t.Fatalf("normalized allowed tools = %v", got.AllowedTools)
		}
	})

	t.Run("capability should mint signed token when manager exposes signer", func(t *testing.T) {
		t.Parallel()

		registry := tools.NewRegistry()
		registry.Register(&stubTool{name: tools.ToolNameFilesystemReadFile, content: "ok"})
		gateway, err := security.NewStaticGateway(security.DecisionAllow, nil)
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
		executor := newSubAgentRuntimeToolExecutor(service).(*subAgentRuntimeToolExecutor)
		workdir := t.TempDir()
		got := executor.resolveCapabilityToken(subagent.ToolExecutionInput{
			TaskID:  "task-capability-sign",
			AgentID: "subagent:capability-sign",
			Call: providertypes.ToolCall{
				Name: tools.ToolNameFilesystemReadFile,
			},
			Capability: subagent.Capability{
				AllowedTools: []string{tools.ToolNameFilesystemReadFile},
				AllowedPaths: []string{workdir, workdir},
			},
		})
		if got == nil {
			t.Fatalf("expected signed capability token")
		}
		if strings.TrimSpace(got.Signature) == "" {
			t.Fatalf("expected non-empty signature")
		}
		if len(got.AllowedPaths) != 1 || got.AllowedPaths[0] != workdir {
			t.Fatalf("allowed_paths = %v, want [%s]", got.AllowedPaths, workdir)
		}
		if err := manager.CapabilitySigner().Verify(*got); err != nil {
			t.Fatalf("verify signed token: %v", err)
		}
	})

	t.Run("capability should be skipped when no constraints", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			&stubToolManager{},
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service).(*subAgentRuntimeToolExecutor)
		got := executor.resolveCapabilityToken(subagent.ToolExecutionInput{
			TaskID: "task-empty-cap",
			Call: providertypes.ToolCall{
				Name: tools.ToolNameFilesystemReadFile,
			},
		})
		if got != nil {
			t.Fatalf("expected nil token, got %+v", got)
		}
	})

	t.Run("capability should be skipped when manager has no signer provider", func(t *testing.T) {
		t.Parallel()

		service := NewWithFactory(
			newRuntimeConfigManager(t),
			&stubToolManager{},
			newMemoryStore(),
			&scriptedProviderFactory{provider: &scriptedProvider{}},
			nil,
		)
		executor := newSubAgentRuntimeToolExecutor(service).(*subAgentRuntimeToolExecutor)
		got := executor.resolveCapabilityToken(subagent.ToolExecutionInput{
			TaskID:  "task-no-signer",
			AgentID: "subagent:no-signer",
			Call: providertypes.ToolCall{
				Name: tools.ToolNameFilesystemReadFile,
			},
			Capability: subagent.Capability{
				AllowedTools: []string{tools.ToolNameFilesystemReadFile},
				AllowedPaths: []string{t.TempDir()},
			},
		})
		if got != nil {
			t.Fatalf("expected nil token when signer provider is unavailable, got %+v", got)
		}
	})

	t.Run("capability should fall back to call name when allowed_tools is empty", func(t *testing.T) {
		t.Parallel()

		registry := tools.NewRegistry()
		registry.Register(&stubTool{name: tools.ToolNameFilesystemReadFile, content: "ok"})
		gateway, err := security.NewStaticGateway(security.DecisionAllow, nil)
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
		executor := newSubAgentRuntimeToolExecutor(service).(*subAgentRuntimeToolExecutor)
		got := executor.resolveCapabilityToken(subagent.ToolExecutionInput{
			TaskID:  "task-fallback-call",
			AgentID: "subagent:fallback-call",
			Call: providertypes.ToolCall{
				Name: tools.ToolNameFilesystemReadFile,
			},
			Capability: subagent.Capability{
				AllowedPaths: []string{t.TempDir()},
			},
		})
		if got == nil {
			t.Fatalf("expected signed capability token from call-name fallback")
		}
		if len(got.AllowedTools) != 1 || got.AllowedTools[0] != tools.ToolNameFilesystemReadFile {
			t.Fatalf("allowed_tools = %v, want [%s]", got.AllowedTools, tools.ToolNameFilesystemReadFile)
		}
	})
}

func TestSubAgentCapabilityAllowlistHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeAllowlistToList(nil); got != nil {
		t.Fatalf("normalizeAllowlistToList(nil) = %v, want nil", got)
	}
	if got := normalizeAllowlistToList([]string{" Bash ", "bash", "filesystem_read_file"}); len(got) != 2 || got[0] != "bash" {
		t.Fatalf("normalizeAllowlistToList unexpected result: %v", got)
	}
	if got := normalizePathAllowlist([]string{" ", "/a", "/a", "/b"}); len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("normalizePathAllowlist unexpected result: %v", got)
	}
}
