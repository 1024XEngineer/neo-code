package runtime

import (
	"testing"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

func TestRuntimeHookPointUserAllowedMatchesConfigValidation(t *testing.T) {
	t.Parallel()

	points := []runtimehooks.HookPoint{
		runtimehooks.HookPointBeforeToolCall,
		runtimehooks.HookPointAfterToolResult,
		runtimehooks.HookPointBeforeCompletionDecision,
		runtimehooks.HookPointBeforePermissionDecision,
		runtimehooks.HookPointAfterToolFailure,
		runtimehooks.HookPointSessionStart,
		runtimehooks.HookPointSessionEnd,
		runtimehooks.HookPointUserPromptSubmit,
		runtimehooks.HookPointPreCompact,
		runtimehooks.HookPointPostCompact,
		runtimehooks.HookPointSubAgentStart,
		runtimehooks.HookPointSubAgentStop,
	}

	for _, point := range points {
		point := point
		t.Run(string(point), func(t *testing.T) {
			t.Parallel()

			capability, ok := runtimehooks.HookPointCapabilities(point)
			if !ok {
				t.Fatalf("missing capability for point %q", point)
			}

			item := config.RuntimeHookItemConfig{
				ID:            "consistency-" + string(point),
				Point:         string(point),
				Scope:         "user",
				Kind:          "builtin",
				Mode:          "sync",
				Handler:       "add_context_note",
				TimeoutSec:    1,
				FailurePolicy: "warn_only",
				Params: map[string]any{
					"note": "consistency",
				},
			}

			err := item.Validate("warn_only")
			if capability.UserAllowed && err != nil {
				t.Fatalf("point %q should allow user hooks, got error: %v", point, err)
			}
			if !capability.UserAllowed && err == nil {
				t.Fatalf("point %q should reject user hooks by config validation", point)
			}
		})
	}
}
