package hooks

import (
	"context"
	"errors"
	"testing"
)

func TestHookSpecNormalizeAndValidateDefaults(t *testing.T) {
	t.Parallel()

	spec, err := (HookSpec{
		ID:      "  hook-1  ",
		Point:   HookPoint(" before_tool_call "),
		Handler: func(context.Context, HookContext) HookResult { return HookResult{} },
	}).normalizeAndValidate()
	if err != nil {
		t.Fatalf("normalizeAndValidate() error = %v", err)
	}
	if spec.ID != "hook-1" {
		t.Fatalf("ID = %q, want hook-1", spec.ID)
	}
	if spec.Point != HookPointBeforeToolCall {
		t.Fatalf("Point = %q, want %q", spec.Point, HookPointBeforeToolCall)
	}
	if spec.Scope != HookScopeInternal {
		t.Fatalf("Scope = %q, want %q", spec.Scope, HookScopeInternal)
	}
	if spec.Kind != HookKindFunction {
		t.Fatalf("Kind = %q, want %q", spec.Kind, HookKindFunction)
	}
	if spec.Mode != HookModeSync {
		t.Fatalf("Mode = %q, want %q", spec.Mode, HookModeSync)
	}
	if spec.FailurePolicy != FailurePolicyFailOpen {
		t.Fatalf("FailurePolicy = %q, want %q", spec.FailurePolicy, FailurePolicyFailOpen)
	}
}

func TestHookSpecNormalizeAndValidateErrors(t *testing.T) {
	t.Parallel()

	handler := func(context.Context, HookContext) HookResult { return HookResult{} }
	cases := []struct {
		name string
		spec HookSpec
	}{
		{
			name: "missing id",
			spec: HookSpec{
				Point:   HookPointBeforeToolCall,
				Handler: handler,
			},
		},
		{
			name: "missing point",
			spec: HookSpec{
				ID:      "hook-1",
				Handler: handler,
			},
		},
		{
			name: "unsupported point",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPoint("unsupported"),
				Handler: handler,
			},
		},
		{
			name: "missing handler",
			spec: HookSpec{
				ID:    "hook-1",
				Point: HookPointBeforeToolCall,
			},
		},
		{
			name: "unsupported scope",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Scope:   HookScope("external"),
				Handler: handler,
			},
		},
		{
			name: "unsupported source",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Scope:   HookScopeRepo,
				Source:  HookSource("external"),
				Handler: handler,
			},
		},
		{
			name: "unsupported kind",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Kind:    HookKindHTTP,
				Handler: handler,
			},
		},
		{
			name: "unsupported mode",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Mode:    HookMode("async_invalid"),
				Handler: handler,
			},
		},
		{
			name: "user async not allowed",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Scope:   HookScopeUser,
				Mode:    HookModeAsync,
				Handler: handler,
			},
		},
		{
			name: "repo async_rewake not allowed",
			spec: HookSpec{
				ID:      "hook-1",
				Point:   HookPointBeforeToolCall,
				Scope:   HookScopeRepo,
				Mode:    HookModeAsyncRewake,
				Handler: handler,
			},
		},
		{
			name: "invalid failure policy",
			spec: HookSpec{
				ID:            "hook-1",
				Point:         HookPointBeforeToolCall,
				FailurePolicy: FailurePolicy("ignore"),
				Handler:       handler,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.spec.normalizeAndValidate()
			if !errors.Is(err, ErrInvalidHookSpec) {
				t.Fatalf("normalizeAndValidate() error = %v, want ErrInvalidHookSpec", err)
			}
		})
	}
}

func TestHookPointCapabilities(t *testing.T) {
	t.Parallel()

	capability, ok := HookPointCapabilities(HookPointBeforePermissionDecision)
	if !ok {
		t.Fatal("expected before_permission_decision capability to exist")
	}
	if capability.UserAllowed {
		t.Fatal("before_permission_decision should not allow user hooks")
	}
	if !capability.CanBlock {
		t.Fatal("before_permission_decision should allow block")
	}

	capability, ok = HookPointCapabilities(HookPointAfterToolFailure)
	if !ok {
		t.Fatal("expected after_tool_failure capability to exist")
	}
	if capability.CanBlock {
		t.Fatal("after_tool_failure should be observe-only")
	}

	capability, ok = HookPointCapabilities(HookPointBeforeCompletionDecision)
	if !ok {
		t.Fatal("expected before_completion_decision capability to exist")
	}
	if capability.CanBlock {
		t.Fatal("before_completion_decision should be observe-only in current runtime flow")
	}

	if _, exists := HookPointCapabilities(HookPoint("unknown")); exists {
		t.Fatal("unknown hook point should not have capability")
	}
}
