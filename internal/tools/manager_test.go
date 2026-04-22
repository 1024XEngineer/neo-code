package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

type executorWithoutOptionalCompactFeatures struct{}

func (executorWithoutOptionalCompactFeatures) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (executorWithoutOptionalCompactFeatures) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	return ToolResult{}, ctx.Err()
}

func (executorWithoutOptionalCompactFeatures) Supports(name string) bool { return false }

func (s *stubSandbox) Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error) {
	s.callCount++
	s.lastAction = action
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.plan, s.err
}

func isWindowsRuntime() bool {
	return runtime.GOOS == "windows"
}

func mustAllowEngine(t *testing.T) security.PermissionEngine {
	t.Helper()
	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	return engine
}

func TestDefaultManagerListAvailableSpecs(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(&managerStubTool{name: "bash"})
	manager, err := NewManager(registry, mustAllowEngine(t), nil)
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

		manager, err := NewManager(executorWithoutOptionalCompactFeatures{}, mustAllowEngine(t), nil)
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

		manager, err := NewManager(registry, mustAllowEngine(t), nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		if got := manager.MicroCompactPolicy("preserve_tool"); got != MicroCompactPolicyPreserveHistory {
			t.Fatalf("expected preserve history, got %q", got)
		}
	})
}

func TestDefaultManagerMicroCompactSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("nil manager returns nil", func(t *testing.T) {
		t.Parallel()

		var manager *DefaultManager
		if got := manager.MicroCompactSummarizer("custom_tool"); got != nil {
			t.Fatalf("expected nil summarizer, got non-nil")
		}
	})

	t.Run("executor without summarizer support returns nil", func(t *testing.T) {
		t.Parallel()

		manager, err := NewManager(executorWithoutOptionalCompactFeatures{}, mustAllowEngine(t), nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		if got := manager.MicroCompactSummarizer("custom_tool"); got != nil {
			t.Fatalf("expected nil summarizer, got non-nil")
		}
	})

	t.Run("executor summarizer is forwarded", func(t *testing.T) {
		t.Parallel()

		registry := NewRegistry()
		registry.RegisterSummarizer("custom_tool", func(content string, metadata map[string]string, isError bool) string {
			return "summary:" + content
		})

		manager, err := NewManager(registry, mustAllowEngine(t), nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}

		summarizer := manager.MicroCompactSummarizer("CUSTOM_TOOL")
		if summarizer == nil {
			t.Fatal("expected non-nil summarizer")
		}
		if got := summarizer("content", nil, false); got != "summary:content" {
			t.Fatalf("unexpected summary output: %q", got)
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
				manager, _ := NewManager(registry, mustAllowEngine(t), nil)
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

	lowRiskOutsidePath := filepath.Join(string(filepath.Separator), "tmp", "snake_game.py")
	workspaceRoot := filepath.Join(string(filepath.Separator), "workspace", "project")
	protectedOutsidePath := filepath.Join(string(filepath.Separator), "etc", "hosts")
	if isWindowsRuntime() {
		lowRiskOutsidePath = `C:\Users\tester\Desktop\SnakeGame\snake_game.py`
		workspaceRoot = `C:\workspace\project`
		protectedOutsidePath = `C:\Windows\System32\drivers\etc\hosts`
	}

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
			name: "low risk outside workspace write becomes ask",
			input: ToolCallInput{
				ID:        "call-6",
				Name:      "filesystem_write_file",
				Arguments: []byte(fmt.Sprintf(`{"path":%q,"content":"hi"}`, lowRiskOutsidePath)),
				Workdir:   workspaceRoot,
				SessionID: "session-low-risk-outside",
			},
			sandboxErr:        fmt.Errorf("security: path %q escapes workspace root", lowRiskOutsidePath),
			expectErr:         sandboxExternalWriteApprovalReason,
			expectContent:     []string{"tool error", "reason: " + sandboxExternalWriteApprovalReason},
			expectDecision:    "ask",
			expectCalls:       0,
			expectSandboxRuns: 1,
		},
		{
			name: "protected outside path keeps hard sandbox reject",
			input: ToolCallInput{
				ID:        "call-7",
				Name:      "filesystem_write_file",
				Arguments: []byte(fmt.Sprintf(`{"path":%q,"content":"hi"}`, protectedOutsidePath)),
				Workdir:   workspaceRoot,
			},
			sandboxErr:        fmt.Errorf("security: path %q escapes workspace root", protectedOutsidePath),
			expectErr:         "escapes workspace root",
			expectContent:     []string{"tool error", "reason: workspace sandbox rejected action", "target: " + protectedOutsidePath},
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

func TestDefaultManagerSandboxOutsideWriteSessionMemory(t *testing.T) {
	t.Parallel()

	outsidePath := filepath.Join(string(filepath.Separator), "tmp", "snake_game.py")
	workspaceRoot := filepath.Join(string(filepath.Separator), "workspace", "project")
	if isWindowsRuntime() {
		outsidePath = `C:\Users\tester\Desktop\SnakeGame\snake_game.py`
		workspaceRoot = `C:\workspace\project`
	}

	registry := NewRegistry()
	writeTool := &managerStubTool{name: "filesystem_write_file", content: "ok"}
	registry.Register(writeTool)

	manager, err := NewManager(registry, mustAllowEngine(t), &stubSandbox{
		err: fmt.Errorf("security: path %q escapes workspace root", outsidePath),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	input := ToolCallInput{
		ID:        "call-outside-ask",
		Name:      "filesystem_write_file",
		Arguments: []byte(fmt.Sprintf(`{"path":%q,"content":"hi"}`, outsidePath)),
		Workdir:   workspaceRoot,
		SessionID: "session-outside-ask",
	}

	_, execErr := manager.Execute(context.Background(), input)
	var permissionErr *PermissionDecisionError
	if !errors.As(execErr, &permissionErr) || permissionErr.Decision() != "ask" {
		t.Fatalf("expected initial ask decision, got %v", execErr)
	}

	if rememberErr := manager.RememberSessionDecision(input.SessionID, permissionErr.Action(), SessionPermissionScopeAlways); rememberErr != nil {
		t.Fatalf("remember outside write allow: %v", rememberErr)
	}

	_, err = manager.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("expected remembered allow retry to execute, got %v", err)
	}
	if writeTool.callCount != 1 {
		t.Fatalf("expected write tool to execute after remembered allow, got %d", writeTool.callCount)
	}
}

func TestSandboxOutsideWriteApprovalCandidate(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(string(filepath.Separator), "workspace", "project")
	lowRiskPath := filepath.Join(string(filepath.Separator), "tmp", "sample.py")
	protectedPath := filepath.Join(string(filepath.Separator), "etc", "hosts")
	highRiskExecutable := filepath.Join(string(filepath.Separator), "tmp", "sample.exe")
	startupProfilePath := filepath.Join(string(filepath.Separator), "home", "tester", ".bashrc")
	if isWindowsRuntime() {
		workspaceRoot = `C:\workspace\project`
		lowRiskPath = `C:\Users\tester\Desktop\sample.py`
		protectedPath = `C:\Windows\System32\drivers\etc\hosts`
		highRiskExecutable = `C:\Users\tester\Desktop\sample.exe`
		startupProfilePath = `C:\Users\tester\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`
	}

	buildAction := func(target string, toolName string) security.Action {
		return security.Action{
			Type: security.ActionTypeWrite,
			Payload: security.ActionPayload{
				ToolName:      toolName,
				Resource:      toolName,
				Operation:     "write_file",
				Workdir:       workspaceRoot,
				TargetType:    security.TargetTypePath,
				Target:        target,
				SandboxTarget: target,
			},
		}
	}

	tests := []struct {
		name       string
		action     security.Action
		sandboxErr error
		want       bool
	}{
		{
			name:       "boundary violation low risk file asks approval",
			action:     buildAction(lowRiskPath, "filesystem_write_file"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root", lowRiskPath),
			want:       true,
		},
		{
			name:       "non-boundary sandbox error keeps hard reject",
			action:     buildAction(lowRiskPath, "filesystem_write_file"),
			sandboxErr: errors.New("workspace denied"),
			want:       false,
		},
		{
			name:       "protected system path keeps hard reject",
			action:     buildAction(protectedPath, "filesystem_write_file"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root", protectedPath),
			want:       false,
		},
		{
			name:       "high risk executable extension keeps hard reject",
			action:     buildAction(highRiskExecutable, "filesystem_write_file"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root", highRiskExecutable),
			want:       false,
		},
		{
			name:       "write tool not in allowlist keeps hard reject",
			action:     buildAction(lowRiskPath, "filesystem_edit"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root", lowRiskPath),
			want:       false,
		},
		{
			name:       "symlink workspace escape keeps hard reject",
			action:     buildAction(lowRiskPath, "filesystem_write_file"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root via symlink", filepath.Join("link", "sample.py")),
			want:       false,
		},
		{
			name:       "startup profile path keeps hard reject",
			action:     buildAction(startupProfilePath, "filesystem_write_file"),
			sandboxErr: fmt.Errorf("security: path %q escapes workspace root", startupProfilePath),
			want:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSandboxOutsideWriteApprovalCandidate(tt.action, tt.sandboxErr)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestSandboxOutsideWriteUtilityHelpers(t *testing.T) {
	t.Parallel()

	t.Run("candidate requires write action", func(t *testing.T) {
		t.Parallel()
		action := security.Action{
			Type: security.ActionTypeRead,
			Payload: security.ActionPayload{
				ToolName:      ToolNameFilesystemWriteFile,
				Resource:      ToolNameFilesystemWriteFile,
				Workdir:       "/workspace/project",
				Target:        "/tmp/note.txt",
				SandboxTarget: "/tmp/note.txt",
			},
		}
		if got := isSandboxOutsideWriteApprovalCandidate(action, errors.New(`security: path "/tmp/note.txt" escapes workspace root`)); got {
			t.Fatalf("expected non-write action not to be candidate")
		}
	})

	t.Run("candidate requires resolvable target path", func(t *testing.T) {
		t.Parallel()
		action := security.Action{
			Type: security.ActionTypeWrite,
			Payload: security.ActionPayload{
				ToolName: ToolNameFilesystemWriteFile,
				Resource: ToolNameFilesystemWriteFile,
				Workdir:  "/workspace/project",
			},
		}
		if got := isSandboxOutsideWriteApprovalCandidate(action, errors.New(`security: path "/tmp/note.txt" escapes workspace root`)); got {
			t.Fatalf("expected empty target not to be candidate")
		}
	})

	t.Run("workspace error recognizers handle nil", func(t *testing.T) {
		t.Parallel()
		if isWorkspaceBoundaryViolationError(nil) {
			t.Fatalf("expected nil error not to be workspace boundary violation")
		}
		if isWorkspaceSymlinkViolationError(nil) {
			t.Fatalf("expected nil error not to be workspace symlink violation")
		}
	})

	t.Run("resolve action sandbox target path branches", func(t *testing.T) {
		t.Parallel()
		if got := resolveActionSandboxTargetPath(security.Action{}); got != "" {
			t.Fatalf("expected empty target path, got %q", got)
		}

		actionWithTarget := security.Action{
			Payload: security.ActionPayload{
				Target:  "logs/app.log",
				Workdir: "/workspace/project",
			},
		}
		resolved := resolveActionSandboxTargetPath(actionWithTarget)
		if !strings.HasSuffix(filepath.ToSlash(resolved), "/workspace/project/logs/app.log") {
			t.Fatalf("expected target fallback with workdir join, got %q", resolved)
		}

		actionWithSandboxTarget := security.Action{
			Payload: security.ActionPayload{
				Target:        "/tmp/ignored.txt",
				SandboxTarget: "/tmp/final.txt",
			},
		}
		if got := resolveActionSandboxTargetPath(actionWithSandboxTarget); !strings.HasSuffix(filepath.ToSlash(got), "/tmp/final.txt") {
			t.Fatalf("expected sandbox target to win, got %q", got)
		}
	})

	t.Run("low risk path rejects empty path", func(t *testing.T) {
		t.Parallel()
		if isLowRiskExternalWritePath(" . ") {
			t.Fatalf("expected dot path to be rejected")
		}
	})

	t.Run("startup profile detector os branches", func(t *testing.T) {
		t.Parallel()
		if isUserStartupProfilePathForOS(".", "linux") {
			t.Fatalf("expected dot path not to be startup profile")
		}
		if isUserStartupProfilePathForOS(" / ", "linux") {
			t.Fatalf("expected root path not to be startup profile")
		}
		if !isUserStartupProfilePathForOS(`/Users/tester/Documents/WindowsPowerShell/custom_profile.ps1`, "windows") {
			t.Fatalf("expected windows powershell profile directory to be recognized")
		}
		if !isUserStartupProfilePathForOS(`/Users/tester/Documents/PowerShell/custom_profile.ps1`, "windows") {
			t.Fatalf("expected powershell profile directory to be recognized")
		}
		if isUserStartupProfilePathForOS(`/Users/tester/Documents/PowerShell/readme.txt`, "windows") {
			t.Fatalf("expected non-ps1 path not to be startup profile")
		}
		if !isUserStartupProfilePathForOS(`/home/tester/.config/fish/config.fish`, "linux") {
			t.Fatalf("expected fish config path to be startup profile")
		}
	})

	t.Run("system protected path detector os branches", func(t *testing.T) {
		t.Parallel()
		if !isSystemProtectedPathForOS("/", "linux") {
			t.Fatalf("expected linux root to be protected")
		}
		if !isSystemProtectedPathForOS("/home/tester/.ssh/config", "linux") {
			t.Fatalf("expected .ssh path to be protected")
		}
		if isSystemProtectedPathForOS("/home/tester/Documents/notes.txt", "linux") {
			t.Fatalf("expected regular linux user path not to be protected")
		}
		if !isSystemProtectedPathForOS(`C:\Windows\System32\drivers\etc\hosts`, "windows") {
			t.Fatalf("expected windows system path to be protected")
		}
		if !isSystemProtectedPathForOS(`C:\Users\tester\AppData\Roaming\config`, "windows") {
			t.Fatalf("expected appdata path to be protected")
		}
		if !isSystemProtectedPathForOS(`C:\`, "windows") {
			t.Fatalf("expected windows drive root to be protected")
		}
		if isSystemProtectedPathForOS(`C:\Users\tester\Desktop\note.txt`, "windows") {
			t.Fatalf("expected regular windows user path not to be protected")
		}
	})

	t.Run("error message handles nil", func(t *testing.T) {
		t.Parallel()
		if got := errorMessage(nil); got != "" {
			t.Fatalf("expected empty error message for nil error, got %q", got)
		}
	})
}

func TestSandboxErrorDetailsIncludesWorkspaceContext(t *testing.T) {
	t.Parallel()

	action := security.Action{
		Type: security.ActionTypeWrite,
		Payload: security.ActionPayload{
			ToolName:      "filesystem_write_file",
			Resource:      "filesystem_write_file",
			Workdir:       `C:\workspace\project`,
			Target:        `C:\Users\tester\Desktop\SnakeGame\snake_game.py`,
			SandboxTarget: `C:\Users\tester\Desktop\SnakeGame\snake_game.py`,
		},
	}
	if !isWindowsRuntime() {
		action.Payload.Workdir = "/workspace/project"
		action.Payload.Target = "/tmp/snake_game.py"
		action.Payload.SandboxTarget = "/tmp/snake_game.py"
	}

	details := sandboxErrorDetails(action, errors.New("security: path escapes workspace root"))
	for _, fragment := range []string{
		"security: path escapes workspace root",
		"workdir: " + action.Payload.Workdir,
		"target: " + action.Payload.Target,
		"sandbox_target: " + action.Payload.SandboxTarget,
	} {
		if !strings.Contains(details, fragment) {
			t.Fatalf("expected details containing %q, got %q", fragment, details)
		}
	}

	withoutPrefix := sandboxErrorDetails(action, errors.New("path escapes workspace root"))
	if !strings.Contains(withoutPrefix, "security: path escapes workspace root") {
		t.Fatalf("expected details to normalize security prefix, got %q", withoutPrefix)
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
				manager, _ := NewManager(registry, mustAllowEngine(t), nil)
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
				manager, _ := NewManager(registry, mustAllowEngine(t), nil)
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
	if !errors.Is(err, ErrPermissionApprovalRequired) {
		t.Fatalf("ask decision should match ErrPermissionApprovalRequired")
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
	if !errors.Is(denyErr, ErrPermissionDenied) {
		t.Fatalf("default deny should match ErrPermissionDenied")
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

	capabilityErr := &PermissionDecisionError{
		decision: security.DecisionDeny,
		ruleID:   security.CapabilityRuleID,
	}
	if !errors.Is(capabilityErr, ErrCapabilityDenied) {
		t.Fatalf("capability deny should match ErrCapabilityDenied")
	}
	if !errors.Is(capabilityErr, ErrPermissionDenied) {
		t.Fatalf("capability deny should also match ErrPermissionDenied")
	}
}

func TestNewManagerRejectsNilExecutor(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "executor is nil") {
		t.Fatalf("expected nil executor error, got manager=%v err=%v", manager, err)
	}
}

func TestNewManagerRejectsNilPermissionEngine(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(&managerStubTool{name: "bash"})
	manager, err := NewManager(registry, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "permission engine is nil") {
		t.Fatalf("expected nil engine error, got manager=%v err=%v", manager, err)
	}
}

func TestManagerPermissionHelperBranches(t *testing.T) {
	t.Parallel()

	decision := security.CheckResult{
		Decision: security.DecisionAsk,
		Action: security.Action{
			Type: security.ActionTypeRead,
			Payload: security.ActionPayload{
				ToolName:          "webfetch",
				Resource:          "webfetch",
				Operation:         "fetch",
				TargetType:        security.TargetTypeURL,
				Target:            "https://example.com",
				SandboxTargetType: security.TargetTypePath,
				SandboxTarget:     "/workspace/tmp.txt",
				TaskID:            "task-1",
				AgentID:           "agent-1",
			},
		},
		Rule: &security.Rule{
			ID: "session-memory:" + string(SessionPermissionScopeAlways),
		},
		Reason: "need approval",
	}

	if scope := extractRememberScope(security.CheckResult{}); scope != "" {
		t.Fatalf("expected empty scope for nil rule, got %q", scope)
	}
	if scope := extractRememberScope(decision); scope != SessionPermissionScopeAlways {
		t.Fatalf("expected always session scope, got %q", scope)
	}
	if scope := extractRememberScope(security.CheckResult{Rule: &security.Rule{ID: "session-memory:" + string(SessionPermissionScopeOnce)}}); scope != SessionPermissionScopeOnce {
		t.Fatalf("expected once scope, got %q", scope)
	}
	if scope := extractRememberScope(security.CheckResult{Rule: &security.Rule{ID: "session-memory:" + string(SessionPermissionScopeReject)}}); scope != SessionPermissionScopeReject {
		t.Fatalf("expected reject scope, got %q", scope)
	}
	if scope := extractRememberScope(security.CheckResult{Rule: &security.Rule{ID: "unknown"}}); scope != "" {
		t.Fatalf("expected unknown scope to map empty, got %q", scope)
	}

	if got := sessionDecisionReason(""); got != "session permission remembered" {
		t.Fatalf("unexpected default session decision reason: %q", got)
	}
	if got := sessionDecisionReason(SessionPermissionScopeOnce); !strings.Contains(got, "once") {
		t.Fatalf("unexpected once reason: %q", got)
	}
	if got := sessionDecisionReason(SessionPermissionScopeAlways); !strings.Contains(got, "always") {
		t.Fatalf("unexpected always reason: %q", got)
	}
	if got := sessionDecisionReason(SessionPermissionScopeReject); !strings.Contains(got, "reject") {
		t.Fatalf("unexpected reject reason: %q", got)
	}

	metadata := permissionMetadata(decision)
	for _, key := range []string{
		"permission_decision",
		"permission_rule_id",
		"permission_action_type",
		"permission_resource",
		"permission_operation",
		"permission_target_type",
		"permission_target",
		"permission_sandbox_target_type",
		"permission_sandbox_target",
		"permission_task_id",
		"permission_agent_id",
	} {
		if _, ok := metadata[key]; !ok {
			t.Fatalf("expected metadata key %q, got %+v", key, metadata)
		}
	}

	withoutRule := permissionMetadata(security.CheckResult{
		Decision: security.DecisionDeny,
		Action: security.Action{
			Type: security.ActionTypeRead,
			Payload: security.ActionPayload{
				ToolName: "filesystem_read_file",
				Resource: "filesystem_read_file",
			},
		},
	})
	if _, ok := withoutRule["permission_rule_id"]; ok {
		t.Fatalf("did not expect permission_rule_id for empty rule")
	}
	if _, ok := withoutRule["permission_target_type"]; ok {
		t.Fatalf("did not expect target metadata when target is empty")
	}
	if _, ok := withoutRule["permission_task_id"]; ok {
		t.Fatalf("did not expect task metadata when task id is empty")
	}

	details := permissionDetails(decision)
	for _, fragment := range []string{"type: read", "resource: webfetch", "operation: fetch", "url: https://example.com", "policy: need approval"} {
		if !strings.Contains(details, fragment) {
			t.Fatalf("expected details containing %q, got %q", fragment, details)
		}
	}
	minimalDetails := permissionDetails(security.CheckResult{
		Action: security.Action{Type: security.ActionTypeBash},
	})
	if strings.TrimSpace(minimalDetails) != "type: bash" {
		t.Fatalf("expected minimal details for bash, got %q", minimalDetails)
	}
}

func TestManagerCapabilityDecisionHelpers(t *testing.T) {
	t.Parallel()

	action := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "filesystem_read_file",
			Resource: "filesystem_read_file",
		},
	}
	deny := capabilityDenyDecision(action, "")
	if deny.Decision != security.DecisionDeny {
		t.Fatalf("expected deny decision, got %q", deny.Decision)
	}
	if deny.Rule == nil || deny.Rule.ID != security.CapabilityRuleID {
		t.Fatalf("expected capability rule id, got %+v", deny.Rule)
	}
	if deny.Reason != "capability token denied" {
		t.Fatalf("expected default deny reason, got %q", deny.Reason)
	}

	resultAsk := blockedToolResult(ToolCallInput{
		ID:   "call-ask",
		Name: "webfetch",
	}, security.CheckResult{
		Decision: security.DecisionAsk,
		Action:   action,
	})
	if !strings.Contains(resultAsk.Content, "permission approval required") {
		t.Fatalf("expected ask reason in blocked result, got %q", resultAsk.Content)
	}

	resultDeny := blockedToolResult(ToolCallInput{
		ID:   "call-deny",
		Name: "webfetch",
	}, security.CheckResult{
		Decision: security.DecisionDeny,
		Action:   action,
		Reason:   "explicit deny",
	})
	if !strings.Contains(resultDeny.Content, "explicit deny") {
		t.Fatalf("expected explicit deny reason in blocked result, got %q", resultDeny.Content)
	}

	var nilManager *DefaultManager
	if err := nilManager.RememberSessionDecision("session-1", action, SessionPermissionScopeAlways); err == nil || !strings.Contains(err.Error(), "manager is nil") {
		t.Fatalf("expected nil manager remember error, got %v", err)
	}

	manager := &DefaultManager{}
	if err := manager.RememberSessionDecision("session-2", action, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("expected remember session decision success, got %v", err)
	}
	if manager.sessionDecisions == nil {
		t.Fatalf("expected session memory to be initialized")
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
		wantOp       string
		wantTarget   string
		wantSandbox  string
		wantSemantic string
		wantClass    string
		wantFPPrefix string
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
			wantOp:       "command",
			wantTarget:   "echo hi",
			wantSandbox:  "scripts",
			wantClass:    BashIntentClassificationUnknown,
			wantFPPrefix: "bash.command|sha256=",
		},
		{
			name: "bash defaults sandbox workdir to dot",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"echo hi"}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash",
			wantOp:       "command",
			wantTarget:   "echo hi",
			wantSandbox:  ".",
			wantClass:    BashIntentClassificationUnknown,
			wantFPPrefix: "bash.command|sha256=",
		},
		{
			name: "git read-only bash maps semantic resource",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"git status --short --branch"}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash_git_read_only",
			wantOp:       "git_status",
			wantTarget:   "git status --short --branch",
			wantSandbox:  ".",
			wantSemantic: "git",
			wantClass:    BashIntentClassificationReadOnly,
			wantFPPrefix: "bash.git|read_only|status",
		},
		{
			name: "git remote bash maps semantic resource",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"git push origin main"}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash_git_remote_op",
			wantOp:       "git_push",
			wantTarget:   "git push origin main",
			wantSandbox:  ".",
			wantSemantic: "git",
			wantClass:    BashIntentClassificationRemoteOp,
			wantFPPrefix: "bash.git|remote_op|push",
		},
		{
			name: "git checkout dot maps destructive semantic resource",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"git checkout ."}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash_git_destructive",
			wantOp:       "git_checkout",
			wantTarget:   "git checkout .",
			wantSandbox:  ".",
			wantSemantic: "git",
			wantClass:    BashIntentClassificationDestructive,
			wantFPPrefix: "bash.git|destructive|checkout",
		},
		{
			name: "git only maps unknown semantic operation",
			input: ToolCallInput{
				Name:      "bash",
				Arguments: []byte(`{"command":"git -c core.editor=vim"}`),
			},
			wantType:     security.ActionTypeBash,
			wantResource: "bash_git_unknown",
			wantOp:       "git_unknown",
			wantTarget:   "git -c core.editor=vim",
			wantSandbox:  ".",
			wantSemantic: "git",
			wantClass:    BashIntentClassificationUnknown,
			wantFPPrefix: "bash.git.unknown|sha256=",
		},
		{
			name: "read file maps to read action",
			input: ToolCallInput{
				Name:      "filesystem_read_file",
				Arguments: []byte(`{"path":"main.go"}`),
			},
			wantType:     security.ActionTypeRead,
			wantResource: "filesystem_read_file",
			wantOp:       "read_file",
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
			wantOp:       "grep",
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
			wantOp:       "glob",
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
			wantOp:       "write_file",
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
			wantOp:       "fetch",
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
			wantOp:       "edit",
			wantTarget:   "main.go",
			wantSandbox:  "main.go",
		},
		{
			name: "todo write maps to write action",
			input: ToolCallInput{
				Name:      "todo_write",
				Arguments: []byte(`{"action":"set_status","id":"todo-1"}`),
			},
			wantType:     security.ActionTypeWrite,
			wantResource: "todo_write",
			wantOp:       "todo_write",
			wantTarget:   "todo-1",
		},
		{
			name: "spawn subagent maps to write action",
			input: ToolCallInput{
				Name:      ToolNameSpawnSubAgent,
				Arguments: []byte(`{"items":[{"id":"task-a"},{"id":"task-b"}]}`),
			},
			wantType:     security.ActionTypeWrite,
			wantResource: ToolNameSpawnSubAgent,
			wantOp:       ToolNameSpawnSubAgent,
			wantTarget:   "task-a,task-b",
		},
		{
			name: "spawn subagent empty target returns error",
			input: ToolCallInput{
				Name:      ToolNameSpawnSubAgent,
				Arguments: []byte(`{"prompt":"   ","id":"  ","items":[{"id":" "}]}`),
			},
			wantErr: "spawn_subagent permission target is empty",
		},
		{
			name: "mcp tool maps to mcp action",
			input: ToolCallInput{
				Name:      "mcp.github.create_issue",
				Arguments: []byte(`{"title":"hello"}`),
			},
			wantType:     security.ActionTypeMCP,
			wantResource: "mcp.github.create_issue",
			wantOp:       "invoke",
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
			if tt.wantOp != "" && action.Payload.Operation != tt.wantOp {
				t.Fatalf("expected operation %q, got %q", tt.wantOp, action.Payload.Operation)
			}
			if action.Payload.Target != tt.wantTarget {
				t.Fatalf("expected target %q, got %q", tt.wantTarget, action.Payload.Target)
			}
			if action.Payload.SandboxTarget != tt.wantSandbox {
				t.Fatalf("expected sandbox target %q, got %q", tt.wantSandbox, action.Payload.SandboxTarget)
			}
			if action.Payload.SemanticType != tt.wantSemantic {
				t.Fatalf("expected semantic type %q, got %q", tt.wantSemantic, action.Payload.SemanticType)
			}
			if action.Payload.SemanticClass != tt.wantClass {
				t.Fatalf("expected semantic class %q, got %q", tt.wantClass, action.Payload.SemanticClass)
			}
			if tt.wantFPPrefix != "" && !strings.HasPrefix(action.Payload.PermissionFingerprint, tt.wantFPPrefix) {
				t.Fatalf("expected permission fingerprint prefix %q, got %q", tt.wantFPPrefix, action.Payload.PermissionFingerprint)
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
		spawn      bool
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
			name:  "extract spawn target from items",
			input: []byte(`{"items":[{"id":"task-a"},{"id":" task-b "}],"id":"fallback"}`),
			want:  "task-a,task-b",
			spawn: true,
		},
		{
			name:  "extract spawn target falls back to top level id",
			input: []byte(`{"id":"legacy-task"}`),
			want:  "legacy-task",
			spawn: true,
		},
		{
			name:  "extract spawn target falls back to prompt",
			input: []byte(`{"prompt":"analyze auth module for vulnerabilities"}`),
			want:  "analyze auth module for vulnerabilities",
			spawn: true,
		},
		{
			name:  "extract spawn target falls back to content",
			input: []byte(`{"content":"write regression tests first"}`),
			want:  "write regression tests first",
			spawn: true,
		},
		{
			name:  "extract spawn target trims prompt to max length",
			input: []byte(`{"prompt":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}`),
			want:  "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzab...",
			spawn: true,
		},
		{
			name:  "extract spawn target empty when no fallback",
			input: []byte(`{"items":[{"id":" "}]}`),
			want:  "",
			spawn: true,
		},
		{
			name:  "extract spawn target invalid json returns empty",
			input: []byte(`{invalid`),
			want:  "",
			spawn: true,
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
			if tt.spawn {
				if got := extractSpawnSubAgentTarget(tt.input); got != tt.want {
					t.Fatalf("expected spawn target %q, got %q", tt.want, got)
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

func TestDefaultManagerExecuteCapabilityTokenValidation(t *testing.T) {
	t.Parallel()

	makeManager := func(t *testing.T) (*DefaultManager, *managerStubTool) {
		t.Helper()
		registry := NewRegistry()
		readTool := &managerStubTool{name: "filesystem_read_file", content: "ok"}
		registry.Register(readTool)
		engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
		if err != nil {
			t.Fatalf("new static gateway: %v", err)
		}
		manager, err := NewManager(registry, engine, nil)
		if err != nil {
			t.Fatalf("new manager: %v", err)
		}
		return manager, readTool
	}

	now := time.Now().UTC()
	workdir := t.TempDir()
	baseToken := security.CapabilityToken{
		ID:              "token-validation",
		TaskID:          "task-validation",
		AgentID:         "agent-validation",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{workdir},
		NetworkPolicy:   security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
		WritePermission: security.WritePermissionWorkspace,
	}

	testCases := []struct {
		name        string
		buildInput  func(t *testing.T, manager *DefaultManager) ToolCallInput
		expectErr   string
		expectCalls int
	}{
		{
			name: "allow signed token",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-allow",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          baseToken.TaskID,
					AgentID:         baseToken.AgentID,
					CapabilityToken: &signed,
				}
			},
			expectCalls: 1,
		},
		{
			name: "deny tampered signature",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				signed.AllowedTools = []string{"filesystem_write_file"}
				return ToolCallInput{
					ID:              "call-tampered",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          baseToken.TaskID,
					AgentID:         baseToken.AgentID,
					CapabilityToken: &signed,
				}
			},
			expectErr: "invalid capability token signature",
		},
		{
			name: "deny expired token",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				expired := baseToken
				expired.ID = "token-expired"
				expired.IssuedAt = now.Add(-2 * time.Hour)
				expired.ExpiresAt = now.Add(-time.Minute)
				signed, err := manager.CapabilitySigner().Sign(expired)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-expired",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          expired.TaskID,
					AgentID:         expired.AgentID,
					CapabilityToken: &signed,
				}
			},
			expectErr: "capability token expired",
		},
		{
			name: "deny task mismatch",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-mismatch",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          "task-other",
					AgentID:         baseToken.AgentID,
					CapabilityToken: &signed,
				}
			},
			expectErr: "task_id does not match action",
		},
		{
			name: "deny missing task id binding",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-missing-task",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          "",
					AgentID:         baseToken.AgentID,
					CapabilityToken: &signed,
				}
			},
			expectErr: "requires non-empty action task_id",
		},
		{
			name: "deny missing agent id binding",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-missing-agent",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          baseToken.TaskID,
					AgentID:         "",
					CapabilityToken: &signed,
				}
			},
			expectErr: "requires non-empty action agent_id",
		},
		{
			name: "deny agent mismatch",
			buildInput: func(t *testing.T, manager *DefaultManager) ToolCallInput {
				t.Helper()
				signed, err := manager.CapabilitySigner().Sign(baseToken)
				if err != nil {
					t.Fatalf("sign token: %v", err)
				}
				return ToolCallInput{
					ID:              "call-agent-mismatch",
					Name:            "filesystem_read_file",
					Arguments:       []byte(`{"path":"README.md"}`),
					Workdir:         workdir,
					TaskID:          baseToken.TaskID,
					AgentID:         "agent-other",
					CapabilityToken: &signed,
				}
			},
			expectErr: "agent_id does not match action",
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manager, readTool := makeManager(t)
			input := tt.buildInput(t, manager)

			_, execErr := manager.Execute(context.Background(), input)
			if tt.expectErr == "" {
				if execErr != nil {
					t.Fatalf("expected nil error, got %v", execErr)
				}
			} else {
				var permissionErr *PermissionDecisionError
				if !errors.As(execErr, &permissionErr) {
					t.Fatalf("expected permission decision error, got %v", execErr)
				}
				if permissionErr.Decision() != string(security.DecisionDeny) {
					t.Fatalf("expected deny decision, got %q", permissionErr.Decision())
				}
				if permissionErr.RuleID() != security.CapabilityRuleID {
					t.Fatalf("expected capability rule id, got %q", permissionErr.RuleID())
				}
				if !strings.Contains(strings.ToLower(permissionErr.Reason()), strings.ToLower(tt.expectErr)) {
					t.Fatalf("expected reason containing %q, got %q", tt.expectErr, permissionErr.Reason())
				}
			}
			if readTool.callCount != tt.expectCalls {
				t.Fatalf("expected tool call count %d, got %d", tt.expectCalls, readTool.callCount)
			}
		})
	}
}

func TestDefaultManagerExecuteCapabilityTokenPolicyDeny(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	webTool := &managerStubTool{name: "webfetch", content: "ok"}
	readTool := &managerStubTool{name: "filesystem_read_file", content: "ok"}
	registry.Register(webTool)
	registry.Register(readTool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	manager, err := NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	now := time.Now().UTC()
	workdir := t.TempDir()
	token := security.CapabilityToken{
		ID:              "token-engine-deny",
		TaskID:          "task-engine-deny",
		AgentID:         "agent-engine-deny",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{filepath.Join(workdir, "safe")},
		NetworkPolicy:   security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
		WritePermission: security.WritePermissionWorkspace,
	}
	signed, err := manager.CapabilitySigner().Sign(token)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	cases := []struct {
		name      string
		input     ToolCallInput
		wantInErr string
	}{
		{
			name: "tool allowlist miss",
			input: ToolCallInput{
				ID:              "call-tool-miss",
				Name:            "webfetch",
				Arguments:       []byte(`{"url":"https://example.com"}`),
				Workdir:         workdir,
				TaskID:          token.TaskID,
				AgentID:         token.AgentID,
				CapabilityToken: &signed,
			},
			wantInErr: "tool not allowed",
		},
		{
			name: "path traversal blocked",
			input: ToolCallInput{
				ID:              "call-path-traversal",
				Name:            "filesystem_read_file",
				Arguments:       []byte(`{"path":"../secret.txt"}`),
				Workdir:         workdir,
				TaskID:          token.TaskID,
				AgentID:         token.AgentID,
				CapabilityToken: &signed,
			},
			wantInErr: "traversal",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, execErr := manager.Execute(context.Background(), tc.input)
			var permissionErr *PermissionDecisionError
			if !errors.As(execErr, &permissionErr) {
				t.Fatalf("expected permission decision error, got %v", execErr)
			}
			if permissionErr.Decision() != string(security.DecisionDeny) {
				t.Fatalf("expected deny decision, got %q", permissionErr.Decision())
			}
			if permissionErr.RuleID() != security.CapabilityRuleID {
				t.Fatalf("expected capability rule id, got %q", permissionErr.RuleID())
			}
			if !strings.Contains(strings.ToLower(permissionErr.Reason()), strings.ToLower(tc.wantInErr)) {
				t.Fatalf("expected reason containing %q, got %q", tc.wantInErr, permissionErr.Reason())
			}
		})
	}

	if webTool.callCount != 0 || readTool.callCount != 0 {
		t.Fatalf("expected denied calls not to execute tools, got web=%d read=%d", webTool.callCount, readTool.callCount)
	}
}

func TestDefaultManagerCapabilitySignerHelpers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(&managerStubTool{name: "filesystem_read_file", content: "ok"})

	manager, err := NewManager(registry, mustAllowEngine(t), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if manager.CapabilitySigner() == nil {
		t.Fatalf("expected default capability signer")
	}

	if err := manager.SetCapabilitySigner(nil); err == nil || !strings.Contains(err.Error(), "capability signer is nil") {
		t.Fatalf("expected nil signer error, got %v", err)
	}
	customSigner, err := security.NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("new custom signer: %v", err)
	}
	if err := manager.SetCapabilitySigner(customSigner); err != nil {
		t.Fatalf("set custom signer: %v", err)
	}
	if manager.CapabilitySigner() != customSigner {
		t.Fatalf("expected custom signer to be installed")
	}

	var nilManager *DefaultManager
	if err := nilManager.SetCapabilitySigner(customSigner); err == nil || !strings.Contains(err.Error(), "manager is nil") {
		t.Fatalf("expected nil manager error, got %v", err)
	}
}

func TestDefaultManagerExecuteCapabilityTokenWithoutSigner(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	readTool := &managerStubTool{name: "filesystem_read_file", content: "ok"}
	registry.Register(readTool)

	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new static gateway: %v", err)
	}
	manager, err := NewManager(registry, engine, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	now := time.Now().UTC()
	token := security.CapabilityToken{
		ID:              "token-no-signer",
		TaskID:          "task-no-signer",
		AgentID:         "agent-no-signer",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{t.TempDir()},
		NetworkPolicy:   security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
		WritePermission: security.WritePermissionWorkspace,
	}
	signed, err := manager.CapabilitySigner().Sign(token)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	manager.capabilityMu.Lock()
	manager.capabilitySigner = nil
	manager.capabilityMu.Unlock()

	_, execErr := manager.Execute(context.Background(), ToolCallInput{
		ID:              "call-no-signer",
		Name:            "filesystem_read_file",
		Arguments:       []byte(`{"path":"README.md"}`),
		Workdir:         t.TempDir(),
		TaskID:          token.TaskID,
		AgentID:         token.AgentID,
		CapabilityToken: &signed,
	})
	var permissionErr *PermissionDecisionError
	if !errors.As(execErr, &permissionErr) {
		t.Fatalf("expected permission decision error, got %v", execErr)
	}
	if permissionErr.Decision() != string(security.DecisionDeny) || permissionErr.RuleID() != security.CapabilityRuleID {
		t.Fatalf("unexpected decision/rule: decision=%q rule=%q", permissionErr.Decision(), permissionErr.RuleID())
	}
	if !strings.Contains(strings.ToLower(permissionErr.Reason()), "signer is unavailable") {
		t.Fatalf("expected signer unavailable reason, got %q", permissionErr.Reason())
	}
	if !errors.Is(execErr, ErrCapabilityDenied) {
		t.Fatalf("expected capability denied sentinel, got %v", execErr)
	}
	if readTool.callCount != 0 {
		t.Fatalf("expected denied call not to execute tool, got %d", readTool.callCount)
	}
}
