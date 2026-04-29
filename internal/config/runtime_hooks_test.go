package config

import "testing"

func TestRuntimeHooksConfigApplyDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	var hooksCfg RuntimeHooksConfig
	defaults := defaultRuntimeHooksConfig()
	hooksCfg.ApplyDefaults(defaults)

	if !hooksCfg.IsEnabled() {
		t.Fatal("expected hooks enabled by default")
	}
	if !hooksCfg.IsUserHooksEnabled() {
		t.Fatal("expected user hooks enabled by default")
	}
	if hooksCfg.DefaultTimeoutSec != DefaultRuntimeHookTimeoutSec {
		t.Fatalf("default timeout = %d, want %d", hooksCfg.DefaultTimeoutSec, DefaultRuntimeHookTimeoutSec)
	}
	if hooksCfg.DefaultFailurePolicy != runtimeHookFailurePolicyWarnOnly {
		t.Fatalf(
			"default failure policy = %q, want %q",
			hooksCfg.DefaultFailurePolicy,
			runtimeHookFailurePolicyWarnOnly,
		)
	}
	if err := hooksCfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRuntimeHooksConfigValidateUnsupportedFields(t *testing.T) {
	t.Parallel()

	base := RuntimeHooksConfig{
		Enabled:              boolPtr(true),
		UserHooksEnabled:     boolPtr(true),
		DefaultTimeoutSec:    2,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
	}

	tests := []RuntimeHookItemConfig{
		{
			ID:      "bad-scope",
			Point:   runtimeHookPointBeforeToolCall,
			Scope:   "repo",
			Kind:    runtimeHookKindBuiltIn,
			Mode:    runtimeHookModeSync,
			Handler: runtimeHookHandlerWarnOnToolCall,
		},
		{
			ID:      "bad-kind",
			Point:   runtimeHookPointBeforeToolCall,
			Scope:   runtimeHookScopeUser,
			Kind:    "command",
			Mode:    runtimeHookModeSync,
			Handler: runtimeHookHandlerWarnOnToolCall,
		},
		{
			ID:      "bad-mode",
			Point:   runtimeHookPointBeforeToolCall,
			Scope:   runtimeHookScopeUser,
			Kind:    runtimeHookKindBuiltIn,
			Mode:    "async",
			Handler: runtimeHookHandlerWarnOnToolCall,
		},
		{
			ID:      "bad-handler",
			Point:   runtimeHookPointBeforeToolCall,
			Scope:   runtimeHookScopeUser,
			Kind:    runtimeHookKindBuiltIn,
			Mode:    runtimeHookModeSync,
			Handler: "shell_exec",
		},
		{
			ID:      "bad-point",
			Point:   "unknown_point",
			Scope:   runtimeHookScopeUser,
			Kind:    runtimeHookKindBuiltIn,
			Mode:    runtimeHookModeSync,
			Handler: runtimeHookHandlerWarnOnToolCall,
		},
	}

	for _, item := range tests {
		cfg := base.Clone()
		cfg.Items = []RuntimeHookItemConfig{item}
		cfg.ApplyDefaults(defaultRuntimeHooksConfig())
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected validate error for item=%+v", item)
		}
	}
}

func TestRuntimeHooksConfigValidateRejectsDisallowedUserPoint(t *testing.T) {
	t.Parallel()

	cfg := RuntimeHooksConfig{
		Enabled:              boolPtr(true),
		UserHooksEnabled:     boolPtr(true),
		DefaultTimeoutSec:    2,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		Items: []RuntimeHookItemConfig{
			{
				ID:            "deny-pre-compact",
				Point:         runtimeHookPointPreCompact,
				Scope:         runtimeHookScopeUser,
				Kind:          runtimeHookKindBuiltIn,
				Mode:          runtimeHookModeSync,
				Handler:       runtimeHookHandlerAddContextNote,
				TimeoutSec:    2,
				FailurePolicy: runtimeHookFailurePolicyWarnOnly,
				Params:        map[string]any{"note": "test"},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected user-disallowed point to fail validation")
	}
}

func TestRuntimeHooksConfigItemDefaultsAndClone(t *testing.T) {
	t.Parallel()

	cfg := RuntimeHooksConfig{
		Enabled:              boolPtr(true),
		UserHooksEnabled:     boolPtr(true),
		DefaultTimeoutSec:    3,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		Items: []RuntimeHookItemConfig{
			{
				ID:      "warn-bash",
				Point:   runtimeHookPointBeforeToolCall,
				Handler: runtimeHookHandlerWarnOnToolCall,
				Params: map[string]any{
					"tool_name": "bash",
					"tags":      []any{"warn", "tool"},
				},
			},
		},
	}
	cfg.ApplyDefaults(defaultRuntimeHooksConfig())

	item := cfg.Items[0]
	if !item.IsEnabled() {
		t.Fatal("expected hook item enabled by default")
	}
	if item.Scope != runtimeHookScopeUser {
		t.Fatalf("scope=%q, want %q", item.Scope, runtimeHookScopeUser)
	}
	if item.Kind != runtimeHookKindBuiltIn {
		t.Fatalf("kind=%q, want %q", item.Kind, runtimeHookKindBuiltIn)
	}
	if item.Mode != runtimeHookModeSync {
		t.Fatalf("mode=%q, want %q", item.Mode, runtimeHookModeSync)
	}
	if item.TimeoutSec != cfg.DefaultTimeoutSec {
		t.Fatalf("timeout=%d, want %d", item.TimeoutSec, cfg.DefaultTimeoutSec)
	}
	if item.FailurePolicy != runtimeHookFailurePolicyWarnOnly {
		t.Fatalf("failure policy=%q, want %q", item.FailurePolicy, runtimeHookFailurePolicyWarnOnly)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	cloned := cfg.Clone()
	cloned.Items[0].Params["tool_name"] = "filesystem"
	tags, _ := cloned.Items[0].Params["tags"].([]any)
	tags[0] = "changed"
	cloned.Items[0].Params["tags"] = tags

	if cfg.Items[0].Params["tool_name"] == "filesystem" {
		t.Fatal("expected params map to be deep-copied")
	}
	originalTags, _ := cfg.Items[0].Params["tags"].([]any)
	if len(originalTags) > 0 && originalTags[0] == "changed" {
		t.Fatal("expected params slice to be deep-copied")
	}
}

func TestRuntimeHooksConfigValidateItemFailurePolicy(t *testing.T) {
	t.Parallel()

	cfg := RuntimeHooksConfig{
		Enabled:              boolPtr(true),
		UserHooksEnabled:     boolPtr(true),
		DefaultTimeoutSec:    2,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		Items: []RuntimeHookItemConfig{
			{
				ID:            "require-readme",
				Point:         runtimeHookPointBeforeCompletionDecision,
				Scope:         runtimeHookScopeUser,
				Kind:          runtimeHookKindBuiltIn,
				Mode:          runtimeHookModeSync,
				Handler:       runtimeHookHandlerRequireFileExists,
				TimeoutSec:    2,
				FailurePolicy: "invalid_policy",
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid item failure_policy to be rejected")
	}
}

func TestRuntimeHooksConfigValidateWarnOnToolCallRequiresTarget(t *testing.T) {
	t.Parallel()

	cfg := RuntimeHooksConfig{
		Enabled:              boolPtr(true),
		UserHooksEnabled:     boolPtr(true),
		DefaultTimeoutSec:    2,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		Items: []RuntimeHookItemConfig{
			{
				ID:            "warn-missing-target",
				Point:         runtimeHookPointBeforeToolCall,
				Scope:         runtimeHookScopeUser,
				Kind:          runtimeHookKindBuiltIn,
				Mode:          runtimeHookModeSync,
				Handler:       runtimeHookHandlerWarnOnToolCall,
				TimeoutSec:    2,
				FailurePolicy: runtimeHookFailurePolicyWarnOnly,
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected warn_on_tool_call without target to fail validation")
	}
}

func TestRuntimeHooksConfigEdgeBranches(t *testing.T) {
	t.Parallel()

	t.Run("apply defaults fallback when defaults pointers are nil", func(t *testing.T) {
		t.Parallel()
		cfg := RuntimeHooksConfig{}
		cfg.ApplyDefaults(RuntimeHooksConfig{
			DefaultTimeoutSec:    5,
			DefaultFailurePolicy: runtimeHookFailurePolicyFailOpen,
		})
		if cfg.Enabled == nil || !*cfg.Enabled {
			t.Fatal("expected enabled fallback to true")
		}
		if cfg.UserHooksEnabled == nil || !*cfg.UserHooksEnabled {
			t.Fatal("expected user_hooks_enabled fallback to true")
		}
	})

	t.Run("validate root errors and duplicate id", func(t *testing.T) {
		t.Parallel()
		cfg := RuntimeHooksConfig{
			DefaultTimeoutSec:    0,
			DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected timeout validation error")
		}

		cfg = RuntimeHooksConfig{
			DefaultTimeoutSec:    2,
			DefaultFailurePolicy: "bad",
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected default failure policy validation error")
		}

		cfg = RuntimeHooksConfig{
			DefaultTimeoutSec:    2,
			DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
			Items: []RuntimeHookItemConfig{
				{ID: "dup", Point: runtimeHookPointBeforeToolCall, Scope: runtimeHookScopeUser, Kind: runtimeHookKindBuiltIn, Mode: runtimeHookModeSync, Handler: runtimeHookHandlerWarnOnToolCall, TimeoutSec: 1, Params: map[string]any{"tool_name": "bash"}},
				{ID: " DUP ", Point: runtimeHookPointBeforeToolCall, Scope: runtimeHookScopeUser, Kind: runtimeHookKindBuiltIn, Mode: runtimeHookModeSync, Handler: runtimeHookHandlerWarnOnToolCall, TimeoutSec: 1, Params: map[string]any{"tool_name": "bash"}},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected duplicate id error")
		}
	})

	t.Run("item validate missing id and timeout", func(t *testing.T) {
		t.Parallel()
		if err := (RuntimeHookItemConfig{}).Validate(runtimeHookFailurePolicyWarnOnly); err == nil {
			t.Fatal("expected missing id error")
		}
		item := RuntimeHookItemConfig{
			ID:      "x",
			Point:   runtimeHookPointBeforeToolCall,
			Scope:   runtimeHookScopeUser,
			Kind:    runtimeHookKindBuiltIn,
			Mode:    runtimeHookModeSync,
			Handler: runtimeHookHandlerAddContextNote,
			Params:  map[string]any{"note": "ok"},
		}
		if err := item.Validate(runtimeHookFailurePolicyWarnOnly); err == nil {
			t.Fatal("expected timeout error")
		}
	})

	t.Run("helper functions", func(t *testing.T) {
		t.Parallel()
		if !(RuntimeHooksConfig{Enabled: boolPtr(true)}).IsEnabled() {
			t.Fatal("expected enabled true")
		}
		if (RuntimeHooksConfig{Enabled: boolPtr(false)}).IsEnabled() {
			t.Fatal("expected enabled false")
		}
		if !(RuntimeHooksConfig{}).IsEnabled() {
			t.Fatal("expected enabled default true when nil")
		}
		if !(RuntimeHooksConfig{UserHooksEnabled: boolPtr(true)}).IsUserHooksEnabled() {
			t.Fatal("expected user hooks enabled true")
		}
		if (RuntimeHooksConfig{UserHooksEnabled: boolPtr(false)}).IsUserHooksEnabled() {
			t.Fatal("expected user hooks enabled false")
		}
		if !(RuntimeHooksConfig{}).IsUserHooksEnabled() {
			t.Fatal("expected user hooks default true when nil")
		}
		if (RuntimeHookItemConfig{Enabled: boolPtr(false)}).IsEnabled() {
			t.Fatal("expected item disabled false")
		}
		if !(RuntimeHookItemConfig{}).IsEnabled() {
			t.Fatal("expected item default enabled when nil")
		}
		if err := validateRuntimeHookFailurePolicy(runtimeHookFailurePolicyFailClose); err != nil {
			t.Fatalf("expected fail_close valid, got %v", err)
		}
		if err := validateRuntimeHookFailurePolicy("bad"); err == nil {
			t.Fatal("expected invalid policy error")
		}

		original := map[string]any{
			"a": []any{"x", map[string]any{"b": "c"}},
		}
		cloned, ok := cloneRuntimeHookParamValue(original).(map[string]any)
		if !ok {
			t.Fatal("expected cloned map")
		}
		clonedSlice := cloned["a"].([]any)
		nested := clonedSlice[1].(map[string]any)
		nested["b"] = "changed"
		origNested := original["a"].([]any)[1].(map[string]any)
		if origNested["b"] == "changed" {
			t.Fatal("expected deep clone for nested map in slice")
		}

		if hasWarnOnToolCallTargets(nil) {
			t.Fatal("nil params should be false")
		}
		if !hasWarnOnToolCallTargets(map[string]any{"tool_name": "bash"}) {
			t.Fatal("tool_name should pass")
		}
		if !hasWarnOnToolCallTargets(map[string]any{"tool_names": []string{"", "bash"}}) {
			t.Fatal("tool_names []string should pass")
		}
		if !hasWarnOnToolCallTargets(map[string]any{"tool_names": []any{"", "bash"}}) {
			t.Fatal("tool_names []any should pass")
		}
		if hasWarnOnToolCallTargets(map[string]any{"tool_names": "bash"}) {
			t.Fatal("tool_names scalar should fail")
		}
	})
}
