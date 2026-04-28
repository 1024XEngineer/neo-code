package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

func TestBuildUserHookSpecMapsFailurePolicyAndScope(t *testing.T) {
	t.Parallel()

	item := config.RuntimeHookItemConfig{
		ID:            "warn-bash",
		Point:         "before_tool_call",
		Scope:         "user",
		Kind:          "builtin",
		Mode:          "sync",
		Handler:       "warn_on_tool_call",
		Priority:      99,
		TimeoutSec:    7,
		FailurePolicy: "warn_only",
		Params: map[string]any{
			"tool_name": "bash",
			"message":   "tool call warning",
		},
	}

	spec, err := buildUserHookSpec(item, t.TempDir())
	if err != nil {
		t.Fatalf("buildUserHookSpec() error = %v", err)
	}
	if spec.Scope != runtimehooks.HookScopeUser {
		t.Fatalf("scope = %q, want %q", spec.Scope, runtimehooks.HookScopeUser)
	}
	if spec.Kind != runtimehooks.HookKindFunction {
		t.Fatalf("kind = %q, want %q", spec.Kind, runtimehooks.HookKindFunction)
	}
	if spec.Mode != runtimehooks.HookModeSync {
		t.Fatalf("mode = %q, want %q", spec.Mode, runtimehooks.HookModeSync)
	}
	if spec.FailurePolicy != runtimehooks.FailurePolicyFailOpen {
		t.Fatalf("failure_policy = %q, want %q", spec.FailurePolicy, runtimehooks.FailurePolicyFailOpen)
	}
	if spec.Timeout != 7*time.Second {
		t.Fatalf("timeout = %v, want 7s", spec.Timeout)
	}
}

func TestRequireFileExistsHandler(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	requiredFile := filepath.Join(workdir, "README.md")
	if err := os.WriteFile(requiredFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write required file: %v", err)
	}

	handler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "README.md"}, workdir)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	passResult := handler(context.Background(), runtimehooks.HookContext{
		RunID:     "run-1",
		SessionID: "session-1",
		Metadata: map[string]any{
			"workdir": workdir,
		},
	})
	if passResult.Status != runtimehooks.HookResultPass {
		t.Fatalf("status = %q, want pass", passResult.Status)
	}

	missingHandler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "missing.md"}, workdir)
	if err != nil {
		t.Fatalf("build missing handler: %v", err)
	}
	missingResult := missingHandler(context.Background(), runtimehooks.HookContext{
		Metadata: map[string]any{"workdir": workdir},
	})
	if missingResult.Status != runtimehooks.HookResultFailed {
		t.Fatalf("missing status = %q, want failed", missingResult.Status)
	}
	if strings.TrimSpace(missingResult.Message) == "" {
		t.Fatal("expected missing file message")
	}

	outsideHandler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "../outside.md"}, workdir)
	if err != nil {
		t.Fatalf("build outside handler: %v", err)
	}
	outsideResult := outsideHandler(context.Background(), runtimehooks.HookContext{
		Metadata: map[string]any{"workdir": workdir},
	})
	if outsideResult.Status != runtimehooks.HookResultFailed {
		t.Fatalf("outside status = %q, want failed", outsideResult.Status)
	}
}

func TestWarnOnToolCallAndAddContextNoteHandlers(t *testing.T) {
	t.Parallel()

	warnHandler, err := buildUserBuiltinHookHandler("warn_on_tool_call", map[string]any{
		"tool_name": "bash",
		"message":   "bash was called",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build warn handler: %v", err)
	}
	warnResult := warnHandler(context.Background(), runtimehooks.HookContext{
		Metadata: map[string]any{
			"tool_name": "bash",
		},
	})
	if warnResult.Status != runtimehooks.HookResultPass {
		t.Fatalf("warn status = %q, want pass", warnResult.Status)
	}
	if warnResult.Message != "bash was called" {
		t.Fatalf("warn message = %q, want %q", warnResult.Message, "bash was called")
	}

	ignoreResult := warnHandler(context.Background(), runtimehooks.HookContext{
		Metadata: map[string]any{
			"tool_name": "filesystem",
		},
	})
	if strings.TrimSpace(ignoreResult.Message) != "" {
		t.Fatalf("expected unmatched tool to have empty message, got %q", ignoreResult.Message)
	}

	noteHandler, err := buildUserBuiltinHookHandler("add_context_note", map[string]any{
		"note": "manual check required",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("build note handler: %v", err)
	}
	noteResult := noteHandler(context.Background(), runtimehooks.HookContext{})
	if noteResult.Status != runtimehooks.HookResultPass {
		t.Fatalf("note status = %q, want pass", noteResult.Status)
	}
	if noteResult.Message != "manual check required" {
		t.Fatalf("note message = %q", noteResult.Message)
	}
}

func TestConfigureRuntimeHooksFromConfig(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	cfg := *config.StaticDefaults()
	cfg.Workdir = workdir
	cfg.Runtime.Hooks.Items = []config.RuntimeHookItemConfig{
		{
			ID:      "warn-before-tool",
			Enabled: runtimeBoolPtr(true),
			Point:   "before_tool_call",
			Scope:   "user",
			Kind:    "builtin",
			Mode:    "sync",
			Handler: "warn_on_tool_call",
			Params: map[string]any{
				"tool_name": "bash",
			},
		},
	}
	cfg.Runtime.Hooks.ApplyDefaults(config.StaticDefaults().Runtime.Hooks)

	service := &Service{}
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("configureRuntimeHooksFromConfig() error = %v", err)
	}
	if service.hookExecutor == nil {
		t.Fatal("expected hook executor to be configured")
	}

	cfg.Runtime.Hooks.Enabled = runtimeBoolPtr(true)
	cfg.Runtime.Hooks.UserHooksEnabled = runtimeBoolPtr(false)
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("disable user hooks error = %v", err)
	}
	if service.hookExecutor != nil {
		t.Fatal("expected nil hook executor when base executor is nil and user hooks are disabled")
	}

	cfg.Runtime.Hooks.Enabled = runtimeBoolPtr(false)
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("disable hooks error = %v", err)
	}
	if service.hookExecutor != nil {
		t.Fatal("expected hook executor disabled when hooks.enabled=false")
	}
}

func TestConfigureRuntimeHooksFromConfigKeepsBaseExecutorAndComposes(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	cfg := *config.StaticDefaults()
	cfg.Workdir = workdir
	cfg.Runtime.Hooks.Items = []config.RuntimeHookItemConfig{
		{
			ID:      "warn-before-tool",
			Enabled: runtimeBoolPtr(true),
			Point:   "before_tool_call",
			Scope:   "user",
			Kind:    "builtin",
			Mode:    "sync",
			Handler: "warn_on_tool_call",
			Params: map[string]any{
				"tool_name": "bash",
				"message":   "warn",
			},
		},
	}
	cfg.Runtime.Hooks.ApplyDefaults(config.StaticDefaults().Runtime.Hooks)

	base := &countingHookExecutor{
		output: runtimehooks.RunOutput{
			Results: []runtimehooks.HookResult{
				{HookID: "base", Scope: runtimehooks.HookScopeInternal, Status: runtimehooks.HookResultPass},
			},
		},
	}
	service := &Service{
		hookExecutor: base,
		events:       make(chan RuntimeEvent, 32),
	}
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("configureRuntimeHooksFromConfig() error = %v", err)
	}
	if service.hookExecutor == nil {
		t.Fatal("expected composed hook executor")
	}

	output := service.hookExecutor.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{
		Metadata: map[string]any{"tool_name": "bash"},
	})
	if base.calls.Load() == 0 {
		t.Fatal("expected base executor to be invoked")
	}
	if len(output.Results) < 2 {
		t.Fatalf("expected combined results from base+user, got %+v", output.Results)
	}

	cfg.Runtime.Hooks.UserHooksEnabled = runtimeBoolPtr(false)
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("reconfigure disable user hooks error = %v", err)
	}
	if service.hookExecutor != base {
		t.Fatalf("expected base executor to be restored, got %T", service.hookExecutor)
	}

	cfg.Runtime.Hooks.Enabled = runtimeBoolPtr(false)
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("reconfigure disable all hooks error = %v", err)
	}
	if service.hookExecutor != nil {
		t.Fatalf("expected hooks.enabled=false to force nil executor, got %T", service.hookExecutor)
	}
}

type countingHookExecutor struct {
	calls  atomic.Int32
	output runtimehooks.RunOutput
}

func (e *countingHookExecutor) Run(
	_ context.Context,
	_ runtimehooks.HookPoint,
	_ runtimehooks.HookContext,
) runtimehooks.RunOutput {
	e.calls.Add(1)
	return e.output
}

func runtimeBoolPtr(value bool) *bool {
	return &value
}
