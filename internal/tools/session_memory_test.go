package tools

import (
	"strings"
	"testing"

	"neo-code/internal/security"
)

func TestSessionPermissionMemoryRememberAndResolve(t *testing.T) {
	t.Parallel()

	action := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "webfetch",
			Resource: "webfetch",
		},
	}

	t.Run("once decision is consumed after first resolve", func(t *testing.T) {
		t.Parallel()

		memory := newSessionPermissionMemory()
		if err := memory.remember("session-1", action, SessionPermissionScopeOnce); err != nil {
			t.Fatalf("remember() error = %v", err)
		}

		decision, scope, ok := memory.resolve("session-1", action)
		if !ok || decision != security.DecisionAllow || scope != SessionPermissionScopeOnce {
			t.Fatalf("expected once allow decision, got decision=%q scope=%q ok=%v", decision, scope, ok)
		}

		_, _, ok = memory.resolve("session-1", action)
		if ok {
			t.Fatalf("expected once decision to be consumed")
		}
	})

	t.Run("always decision keeps applying", func(t *testing.T) {
		t.Parallel()

		memory := newSessionPermissionMemory()
		if err := memory.remember("session-2", action, SessionPermissionScopeAlways); err != nil {
			t.Fatalf("remember() error = %v", err)
		}

		for i := 0; i < 2; i++ {
			decision, scope, ok := memory.resolve("session-2", action)
			if !ok || decision != security.DecisionAllow || scope != SessionPermissionScopeAlways {
				t.Fatalf("expected always allow decision, got decision=%q scope=%q ok=%v", decision, scope, ok)
			}
		}
	})

	t.Run("reject decision keeps applying", func(t *testing.T) {
		t.Parallel()

		memory := newSessionPermissionMemory()
		if err := memory.remember("session-3", action, SessionPermissionScopeReject); err != nil {
			t.Fatalf("remember() error = %v", err)
		}

		decision, scope, ok := memory.resolve("session-3", action)
		if !ok || decision != security.DecisionDeny || scope != SessionPermissionScopeReject {
			t.Fatalf("expected reject deny decision, got decision=%q scope=%q ok=%v", decision, scope, ok)
		}
	})
}

func TestSessionPermissionMemoryValidationAndMisses(t *testing.T) {
	t.Parallel()

	memory := newSessionPermissionMemory()
	validAction := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "webfetch",
			Resource: "webfetch",
		},
	}

	if err := memory.remember(" ", validAction, SessionPermissionScopeAlways); err == nil {
		t.Fatalf("expected empty session id error")
	}

	invalidAction := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName: "",
			Resource: "webfetch",
		},
	}
	if err := memory.remember("session", invalidAction, SessionPermissionScopeAlways); err == nil {
		t.Fatalf("expected invalid action error")
	}

	if err := memory.remember("session", validAction, SessionPermissionScope("bad")); err == nil {
		t.Fatalf("expected unsupported scope error")
	}

	if _, _, ok := memory.resolve(" ", validAction); ok {
		t.Fatalf("expected empty session resolve miss")
	}
	if _, _, ok := memory.resolve("missing", validAction); ok {
		t.Fatalf("expected missing session resolve miss")
	}
}

func TestSessionPermissionCategoryAndActionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		action   security.Action
		expected string
	}{
		{
			name: "filesystem read category",
			action: security.Action{
				Type: security.ActionTypeRead,
				Payload: security.ActionPayload{
					Resource: "filesystem_grep",
				},
			},
			expected: "filesystem_read",
		},
		{
			name: "filesystem write category",
			action: security.Action{
				Type: security.ActionTypeWrite,
				Payload: security.ActionPayload{
					Resource: "filesystem_edit",
				},
			},
			expected: "filesystem_write",
		},
		{
			name: "webfetch category",
			action: security.Action{
				Type: security.ActionTypeRead,
				Payload: security.ActionPayload{
					Resource: "webfetch",
				},
			},
			expected: "webfetch",
		},
		{
			name: "bash category",
			action: security.Action{
				Type: security.ActionTypeBash,
			},
			expected: "bash",
		},
		{
			name: "mcp with target",
			action: security.Action{
				Type: security.ActionTypeMCP,
				Payload: security.ActionPayload{
					Target: "Server-A",
				},
			},
			expected: "mcp:server-a",
		},
		{
			name: "mcp without target",
			action: security.Action{
				Type: security.ActionTypeMCP,
			},
			expected: "mcp",
		},
		{
			name: "fallback to tool name",
			action: security.Action{
				Type: security.ActionTypeRead,
				Payload: security.ActionPayload{
					ToolName: "CustomTool",
					Resource: "other_resource",
				},
			},
			expected: "customtool",
		},
		{
			name: "fallback to resource",
			action: security.Action{
				Type: security.ActionTypeRead,
				Payload: security.ActionPayload{
					Resource: "custom_resource",
				},
			},
			expected: "custom_resource",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sessionPermissionCategory(tt.action)
			if got != tt.expected {
				t.Fatalf("sessionPermissionCategory() = %q, want %q", got, tt.expected)
			}

			key := sessionPermissionActionKey(tt.action)
			if !strings.Contains(key, "|"+tt.expected+"|") {
				t.Fatalf("sessionPermissionActionKey() = %q, expected category token %q", key, "|"+tt.expected+"|")
			}
		})
	}
}

func TestSessionPermissionMemoryResolveRequiresTargetScopeMatch(t *testing.T) {
	t.Parallel()

	memory := newSessionPermissionMemory()
	sessionID := "session-target-scope"

	webAction := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: security.TargetTypeURL,
			Target:     "https://docs.github.com/en/rest",
		},
	}
	if err := memory.remember(sessionID, webAction, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("remember web action: %v", err)
	}

	sameHost := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: security.TargetTypeURL,
			Target:     "https://docs.github.com/en/actions",
		},
	}
	if _, _, ok := memory.resolve(sessionID, sameHost); !ok {
		t.Fatalf("expected same host/path scope web action to hit memory")
	}

	differentHost := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: security.TargetTypeURL,
			Target:     "https://example.com/en/actions",
		},
	}
	if _, _, ok := memory.resolve(sessionID, differentHost); ok {
		t.Fatalf("expected different host web action to miss memory")
	}

	fileAction := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: security.TargetTypePath,
			Target:     "src/main.go",
		},
	}
	if err := memory.remember(sessionID, fileAction, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("remember file action: %v", err)
	}

	otherFile := security.Action{
		Type: security.ActionTypeRead,
		Payload: security.ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: security.TargetTypePath,
			Target:     "secrets/secret.key",
		},
	}
	if _, _, ok := memory.resolve(sessionID, otherFile); ok {
		t.Fatalf("expected different path file action to miss memory")
	}
}
