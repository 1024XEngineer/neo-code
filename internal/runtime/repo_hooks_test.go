package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

func TestEvaluateWorkspaceTrustFallbackModes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := filepath.Join(homeDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}

	assertUntrustedWithInvalid := func(label string) {
		t.Helper()
		decision := evaluateWorkspaceTrust(workspace)
		if decision.Trusted {
			t.Fatalf("%s: expected untrusted", label)
		}
		if strings.TrimSpace(decision.InvalidReason) == "" {
			t.Fatalf("%s: expected invalid reason", label)
		}
	}

	assertUntrustedWithInvalid("missing")
	if err := os.WriteFile(storePath, []byte(" \n\t "), 0o644); err != nil {
		t.Fatalf("write empty store: %v", err)
	}
	assertUntrustedWithInvalid("empty")

	if err := os.WriteFile(storePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write malformed store: %v", err)
	}
	assertUntrustedWithInvalid("malformed")

	if err := os.WriteFile(storePath, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatalf("write invalid shape store: %v", err)
	}
	assertUntrustedWithInvalid("shape mismatch")

	store := trustedWorkspaceStore{Version: repoHooksTrustStoreVersion, Workspaces: []string{workspace}}
	encoded, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, encoded, 0o644); err != nil {
		t.Fatalf("write trusted store: %v", err)
	}
	decision := evaluateWorkspaceTrust(workspace)
	if !decision.Trusted || strings.TrimSpace(decision.InvalidReason) != "" {
		t.Fatalf("trusted decision = %+v, want trusted and no invalid reason", decision)
	}
}

func TestLoadRepoHookItemsRejectsDuplicateIDWithinRepoSource(t *testing.T) {
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	content := `
hooks:
  items:
    - id: same
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: first
    - id: same
      point: after_tool_result
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: second
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}
	_, err := loadRepoHookItems(hooksPath, config.StaticDefaults().Runtime.Hooks)
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("loadRepoHookItems() error = %v, want duplicate id error", err)
	}
}

func TestConfigureRuntimeHooksFromConfigComposesInternalUserRepoOrder(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	repoHooks := `
hooks:
  items:
    - id: shared-id
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note
`
	if err := os.WriteFile(hooksPath, []byte(repoHooks), 0o644); err != nil {
		t.Fatalf("write repo hooks file: %v", err)
	}

	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	store := trustedWorkspaceStore{Version: repoHooksTrustStoreVersion, Workspaces: []string{workspace}}
	rawStore, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	cfg := *config.StaticDefaults()
	cfg.Workdir = workspace
	cfg.Runtime.Hooks.Items = []config.RuntimeHookItemConfig{
		{
			ID:      "shared-id",
			Enabled: runtimeBoolPtr(true),
			Point:   "before_tool_call",
			Scope:   "user",
			Kind:    "builtin",
			Mode:    "sync",
			Handler: "add_context_note",
			Params:  map[string]any{"note": "user-note"},
		},
	}
	cfg.Runtime.Hooks.ApplyDefaults(config.StaticDefaults().Runtime.Hooks)

	base := &countingHookExecutor{
		output: runtimehooks.RunOutput{
			Results: []runtimehooks.HookResult{
				{
					HookID:  "base-id",
					Scope:   runtimehooks.HookScopeInternal,
					Source:  runtimehooks.HookSourceInternal,
					Status:  runtimehooks.HookResultPass,
					Message: "base-note",
				},
			},
		},
	}
	service := &Service{
		hookExecutor: base,
		events:       make(chan RuntimeEvent, 64),
	}

	if err := configureRuntimeHooksFromConfig(service, cfg); err != nil {
		t.Fatalf("configureRuntimeHooksFromConfig() error = %v", err)
	}
	if service.hookExecutor == nil {
		t.Fatal("expected composed hook executor")
	}

	out := service.hookExecutor.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{
		Metadata: map[string]any{
			"tool_name":      "bash",
			"tool_arguments": "secret-value",
			"workdir":        workspace,
		},
	})
	if len(out.Results) != 3 {
		t.Fatalf("results len = %d, want 3 (%+v)", len(out.Results), out.Results)
	}
	if out.Results[0].Source != runtimehooks.HookSourceInternal {
		t.Fatalf("result[0].source = %q, want internal", out.Results[0].Source)
	}
	if out.Results[1].Source != runtimehooks.HookSourceUser || out.Results[1].Message != "user-note" {
		t.Fatalf("result[1] = %+v, want user source + user-note", out.Results[1])
	}
	if out.Results[2].Source != runtimehooks.HookSourceRepo || out.Results[2].Message != "repo-note" {
		t.Fatalf("result[2] = %+v, want repo source + repo-note", out.Results[2])
	}
}

func TestBuildRepoHookExecutorUntrustedSkipsAndEmitsEvent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	content := `
hooks:
  items:
    - id: repo-hook
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	// trust store 存在但不包含当前 workspace，命中 untrusted 分支。
	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	otherWorkspace := filepath.Join(homeDir, "other")
	if err := os.MkdirAll(otherWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir other workspace: %v", err)
	}
	rawStore, err := json.Marshal(trustedWorkspaceStore{
		Version:    repoHooksTrustStoreVersion,
		Workspaces: []string{otherWorkspace},
	})
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 64)}
	exec, err := buildRepoHookExecutorForWorkspace(service, workspace, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("buildRepoHookExecutorForWorkspace() error = %v", err)
	}
	if exec != nil {
		t.Fatal("expected nil repo executor for untrusted workspace")
	}

	events := collectRuntimeEvents(service.Events())
	if !containsRuntimeEventType(events, EventRepoHooksDiscovered) {
		t.Fatalf("expected %s event", EventRepoHooksDiscovered)
	}
	if !containsRuntimeEventType(events, EventRepoHooksSkippedUntrusted) {
		t.Fatalf("expected %s event", EventRepoHooksSkippedUntrusted)
	}
}

func TestRepoHookExecutionEventCarriesRepoSource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	content := `
hooks:
  items:
    - id: repo-note
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}
	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	rawStore, err := json.Marshal(trustedWorkspaceStore{
		Version:    repoHooksTrustStoreVersion,
		Workspaces: []string{workspace},
	})
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 64)}
	exec, err := buildRepoHookExecutorForWorkspace(service, workspace, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("buildRepoHookExecutorForWorkspace() error = %v", err)
	}
	if exec == nil {
		t.Fatal("expected repo executor for trusted workspace")
	}
	_ = exec.Run(
		context.Background(),
		runtimehooks.HookPointBeforeToolCall,
		runtimehooks.HookContext{Metadata: map[string]any{"tool_name": "bash", "workdir": workspace}},
	)

	events := collectRuntimeEvents(service.Events())
	finishedIndex := eventIndex(events, EventHookFinished)
	if finishedIndex < 0 {
		t.Fatal("expected hook_finished event from repo hook execution")
	}
	payload, ok := events[finishedIndex].Payload.(HookEventPayload)
	if !ok {
		t.Fatalf("payload type = %T, want HookEventPayload", events[finishedIndex].Payload)
	}
	if payload.Source != string(runtimehooks.HookSourceRepo) {
		t.Fatalf("payload.Source = %q, want %q", payload.Source, runtimehooks.HookSourceRepo)
	}
}

func TestBuildRepoHookExecutorMissingTrustStoreEmitsInvalidEvent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	content := `
hooks:
  items:
    - id: repo-hook
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 64)}
	exec, err := buildRepoHookExecutorForWorkspace(service, workspace, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("buildRepoHookExecutorForWorkspace() error = %v", err)
	}
	if exec != nil {
		t.Fatal("expected nil repo executor when trust store is missing")
	}

	events := collectRuntimeEvents(service.Events())
	if !containsRuntimeEventType(events, EventRepoHooksTrustStoreInvalid) {
		t.Fatalf("expected %s event", EventRepoHooksTrustStoreInvalid)
	}
	if !containsRuntimeEventType(events, EventRepoHooksSkippedUntrusted) {
		t.Fatalf("expected %s event", EventRepoHooksSkippedUntrusted)
	}
}

func TestBuildRepoHookExecutorRejectsExternalKindAndDoesNotRegister(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	content := `
hooks:
  items:
    - id: repo-external-command
      point: before_tool_call
      scope: repo
      kind: command
      mode: sync
      handler: warn_on_tool_call
      params:
        tool_name: bash
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks: %v", err)
	}
	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	rawStore, err := json.Marshal(trustedWorkspaceStore{
		Version:    repoHooksTrustStoreVersion,
		Workspaces: []string{workspace},
	})
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	service := &Service{events: make(chan RuntimeEvent, 16)}
	exec, err := buildRepoHookExecutorForWorkspace(service, workspace, config.StaticDefaults().Runtime.Hooks)
	if err == nil {
		t.Fatal("expected external kind in repo hook config to be rejected")
	}
	if !strings.Contains(err.Error(), "not supported in P6-lite") {
		t.Fatalf("error=%q, want contains not supported in P6-lite", err.Error())
	}
	if exec != nil {
		t.Fatalf("unexpected repo executor after rejection: %T", exec)
	}
}

func TestDynamicRepoHookExecutorResolvesByRunWorkdir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspaceA := filepath.Join(homeDir, "workspace-a")
	workspaceB := filepath.Join(homeDir, "workspace-b")
	if err := os.MkdirAll(filepath.Join(workspaceA, ".neocode"), 0o755); err != nil {
		t.Fatalf("mkdir workspaceA hooks dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceB, ".neocode"), 0o755); err != nil {
		t.Fatalf("mkdir workspaceB hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceA, ".neocode", "hooks.yaml"), []byte(`
hooks:
  items:
    - id: repo-a
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note-a
`), 0o644); err != nil {
		t.Fatalf("write workspaceA hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceB, ".neocode", "hooks.yaml"), []byte(`
hooks:
  items:
    - id: repo-b
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note-b
`), 0o644); err != nil {
		t.Fatalf("write workspaceB hooks: %v", err)
	}

	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	rawStore, err := json.Marshal(trustedWorkspaceStore{
		Version:    repoHooksTrustStoreVersion,
		Workspaces: []string{workspaceA, workspaceB},
	})
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	cfg := *config.StaticDefaults()
	cfg.Workdir = workspaceA
	service := &Service{events: make(chan RuntimeEvent, 64)}
	repoExecutor, err := buildRepoHookExecutor(service, cfg, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("buildRepoHookExecutor() error = %v", err)
	}
	if repoExecutor == nil {
		t.Fatal("expected dynamic repo executor")
	}

	run := func(workdir string) runtimehooks.RunOutput {
		return repoExecutor.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{
			Metadata: map[string]any{
				"tool_name": "bash",
				"workdir":   workdir,
			},
		})
	}

	first := run(workspaceA)
	if len(first.Results) != 1 || first.Results[0].Message != "repo-note-a" {
		t.Fatalf("workspaceA output = %+v, want repo-note-a", first.Results)
	}
	second := run(workspaceB)
	if len(second.Results) != 1 || second.Results[0].Message != "repo-note-b" {
		t.Fatalf("workspaceB output = %+v, want repo-note-b", second.Results)
	}
}

func containsRuntimeEventType(events []RuntimeEvent, target EventType) bool {
	for _, event := range events {
		if event.Type == target {
			return true
		}
	}
	return false
}

func TestValidateRepoHookItemBranches(t *testing.T) {
	base := config.RuntimeHookItemConfig{
		ID:            "repo-hook",
		Point:         "before_tool_call",
		Scope:         "repo",
		Kind:          "builtin",
		Mode:          "sync",
		Handler:       "add_context_note",
		TimeoutSec:    2,
		FailurePolicy: "warn_only",
		Params:        map[string]any{"note": "x"},
	}

	if err := validateRepoHookItem(base); err != nil {
		t.Fatalf("validateRepoHookItem(valid) error = %v", err)
	}

	cases := []struct {
		name string
		edit func(*config.RuntimeHookItemConfig)
	}{
		{name: "missing id", edit: func(item *config.RuntimeHookItemConfig) { item.ID = "" }},
		{name: "bad point", edit: func(item *config.RuntimeHookItemConfig) { item.Point = "unknown_point" }},
		{name: "repo disallowed point", edit: func(item *config.RuntimeHookItemConfig) { item.Point = "pre_compact" }},
		{name: "bad scope", edit: func(item *config.RuntimeHookItemConfig) { item.Scope = "user" }},
		{name: "bad kind", edit: func(item *config.RuntimeHookItemConfig) { item.Kind = "command" }},
		{name: "bad mode", edit: func(item *config.RuntimeHookItemConfig) { item.Mode = "async" }},
		{name: "bad timeout", edit: func(item *config.RuntimeHookItemConfig) { item.TimeoutSec = 0 }},
		{name: "bad policy", edit: func(item *config.RuntimeHookItemConfig) { item.FailurePolicy = "deny" }},
		{name: "bad handler", edit: func(item *config.RuntimeHookItemConfig) { item.Handler = "unknown" }},
		{
			name: "warn_on_tool_call missing target",
			edit: func(item *config.RuntimeHookItemConfig) {
				item.Handler = "warn_on_tool_call"
				item.Params = map[string]any{}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			item := base.Clone()
			tc.edit(&item)
			if err := validateRepoHookItem(item); err == nil {
				t.Fatalf("validateRepoHookItem(%s) expected error", tc.name)
			}
		})
	}
}

func TestValidateRepoHookItemRejectsExternalKindsWithP6LiteMessage(t *testing.T) {
	t.Parallel()

	base := config.RuntimeHookItemConfig{
		ID:            "repo-hook",
		Point:         "before_tool_call",
		Scope:         "repo",
		Kind:          "builtin",
		Mode:          "sync",
		Handler:       "add_context_note",
		TimeoutSec:    2,
		FailurePolicy: "warn_only",
		Params:        map[string]any{"note": "x"},
	}
	externalKinds := []string{"command", "http", "prompt", "agent"}
	for _, kind := range externalKinds {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			item := base.Clone()
			item.Kind = kind
			err := validateRepoHookItem(item)
			if err == nil {
				t.Fatalf("expected external kind %q to be rejected", kind)
			}
			if !strings.Contains(err.Error(), "not supported in P6-lite") {
				t.Fatalf("error=%q, want contains not supported in P6-lite", err.Error())
			}
		})
	}
}

func TestRuntimeHasWarnOnToolCallTargetsBranches(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   bool
	}{
		{name: "nil", params: nil, want: false},
		{name: "tool_name", params: map[string]any{"tool_name": "bash"}, want: true},
		{name: "tool_name blank", params: map[string]any{"tool_name": " "}, want: false},
		{name: "tool_names", params: map[string]any{"tool_names": []any{"bash"}}, want: true},
		{name: "tool_names blank", params: map[string]any{"tool_names": []any{" "}}, want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimeHasWarnOnToolCallTargets(tc.params); got != tc.want {
				t.Fatalf("runtimeHasWarnOnToolCallTargets() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveRepoHooksPathBranches(t *testing.T) {
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")

	path, found, err := resolveRepoHooksPath(workspace)
	if err != nil || found || path != hooksPath {
		t.Fatalf("resolveRepoHooksPath(missing) = (%q,%v,%v), want (%q,false,nil)", path, found, err, hooksPath)
	}

	if err := os.MkdirAll(hooksPath, 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	_, _, err = resolveRepoHooksPath(workspace)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "directory") {
		t.Fatalf("resolveRepoHooksPath(directory) error = %v, want directory error", err)
	}
}

func TestNormalizeTrustedWorkspacePathBranches(t *testing.T) {
	if _, err := normalizeTrustedWorkspacePath(""); err == nil {
		t.Fatal("expected empty path error")
	}
	if _, err := normalizeTrustedWorkspacePath("relative/path"); err == nil {
		t.Fatal("expected relative path error")
	}
	workspace := t.TempDir()
	got, err := normalizeTrustedWorkspacePath(workspace)
	if err != nil {
		t.Fatalf("normalizeTrustedWorkspacePath(abs) error = %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("normalized workspace path should not be empty")
	}
}

func TestDynamicRepoHookExecutorCachesWorkspaceResult(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspace := filepath.Join(homeDir, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, ".neocode"), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".neocode", "hooks.yaml"), []byte(`
hooks:
  items:
    - id: repo-cache
      point: before_tool_call
      scope: repo
      kind: builtin
      mode: sync
      handler: add_context_note
      params:
        note: repo-note-cache
`), 0o644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}
	storePath := resolveTrustedWorkspacesPath()
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir trust store dir: %v", err)
	}
	rawStore, err := json.Marshal(trustedWorkspaceStore{
		Version:    repoHooksTrustStoreVersion,
		Workspaces: []string{workspace},
	})
	if err != nil {
		t.Fatalf("marshal trust store: %v", err)
	}
	if err := os.WriteFile(storePath, rawStore, 0o644); err != nil {
		t.Fatalf("write trust store: %v", err)
	}

	cfg := *config.StaticDefaults()
	cfg.Workdir = workspace
	service := &Service{events: make(chan RuntimeEvent, 64)}
	exec, err := buildRepoHookExecutor(service, cfg, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("buildRepoHookExecutor() error = %v", err)
	}
	dynamic, ok := exec.(*dynamicRepoHookExecutor)
	if !ok {
		t.Fatalf("expected dynamicRepoHookExecutor, got %T", exec)
	}

	input := runtimehooks.HookContext{Metadata: map[string]any{"workdir": workspace}}
	first := dynamic.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, input)
	second := dynamic.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, input)
	if len(first.Results) != 1 || len(second.Results) != 1 {
		t.Fatalf("unexpected cached run outputs: first=%+v second=%+v", first.Results, second.Results)
	}
	if first.Results[0].Message != "repo-note-cache" || second.Results[0].Message != "repo-note-cache" {
		t.Fatalf("unexpected note messages: first=%+v second=%+v", first.Results, second.Results)
	}
	dynamic.mu.RLock()
	cacheSize := len(dynamic.cache)
	dynamic.mu.RUnlock()
	if cacheSize != 1 {
		t.Fatalf("cache size = %d, want 1", cacheSize)
	}
}

func TestDynamicRepoHookExecutorEarlyReturnBranches(t *testing.T) {
	exec := &dynamicRepoHookExecutor{}
	out := exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if len(out.Results) != 0 || out.Blocked {
		t.Fatalf("expected empty output for nil-config executor, got %+v", out)
	}

	exec = &dynamicRepoHookExecutor{fallbackWorkdir: " "}
	out = exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if len(out.Results) != 0 || out.Blocked {
		t.Fatalf("expected empty output for blank workspace, got %+v", out)
	}

	exec = &dynamicRepoHookExecutor{fallbackWorkdir: "relative/path"}
	out = exec.Run(context.Background(), runtimehooks.HookPointBeforeToolCall, runtimehooks.HookContext{})
	if len(out.Results) != 0 || out.Blocked {
		t.Fatalf("expected empty output for invalid workspace path, got %+v", out)
	}
}

func TestLoadRepoHookItemsAndDefaultsBranches(t *testing.T) {
	workspace := t.TempDir()
	hooksPath := filepath.Join(workspace, ".neocode", "hooks.yaml")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}

	if err := os.WriteFile(hooksPath, []byte(" \n\t "), 0o644); err != nil {
		t.Fatalf("write empty hooks file: %v", err)
	}
	if _, err := loadRepoHookItems(hooksPath, config.StaticDefaults().Runtime.Hooks); err == nil {
		t.Fatal("expected empty hooks file error")
	}

	content := `
hooks:
  items:
    - id: disabled
      enabled: false
      point: before_tool_call
      handler: add_context_note
      params:
        note: skip
    - id: enabled-defaults
      point: before_tool_call
      handler: add_context_note
      params:
        note: ok
`
	if err := os.WriteFile(hooksPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}
	items, err := loadRepoHookItems(hooksPath, config.StaticDefaults().Runtime.Hooks)
	if err != nil {
		t.Fatalf("loadRepoHookItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item := items[0]
	if item.Scope != "repo" || item.Kind != "builtin" || item.Mode != "sync" {
		t.Fatalf("unexpected defaults: scope=%q kind=%q mode=%q", item.Scope, item.Kind, item.Mode)
	}
}

func TestResolveTrustedWorkspacesPathFallbackBranches(t *testing.T) {
	t.Setenv("HOME", "relative-home")
	path := resolveTrustedWorkspacesPath()
	if !strings.Contains(path, filepath.Join(".neocode", repoHooksTrustStoreFileName)) {
		t.Fatalf("unexpected trust store path: %q", path)
	}

	t.Setenv("HOME", "")
	path = resolveTrustedWorkspacesPath()
	if !strings.Contains(path, filepath.Join(".neocode", repoHooksTrustStoreFileName)) {
		t.Fatalf("unexpected trust store path with empty HOME: %q", path)
	}
}

func TestRepoHookEventEmittersAndHelpers(t *testing.T) {
	emitRepoHooksLifecycleEvent(nil, EventRepoHooksDiscovered, RepoHooksLifecyclePayload{})
	emitRepoHooksTrustStoreInvalidEvent(nil, RepoHooksTrustStoreInvalidPayload{})

	if got := coalesceHookMessage(" ", "fallback", "other"); got != "fallback" {
		t.Fatalf("coalesceHookMessage() = %q, want fallback", got)
	}
	if got := coalesceHookMessage(" ", "\t"); got != "" {
		t.Fatalf("coalesceHookMessage(blank) = %q, want empty", got)
	}
}
