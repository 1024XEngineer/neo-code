package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
	"neo-code/internal/tools/mcp"
)

type managerStubTool struct {
	name      string
	content   string
	err       error
	policy    MicroCompactPolicy
	callCount int
	lastCall  ToolCallInput
}

func (t *managerStubTool) Name() string { return t.name }

func (t *managerStubTool) Description() string { return "stub tool" }

func (t *managerStubTool) Schema() map[string]any { return map[string]any{"type": "object"} }

func (t *managerStubTool) MicroCompactPolicy() MicroCompactPolicy { return t.policy }

func (t *managerStubTool) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	t.callCount++
	t.lastCall = call
	return ToolResult{
		Name:    t.name,
		Content: t.content,
	}, t.err
}

type stubSandbox struct {
	err        error
	plan       *security.WorkspaceExecutionPlan
	callCount  int
	lastAction security.Action
}

type executorWithoutMicroCompactPolicy struct{}

func (executorWithoutMicroCompactPolicy) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (executorWithoutMicroCompactPolicy) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	return ToolResult{}, ctx.Err()
}

func (executorWithoutMicroCompactPolicy) Supports(name string) bool { return false }

func (s *stubSandbox) Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error) {
	s.callCount++
	s.lastAction = action
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.plan, s.err
}

func TestDefaultManagerListAvailableSpecs(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(&managerStubTool{name: "bash"})
	manager, err := NewManager(registry, nil, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	specs, err := manager.ListAvailableSpecs(context.Background(), SpecListInput{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("list specs: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "bash" {
		t.Fatalf("unexpected specs: %+v", specs)
	}
}

func TestDefaultManagerMicroCompactPolicy(t *testing.T) {
	t.Parallel()

	t.Run("nil manager defaults to compact", func(t *testing.T) {
		t.Parallel()

		var manager *DefaultManager
		if got := manager.MicroCompactPolicy("custom_tool"); got != MicroCompactPolicyCompact {
			t.Fatalf("expected compact default, got %q", got)
		}
	})

	t.Run("executor without policy support defaults to compact", func(t *testing.T) {
		t.Parallel()

		manager, err := NewManager(executorWithoutMicroCompactPolicy{}, nil, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		if got := manager.MicroCompactPolicy("custom_tool"); got != MicroCompactPolicyCompact {
			t.Fatalf("expected compact default, got %q", got)
		}
	})

	t.Run("executor policy is forwarded", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		registry.Register(&managerStubTool{name: "preserve_tool", policy: MicroCompactPolicyPreserveHistory})

		manager, err := NewManager(registry, nil, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		if got := manager.MicroCompactPolicy("preserve_tool"); got != MicroCompactPolicyPreserveHistory {
			t.Fatalf("expected preserve history, got %q", got)
		}
	})
}

func TestDefaultManagerListAvailableSpecsBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		manager   *DefaultManager
		ctx       func() context.Context
		expectErr string
	}{
		{
			name:      "nil manager executor",
			manager:   &DefaultManager{},
			ctx:       context.Background,
			expectErr: "manager executor is nil",
		},
		{
			name: func() string { return "canceled context" }(),
			manager: func() *DefaultManager {
				registry := NewRegistry()
				registry.Register(&managerStubTool{name: "bash"})
				manager, _ := NewManager(registry, nil, nil)
				return manager
			}(),
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expectErr: context.Canceled.Error(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.manager.ListAvailableSpecs(tt.ctx(), SpecListInput{})
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestDefaultManagerExecute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		rules             []security.Rule
		sandboxErr        error
		input             ToolCallInput
		expectErr         string
		expectContent     []string
		expectDecision    string
		expectCalls       int
		expectSandboxRuns int
	}{
		{
			name: "allow executes tool",
			input: ToolCallInput{
				ID:        "call-1",
				Name:      "bash",
				Arguments: []byte(`{"command":"echo hi"}`),
			},
			expectContent:     []string{"ok"},
			expectCalls:       1,
			expectSandboxRuns: 1,
		},
		{
			name: "deny blocks execution before sandbox",
			rules: []security.Rule{
				{ID: "deny-bash", Resource: "bash", Type: security.ActionTypeBash, Decision: security.DecisionDeny, Reason: "bash denied"},
			},
			input: ToolCallInput{
				ID:        "call-2",
				Name:      "bash",
				Arguments: []byte(`{"command":"echo hi"}`),
			},
			expectErr:         "bash denied",
			expectContent:     []string{"tool error", "tool: bash", "reason: bash denied"},
			expectDecision:    "deny",
			expectCalls:       0,
			expectSandboxRuns: 0,
		},
		{
			name: "ask blocks execution before sandbox",
			rules: []security.Rule{
				{ID: "ask-private", Resource: "webfetch", Type: security.ActionTypeRead, Decision: security.DecisionAsk, Reason: "requires approval"},
			},
			input: ToolCallInput{
				ID:        "call-3",
				Name:      "webfetch",
				Arguments: []byte(`{"url":"https://example.com"}`),
			},
			expectErr:         "requires approval",
			expectContent:     []string{"tool error", "tool: webfetch", "reason: requires approval"},
			expectDecision:    "ask",
			expectCalls:       0,
			expectSandboxRuns: 0,
		},
		{
			name: "sandbox blocks after allow",
			input: ToolCallInput{
				ID:        "call-5",
				Name:      "filesystem_write_file",
				Arguments: []byte(`{"path":"notes.txt","content":"hi"}`),
			},
			sandboxErr:        errors.New("workspace denied"),
			expectErr:         "workspace denied",
			expectContent:     []string{"tool error", "reason: workspace sandbox rejected action"},
			expectCalls:       0,
			expectSandboxRuns: 1,
		},
		{
			name: "unknown tool uses executor error",
			input: ToolCallInput{
				ID:   "call-4",
				Name: "missing",
			},
			expectErr:         "tool: not found",
			expectContent:     []string{"tool error", "tool: missing"},
			expectDecision:    "",
			expectCalls:       0,
			expectSandboxRuns: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := NewRegistry()
			bashTool := &managerStubTool{name: "bash", content: "ok"}
			webTool := &managerStubTool{name: "webfetch", content: "ok"}
			writeTool := &managerStubTool{name: "filesystem_write_file", content: "ok"}
			registry.Register(bashTool)
			registry.Register(webTool)
			registry.Register(writeTool)

			engine, err := security.NewStaticGateway(security.DecisionAllow, tt.rules)
			if err != nil {
				t.Fatalf("new engine: %v", err)
			}
			sandbox := &stubSandbox{err: tt.sandboxErr}
			manager, err := NewManager(registry, engine, sandbox)
			if err != nil {
				t.Fatalf("new manager: %v", err)
			}

			result, execErr := manager.Execute(context.Background(), tt.input)
			if tt.expectErr != "" {
				if execErr == nil || !strings.Contains(execErr.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, execErr)
				}
			} else if execErr != nil {
				t.Fatalf("unexpected error: %v", execErr)
			}

			for _, fragment := range tt.expectContent {
				if !strings.Contains(result.Content, fragment) {
					t.Fatalf("expected content containing %q, got %q", fragment, result.Content)
				}
			}
			if decision, _ := result.Metadata["permission_decision"].(string); decision != tt.expectDecision {
				t.Fatalf("expected permission decision %q, got %q", tt.expectDecision, decision)
			}

			totalCalls := bashTool.callCount + webTool.callCount + writeTool.callCount
			if totalCalls != tt.expectCalls {
				t.Fatalf("expected %d tool calls, got %d", tt.expectCalls, totalCalls)
			}
			if sandbox.callCount != tt.expectSandboxRuns {
				t.Fatalf("expected sandbox runs %d, got %d", tt.expectSandboxRuns, sandbox.callCount)
			}
		})
	}
}

func TestDefaultManagerExecuteBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		manager   *DefaultManager
		input     ToolCallInput
		expectErr string
	}{
		{
			name:      "nil manager executor",
			manager:   &DefaultManager{},
			input:     ToolCallInput{Name: "bash"},
			expectErr: "manager executor is nil",
		},
		{
			name: "invalid permission mapping",
			manager: func() *DefaultManager {
				registry := NewRegistry()
				registry.Register(&managerStubTool{name: "custom_tool"})
				manager, _ := NewManager(registry, nil, nil)
				return manager
			}(),
			input:     ToolCallInput{Name: "custom_tool"},
			expectErr: "unsupported permission mapping",
		},
		{
			name: "canceled evaluation context",
			manager: func() *DefaultManager {
				registry := NewRegistry()
				registry.Register(&managerStubTool{name: "bash"})
				manager, _ := NewManager(registry, nil, nil)
				return manager
			}(),
			input:     ToolCallInput{Name: "bash", Arguments: []byte(`{"command":"echo hi"}`)},
			expectErr: context.Canceled.Error(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			if tt.expectErr == context.Canceled.Error() {
				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = canceled
			}

			_, err := tt.manager.Execute(ctx, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestDefaultManagerExecuteWithWorkspaceSandbox(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	tool := &managerStubTool{name: "filesystem_write_file", content: "ok"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	manager, err := NewManager(registry, engine, security.NewWorkspaceSandbox())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	workdir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(workdir, "link")); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	_, execErr := manager.Execute(context.Background(), ToolCallInput{
		Name:      "filesystem_write_file",
		Arguments: []byte(`{"path":"link/outside.txt","content":"hello"}`),
		Workdir:   workdir,
	})
	if execErr == nil || !strings.Contains(execErr.Error(), "escapes workspace root via symlink") {
		t.Fatalf("expected sandbox escape error, got %v", execErr)
	}
	if tool.callCount != 0 {
		t.Fatalf("expected blocked tool not to execute, got %d calls", tool.callCount)
	}
}

func TestDefaultManagerExecuteForwardsWorkspacePlanToTool(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	tool := &managerStubTool{name: "filesystem_write_file", content: "ok"}
	registry.Register(tool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	plan := &security.WorkspaceExecutionPlan{
		Root:            "workspace-root",
		Target:          "workspace-root/notes.txt",
		RequestedTarget: "notes.txt",
	}
	manager, err := NewManager(registry, engine, &stubSandbox{plan: plan})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	result, execErr := manager.Execute(context.Background(), ToolCallInput{
		Name:      "filesystem_write_file",
		Arguments: []byte(`{"path":"notes.txt","content":"hello"}`),
		Workdir:   t.TempDir(),
	})
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("expected ok result, got %+v", result)
	}
	if tool.lastCall.WorkspacePlan == nil || tool.lastCall.WorkspacePlan.Target != plan.Target {
		t.Fatalf("expected workspace plan to be forwarded, got %+v", tool.lastCall.WorkspacePlan)
	}
}

func TestPermissionDecisionError(t *testing.T) {
	t.Parallel()

	err := &PermissionDecisionError{
		decision: security.DecisionAsk,
		toolName: "webfetch",
		action: security.Action{
			Type: security.ActionTypeRead,
			Payload: security.ActionPayload{
				ToolName: "webfetch",
				Resource: "webfetch",
			},
		},
		reason: "approval required",
		ruleID: "rule-ask-webfetch",
	}
	if !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("expected reason in error, got %q", err.Error())
	}
	if err.Decision() != "ask" {
		t.Fatalf("expected ask decision, got %q", err.Decision())
	}
	if err.ToolName() != "webfetch" {
		t.Fatalf("expected tool name webfetch, got %q", err.ToolName())
	}
	if err.Reason() != "approval required" {
		t.Fatalf("expected approval reason, got %q", err.Reason())
	}
	if err.RuleID() != "rule-ask-webfetch" {
		t.Fatalf("expected rule id rule-ask-webfetch, got %q", err.RuleID())
	}
	if err.Action().Type != security.ActionTypeRead {
		t.Fatalf("expected action type read, got %q", err.Action().Type)
	}
	if err.RememberScope() != "" {
		t.Fatalf("expected empty remember scope, got %q", err.RememberScope())
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("permission error should not match unrelated errors")
	}

	denyErr := &PermissionDecisionError{}
	if !strings.Contains(denyErr.Error(), "permission denied") {
		t.Fatalf("expected default deny message, got %q", denyErr.Error())
	}
	if denyErr.Decision() != "" {
		t.Fatalf("expected empty decision, got %q", denyErr.Decision())
	}
	if denyErr.ToolName() != "" {
		t.Fatalf("expected empty tool name, got %q", denyErr.ToolName())
	}
	if denyErr.RememberScope() != "" {
		t.Fatalf("expected empty remember scope, got %q", denyErr.RememberScope())
	}

	var nilErr *PermissionDecisionError
	if nilErr.Error() != "" || nilErr.Decision() != "" || nilErr.ToolName() != "" || nilErr.RememberScope() != "" {
		t.Fatalf("expected nil permission error helpers to be empty")
	}
	if nilErr.Reason() != "" || nilErr.RuleID() != "" || nilErr.Action() != (security.Action{}) {
		t.Fatalf("expected nil permission error extended helpers to be empty")
	}

	defaultAsk := &PermissionDecisionError{decision: security.DecisionAsk}
	if !strings.Contains(defaultAsk.Error(), "permission approval required") {
		t.Fatalf("expected default ask message, got %q", defaultAsk.Error())
	}
}

func TestNewManagerRejectsNilExecutor(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "executor is nil") {
		t.Fatalf("expected nil executor error, got manager=%v err=%v", manager, err)
	}
}

func TestDefaultManagerSessionPermissionMemory(t *testing.T) {
	t.Parallel()

	newAskManager := func(t *testing.T) (*DefaultManager, *managerStubTool) {
		t.Helper()
		registry := NewRegistry()
		webTool := &managerStubTool{name: "webfetch", content: "ok"}
		registry.Register(webTool)
		engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
			{
				ID:       "ask-webfetch",
				Type:     security.ActionTypeRead,
				Resource: "webfetch",
				Decision: security.DecisionAsk,
				Reason:   "requires approval",
			},
		})
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		manager, err := NewManager(registry, engine, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		return manager, webTool
	}

	t.Run("once allows only first follow-up", func(t *testing.T) {
		t.Parallel()
		manager, webTool := newAskManager(t)
		input := ToolCallInput{
			ID:        "call-once",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/once"}`),
			SessionID: "session-once",
		}

		_, err := manager.Execute(context.Background(), input)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected initial ask decision, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(input.SessionID, permissionErr.Action(), SessionPermissionScopeOnce); rememberErr != nil {
			t.Fatalf("remember once: %v", rememberErr)
		}

		result, err := manager.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("expected remembered once allow, got %v", err)
		}
		if result.IsError {
			t.Fatalf("expected non-error result, got %+v", result)
		}
		if webTool.callCount != 1 {
			t.Fatalf("expected tool call count 1 after once allow, got %d", webTool.callCount)
		}

		_, err = manager.Execute(context.Background(), input)
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected ask after once consumed, got %v", err)
		}
	})

	t.Run("always(session) keeps allowing in same session", func(t *testing.T) {
		t.Parallel()
		manager, webTool := newAskManager(t)
		input := ToolCallInput{
			ID:        "call-always",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/always"}`),
			SessionID: "session-always",
		}

		_, err := manager.Execute(context.Background(), input)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected initial ask decision, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(input.SessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
			t.Fatalf("remember always: %v", rememberErr)
		}

		for i := 0; i < 2; i++ {
			if _, err := manager.Execute(context.Background(), input); err != nil {
				t.Fatalf("expected always allow on iteration %d, got %v", i, err)
			}
		}
		if webTool.callCount != 2 {
			t.Fatalf("expected tool to execute twice, got %d", webTool.callCount)
		}
	})

	t.Run("reject denies in same session and keeps scope metadata", func(t *testing.T) {
		t.Parallel()
		manager, webTool := newAskManager(t)
		input := ToolCallInput{
			ID:        "call-reject",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/reject"}`),
			SessionID: "session-reject",
		}

		_, err := manager.Execute(context.Background(), input)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected initial ask decision, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(input.SessionID, permissionErr.Action(), SessionPermissionScopeReject); rememberErr != nil {
			t.Fatalf("remember reject: %v", rememberErr)
		}

		_, err = manager.Execute(context.Background(), input)
		if !errors.As(err, &permissionErr) {
			t.Fatalf("expected permission error, got %v", err)
		}
		if permissionErr.Decision() != "deny" {
			t.Fatalf("expected deny from remembered reject, got %q", permissionErr.Decision())
		}
		if permissionErr.RememberScope() != string(SessionPermissionScopeReject) {
			t.Fatalf("expected reject remember scope, got %q", permissionErr.RememberScope())
		}
		if webTool.callCount != 0 {
			t.Fatalf("expected rejected call to skip tool execution, got %d", webTool.callCount)
		}
	})

	t.Run("session memory does not leak across sessions", func(t *testing.T) {
		t.Parallel()
		manager, _ := newAskManager(t)
		inputA := ToolCallInput{
			ID:        "call-session-a",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/session-a"}`),
			SessionID: "session-a",
		}
		inputB := ToolCallInput{
			ID:        "call-session-b",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/session-a"}`),
			SessionID: "session-b",
		}

		_, err := manager.Execute(context.Background(), inputA)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) {
			t.Fatalf("expected permission ask on session A, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(inputA.SessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
			t.Fatalf("remember session A always: %v", rememberErr)
		}
		if _, err := manager.Execute(context.Background(), inputA); err != nil {
			t.Fatalf("expected session A to be allowed, got %v", err)
		}

		_, err = manager.Execute(context.Background(), inputB)
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected session B remain ask, got %v", err)
		}
	})

	t.Run("category matching shares decision across same tool category", func(t *testing.T) {
		t.Parallel()
		manager, _ := newAskManager(t)
		inputA := ToolCallInput{
			ID:        "call-target-a",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/a"}`),
			SessionID: "session-target",
		}
		inputB := ToolCallInput{
			ID:        "call-target-b",
			Name:      "webfetch",
			Arguments: []byte(`{"url":"https://example.com/b"}`),
			SessionID: "session-target",
		}

		_, err := manager.Execute(context.Background(), inputA)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) {
			t.Fatalf("expected permission ask on target A, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(inputA.SessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
			t.Fatalf("remember target A: %v", rememberErr)
		}
		if _, err := manager.Execute(context.Background(), inputA); err != nil {
			t.Fatalf("expected target A to be allowed, got %v", err)
		}

		if _, err := manager.Execute(context.Background(), inputB); err != nil {
			t.Fatalf("expected target B to inherit same-category allow, got %v", err)
		}
	})

	t.Run("filesystem read category applies across file/grep/glob", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		readTool := &managerStubTool{name: "filesystem_read_file", content: "ok"}
		grepTool := &managerStubTool{name: "filesystem_grep", content: "ok"}
		globTool := &managerStubTool{name: "filesystem_glob", content: "ok"}
		registry.Register(readTool)
		registry.Register(grepTool)
		registry.Register(globTool)

		engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
			{
				ID:       "ask-filesystem-read",
				Type:     security.ActionTypeRead,
				Resource: "filesystem_read_file",
				Decision: security.DecisionAsk,
				Reason:   "requires approval",
			},
			{
				ID:       "ask-filesystem-grep",
				Type:     security.ActionTypeRead,
				Resource: "filesystem_grep",
				Decision: security.DecisionAsk,
				Reason:   "requires approval",
			},
			{
				ID:       "ask-filesystem-glob",
				Type:     security.ActionTypeRead,
				Resource: "filesystem_glob",
				Decision: security.DecisionAsk,
				Reason:   "requires approval",
			},
		})
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		manager, err := NewManager(registry, engine, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}

		sessionID := "session-fs-read"
		readInput := ToolCallInput{
			ID:        "call-read",
			Name:      "filesystem_read_file",
			Arguments: []byte(`{"path":"internal/README.md"}`),
			SessionID: sessionID,
		}
		grepInput := ToolCallInput{
			ID:        "call-grep",
			Name:      "filesystem_grep",
			Arguments: []byte(`{"dir":"internal","pattern":"TODO"}`),
			SessionID: sessionID,
		}
		globInput := ToolCallInput{
			ID:        "call-glob",
			Name:      "filesystem_glob",
			Arguments: []byte(`{"dir":"internal","pattern":"*.go"}`),
			SessionID: sessionID,
		}

		_, err = manager.Execute(context.Background(), readInput)
		var permissionErr *PermissionDecisionError
		if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
			t.Fatalf("expected initial read ask, got %v", err)
		}
		if rememberErr := manager.RememberSessionDecision(sessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
			t.Fatalf("remember filesystem read category: %v", rememberErr)
		}

		if _, err := manager.Execute(context.Background(), grepInput); err != nil {
			t.Fatalf("expected grep allow via filesystem_read category, got %v", err)
		}
		if _, err := manager.Execute(context.Background(), globInput); err != nil {
			t.Fatalf("expected glob allow via filesystem_read category, got %v", err)
		}
	})

	t.Run("remembered allow does not override hard deny", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		readTool := &managerStubTool{name: "filesystem_read_file", content: "ok"}
		registry.Register(readTool)

		engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
			{
				ID:       "deny-private-key",
				Type:     security.ActionTypeRead,
				Resource: "filesystem_read_file",
				Decision: security.DecisionDeny,
				Reason:   "private key blocked",
			},
		})
		if err != nil {
			t.Fatalf("new engine: %v", err)
		}
		manager, err := NewManager(registry, engine, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}

		sessionID := "session-deny-priority"
		action := security.Action{
			Type: security.ActionTypeRead,
			Payload: security.ActionPayload{
				ToolName:   "filesystem_read_file",
				Resource:   "filesystem_read_file",
				Operation:  "read_file",
				TargetType: security.TargetTypePath,
				Target:     "README.md",
			},
		}
		if err := manager.RememberSessionDecision(sessionID, action, SessionPermissionScopeAlways); err != nil {
			t.Fatalf("remember allow: %v", err)
		}

		_, execErr := manager.Execute(context.Background(), ToolCallInput{
			ID:        "call-deny-priority",
			Name:      "filesystem_read_file",
			Arguments: []byte(`{"path":"C:/Users/test/.ssh/id_rsa"}`),
			SessionID: sessionID,
		})
		var permissionErr *PermissionDecisionError
		if !errors.As(execErr, &permissionErr) {
			t.Fatalf("expected permission error, got %v", execErr)
		}
		if permissionErr.Decision() != "deny" {
			t.Fatalf("expected hard deny to win, got %q", permissionErr.Decision())
		}
		if permissionErr.RuleID() != "deny-private-key" {
			t.Fatalf("expected deny rule id, got %q", permissionErr.RuleID())
		}
		if readTool.callCount != 0 {
			t.Fatalf("expected blocked call not to execute tool, got %d", readTool.callCount)
		}
	})
}

func TestBuildPermissionAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        ToolCallInput
		wantType     security.ActionType
		wantResource string
		wantTarget   string
		wantSandbox  string
		wantErr      string
	}{
		{
			name: "bash maps to bash action",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"echo hi","workdir":"scripts"}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash",
			wantTarget:   "echo hi",
			wantSandbox:  "scripts",
		},
		{
			name: "read file maps to read action",
			input: ToolCallInput{
				Name:      "filesystem_read_file",
				Arguments: []byte(`{"path":"main.go"}`),
			},
			wantType:     security.ActionTypeRead,
			wantResource: "filesystem_read_file",
			wantTarget:   "main.go",
			wantSandbox:  "main.go",
		},
		{
			name: "grep maps to read action",
			input: ToolCallInput{
				Name:      "filesystem_grep",
				Arguments: []byte(`{"dir":"internal"}`),
			},
			wantType:     security.ActionTypeRead,
			wantResource: "filesystem_grep",
			wantTarget:   "internal",
			wantSandbox:  "internal",
		},
		{
			name: "glob maps to read action",
			input: ToolCallInput{
				Name:      "filesystem_glob",
				Arguments: []byte(`{"dir":"cmd"}`),
			},
			wantType:     security.ActionTypeRead,
			wantResource: "filesystem_glob",
			wantTarget:   "cmd",
			wantSandbox:  "cmd",
		},
		{
			name: "write file maps to write action",
			input: ToolCallInput{
				Name:      "filesystem_write_file",
				Arguments: []byte(`{"path":"main.go"}`),
			},
			wantType:     security.ActionTypeWrite,
			wantResource: "filesystem_write_file",
			wantTarget:   "main.go",
			wantSandbox:  "main.go",
		},
		{
			name: "webfetch maps to read action",
			input: ToolCallInput{
				Name:      "webfetch",
				Arguments: []byte(`{"url":"https://example.com"}`),
			},
			wantType:     security.ActionTypeRead,
			wantResource: "webfetch",
			wantTarget:   "https://example.com",
		},
		{
			name: "write maps to write action",
			input: ToolCallInput{
				Name:      "filesystem_edit",
				Arguments: []byte(`{"path":"main.go"}`),
			},
			wantType:     security.ActionTypeWrite,
			wantResource: "filesystem_edit",
			wantTarget:   "main.go",
			wantSandbox:  "main.go",
		},
		{
			name: "mcp tool maps to mcp action",
			input: ToolCallInput{
				Name:      "mcp.github.create_issue",
				Arguments: []byte(`{"title":"hello"}`),
			},
			wantType:     security.ActionTypeMCP,
			wantResource: "mcp.github.create_issue",
			wantTarget:   "mcp.github.create_issue",
		},
		{
			name: "unsupported tool returns error",
			input: ToolCallInput{
				Name: "custom_tool",
			},
			wantErr: "unsupported permission mapping",
		},
		{
			name:    "empty tool name returns error",
			input:   ToolCallInput{},
			wantErr: "tool name is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			action, err := buildPermissionAction(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action.Type != tt.wantType {
				t.Fatalf("expected type %q, got %q", tt.wantType, action.Type)
			}
			if action.Payload.Resource != tt.wantResource {
				t.Fatalf("expected resource %q, got %q", tt.wantResource, action.Payload.Resource)
			}
			if action.Payload.Target != tt.wantTarget {
				t.Fatalf("expected target %q, got %q", tt.wantTarget, action.Payload.Target)
			}
			if action.Payload.SandboxTarget != tt.wantSandbox {
				t.Fatalf("expected sandbox target %q, got %q", tt.wantSandbox, action.Payload.SandboxTarget)
			}
		})
	}
}

func TestPermissionMapperHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      []byte
		key        string
		want       string
		serverTool string
		serverWant string
	}{
		{
			name:  "extracts string value",
			input: []byte(`{"path":"main.go"}`),
			key:   "path",
			want:  "main.go",
		},
		{
			name:  "invalid json returns empty",
			input: []byte(`{invalid`),
			key:   "path",
			want:  "",
		},
		{
			name:  "missing key returns empty",
			input: []byte(`{"url":"https://example.com"}`),
			key:   "path",
			want:  "",
		},
		{
			name:  "non string returns empty",
			input: []byte(`{"path":123}`),
			key:   "path",
			want:  "",
		},
		{
			name:       "mcp server target with server and tool",
			serverTool: "mcp.github.create_issue",
			serverWant: "mcp.github",
		},
		{
			name:       "mcp server target keeps dotted server id",
			serverTool: "mcp.github.enterprise.create_issue",
			serverWant: "mcp.github.enterprise",
		},
		{
			name:       "mcp server target without server",
			serverTool: "mcp",
			serverWant: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.key != "" {
				if got := extractStringArgument(tt.input, tt.key); got != tt.want {
					t.Fatalf("expected %q, got %q", tt.want, got)
				}
			}
			if tt.serverTool != "" {
				if got := mcpServerTarget(tt.serverTool); got != tt.serverWant {
					t.Fatalf("expected server %q, got %q", tt.serverWant, got)
				}
			}
		})
	}
}

func TestDefaultManagerExecuteMCPRememberDoesNotBroadenAcrossTools(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create"},
			{Name: "list_issues", Description: "list"},
		},
		callResult: mcp.CallResult{Content: "ok"},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:       "ask-github-create-issue",
			Type:     security.ActionTypeMCP,
			Resource: "mcp.github.create_issue",
			Decision: security.DecisionAsk,
			Reason:   "create issue requires approval",
		},
		{
			ID:       "ask-github-list-issues",
			Type:     security.ActionTypeMCP,
			Resource: "mcp.github.list_issues",
			Decision: security.DecisionAsk,
			Reason:   "list issues requires approval",
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	manager, err := NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	sessionID := "session-mcp-target-scope"
	createInput := ToolCallInput{
		ID:        "call-create",
		Name:      "mcp.github.create_issue",
		Arguments: []byte(`{"title":"hello"}`),
		SessionID: sessionID,
	}
	listInput := ToolCallInput{
		ID:        "call-list",
		Name:      "mcp.github.list_issues",
		Arguments: []byte(`{"state":"open"}`),
		SessionID: sessionID,
	}

	_, err = manager.Execute(context.Background(), createInput)
	var permissionErr *PermissionDecisionError
	if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
		t.Fatalf("expected initial MCP ask, got %v", err)
	}
	if rememberErr := manager.RememberSessionDecision(sessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
		t.Fatalf("remember mcp create_issue: %v", rememberErr)
	}

	if _, err := manager.Execute(context.Background(), createInput); err != nil {
		t.Fatalf("expected remembered create_issue allow, got %v", err)
	}

	_, err = manager.Execute(context.Background(), listInput)
	if !errors.As(err, &permissionErr) || permissionErr.Decision() != "ask" {
		t.Fatalf("expected list_issues to require independent approval, got %v", err)
	}
}

func TestDefaultManagerExecuteMCPServerDenyUsesTraceableRule(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create"},
		},
		callResult: mcp.CallResult{Content: "ok"},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewStaticGateway(security.DecisionAllow, []security.Rule{
		{
			ID:           "deny-github-server",
			Type:         security.ActionTypeMCP,
			TargetPrefix: "mcp.github",
			Decision:     security.DecisionDeny,
			Reason:       "github MCP server denied",
		},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	manager, err := NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_, execErr := manager.Execute(context.Background(), ToolCallInput{
		ID:        "call-mcp-deny",
		Name:      "mcp.github.create_issue",
		Arguments: []byte(`{"title":"hello"}`),
		SessionID: "session-mcp-deny",
	})
	var permissionErr *PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		t.Fatalf("expected permission error, got %v", execErr)
	}
	if permissionErr.Decision() != "deny" {
		t.Fatalf("expected deny, got %q", permissionErr.Decision())
	}
	if permissionErr.RuleID() != "deny-github-server" {
		t.Fatalf("expected rule id deny-github-server, got %q", permissionErr.RuleID())
	}
	if permissionErr.Reason() != "github MCP server denied" {
		t.Fatalf("expected deny reason propagated, got %q", permissionErr.Reason())
	}
	if permissionErr.Action().Payload.Target != "mcp.github.create_issue" {
		t.Fatalf("expected full mcp target identity, got %q", permissionErr.Action().Payload.Target)
	}
}

func TestDefaultManagerExecuteMCPServerDenyPriorityOverridesToolRules(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	client := &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "create_issue", Description: "create"},
			{Name: "list_issues", Description: "list"},
			{Name: "search", Description: "search"},
		},
		callResult: mcp.CallResult{Content: "ok"},
	}
	if err := mcpRegistry.RegisterServer("github", "stdio", "v1", client); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register docs server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "github"); err != nil {
		t.Fatalf("refresh github tools: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh docs tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	engine, err := security.NewPolicyEngine(security.DecisionAllow, []security.PolicyRule{
		{
			ID:             "deny-github-server",
			Priority:       830,
			Decision:       security.DecisionDeny,
			Reason:         "github server denied",
			ActionTypes:    []security.ActionType{security.ActionTypeMCP},
			ToolCategories: []string{"mcp.github"},
			TargetTypes:    []security.TargetType{security.TargetTypeMCP},
		},
		{
			ID:               "allow-github-create",
			Priority:         700,
			Decision:         security.DecisionAllow,
			Reason:           "github create allowed",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.github.create_issue"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
		{
			ID:               "ask-github-list",
			Priority:         720,
			Decision:         security.DecisionAsk,
			Reason:           "github list requires approval",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.github.list_issues"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
		{
			ID:               "allow-docs-search",
			Priority:         700,
			Decision:         security.DecisionAllow,
			Reason:           "docs search allowed",
			ActionTypes:      []security.ActionType{security.ActionTypeMCP},
			ResourcePatterns: []string{"mcp.docs.search"},
			TargetTypes:      []security.TargetType{security.TargetTypeMCP},
		},
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
	}

	manager, err := NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	for _, input := range []ToolCallInput{
		{ID: "call-github-create", Name: "mcp.github.create_issue", Arguments: []byte(`{"title":"hello"}`), SessionID: "session-priority"},
		{ID: "call-github-list", Name: "mcp.github.list_issues", Arguments: []byte(`{"state":"open"}`), SessionID: "session-priority"},
	} {
		_, execErr := manager.Execute(context.Background(), input)
		var permissionErr *PermissionDecisionError
		if !errors.As(execErr, &permissionErr) {
			t.Fatalf("expected permission error for %s, got %v", input.Name, execErr)
		}
		if permissionErr.Decision() != "deny" || permissionErr.RuleID() != "deny-github-server" {
			t.Fatalf("expected server-level deny for %s, got decision=%q rule=%q", input.Name, permissionErr.Decision(), permissionErr.RuleID())
		}
	}

	result, execErr := manager.Execute(context.Background(), ToolCallInput{
		ID:        "call-docs-search",
		Name:      "mcp.docs.search",
		Arguments: []byte(`{"query":"neo-code"}`),
		SessionID: "session-priority",
	})
	if execErr != nil {
		t.Fatalf("expected docs search allow, got %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("expected docs search to execute, got %+v", result)
	}
}

func TestNoopWorkspaceSandbox(t *testing.T) {
	t.Parallel()

	sandbox := NoopWorkspaceSandbox{}
	plan, err := sandbox.Check(context.Background(), security.Action{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if plan != nil {
		t.Fatalf("expected nil workspace plan, got %#v", plan)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sandbox.Check(ctx, security.Action{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
