package security

import (
	"context"
	"testing"
)

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
			name: "bash always ask",
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
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, checkErr := engine.Check(context.Background(), tt.action)
			if checkErr != nil {
				t.Fatalf("Check() error = %v", checkErr)
			}
			if result.Decision != tt.wantDecision {
				t.Fatalf("expected decision %q, got %q", tt.wantDecision, result.Decision)
			}
			if tt.wantRuleID == "" {
				if result.Rule != nil {
					t.Fatalf("expected no matched rule, got %+v", result.Rule)
				}
				return
			}
			if result.Rule == nil || result.Rule.ID != tt.wantRuleID {
				t.Fatalf("expected rule id %q, got %+v", tt.wantRuleID, result.Rule)
			}
		})
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
