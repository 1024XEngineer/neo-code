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
			name: "bash git category",
			action: security.Action{
				Type: security.ActionTypeBash,
				Payload: security.ActionPayload{
					SemanticType:  "git",
					SemanticClass: BashIntentClassificationRemoteOp,
				},
			},
			expected: "bash_git_remote_op",
		},
		{
			name: "mcp with target",
			action: security.Action{
				Type: security.ActionTypeMCP,
				Payload: security.ActionPayload{
					Target: "mcp.server-a.tool-a",
				},
			},
			expected: "mcp.server-a",
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

func TestSessionPermissionMemoryResolveRequiresMCPToolScopeMatch(t *testing.T) {
	t.Parallel()

	memory := newSessionPermissionMemory()
	sessionID := "session-mcp-tool-scope"

	createIssue := security.Action{
		Type: security.ActionTypeMCP,
		Payload: security.ActionPayload{
			ToolName:   "mcp.github.create_issue",
			Resource:   "mcp.github.create_issue",
			TargetType: security.TargetTypeMCP,
			Target:     "mcp.github.create_issue",
		},
	}
	if err := memory.remember(sessionID, createIssue, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("remember create_issue action: %v", err)
	}

	sameTool := security.Action{
		Type: security.ActionTypeMCP,
		Payload: security.ActionPayload{
			ToolName:   "mcp.github.create_issue",
			Resource:   "mcp.github.create_issue",
			TargetType: security.TargetTypeMCP,
			Target:     "mcp.github.create_issue",
		},
	}
	if _, _, ok := memory.resolve(sessionID, sameTool); !ok {
		t.Fatalf("expected same MCP tool identity to hit memory")
	}

	otherToolSameServer := security.Action{
		Type: security.ActionTypeMCP,
		Payload: security.ActionPayload{
			ToolName:   "mcp.github.list_issues",
			Resource:   "mcp.github.list_issues",
			TargetType: security.TargetTypeMCP,
			Target:     "mcp.github.list_issues",
		},
	}
	if _, _, ok := memory.resolve(sessionID, otherToolSameServer); ok {
		t.Fatalf("expected other MCP tool on same server to miss memory")
	}
}

func TestSessionPermissionMemoryResolveMatchesNormalizedCommandScope(t *testing.T) {
	t.Parallel()

	memory := newSessionPermissionMemory()
	sessionID := "session-bash-command-scope"

	remembered := security.Action{
		Type: security.ActionTypeBash,
		Payload: security.ActionPayload{
			ToolName:              "bash",
			Resource:              "bash",
			TargetType:            security.TargetTypeCommand,
			Target:                "Get-ChildItem   -Force\r\n|   Select-String    'TODO'",
			PermissionFingerprint: "bash.command.get-childitem -force | select-string 'todo'",
		},
	}
	if err := memory.remember(sessionID, remembered, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("remember bash action: %v", err)
	}

	normalizedEquivalent := security.Action{
		Type: security.ActionTypeBash,
		Payload: security.ActionPayload{
			ToolName:              "bash",
			Resource:              "bash",
			TargetType:            security.TargetTypeCommand,
			Target:                "Get-ChildItem -Force | Select-String 'TODO'",
			PermissionFingerprint: "bash.command.get-childitem -force | select-string 'todo'",
		},
	}
	if _, _, ok := memory.resolve(sessionID, normalizedEquivalent); !ok {
		t.Fatalf("expected normalized-equivalent command to hit session memory")
	}
}

func TestSessionPermissionMemorySkipsRemoteGitRemember(t *testing.T) {
	t.Parallel()

	memory := newSessionPermissionMemory()
	sessionID := "session-git-remote-no-cache"
	remoteAction := security.Action{
		Type: security.ActionTypeBash,
		Payload: security.ActionPayload{
			ToolName:              "bash",
			Resource:              "bash_git_remote_op",
			TargetType:            security.TargetTypeCommand,
			Target:                "git push origin main",
			SemanticType:          "git",
			SemanticClass:         BashIntentClassificationRemoteOp,
			PermissionFingerprint: "bash.git|remote_op|push|a=origin,main",
		},
	}

	if err := memory.remember(sessionID, remoteAction, SessionPermissionScopeAlways); err != nil {
		t.Fatalf("remember remote action: %v", err)
	}
	if _, _, ok := memory.resolve(sessionID, remoteAction); ok {
		t.Fatalf("expected remote git action not to be cached")
	}
}

func TestSessionPermissionMemoryAllowsRemoteGitRememberForNonAlwaysScopes(t *testing.T) {
	t.Parallel()

	remoteAction := security.Action{
		Type: security.ActionTypeBash,
		Payload: security.ActionPayload{
			ToolName:              "bash",
			Resource:              "bash_git_remote_op",
			TargetType:            security.TargetTypeCommand,
			Target:                "git push origin main",
			SemanticType:          "git",
			SemanticClass:         BashIntentClassificationRemoteOp,
			PermissionFingerprint: "bash.git|remote_op|push|a=origin,main",
		},
	}

	tests := []struct {
		name      string
		sessionID string
		scope     SessionPermissionScope
		validate  func(t *testing.T, memory *sessionPermissionMemory)
	}{
		{
			name:      "once can be remembered for remote git",
			sessionID: "session-git-remote-once",
			scope:     SessionPermissionScopeOnce,
			validate: func(t *testing.T, memory *sessionPermissionMemory) {
				t.Helper()
				if _, _, ok := memory.resolve("session-git-remote-once", remoteAction); !ok {
					t.Fatalf("expected remote git once decision to be cached")
				}
				if _, _, ok := memory.resolve("session-git-remote-once", remoteAction); ok {
					t.Fatalf("expected remote git once decision to be consumed")
				}
			},
		},
		{
			name:      "reject can be remembered for remote git",
			sessionID: "session-git-remote-reject",
			scope:     SessionPermissionScopeReject,
			validate: func(t *testing.T, memory *sessionPermissionMemory) {
				t.Helper()
				decision, scope, ok := memory.resolve("session-git-remote-reject", remoteAction)
				if !ok || decision != security.DecisionDeny || scope != SessionPermissionScopeReject {
					t.Fatalf("expected remote git reject decision to be cached, got decision=%q scope=%q ok=%v", decision, scope, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			memory := newSessionPermissionMemory()
			if err := memory.remember(tt.sessionID, remoteAction, tt.scope); err != nil {
				t.Fatalf("remember remote action: %v", err)
			}
			tt.validate(t, memory)
		})
	}
}

func TestNormalizePermissionCommandTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty returns wildcard",
			raw:  " \n\t ",
			want: "*",
		},
		{
			name: "normalizes line endings and spaces",
			raw:  "Get-ChildItem   -Force\r\n|   Select-String    'TODO'\r",
			want: "get-childitem -force | select-string 'todo'",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizePermissionCommandTarget(tt.raw); got != tt.want {
				t.Fatalf("normalizePermissionCommandTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}
