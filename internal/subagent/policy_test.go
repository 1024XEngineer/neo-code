package subagent

import "testing"

func TestDefaultRolePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role Role
	}{
		{name: "researcher", role: RoleResearcher},
		{name: "coder", role: RoleCoder},
		{name: "reviewer", role: RoleReviewer},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy, err := DefaultRolePolicy(tt.role)
			if err != nil {
				t.Fatalf("DefaultRolePolicy() error = %v", err)
			}
			if policy.Role != tt.role {
				t.Fatalf("policy role = %q, want %q", policy.Role, tt.role)
			}
			if policy.DefaultBudget.MaxSteps <= 0 {
				t.Fatalf("invalid max steps: %d", policy.DefaultBudget.MaxSteps)
			}
			if len(policy.AllowedTools) == 0 {
				t.Fatalf("expected non-empty allowed tools")
			}
			if len(policy.RequiredSections) == 0 {
				t.Fatalf("expected non-empty required sections")
			}
			if err := policy.Validate(); err != nil {
				t.Fatalf("policy.Validate() error = %v", err)
			}
		})
	}
}

func TestDefaultRolePolicyInvalidRole(t *testing.T) {
	t.Parallel()

	if _, err := DefaultRolePolicy(Role("unknown")); err == nil {
		t.Fatalf("expected invalid role error")
	}
}

func TestRolePolicyValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  RolePolicy
		wantErr bool
	}{
		{
			name: "valid",
			policy: RolePolicy{
				Role:             RoleResearcher,
				SystemPrompt:     "prompt",
				AllowedTools:     []string{"filesystem_grep"},
				RequiredSections: []string{"summary"},
			},
		},
		{
			name: "empty prompt",
			policy: RolePolicy{
				Role:             RoleResearcher,
				AllowedTools:     []string{"filesystem_grep"},
				RequiredSections: []string{"summary"},
			},
			wantErr: true,
		},
		{
			name: "empty tools",
			policy: RolePolicy{
				Role:             RoleResearcher,
				SystemPrompt:     "prompt",
				RequiredSections: []string{"summary"},
			},
			wantErr: true,
		},
		{
			name: "invalid role",
			policy: RolePolicy{
				Role:             Role("x"),
				SystemPrompt:     "prompt",
				AllowedTools:     []string{"filesystem_grep"},
				RequiredSections: []string{"summary"},
			},
			wantErr: true,
		},
		{
			name: "unsupported required section",
			policy: RolePolicy{
				Role:             RoleResearcher,
				SystemPrompt:     "prompt",
				AllowedTools:     []string{"filesystem_grep"},
				RequiredSections: []string{"unknown_section"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.policy.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
