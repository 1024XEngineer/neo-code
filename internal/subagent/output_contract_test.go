package subagent

import "testing"

func TestValidateOutputContractRequiresAllDeclaredSections(t *testing.T) {
	t.Parallel()

	policy := RolePolicy{
		Role:             RoleCoder,
		SystemPrompt:     "prompt",
		AllowedTools:     []string{"bash"},
		RequiredSections: []string{"summary", "findings", "patches", "risks", "next_actions", "artifacts"},
	}

	full := Output{
		Summary:     "ok",
		Findings:    []string{"f"},
		Patches:     []string{"p"},
		Risks:       []string{"r"},
		NextActions: []string{"n"},
		Artifacts:   []string{"a"},
	}
	if err := validateOutputContract(policy, full); err != nil {
		t.Fatalf("validateOutputContract() error = %v", err)
	}

	cases := []struct {
		name   string
		output Output
	}{
		{name: "missing summary", output: Output{
			Findings: []string{"f"}, Patches: []string{"p"}, Risks: []string{"r"}, NextActions: []string{"n"}, Artifacts: []string{"a"},
		}},
		{name: "missing findings", output: Output{
			Summary: "ok", Patches: []string{"p"}, Risks: []string{"r"}, NextActions: []string{"n"}, Artifacts: []string{"a"},
		}},
		{name: "missing patches", output: Output{
			Summary: "ok", Findings: []string{"f"}, Risks: []string{"r"}, NextActions: []string{"n"}, Artifacts: []string{"a"},
		}},
		{name: "missing risks", output: Output{
			Summary: "ok", Findings: []string{"f"}, Patches: []string{"p"}, NextActions: []string{"n"}, Artifacts: []string{"a"},
		}},
		{name: "missing next actions", output: Output{
			Summary: "ok", Findings: []string{"f"}, Patches: []string{"p"}, Risks: []string{"r"}, Artifacts: []string{"a"},
		}},
		{name: "missing artifacts", output: Output{
			Summary: "ok", Findings: []string{"f"}, Patches: []string{"p"}, Risks: []string{"r"}, NextActions: []string{"n"},
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := validateOutputContract(policy, tc.output); err == nil {
				t.Fatalf("expected contract validation error")
			}
		})
	}
}

func TestValidateOutputContractUnsupportedSection(t *testing.T) {
	t.Parallel()

	policy := RolePolicy{
		Role:             RoleResearcher,
		SystemPrompt:     "prompt",
		AllowedTools:     []string{"filesystem_grep"},
		RequiredSections: []string{"summary", "x"},
	}
	if err := validateOutputContract(policy, Output{Summary: "ok"}); err == nil {
		t.Fatalf("expected unsupported section error")
	}
}
