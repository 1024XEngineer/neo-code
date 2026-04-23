package security

import (
	"context"
	"strings"
	"testing"
	"time"
)

func assertPolicyCheckResult(t *testing.T, engine *PolicyEngine, action Action, wantDecision Decision, wantRuleID string, wantReason string) {
	t.Helper()

	result, checkErr := engine.Check(context.Background(), action)
	if checkErr != nil {
		t.Fatalf("Check() error = %v", checkErr)
	}
	if result.Decision != wantDecision {
		t.Fatalf("expected decision %q, got %q", wantDecision, result.Decision)
	}
	if wantRuleID == "" {
		if result.Rule != nil {
			t.Fatalf("expected no matched rule, got %+v", result.Rule)
		}
		return
	}
	if result.Rule == nil || result.Rule.ID != wantRuleID {
		t.Fatalf("expected rule id %q, got %+v", wantRuleID, result.Rule)
	}
	if wantReason != "" && result.Reason != wantReason {
		t.Fatalf("expected reason %q, got %q", wantReason, result.Reason)
	}
}

func TestPolicyEngineRecommendedRules(t *testing.T) {
	t.Parallel()

	engine, err := NewRecommendedPolicyEngine()
	if err != nil {
		t.Fatalf("new recommended engine: %v", err)
	}

	tests := []struct {
		name         string
		action       Action
		wantDecision Decision
		wantRuleID   string
	}{
		{
			name: "git read-only bash allow",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_status",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git status",
					TargetType:            TargetTypeCommand,
					Target:                "git status --short --branch",
					PermissionFingerprint: "bash.git|read_only|status",
				},
			},
			wantDecision: DecisionAllow,
			wantRuleID:   "allow-bash-git-read-only",
		},
		{
			name: "git read-only sensitive path ask",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_show",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git show",
					TargetType:            TargetTypeCommand,
					Target:                "git show HEAD:.env.production",
					PermissionFingerprint: "bash.git|read_only|show",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-bash-git-read-only-sensitive",
		},
		{
			name: "git read-only private key deny",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_show",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git show",
					TargetType:            TargetTypeCommand,
					Target:                "git show HEAD:.ssh/id_rsa",
					PermissionFingerprint: "bash.git|read_only|show",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-bash-git-read-only-private-keys",
		},
		{
			name: "git read-only id_dsa deny even when non-sensitive keyword missing",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_show",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git show",
					TargetType:            TargetTypeCommand,
					Target:                "git show HEAD:keys/id_dsa",
					PermissionFingerprint: "bash.git|read_only|show",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-bash-git-read-only-private-keys",
		},
		{
			name: "git read-only p12 deny even when path is not generally sensitive",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_show",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git show",
					TargetType:            TargetTypeCommand,
					Target:                "git show HEAD:certs/client.p12",
					PermissionFingerprint: "bash.git|read_only|show",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-bash-git-read-only-private-keys",
		},
		{
			name: "git read-only quoted private key deny",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:              "bash",
					Resource:              "bash_git_read_only",
					Operation:             "git_show",
					SemanticType:          "git",
					SemanticClass:         "read_only",
					NormalizedIntent:      "git show",
					TargetType:            TargetTypeCommand,
					Target:                "git show 'HEAD:.ssh/id_rsa'",
					PermissionFingerprint: "bash.git|read_only|show",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-bash-git-read-only-private-keys",
		},
		{
			name: "git remote bash ask",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:      "bash",
					Resource:      "bash_git_remote_op",
					Operation:     "git_push",
					SemanticType:  "git",
					SemanticClass: "remote_op",
					TargetType:    TargetTypeCommand,
					Target:        "git push origin main",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-bash-git-remote-op",
		},
		{
			name: "git destructive bash ask",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:      "bash",
					Resource:      "bash_git_destructive",
					Operation:     "git_reset",
					SemanticType:  "git",
					SemanticClass: "destructive",
					TargetType:    TargetTypeCommand,
					Target:        "git reset --hard HEAD~1",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-bash-git-destructive",
		},
		{
			name: "bash fallback ask",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:   "bash",
					Resource:   "bash",
					Operation:  "command",
					TargetType: TargetTypeCommand,
					Target:     "ls -la",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-all-bash",
		},
		{
			name: "filesystem write ask",
			action: Action{
				Type: ActionTypeWrite,
				Payload: ActionPayload{
					ToolName:   "filesystem_write_file",
					Resource:   "filesystem_write_file",
					Operation:  "write_file",
					TargetType: TargetTypePath,
					Target:     "README.md",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-filesystem-write",
		},
		{
			name: "filesystem read sensitive path ask",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "filesystem_read_file",
					Resource:   "filesystem_read_file",
					Operation:  "read_file",
					TargetType: TargetTypePath,
					Target:     ".env.production",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-sensitive-filesystem-read",
		},
		{
			name: "filesystem read private key deny",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "filesystem_read_file",
					Resource:   "filesystem_read_file",
					Operation:  "read_file",
					TargetType: TargetTypePath,
					Target:     "C:/Users/test/.ssh/id_rsa",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-sensitive-private-keys",
		},
		{
			name: "filesystem read normal source allow",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "filesystem_read_file",
					Resource:   "filesystem_read_file",
					Operation:  "read_file",
					TargetType: TargetTypePath,
					Target:     "internal/runtime/runtime.go",
				},
			},
			wantDecision: DecisionAllow,
			wantRuleID:   "",
		},
		{
			name: "webfetch whitelist allow",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "webfetch",
					Resource:   "webfetch",
					Operation:  "fetch",
					TargetType: TargetTypeURL,
					Target:     "https://github.com/1024XEngineer/neo-code",
				},
			},
			wantDecision: DecisionAllow,
			wantRuleID:   "allow-webfetch-whitelist",
		},
		{
			name: "webfetch non-whitelist ask",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "webfetch",
					Resource:   "webfetch",
					Operation:  "fetch",
					TargetType: TargetTypeURL,
					Target:     "https://example.com",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-webfetch-non-whitelist",
		},
		{
			name: "webfetch docs wildcard host is not implicitly trusted",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "webfetch",
					Resource:   "webfetch",
					Operation:  "fetch",
					TargetType: TargetTypeURL,
					Target:     "https://docs.attacker.com",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-webfetch-non-whitelist",
		},
		{
			name: "mcp defaults to ask",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.docs.search",
					Resource:   "mcp.docs.search",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "mcp.docs.search",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-all-mcp",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPolicyCheckResult(t, engine, tt.action, tt.wantDecision, tt.wantRuleID, "")
		})
	}
}

func TestClassifySensitiveGitReadOnlyCommandPathspecVariants(t *testing.T) {
	t.Parallel()

	if !classifySensitiveGitReadOnlyCommand(`git show "HEAD:.env.production"`) {
		t.Fatalf("expected quoted HEAD:path env selector to be sensitive")
	}
	if !classifySensitiveGitReadOnlyCommand(`git diff -- ":(glob)**/.env"`) {
		t.Fatalf("expected pathspec magic env selector to be sensitive")
	}
	if classifySensitiveGitReadOnlyCommand(`git status --short`) {
		t.Fatalf("expected plain git status to be non-sensitive")
	}
}

func TestClassifyPrivateKeyGitReadOnlyCommandVariants(t *testing.T) {
	t.Parallel()

	if !classifyPrivateKeyGitReadOnlyCommand(`git show 'HEAD:.ssh/id_rsa'`) {
		t.Fatalf("expected quoted private key selector to be detected")
	}
	if !classifyPrivateKeyGitReadOnlyCommand(`git show HEAD:secrets/deploy.pem`) {
		t.Fatalf("expected pem selector to be detected as private key")
	}
	if classifyPrivateKeyGitReadOnlyCommand(`git show HEAD:README.md`) {
		t.Fatalf("expected regular file selector to be non-private-key")
	}
}

func TestNewPolicyEngineValidation(t *testing.T) {
	t.Parallel()

	_, err := NewPolicyEngine(Decision("invalid"), nil)
	if err == nil {
		t.Fatalf("expected invalid default decision error")
	}

	_, err = NewPolicyEngine(DecisionAllow, []PolicyRule{
		{ID: "", Decision: DecisionAsk},
	})
	if err == nil {
		t.Fatalf("expected missing rule id error")
	}

	_, err = NewPolicyEngine(DecisionAllow, []PolicyRule{
		{ID: "r1", Decision: Decision("invalid")},
	})
	if err == nil {
		t.Fatalf("expected invalid rule decision error")
	}
}

func TestPolicyEngineMCPRuleTemplates(t *testing.T) {
	t.Parallel()

	engine, err := NewPolicyEngine(DecisionAllow, []PolicyRule{
		newMCPToolPolicyRule("allow-github-create-issue", DecisionAllow, "github", "create_issue", "tool allowed"),
		newMCPServerPolicyRule("deny-github-server", DecisionDeny, "github", "server blocked"),
		newMCPToolPolicyRule("ask-docs-search", DecisionAsk, "docs", "search", "search requires approval"),
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
	}

	tests := []struct {
		name         string
		action       Action
		wantDecision Decision
		wantRuleID   string
		wantReason   string
	}{
		{
			name: "server-level deny overrides tool-level allow",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.github.create_issue",
					Resource:   "mcp.github.create_issue",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "mcp.github.create_issue",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-github-server",
			wantReason:   "server blocked",
		},
		{
			name: "server-level deny covers all tools on same server",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.github.list_issues",
					Resource:   "mcp.github.list_issues",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "mcp.github.list_issues",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-github-server",
			wantReason:   "server blocked",
		},
		{
			name: "tool-level ask hits exact tool identity",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.docs.search",
					Resource:   "mcp.docs.search",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "mcp.docs.search",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-docs-search",
			wantReason:   "search requires approval",
		},
		{
			name: "other MCP tool falls back to default allow",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.docs.read",
					Resource:   "mcp.docs.read",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "mcp.docs.read",
				},
			},
			wantDecision: DecisionAllow,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPolicyCheckResult(t, engine, tt.action, tt.wantDecision, tt.wantRuleID, tt.wantReason)
		})
	}
}

func TestCanonicalMCPServerIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "extract from full tool identity with dotted server",
			input: "mcp.github.enterprise.create_issue",
			want:  "mcp.github.enterprise",
		},
		{
			name:  "normalize raw server id with dot",
			input: "github.enterprise",
			want:  "mcp.github.enterprise",
		},
		{
			name:  "extract from normal tool identity",
			input: "mcp.github.search",
			want:  "mcp.github",
		},
		{
			name:  "invalid mcp token returns empty",
			input: "mcp",
			want:  "",
		},
		{
			name:  "public wrapper follows canonical behavior",
			input: "mcp.github.public.search",
			want:  "mcp.github.public",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := canonicalMCPServerIdentity(tt.input); got != tt.want {
				t.Fatalf("canonicalMCPServerIdentity() = %q, want %q", got, tt.want)
			}
			if got := CanonicalMCPServerIdentity(tt.input); got != tt.want {
				t.Fatalf("CanonicalMCPServerIdentity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPolicyEngineMCPDottedServerIsolation(t *testing.T) {
	t.Parallel()

	engine, err := NewPolicyEngine(DecisionAllow, []PolicyRule{
		newMCPServerPolicyRule("deny-github-enterprise", DecisionDeny, "github.enterprise", "enterprise denied"),
		newMCPToolPolicyRule("allow-github-public-search", DecisionAllow, "github.public", "search", "public allowed"),
	})
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
	}

	enterpriseAction := Action{
		Type: ActionTypeMCP,
		Payload: ActionPayload{
			ToolName:   "mcp.github.enterprise.search",
			Resource:   "mcp.github.enterprise.search",
			Operation:  "invoke",
			TargetType: TargetTypeMCP,
			Target:     "mcp.github.enterprise.search",
		},
	}
	enterpriseResult, checkErr := engine.Check(context.Background(), enterpriseAction)
	if checkErr != nil {
		t.Fatalf("check enterprise action: %v", checkErr)
	}
	if enterpriseResult.Decision != DecisionDeny {
		t.Fatalf("expected enterprise action deny, got %q", enterpriseResult.Decision)
	}
	if enterpriseResult.Rule == nil || enterpriseResult.Rule.ID != "deny-github-enterprise" {
		t.Fatalf("expected enterprise deny rule, got %+v", enterpriseResult.Rule)
	}

	publicAction := Action{
		Type: ActionTypeMCP,
		Payload: ActionPayload{
			ToolName:   "mcp.github.public.search",
			Resource:   "mcp.github.public.search",
			Operation:  "invoke",
			TargetType: TargetTypeMCP,
			Target:     "mcp.github.public.search",
		},
	}
	publicResult, checkErr := engine.Check(context.Background(), publicAction)
	if checkErr != nil {
		t.Fatalf("check public action: %v", checkErr)
	}
	if publicResult.Decision != DecisionAllow {
		t.Fatalf("expected public action allow, got %q", publicResult.Decision)
	}
	if publicResult.Rule == nil || publicResult.Rule.ID != "allow-github-public-search" {
		t.Fatalf("expected public allow rule, got %+v", publicResult.Rule)
	}
}

func TestMCPPolicyPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision Decision
		want     int
	}{
		{name: "deny", decision: DecisionDeny, want: 830},
		{name: "ask", decision: DecisionAsk, want: 820},
		{name: "allow", decision: DecisionAllow, want: 810},
		{name: "unknown", decision: Decision("unknown"), want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mcpPolicyPriority(tt.decision); got != tt.want {
				t.Fatalf("mcpPolicyPriority() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMCPServerIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name: "non mcp action returns empty",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					Target: "mcp.docs.search",
				},
			},
			want: "",
		},
		{
			name: "use target first",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					Target: "mcp.docs.search",
				},
			},
			want: "mcp.docs",
		},
		{
			name: "fallback to resource when target invalid",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					Target:   "mcp.",
					Resource: "mcp.repo.search",
				},
			},
			want: "mcp.repo",
		},
		{
			name: "fallback to tool name when target and resource invalid",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					Target:   "mcp.",
					Resource: "mcp.",
					ToolName: "mcp.docs.read",
				},
			},
			want: "mcp.docs",
		},
		{
			name: "all empty returns empty",
			action: Action{
				Type: ActionTypeMCP,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mcpServerIdentity(tt.action); got != tt.want {
				t.Fatalf("mcpServerIdentity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalMCPServerIdentityEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single segment mcp identity", input: "mcp.docs", want: "mcp.docs"},
		{name: "leading dot in body is invalid", input: "mcp..search", want: ""},
		{name: "trailing dot in body is invalid", input: "mcp.docs.", want: ""},
		{name: "empty mcp identity body is invalid", input: "mcp.", want: ""},
		{name: "trim and lowercase", input: "  MCP.DOCS.SEARCH  ", want: "mcp.docs"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalMCPServerIdentity(tt.input); got != tt.want {
				t.Fatalf("canonicalMCPServerIdentity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCanonicalMCPToolIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		serverID string
		toolName string
		want     string
	}{
		{name: "normalize to full tool identity", serverID: "docs", toolName: "search", want: "mcp.docs.search"},
		{name: "invalid server id returns empty", serverID: "mcp.", toolName: "search", want: ""},
		{name: "empty tool returns empty", serverID: "docs", toolName: " ", want: ""},
		{name: "dotted tool name rejected", serverID: "github", toolName: "enterprise.create_issue", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalMCPToolIdentity(tt.serverID, tt.toolName); got != tt.want {
				t.Fatalf("canonicalMCPToolIdentity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveToolCategoryMCP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name: "derive mcp server category",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					Target: "mcp.docs.search",
				},
			},
			want: "mcp.docs",
		},
		{
			name: "fallback to mcp category when no identity",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					Target: "mcp.",
				},
			},
			want: "mcp",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveToolCategory(tt.action); got != tt.want {
				t.Fatalf("deriveToolCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPolicyEngineCheckCapabilityTokenDeny(t *testing.T) {
	t.Parallel()

	engine, err := NewPolicyEngine(DecisionAllow, nil)
	if err != nil {
		t.Fatalf("new policy engine: %v", err)
	}

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "token-policy",
		TaskID:          "task-policy",
		AgentID:         "agent-policy",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionWorkspace,
	}

	result, err := engine.Check(context.Background(), Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:        "webfetch",
			Resource:        "webfetch",
			TargetType:      TargetTypeURL,
			Target:          "https://example.com",
			CapabilityToken: &token,
		},
	})
	if err != nil {
		t.Fatalf("policy check: %v", err)
	}
	if result.Decision != DecisionDeny {
		t.Fatalf("expected deny decision, got %q", result.Decision)
	}
	if result.Rule == nil || result.Rule.ID != CapabilityRuleID {
		t.Fatalf("expected capability rule id, got %+v", result.Rule)
	}
	if !strings.Contains(strings.ToLower(result.Reason), "tool not allowed") {
		t.Fatalf("expected tool-not-allowed reason, got %q", result.Reason)
	}
}
