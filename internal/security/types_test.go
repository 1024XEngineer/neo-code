package security

import "testing"

func TestActionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		action  Action
		wantErr string
	}{
		{
			name: "valid action",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName: "filesystem_read_file",
					Resource: "filesystem_read_file",
				},
			},
		},
		{
			name: "missing resource",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName: "filesystem_read_file",
				},
			},
			wantErr: "resource is empty",
		},
		{
			name: "missing tool name",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					Resource: "filesystem_read_file",
				},
			},
			wantErr: "tool_name is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.action.Validate()
			if tt.wantErr != "" {
				if err == nil || err.Error() == "" || !containsText(err, tt.wantErr) {
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

func TestRuleValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    Rule
		wantErr string
	}{
		{
			name: "valid rule",
			rule: Rule{
				Type:     ActionTypeBash,
				Resource: "bash",
				Decision: DecisionDeny,
			},
		},
		{
			name: "invalid decision",
			rule: Rule{
				Resource: "bash",
				Decision: Decision("maybe"),
			},
			wantErr: "invalid decision",
		},
		{
			name: "invalid action type",
			rule: Rule{
				Type:     ActionType("custom"),
				Resource: "bash",
				Decision: DecisionAllow,
			},
			wantErr: "invalid action type",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.rule.Validate()
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
