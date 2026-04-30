package promptasset

import (
	"strings"
	"testing"
)

func TestCoreSections(t *testing.T) {
	t.Parallel()

	wantTitles := []string{
		"Agent Identity",
		"Tool Usage",
		"Failure Recovery",
		"Response Style",
		"Security Boundaries",
		"Context Management",
	}

	sections := CoreSections()
	if len(sections) != len(wantTitles) {
		t.Fatalf("expected %d core sections, got %d", len(wantTitles), len(sections))
	}

	for index, want := range wantTitles {
		if sections[index].Title != want {
			t.Fatalf("section %d title = %q, want %q", index, sections[index].Title, want)
		}
		if strings.TrimSpace(sections[index].Content) == "" {
			t.Fatalf("section %q content should not be empty", want)
		}
	}
}

func TestCorePromptContainsOperationalGuidance(t *testing.T) {
	t.Parallel()

	prompt := joinCoreSectionContent()
	wantSubstrings := []string{
		"## Instruction priority",
		"`completion_gate`",
		"`verification_gate`",
		"`acceptance_decision`",
		"MCP tools may appear dynamically as `mcp.<server>.<tool>`",
		"Required todos are acceptance-relevant",
		"set verification intent",
		"A subagent is a helper, not the source of final truth",
		"Preserve existing user or repository changes",
		"Use UTF-8-safe reads and edits",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected core prompt to contain %q", want)
		}
	}
}

func TestRuntimeReminderTemplates(t *testing.T) {
	t.Parallel()

	if !strings.Contains(NoProgressReminder(), "multiple consecutive attempts") {
		t.Fatalf("expected no-progress reminder guidance, got %q", NoProgressReminder())
	}
	if !strings.Contains(RepeatCycleReminder(), "exact same arguments") {
		t.Fatalf("expected repeat-cycle reminder guidance, got %q", RepeatCycleReminder())
	}
}

func TestPlanModePromptTemplates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stage string
		want  string
	}{
		{name: "plan", stage: "plan", want: "planning stage"},
		{name: "build execute", stage: "build_execute", want: "build execution"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prompt := PlanModePrompt(tt.stage)
			if strings.TrimSpace(prompt) == "" {
				t.Fatalf("PlanModePrompt(%q) should not be empty", tt.stage)
			}
			if !strings.Contains(prompt, tt.want) {
				t.Fatalf("PlanModePrompt(%q) = %q, want substring %q", tt.stage, prompt, tt.want)
			}
		})
	}

	if got := PlanModePrompt("unknown"); got != "" {
		t.Fatalf("PlanModePrompt(unknown) = %q, want empty", got)
	}
}

func joinCoreSectionContent() string {
	sections := CoreSections()
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, section.Content)
	}
	return strings.Join(parts, "\n\n")
}

func TestCompactSystemPromptInterpolatesPlaceholders(t *testing.T) {
	t.Parallel()

	prompt := CompactSystemPrompt(`{"task_state":{}}`, "[compact_summary]\ndone:\n- none")
	if strings.Contains(prompt, compactTaskStateContractPlaceholder) {
		t.Fatalf("expected task state placeholder to be replaced, got %q", prompt)
	}
	if strings.Contains(prompt, compactSummaryFormatTemplatePlaceholder) {
		t.Fatalf("expected summary format placeholder to be replaced, got %q", prompt)
	}
	if !strings.Contains(prompt, `{"task_state":{}}`) {
		t.Fatalf("expected injected task state contract, got %q", prompt)
	}
	if !strings.Contains(prompt, "[compact_summary]") {
		t.Fatalf("expected injected summary format, got %q", prompt)
	}
}

func TestSubagentRolePrompts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{name: "researcher", prompt: ResearcherRolePrompt(), want: "research sub-agent"},
		{name: "coder", prompt: CoderRolePrompt(), want: "implementation sub-agent"},
		{name: "reviewer", prompt: ReviewerRolePrompt(), want: "review sub-agent"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if strings.TrimSpace(tt.prompt) == "" {
				t.Fatalf("prompt should not be empty")
			}
			if !strings.Contains(tt.prompt, tt.want) {
				t.Fatalf("prompt = %q, want substring %q", tt.prompt, tt.want)
			}
		})
	}
}
