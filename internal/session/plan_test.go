package session

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeSummaryViewFallsBackToBuiltSummaryWhenStructurallyInvalid(t *testing.T) {
	t.Parallel()

	spec, err := NormalizePlanSpec(PlanSpec{
		Goal:        "为 runtime 引入 plan/build 模式",
		Steps:       []string{"扩展 session", "过滤工具", "调整 runtime"},
		Constraints: []string{"plan 模式禁止写工具"},
		Verify:      []string{"build 结束后进入 verify"},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "扩展 session", Status: TodoStatusPending},
			{ID: "todo-2", Content: "过滤工具", Status: TodoStatusCompleted},
		},
	})
	if err != nil {
		t.Fatalf("NormalizePlanSpec() error = %v", err)
	}

	got := NormalizeSummaryView(SummaryView{
		Goal:          "  ",
		KeySteps:      []string{"仅一步"},
		Verify:        []string{"验收"},
		ActiveTodoIDs: []string{"missing"},
	}, spec)
	want := BuildSummaryView(spec)

	if got.Goal != want.Goal {
		t.Fatalf("Goal = %q, want %q", got.Goal, want.Goal)
	}
	if len(got.KeySteps) != len(want.KeySteps) || got.KeySteps[0] != want.KeySteps[0] {
		t.Fatalf("KeySteps = %+v, want %+v", got.KeySteps, want.KeySteps)
	}
	if len(got.ActiveTodoIDs) != 1 || got.ActiveTodoIDs[0] != "todo-1" {
		t.Fatalf("ActiveTodoIDs = %+v, want [todo-1]", got.ActiveTodoIDs)
	}
}

func TestBuildSummaryViewUsesActiveNonTerminalTodosOnly(t *testing.T) {
	t.Parallel()

	spec, err := NormalizePlanSpec(PlanSpec{
		Goal:   "整理当前执行摘要",
		Steps:  []string{"步骤一", "步骤二"},
		Verify: []string{"验证一"},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "待执行", Status: TodoStatusPending},
			{ID: "todo-2", Content: "执行中", Status: TodoStatusInProgress},
			{ID: "todo-3", Content: "已完成", Status: TodoStatusCompleted},
		},
	})
	if err != nil {
		t.Fatalf("NormalizePlanSpec() error = %v", err)
	}

	summary := BuildSummaryView(spec)
	if len(summary.ActiveTodoIDs) != 2 {
		t.Fatalf("ActiveTodoIDs length = %d, want 2", len(summary.ActiveTodoIDs))
	}
	if summary.ActiveTodoIDs[0] != "todo-1" || summary.ActiveTodoIDs[1] != "todo-2" {
		t.Fatalf("ActiveTodoIDs = %+v, want [todo-1 todo-2]", summary.ActiveTodoIDs)
	}
	if len(summary.KeySteps) != 2 || summary.KeySteps[0] != "步骤一" {
		t.Fatalf("KeySteps = %+v", summary.KeySteps)
	}
}

func TestNormalizePlanArtifactDefaultsAndStatusNormalization(t *testing.T) {
	t.Parallel()

	plan, err := NormalizePlanArtifact(&PlanArtifact{
		ID:       "plan-1",
		Revision: 0,
		Status:   PlanStatus("unknown"),
		Spec: PlanSpec{
			Goal:   "规范化计划对象",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizePlanArtifact() error = %v", err)
	}
	if plan.Revision != 1 {
		t.Fatalf("Revision = %d, want 1", plan.Revision)
	}
	if plan.Status != PlanStatusDraft {
		t.Fatalf("Status = %q, want %q", plan.Status, PlanStatusDraft)
	}
	if plan.CreatedAt.IsZero() || plan.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be populated: %+v", plan)
	}
	if plan.Summary.Goal != "规范化计划对象" {
		t.Fatalf("Summary.Goal = %q", plan.Summary.Goal)
	}
}

func TestNormalizePlanArtifactPreservesCreatedAtAndNormalizesUpdatedAt(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 4, 29, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	updated := created.Add(2 * time.Hour)
	plan, err := NormalizePlanArtifact(&PlanArtifact{
		ID:        "plan-2",
		Revision:  2,
		Status:    PlanStatusApproved,
		CreatedAt: created,
		UpdatedAt: updated,
		Spec: PlanSpec{
			Goal:   "保留时间字段",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizePlanArtifact() error = %v", err)
	}
	if !plan.CreatedAt.Equal(created.UTC()) {
		t.Fatalf("CreatedAt = %v, want %v", plan.CreatedAt, created.UTC())
	}
	if !plan.UpdatedAt.Equal(updated.UTC()) {
		t.Fatalf("UpdatedAt = %v, want %v", plan.UpdatedAt, updated.UTC())
	}
}

func TestNormalizeSummaryViewAllowsEmptyTodoRefsWhenPlanHasNoTodos(t *testing.T) {
	t.Parallel()

	spec, err := NormalizePlanSpec(PlanSpec{
		Goal:   "无 todo 计划",
		Steps:  []string{"步骤一"},
		Verify: []string{"验证一"},
	})
	if err != nil {
		t.Fatalf("NormalizePlanSpec() error = %v", err)
	}

	summary := NormalizeSummaryView(SummaryView{
		Goal:     "无 todo 计划",
		KeySteps: []string{"步骤一"},
		Verify:   []string{"验证一"},
	}, spec)
	if summary.Goal != "无 todo 计划" {
		t.Fatalf("Goal = %q", summary.Goal)
	}
	if len(summary.ActiveTodoIDs) != 0 {
		t.Fatalf("ActiveTodoIDs = %+v, want empty", summary.ActiveTodoIDs)
	}
}

func TestRenderPlanContentIncludesAllSections(t *testing.T) {
	t.Parallel()

	rendered := RenderPlanContent(PlanSpec{
		Goal:          "输出完整计划正文",
		Steps:         []string{"步骤一", "步骤二"},
		Constraints:   []string{"约束一"},
		Verify:        []string{"验证一"},
		OpenQuestions: []string{"问题一"},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "待执行", Status: TodoStatusPending},
			{ID: "todo-2", Content: "已完成", Status: TodoStatusCompleted},
		},
	})

	wantSubstrings := []string{
		"目标",
		"输出完整计划正文",
		"实施步骤",
		"约束",
		"验证",
		"当前待办",
		"id=todo-1",
		"未决问题",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderPlanContent() = %q, want substring %q", rendered, want)
		}
	}
	if strings.Contains(rendered, "todo-2") {
		t.Fatalf("RenderPlanContent() should skip terminal todos, got %q", rendered)
	}
}
