package compact

import (
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/provider"
)

func TestCompactionPlannerKeepRecentPlan(t *testing.T) {
	t.Parallel()

	planner := compactionPlanner{}
	plan, err := planner.Plan(ModeManual, []provider.Message{
		{Role: provider.RoleUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool result"},
		{Role: provider.RoleUser, Content: "latest instruction"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
	}, config.CompactConfig{
		ManualStrategy:           config.CompactManualStrategyKeepRecent,
		ManualKeepRecentMessages: 3,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.Applied {
		t.Fatalf("expected keep_recent plan applied")
	}
	if len(plan.Archived) != 2 || len(plan.Retained) != 4 {
		t.Fatalf("unexpected keep_recent plan: %+v", plan)
	}
	if plan.Retained[0].Role != provider.RoleAssistant || len(plan.Retained[0].ToolCalls) != 1 {
		t.Fatalf("expected retained tool block start, got %+v", plan.Retained[0])
	}
	if plan.Retained[1].Role != provider.RoleTool {
		t.Fatalf("expected retained tool result, got %+v", plan.Retained[1])
	}
}

func TestCompactionPlannerFullReplaceProtectsLatestExplicitUserInstruction(t *testing.T) {
	t.Parallel()

	planner := compactionPlanner{}
	plan, err := planner.Plan(ModeManual, []provider.Message{
		{Role: provider.RoleUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "latest instruction"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
	}, config.CompactConfig{
		ManualStrategy: config.CompactManualStrategyFullReplace,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.Applied {
		t.Fatalf("expected full_replace plan applied")
	}
	if len(plan.Archived) != 2 || len(plan.Retained) != 2 {
		t.Fatalf("unexpected full_replace plan: %+v", plan)
	}
	if plan.Retained[0].Role != provider.RoleUser || plan.Retained[0].Content != "latest instruction" {
		t.Fatalf("expected latest explicit user instruction to stay retained, got %+v", plan.Retained)
	}
}

func TestCompactionPlannerRejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()

	_, err := (compactionPlanner{}).Plan(ModeManual, nil, config.CompactConfig{ManualStrategy: "unknown"})
	if err == nil {
		t.Fatalf("expected unsupported strategy error")
	}
}

func TestCompactionPlannerReactiveModeAlwaysUsesKeepRecentStrategy(t *testing.T) {
	t.Parallel()

	plan, err := (compactionPlanner{}).Plan(ModeReactive, []provider.Message{
		{Role: provider.RoleUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "latest request"},
		{Role: provider.RoleAssistant, Content: "latest answer"},
	}, config.CompactConfig{
		ManualStrategy:           "unsupported",
		ManualKeepRecentMessages: 2,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.Applied {
		t.Fatalf("expected reactive plan to keep recent messages")
	}
	if len(plan.Archived) != 2 || len(plan.Retained) != 2 {
		t.Fatalf("unexpected reactive plan: %+v", plan)
	}
}
