package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/security"
)

type managerStubTool struct {
	name      string
	content   string
	err       error
	callCount int
	lastCall  ToolCallInput
}

func (t *managerStubTool) Name() string { return t.name }

func (t *managerStubTool) Description() string { return "stub tool" }

func (t *managerStubTool) Schema() map[string]any { return map[string]any{"type": "object"} }

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
	callCount  int
	lastAction security.Action
}

func (s *stubSandbox) Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error) {
	s.callCount++
	s.lastAction = action
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, s.err
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

	var nilErr *PermissionDecisionError
	if nilErr.Error() != "" || nilErr.Decision() != "" || nilErr.ToolName() != "" {
		t.Fatalf("expected nil permission error helpers to be empty")
	}
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
			wantTarget:   "github",
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
			serverWant: "github",
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
