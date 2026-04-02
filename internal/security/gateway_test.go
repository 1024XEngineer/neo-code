package security

import (
	"context"
	"strings"
	"testing"
)

func TestNewStaticGateway(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		defaultDecision Decision
		rules           []Rule
		wantErr         string
	}{
		{
			name:            "default allow with no rules",
			defaultDecision: DecisionAllow,
		},
		{
			name:            "empty default falls back to allow",
			defaultDecision: "",
		},
		{
			name:            "rejects invalid default decision",
			defaultDecision: Decision("block"),
			wantErr:         "invalid decision",
		},
		{
			name:            "rejects invalid rule action",
			defaultDecision: DecisionAllow,
			rules: []Rule{
				{Resource: "bash", Type: ActionType("boom"), Decision: DecisionDeny},
			},
			wantErr: "invalid action type",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewStaticGateway(tt.defaultDecision, tt.rules)
			if tt.wantErr != "" {
				if err == nil || !containsText(err, tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStaticGatewayCheck(t *testing.T) {
	t.Parallel()

	gateway, err := NewStaticGateway(DecisionAllow, []Rule{
		{ID: "deny-bash", Resource: "bash", Type: ActionTypeBash, Decision: DecisionDeny, Reason: "shell is blocked"},
		{ID: "ask-private", Resource: "webfetch", Type: ActionTypeRead, TargetPrefix: "http://127.", Decision: DecisionAsk, Reason: "requires approval"},
		{ID: "deny-all-writes", Type: ActionTypeWrite, Decision: DecisionDeny, Reason: "writes disabled"},
		{ID: "ask-mcp-tool", Resource: "mcp.github.create_issue", Type: ActionTypeMCP, Decision: DecisionAsk, Reason: "mcp approval"},
	})
	if err != nil {
		t.Fatalf("new gateway: %v", err)
	}

	tests := []struct {
		name         string
		ctx          func() context.Context
		action       Action
		wantDecision Decision
		wantRuleID   string
		wantReason   string
		wantErr      string
	}{
		{
			name: "default allow",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "filesystem_read_file",
					Resource:   "filesystem_read_file",
					Operation:  "read_file",
					TargetType: TargetTypePath,
					Target:     "main.go",
				},
			},
			wantDecision: DecisionAllow,
		},
		{
			name: "matched deny by tool and action",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName:   "bash",
					Resource:   "bash",
					Operation:  "command",
					TargetType: TargetTypeCommand,
					Target:     "rm -rf .",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-bash",
			wantReason:   "shell is blocked",
		},
		{
			name: "matched ask by target prefix",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "webfetch",
					Resource:   "webfetch",
					Operation:  "fetch",
					TargetType: TargetTypeURL,
					Target:     "http://127.0.0.1:8080",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-private",
			wantReason:   "requires approval",
		},
		{
			name: "matched wildcard action rule",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeWrite,
				Payload: ActionPayload{
					ToolName:   "filesystem_write_file",
					Resource:   "filesystem_write_file",
					Operation:  "write_file",
					TargetType: TargetTypePath,
					Target:     "notes.txt",
				},
			},
			wantDecision: DecisionDeny,
			wantRuleID:   "deny-all-writes",
			wantReason:   "writes disabled",
		},
		{
			name: "matched mcp resource rule",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.github.create_issue",
					Resource:   "mcp.github.create_issue",
					Operation:  "invoke",
					TargetType: TargetTypeMCP,
					Target:     "github",
				},
			},
			wantDecision: DecisionAsk,
			wantRuleID:   "ask-mcp-tool",
			wantReason:   "mcp approval",
		},
		{
			name: "invalid action type",
			ctx:  context.Background,
			action: Action{
				Type: ActionType("invalid"),
				Payload: ActionPayload{
					ToolName: "bash",
					Resource: "bash",
				},
			},
			wantErr: "invalid action type",
		},
		{
			name: "empty payload tool name",
			ctx:  context.Background,
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					Resource: "filesystem_read_file",
				},
			},
			wantErr: "tool_name is empty",
		},
		{
			name: "context canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					ToolName: "bash",
					Resource: "bash",
				},
			},
			wantErr: context.Canceled.Error(),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := gateway.Check(tt.ctx(), tt.action)
			if tt.wantErr != "" {
				if err == nil || !containsText(err, tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Decision != tt.wantDecision {
				t.Fatalf("expected decision %q, got %q", tt.wantDecision, result.Decision)
			}
			if tt.wantRuleID == "" {
				if result.Rule != nil {
					t.Fatalf("expected no matched rule, got %+v", result.Rule)
				}
			} else {
				if result.Rule == nil || result.Rule.ID != tt.wantRuleID {
					t.Fatalf("expected rule id %q, got %+v", tt.wantRuleID, result.Rule)
				}
			}
			if result.Reason != tt.wantReason {
				t.Fatalf("expected reason %q, got %q", tt.wantReason, result.Reason)
			}
		})
	}
}

func containsText(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
