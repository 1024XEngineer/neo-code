package context

import (
	"context"
	"strings"
	"testing"

	agentsession "neo-code/internal/session"
)

func TestPlanModeSectionsReturnsNilForEmptyStage(t *testing.T) {
	t.Parallel()

	source := planModeContextSource{}
	sections, err := source.Sections(context.Background(), BuildInput{
		AgentMode: agentsession.AgentModePlan,
		PlanStage: "",
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if sections != nil {
		t.Fatalf("expected nil sections for empty stage, got %v", sections)
	}
}

func TestPlanModeSectionsReturnsWithoutPlanWhenNil(t *testing.T) {
	t.Parallel()

	source := planModeContextSource{}
	sections, err := source.Sections(context.Background(), BuildInput{
		AgentMode:   agentsession.AgentModeBuild,
		PlanStage:   "build",
		CurrentPlan: nil,
	})
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	if len(sections) == 0 {
		t.Fatal("expected at least plan mode section")
	}
	hasPlanSection := false
	for _, s := range sections {
		if s.Title == "Current Plan" {
			hasPlanSection = true
		}
	}
	if hasPlanSection {
		t.Fatal("expected no Current Plan section when CurrentPlan is nil")
	}
}

func TestPlanModeSectionsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := planModeContextSource{}
	_, err := source.Sections(ctx, BuildInput{
		PlanStage: "plan",
	})
	if err == nil {
		t.Fatal("expected context error")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestRenderCurrentPlanSectionNilPlan(t *testing.T) {
	t.Parallel()

	section := renderCurrentPlanSection(nil, false)
	if section.Title != "" || section.Content != "" {
		t.Fatalf("expected empty section for nil plan, got %+v", section)
	}
}

func TestRenderPlanModeSectionBuildStage(t *testing.T) {
	t.Parallel()

	section := renderPlanModeSection(agentsession.AgentModeBuild, "build")
	if section.Title != "Plan Mode" {
		t.Fatalf("title = %q, want %q", section.Title, "Plan Mode")
	}
	if !strings.Contains(section.Content, `"build"`) {
		t.Fatalf("content = %q, want contains build mode", section.Content)
	}
}

func TestRenderPlanModeSectionUnknownStage(t *testing.T) {
	t.Parallel()

	section := renderPlanModeSection(agentsession.AgentModePlan, "unknown-stage")
	if section.Title != "Plan Mode" {
		t.Fatalf("title = %q, want %q", section.Title, "Plan Mode")
	}
	if !strings.Contains(section.Content, `"unknown-stage"`) {
		t.Fatalf("content = %q, want contains stage", section.Content)
	}
}

func TestRenderCurrentPlanSectionInjectsFullPlan(t *testing.T) {
	t.Parallel()

	plan := &agentsession.PlanArtifact{
		ID:       "plan-full",
		Revision: 2,
		Status:   agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Goal:        "完整计划",
			Steps:       []string{"步骤一"},
			Verify:      []string{"验证一"},
			OpenQuestions: []string{"问题一"},
		},
		Summary: agentsession.SummaryView{
			Goal: "完整计划",
		},
	}

	section := renderCurrentPlanSection(plan, true)
	if section.Title != "Current Plan" {
		t.Fatalf("title = %q, want %q", section.Title, "Current Plan")
	}
	if !strings.Contains(section.Content, "full_plan_view:") {
		t.Fatalf("content should include full_plan_view, got %q", section.Content)
	}
}
