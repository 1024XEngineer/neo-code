package runtime

import (
	"testing"
	"time"

	agentsession "neo-code/internal/session"
)

func TestSelectPlanOwnedTodosIncludesPostPlanRequired(t *testing.T) {
	t.Parallel()

	required := true
	optional := false
	createdAt := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	plan := &agentsession.PlanArtifact{
		Status:    agentsession.PlanStatusApproved,
		CreatedAt: createdAt,
		Spec: agentsession.PlanSpec{Todos: []agentsession.TodoItem{
			{ID: "plan-owned", Status: agentsession.TodoStatusPending},
		}},
	}
	todos := []agentsession.TodoItem{
		{
			ID:        "plan-owned",
			Status:    agentsession.TodoStatusPending,
			Required:  &required,
			CreatedAt: createdAt.Add(-time.Hour),
		},
		{
			ID:        "post-required",
			Status:    agentsession.TodoStatusPending,
			Required:  &required,
			CreatedAt: createdAt.Add(time.Minute),
		},
		{
			ID:        "old-required",
			Status:    agentsession.TodoStatusPending,
			Required:  &required,
			CreatedAt: createdAt.Add(-time.Minute),
		},
		{
			ID:        "post-optional",
			Status:    agentsession.TodoStatusPending,
			Required:  &optional,
			CreatedAt: createdAt.Add(time.Minute),
		},
		{
			ID:        "post-completed",
			Status:    agentsession.TodoStatusCompleted,
			Required:  &required,
			CreatedAt: createdAt.Add(time.Minute),
		},
	}

	selected := selectPlanOwnedTodos(plan, todos)
	if len(selected) != 2 {
		t.Fatalf("selected length = %d, want 2: %+v", len(selected), selected)
	}
	if selected[0].ID != "plan-owned" || selected[1].ID != "post-required" {
		t.Fatalf("selected IDs = [%s %s], want [plan-owned post-required]", selected[0].ID, selected[1].ID)
	}
}

func TestSelectPlanOwnedTodosRequiresPlanForPostPlanRequired(t *testing.T) {
	t.Parallel()

	required := true
	todos := []agentsession.TodoItem{
		{
			ID:        "post-required",
			Status:    agentsession.TodoStatusPending,
			Required:  &required,
			CreatedAt: time.Now(),
		},
	}

	if selected := selectPlanOwnedTodos(nil, todos); selected != nil {
		t.Fatalf("selectPlanOwnedTodos(nil) = %+v, want nil", selected)
	}
}
