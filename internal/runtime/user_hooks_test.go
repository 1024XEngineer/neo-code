package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	gruntime "runtime"
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

func TestConfigureRuntimeHooksWrapperNilService(t *testing.T) {
	t.Parallel()
	if err := ConfigureRuntimeHooks(nil, *config.StaticDefaults()); err != nil {
		t.Fatalf("ConfigureRuntimeHooks() error = %v", err)
	}
}

func TestUserComposedHookExecutorAndHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	point := runtimehooks.HookPointBeforeToolCall
	input := runtimehooks.HookContext{}

	t.Run("base blocks short-circuit user", func(t *testing.T) {
		base := &countingHookExecutor{
			output: runtimehooks.RunOutput{
				Blocked:   true,
				BlockedBy: "base",
				Results:   []runtimehooks.HookResult{{HookID: "base", Status: runtimehooks.HookResultBlock}},
			},
		}
		user := &countingHookExecutor{
			output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "user"}}},
		}
		out := (&userComposedHookExecutor{base: base, user: user}).Run(ctx, point, input)
		if !out.Blocked || out.BlockedBy != "base" {
			t.Fatalf("unexpected block output: %+v", out)
		}
		if user.calls.Load() != 0 {
			t.Fatal("user executor should not run when base blocked")
		}
	})

	t.Run("merge results and adopt user block", func(t *testing.T) {
		base := &countingHookExecutor{
			output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "base"}}},
		}
		user := &countingHookExecutor{
			output: runtimehooks.RunOutput{
				Blocked:   true,
				BlockedBy: "user",
				Results:   []runtimehooks.HookResult{{HookID: "user"}},
			},
		}
		out := (&userComposedHookExecutor{base: base, user: user}).Run(ctx, point, input)
		if len(out.Results) != 2 {
			t.Fatalf("expected merged results, got %+v", out.Results)
		}
		if !out.Blocked || out.BlockedBy != "user" {
			t.Fatalf("unexpected block output: %+v", out)
		}
	})

	t.Run("unwrap and safe run", func(t *testing.T) {
		base := &countingHookExecutor{}
		composed := &userComposedHookExecutor{base: base}
		if unwrapBaseHookExecutor(composed) != base {
			t.Fatal("unwrap should return base from composed")
		}
		if unwrapBaseHookExecutor(base) != base {
			t.Fatal("unwrap should return executor itself")
		}
		if got := runHookExecutorSafely(nil, ctx, point, input); len(got.Results) != 0 || got.Blocked {
			t.Fatalf("nil executor should return zero output, got %+v", got)
		}
		_ = runHookExecutorSafely(base, ctx, point, input)
		if base.calls.Load() == 0 {
			t.Fatal("safe run should execute non-nil executor")
		}
	})
}

func TestUserHookHelpersAndErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := buildUserHookSpec(config.RuntimeHookItemConfig{
		ID:      "bad",
		Point:   "before_tool_call",
		Handler: "unknown",
	}, t.TempDir()); err == nil {
		t.Fatal("expected unsupported handler error")
	}

	if got := mapRuntimeHookFailurePolicy("fail_closed"); got != runtimehooks.FailurePolicyFailClosed {
		t.Fatalf("unexpected mapping: %q", got)
	}
	if got := mapRuntimeHookFailurePolicy("unknown"); got != runtimehooks.FailurePolicyFailOpen {
		t.Fatalf("unexpected default mapping: %q", got)
	}

	if got := readHookParamString(nil, "x"); got != "" {
		t.Fatalf("readHookParamString nil params = %q", got)
	}
	if got := readHookParamString(map[string]any{"x": 123}, "x"); got != "123" {
		t.Fatalf("readHookParamString non-string = %q", got)
	}
	if got := readHookParamStringSlice(map[string]any{"x": []any{"a", nil, 1}}, "x"); len(got) != 2 || got[1] != "1" {
		t.Fatalf("readHookParamStringSlice []any = %#v", got)
	}
	if got := readHookParamStringSlice(map[string]any{"x": "bad"}, "x"); got != nil {
		t.Fatalf("readHookParamStringSlice scalar should be nil, got %#v", got)
	}
	if got := normalizeHookParamStringSlice([]string{" a ", "", "B", "a"}); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("normalizeHookParamStringSlice = %#v", got)
	}
	if got := readHookContextMetadataString(runtimehooks.HookContext{}, "x"); got != "" {
		t.Fatalf("readHookContextMetadataString empty = %q", got)
	}
	if got := readHookContextMetadataString(runtimehooks.HookContext{Metadata: map[string]any{"x": 42}}, "x"); got != "42" {
		t.Fatalf("readHookContextMetadataString non-string = %q", got)
	}

	workdir := t.TempDir()
	if got := resolveHookWorkdir(runtimehooks.HookContext{}, workdir); got != workdir {
		t.Fatalf("resolveHookWorkdir default = %q", got)
	}
	if got := resolveHookWorkdir(runtimehooks.HookContext{Metadata: map[string]any{"workdir": 7}}, workdir); got != "7" {
		t.Fatalf("resolveHookWorkdir non-string = %q", got)
	}

	if _, err := resolveHookPathWithinWorkdir("", "a"); err == nil {
		t.Fatal("expected empty workdir error")
	}
	if _, err := resolveHookPathWithinWorkdir(workdir, ""); err == nil {
		t.Fatal("expected empty path error")
	}

	outside := filepath.Join(filepath.Dir(workdir), "outside-user-hooks-test")
	if err := ensureHookPathWithinBase(workdir, outside); err == nil {
		t.Fatal("expected outside path error")
	}
	if err := ensureHookPathWithinBase(workdir, workdir); err != nil {
		t.Fatalf("base equals target should pass: %v", err)
	}
	if err := ensureHookPathWithinBase("", workdir); err == nil {
		t.Fatal("empty base should fail")
	}

	if got := normalizeHookComparablePath(""); got != "." {
		t.Fatalf("normalizeHookComparablePath empty = %q", got)
	}
	if got := normalizeHookComparablePath(" ./a "); got == "" {
		t.Fatal("normalizeHookComparablePath non-empty should not be empty")
	}

	fileInWorkdir := filepath.Join(workdir, "ok.txt")
	if err := os.WriteFile(fileInWorkdir, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file in workdir: %v", err)
	}
	resolved, err := resolveHookPathWithinWorkdir(workdir, "ok.txt")
	if err != nil {
		t.Fatalf("resolveHookPathWithinWorkdir success error: %v", err)
	}
	if resolved != fileInWorkdir {
		t.Fatalf("resolved path = %q, want %q", resolved, fileInWorkdir)
	}

	missingRel := filepath.Join(workdir, "missing.txt")
	hasSymlink, err := hookPathContainsSymlink(workdir, missingRel)
	if err != nil {
		t.Fatalf("hookPathContainsSymlink missing path error: %v", err)
	}
	if hasSymlink {
		t.Fatal("missing path should not be symlink")
	}

	hasSymlink, err = hookPathContainsSymlink(workdir, fileInWorkdir)
	if err != nil {
		t.Fatalf("hookPathContainsSymlink file path error: %v", err)
	}
	if hasSymlink {
		t.Fatal("regular file should not be symlink")
	}

	linkDir := filepath.Join(workdir, "linked")
	targetDir := t.TempDir()
	if err := os.Symlink(targetDir, linkDir); err == nil {
		has, checkErr := hookPathContainsSymlink(workdir, filepath.Join(linkDir, "x"))
		if checkErr != nil {
			t.Fatalf("hookPathContainsSymlink symlink check error: %v", checkErr)
		}
		if !has {
			t.Fatal("expected symlink detection")
		}
	}
}

func TestConfigureRuntimeHooksFromConfigNoEnabledUserItemsRestoresBase(t *testing.T) {
	t.Parallel()

	base := &countingHookExecutor{}
	service := &Service{hookExecutor: base}
	cfg := *config.StaticDefaults()
	cfg.Runtime.Hooks.Items = []config.RuntimeHookItemConfig{
		{
			ID:      "disabled-user-hook",
			Enabled: runtimeBoolPtr(false),
			Point:   "before_tool_call",
			Scope:   "user",
			Kind:    "builtin",
			Mode:    "sync",
			Handler: "warn_on_tool_call",
			Params:  map[string]any{"tool_name": "bash"},
		},
	}
	cfg.Runtime.Hooks.ApplyDefaults(config.StaticDefaults().Runtime.Hooks)

	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("configureRuntimeHooksFromConfig() error = %v", err)
	}
	if service.hookExecutor != base {
		t.Fatalf("expected base executor restored, got %T", service.hookExecutor)
	}
}

func TestBuildUserBuiltinHookHandlerEdgeCases(t *testing.T) {
	t.Parallel()

	if _, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{}, t.TempDir()); err == nil {
		t.Fatal("expected missing path error")
	}
	if _, err := buildUserBuiltinHookHandler("warn_on_tool_call", map[string]any{}, t.TempDir()); err == nil {
		t.Fatal("expected missing target error")
	}
	if _, err := buildUserBuiltinHookHandler("add_context_note", map[string]any{}, t.TempDir()); err == nil {
		t.Fatal("expected missing note/message error")
	}

	workdir := t.TempDir()
	handler, err := buildUserBuiltinHookHandler("warn_on_tool_call", map[string]any{"tool_names": []any{"BASH"}, "message": "hit"}, workdir)
	if err != nil {
		t.Fatalf("build warn_on_tool_call tool_names error: %v", err)
	}
	pass := handler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"tool_name": "bash"}})
	if pass.Message != "hit" {
		t.Fatalf("expected match message, got %q", pass.Message)
	}
	noTool := handler(context.Background(), runtimehooks.HookContext{})
	if noTool.Status != runtimehooks.HookResultPass || noTool.Message != "" {
		t.Fatalf("unexpected no-tool result: %+v", noTool)
	}

	noteHandler, err := buildUserBuiltinHookHandler("add_context_note", map[string]any{"message": "fallback"}, workdir)
	if err != nil {
		t.Fatalf("build add_context_note fallback error: %v", err)
	}
	if got := noteHandler(context.Background(), runtimehooks.HookContext{}); got.Message != "fallback" {
		t.Fatalf("unexpected note fallback message: %+v", got)
	}

	requireHandler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "file.txt", "message": "missing"}, workdir)
	if err != nil {
		t.Fatalf("build require_file_exists error: %v", err)
	}
	got := requireHandler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"workdir": workdir}})
	if got.Status != runtimehooks.HookResultFailed || got.Message != "missing" {
		t.Fatalf("unexpected require_file_exists missing output: %+v", got)
	}

	dirPath := filepath.Join(workdir, "dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir dir path: %v", err)
	}
	dirHandler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "dir"}, workdir)
	if err != nil {
		t.Fatalf("build directory require_file_exists error: %v", err)
	}
	dirResult := dirHandler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"workdir": workdir}})
	if dirResult.Status != runtimehooks.HookResultFailed {
		t.Fatalf("expected directory to fail, got %+v", dirResult)
	}

	defaultWorkdirHandler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "x"}, workdir)
	if err != nil {
		t.Fatalf("build default workdir handler: %v", err)
	}
	_ = defaultWorkdirHandler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"workdir": ""}})

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

func TestConfigureRuntimeHooksAndHelpers(t *testing.T) {
	t.Parallel()

	if err := ConfigureRuntimeHooks(nil, *config.StaticDefaults()); err != nil {
		t.Fatalf("ConfigureRuntimeHooks(nil) error = %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 1)}
	cfg := *config.StaticDefaults()
	cfg.Runtime.Hooks.Items = []config.RuntimeHookItemConfig{
		{
			ID:      "disabled-item",
			Enabled: runtimeBoolPtr(false),
			Point:   "before_tool_call",
			Scope:   "user",
			Kind:    "builtin",
			Mode:    "sync",
			Handler: "warn_on_tool_call",
			Params:  map[string]any{"tool_name": "bash"},
		},
	}
	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("configureRuntimeHooksFromConfig() error = %v", err)
	}
	if service.hookExecutor != nil {
		t.Fatalf("expected no executor when no enabled items")
	}

	cfg.Runtime.Hooks.Items[0].Enabled = runtimeBoolPtr(true)
	cfg.Runtime.Hooks.Items[0].Handler = "unsupported"
	if err := configureRuntimeHooksFromConfig(service, cfg); err == nil {
		t.Fatal("expected invalid handler error")
	}

	if got := mapRuntimeHookFailurePolicy("fail_closed"); got != runtimehooks.FailurePolicyFailClosed {
		t.Fatalf("unexpected failure policy: %q", got)
	}
	if got := mapRuntimeHookFailurePolicy("warn_only"); got != runtimehooks.FailurePolicyFailOpen {
		t.Fatalf("unexpected warn_only mapping: %q", got)
	}
	if got := mapRuntimeHookFailurePolicy("unknown"); got != runtimehooks.FailurePolicyFailOpen {
		t.Fatalf("unexpected default mapping: %q", got)
	}

	if unwrapBaseHookExecutor(nil) != nil {
		t.Fatal("expected nil unwrap")
	}
	base := &countingHookExecutor{}
	composed := &userComposedHookExecutor{base: base}
	if unwrapBaseHookExecutor(composed) != base {
		t.Fatal("expected unwrap composed base executor")
	}
}

func TestUserComposedHookExecutorBranches(t *testing.T) {
	t.Parallel()

	baseBlocked := &countingHookExecutor{output: runtimehooks.RunOutput{Blocked: true, BlockedBy: "base"}}
	userCalled := &countingHookExecutor{output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "user"}}}}
	exec := &userComposedHookExecutor{base: baseBlocked, user: userCalled}
	out := exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if !out.Blocked || out.BlockedBy != "base" {
		t.Fatalf("expected base block to short-circuit, got %+v", out)
	}
	if userCalled.calls.Load() != 0 {
		t.Fatalf("expected user executor not called when base blocked")
	}

	exec = &userComposedHookExecutor{
		base: &countingHookExecutor{output: runtimehooks.RunOutput{}},
		user: &countingHookExecutor{output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "u1"}}}},
	}
	out = exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if len(out.Results) != 1 || out.Results[0].HookID != "u1" {
		t.Fatalf("expected user-only result, got %+v", out.Results)
	}

	exec = &userComposedHookExecutor{
		base: &countingHookExecutor{output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "b1"}}}},
		user: &countingHookExecutor{output: runtimehooks.RunOutput{}},
	}
	out = exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if len(out.Results) != 1 || out.Results[0].HookID != "b1" {
		t.Fatalf("expected base-only result, got %+v", out.Results)
	}

	exec = &userComposedHookExecutor{
		base: &countingHookExecutor{output: runtimehooks.RunOutput{Results: []runtimehooks.HookResult{{HookID: "b1"}}}},
		user: &countingHookExecutor{output: runtimehooks.RunOutput{
			Results:   []runtimehooks.HookResult{{HookID: "u1"}},
			Blocked:   true,
			BlockedBy: "u1",
		}},
	}
	out = exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if !out.Blocked || out.BlockedBy != "u1" || len(out.Results) != 2 {
		t.Fatalf("expected merged blocked output, got %+v", out)
	}
}

func TestUserHookHelperBranches(t *testing.T) {
	t.Parallel()

	if got := readHookParamString(nil, "k"); got != "" {
		t.Fatalf("expected empty from nil map, got %q", got)
	}
	if got := readHookParamString(map[string]any{"k": 12}, "k"); got != "12" {
		t.Fatalf("expected fmt string, got %q", got)
	}
	if got := readHookParamStringSlice(nil, "k"); got != nil {
		t.Fatalf("expected nil slice, got %+v", got)
	}
	if got := readHookParamStringSlice(map[string]any{"k": []string{"a"}}, "k"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("unexpected []string conversion: %+v", got)
	}
	gotAny := readHookParamStringSlice(map[string]any{"k": []any{" a ", nil, 3}}, "k")
	if len(gotAny) != 2 || gotAny[0] != "a" || gotAny[1] != "3" {
		t.Fatalf("unexpected []any conversion: %+v", gotAny)
	}
	if got := readHookParamStringSlice(map[string]any{"k": "bad"}, "k"); got != nil {
		t.Fatalf("expected unsupported type -> nil, got %+v", got)
	}

	normalized := normalizeHookParamStringSlice([]string{" BASH ", "", " Filesystem "})
	if len(normalized) != 2 || normalized[0] != "bash" || normalized[1] != "filesystem" {
		t.Fatalf("unexpected normalized values: %+v", normalized)
	}

	meta := runtimehooks.HookContext{Metadata: map[string]any{"tool_name": 123, "workdir": " /tmp/x "}}
	if got := readHookContextMetadataString(meta, " TOOL_NAME "); got != "123" {
		t.Fatalf("unexpected metadata conversion: %q", got)
	}
	if got := readHookContextMetadataString(runtimehooks.HookContext{}, "k"); got != "" {
		t.Fatalf("expected empty metadata read, got %q", got)
	}
	if got := resolveHookWorkdir(meta, "fallback"); strings.TrimSpace(got) != "/tmp/x" {
		t.Fatalf("expected metadata workdir, got %q", got)
	}
	if got := resolveHookWorkdir(runtimehooks.HookContext{}, "  fallback "); got != "fallback" {
		t.Fatalf("expected fallback workdir, got %q", got)
	}
}

func TestUserHookHandlersAndPathChecks(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()

	if _, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{}, workdir); err == nil {
		t.Fatal("expected missing path error")
	}
	if _, err := buildUserBuiltinHookHandler("warn_on_tool_call", map[string]any{}, workdir); err == nil {
		t.Fatal("expected missing tool target error")
	}
	if _, err := buildUserBuiltinHookHandler("add_context_note", map[string]any{}, workdir); err == nil {
		t.Fatal("expected missing note/message error")
	}
	if _, err := buildUserBuiltinHookHandler("unknown", map[string]any{}, workdir); err == nil {
		t.Fatal("expected unsupported handler error")
	}

	warnHandler, err := buildUserBuiltinHookHandler("warn_on_tool_call", map[string]any{
		"tool_names": []any{"Bash", "filesystem"},
	}, workdir)
	if err != nil {
		t.Fatalf("build warn handler: %v", err)
	}
	result := warnHandler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"tool_name": "bash"}})
	if result.Message == "" {
		t.Fatalf("expected default warn message for matched tool")
	}
	result = warnHandler(context.Background(), runtimehooks.HookContext{})
	if result.Message != "" {
		t.Fatalf("expected empty message when no tool_name metadata, got %q", result.Message)
	}

	noteHandler, err := buildUserBuiltinHookHandler("add_context_note", map[string]any{"message": "note-via-message"}, workdir)
	if err != nil {
		t.Fatalf("build note handler: %v", err)
	}
	if result := noteHandler(context.Background(), runtimehooks.HookContext{}); result.Message != "note-via-message" {
		t.Fatalf("unexpected note message: %+v", result)
	}

	sub := filepath.Join(workdir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	dirPath := filepath.Join(workdir, "dir-only")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir dir-only: %v", err)
	}
	handler, err := buildUserBuiltinHookHandler("require_file_exists", map[string]any{"path": "dir-only"}, workdir)
	if err != nil {
		t.Fatalf("build require_file_exists dir handler: %v", err)
	}
	dirResult := handler(context.Background(), runtimehooks.HookContext{Metadata: map[string]any{"workdir": workdir}})
	if dirResult.Status != runtimehooks.HookResultFailed || !strings.Contains(dirResult.Message, "directory") {
		t.Fatalf("expected directory failure, got %+v", dirResult)
	}

	if _, err := resolveHookPathWithinWorkdir("", "a.txt"); err == nil {
		t.Fatal("expected empty workdir error")
	}
	if _, err := resolveHookPathWithinWorkdir(workdir, " "); err == nil {
		t.Fatal("expected empty path error")
	}
	if err := ensureHookPathWithinBase(workdir, filepath.Join(workdir, "sub", "f.txt")); err != nil {
		t.Fatalf("expected in-base path allowed: %v", err)
	}
	if err := ensureHookPathWithinBase(workdir, filepath.Clean(filepath.Join(workdir, ".."))); err == nil {
		t.Fatal("expected outside base path rejection")
	}
	if err := ensureHookPathWithinBase("", "x"); err == nil {
		t.Fatal("expected empty comparable path rejection")
	}
	if changed := normalizeHookComparablePath(`\\?\C:\Temp\Demo`); gruntime.GOOS == "windows" && strings.HasPrefix(changed, `\\?\`) {
		t.Fatalf("expected windows prefix normalized, got %q", changed)
	}

	symlinkPath := filepath.Join(workdir, "link-to-sub")
	if err := os.Symlink(sub, symlinkPath); err == nil {
		contains, checkErr := hookPathContainsSymlink(workdir, filepath.Join(symlinkPath, "f.txt"))
		if checkErr != nil {
			t.Fatalf("hookPathContainsSymlink() error = %v", checkErr)
		}
		if !contains {
			t.Fatalf("expected symlink path detection to be true")
		}
	} else if !errors.Is(err, os.ErrPermission) && !strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
		t.Fatalf("symlink creation error: %v", err)
	}

	contains, err := hookPathContainsSymlink(workdir, filepath.Join(workdir, "missing", "file.txt"))
	if err != nil {
		t.Fatalf("hookPathContainsSymlink missing path error = %v", err)
	}
	if contains {
		t.Fatal("expected missing path to report no symlink")
	}

	if _, err := resolveHookPathWithinWorkdir(workdir, "../x"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
}

func TestHookPathContainsSymlinkAndResolvePathErrorBranches(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	file := filepath.Join(workdir, "file.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := resolveHookPathWithinWorkdir(workdir, file); err != nil {
		t.Fatalf("resolveHookPathWithinWorkdir(abs) error = %v", err)
	}

	base := filepath.Join(workdir, "base-file")
	if err := os.WriteFile(base, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}
	notDirChild := filepath.Join(base, "child")
	contains, err := hookPathContainsSymlink(base, notDirChild)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not a directory") {
		t.Fatalf("hookPathContainsSymlink() unexpected error = %v", err)
	}
	if contains {
		t.Fatalf("expected non-existent child under file base to report no symlink")
	}
}
