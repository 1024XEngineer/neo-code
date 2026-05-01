package context

import (
	"context"
	"fmt"
	"strings"

	"neo-code/internal/promptasset"
	agentsession "neo-code/internal/session"
)

// planModeContextSource injects plan/build mode guidance plus the current plan projection.
type planModeContextSource struct{}

// Sections renders the relevant mode guidance and optional current plan section.
func (planModeContextSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mode := agentsession.NormalizeAgentMode(input.AgentMode)
	stage := strings.TrimSpace(input.PlanStage)
	if stage == "" {
		return nil, nil
	}

	sections := make([]promptSection, 0, 2)
	modeSection := renderPlanModeSection(mode, stage)
	if modeSection.Content != "" {
		sections = append(sections, modeSection)
	}

	if input.CurrentPlan == nil {
		return sections, nil
	}
	planSection := renderCurrentPlanSection(input.CurrentPlan, input.InjectFullPlan)
	if planSection.Content != "" {
		sections = append(sections, planSection)
	}
	return sections, nil
}

func renderPlanModeSection(mode agentsession.AgentMode, stage string) promptSection {
	lines := make([]string, 0, 4)
	lines = append(lines, fmt.Sprintf("current_mode: %q", mode))
	lines = append(lines, fmt.Sprintf("current_stage: %q", stage))
	if content := strings.TrimSpace(promptasset.PlanModePrompt(stage)); content != "" {
		lines = append(lines, content)
	}
	return promptSection{
		Title:   "Plan Mode",
		Content: strings.Join(lines, "\n"),
	}
}

func renderCurrentPlanSection(plan *agentsession.PlanArtifact, injectFull bool) promptSection {
	if plan == nil {
		return promptSection{}
	}
	lines := make([]string, 0, 16)
	lines = append(lines,
		fmt.Sprintf("plan_id: %q", strings.TrimSpace(plan.ID)),
		fmt.Sprintf("revision: %d", plan.Revision),
		fmt.Sprintf("status: %q", agentsession.NormalizePlanStatus(plan.Status)),
	)
	if goal := strings.TrimSpace(plan.Summary.Goal); goal != "" {
		lines = append(lines, fmt.Sprintf("goal: %q", goal))
	}
	if len(plan.Summary.KeySteps) > 0 {
		lines = append(lines, "key_steps:")
		for _, step := range plan.Summary.KeySteps {
			lines = append(lines, "- "+step)
		}
	}
	if len(plan.Summary.Constraints) > 0 {
		lines = append(lines, "constraints:")
		for _, constraint := range plan.Summary.Constraints {
			lines = append(lines, "- "+constraint)
		}
	}
	if len(plan.Summary.Verify) > 0 {
		lines = append(lines, "verify:")
		for _, check := range plan.Summary.Verify {
			lines = append(lines, "- "+check)
		}
	}
	if len(plan.Summary.ActiveTodoIDs) > 0 {
		lines = append(lines, "active_todo_ids:")
		for _, todoID := range plan.Summary.ActiveTodoIDs {
			lines = append(lines, "- "+todoID)
		}
	}
	if injectFull {
		if full := strings.TrimSpace(agentsession.RenderPlanContent(plan.Spec)); full != "" {
			lines = append(lines, "", "full_plan_view:", full)
		}
	}
	return promptSection{
		Title:   "Current Plan",
		Content: strings.Join(lines, "\n"),
	}
}
