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
			Point:   "session_start",
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
