package session

import (
	"fmt"
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

func TestRenderPlanContentReturnsEmptyOnEmptyGoal(t *testing.T) {
	t.Parallel()

	rendered := RenderPlanContent(PlanSpec{
		Goal:  "",
		Steps: []string{"步骤一"},
	})
	if rendered != "" {
		t.Fatalf("RenderPlanContent() = %q, want empty", rendered)
	}
}

func TestRenderPlanContentOmittedSections(t *testing.T) {
	t.Parallel()

	rendered := RenderPlanContent(PlanSpec{
		Goal: "仅目标",
	})
	wantSubstrings := []string{"目标", "仅目标"}
	for _, want := range wantSubstrings {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderPlanContent() = %q, want substring %q", rendered, want)
		}
	}
	// Should not contain these sections when empty
	unwanted := []string{"实施步骤", "约束", "验证", "当前待办", "未决问题"}
	for _, unwantedStr := range unwanted {
		if strings.Contains(rendered, unwantedStr) {
			t.Fatalf("RenderPlanContent() = %q, should not contain %q", rendered, unwantedStr)
		}
	}
}

func TestNormalizePlanStatusDefaultsToDraft(t *testing.T) {
	t.Parallel()

	if got := NormalizePlanStatus(""); got != PlanStatusDraft {
		t.Fatalf("NormalizePlanStatus('') = %q, want %q", got, PlanStatusDraft)
	}
	if got := NormalizePlanStatus("invalid"); got != PlanStatusDraft {
		t.Fatalf("NormalizePlanStatus('invalid') = %q, want %q", got, PlanStatusDraft)
	}
}

func TestNormalizePlanArtifactNilPlan(t *testing.T) {
	t.Parallel()

	got, err := NormalizePlanArtifact(nil)
	if err != nil {
		t.Fatalf("NormalizePlanArtifact(nil) error = %v", err)
	}
	if got != nil {
		t.Fatalf("NormalizePlanArtifact(nil) = %v, want nil", got)
	}
}

func TestNormalizePlanArtifactEmptyID(t *testing.T) {
	t.Parallel()

	_, err := NormalizePlanArtifact(&PlanArtifact{
		ID:  "",
		Spec: PlanSpec{
			Goal:   "测试",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	})
	if err == nil {
		t.Fatal("expected empty id error")
	}
	if !strings.Contains(err.Error(), "plan id is empty") {
		t.Fatalf("error = %v, want contains %q", err, "plan id is empty")
	}
}

func TestNormalizePlanSpecEmptyGoalError(t *testing.T) {
	t.Parallel()

	_, err := NormalizePlanSpec(PlanSpec{
		Goal:  "",
		Steps: []string{"步骤一"},
	})
	if err == nil {
		t.Fatal("expected empty goal error")
	}
	if !strings.Contains(err.Error(), "plan goal is empty") {
		t.Fatalf("error = %v, want contains %q", err, "plan goal is empty")
	}
}

func TestNormalizePlanSpecGoalTrimsWhitespace(t *testing.T) {
	t.Parallel()

	_, err := NormalizePlanSpec(PlanSpec{
		Goal:  "  ",
		Steps: []string{"步骤一"},
	})
	if err == nil {
		t.Fatal("expected error for whitespace-only goal")
	}
}

func TestBuildSummaryViewErrorReturnsEmpty(t *testing.T) {
	t.Parallel()

	summary := BuildSummaryView(PlanSpec{Goal: ""})
	if summary.Goal != "" {
		t.Fatalf("Goal = %q, want empty", summary.Goal)
	}
}

func TestClampStringListMaxItems(t *testing.T) {
	t.Parallel()

	items := []string{"a", "b", "c", "d", "e"}
	got := clampStringList(items, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("clampStringList = %+v, want [a b c]", got)
	}

	gotAll := clampStringList(items, 10)
	if len(gotAll) != 5 {
		t.Fatalf("len = %d, want 5", len(gotAll))
	}
	gotZero := clampStringList(items, 0)
	if len(gotZero) != 5 {
		t.Fatalf("len = %d, want 5 when maxItems <= 0", len(gotZero))
	}
}

func TestSummaryViewStructurallyValidDetectsInvalid(t *testing.T) {
	t.Parallel()

	spec := PlanSpec{Goal: "目标", Steps: []string{"步骤一"}, Verify: []string{"验证一"}}
	// Empty goal
	if summaryViewStructurallyValid(SummaryView{}, spec) {
		t.Fatal("expected false for empty summary")
	}
	// Missing key steps
	if summaryViewStructurallyValid(SummaryView{Goal: "目标", Verify: []string{"v"}}, spec) {
		t.Fatal("expected false for missing key steps")
	}
	// Unknown active todo IDs
	if summaryViewStructurallyValid(SummaryView{
		Goal:          "目标",
		KeySteps:      []string{"步骤一"},
		Verify:        []string{"验证一"},
		ActiveTodoIDs: []string{"unknown"},
	}, spec) {
		t.Fatal("expected false for unknown todo IDs")
	}
}

func TestCollectActiveTodoIDsEmptyOrLimit(t *testing.T) {
	t.Parallel()

	// Empty items
	if got := collectActiveTodoIDs(nil, 5); got != nil {
		t.Fatalf("expected nil for empty items, got %v", got)
	}
	// Limit <= 0
	if got := collectActiveTodoIDs([]TodoItem{{ID: "t1", Status: TodoStatusPending}}, 0); got != nil {
		t.Fatalf("expected nil for zero limit, got %v", got)
	}
	// All terminal → empty
	items := []TodoItem{
		{ID: "t1", Content: "已完成", Status: TodoStatusCompleted},
		{ID: "t2", Content: "已失败", Status: TodoStatusFailed},
	}
	if got := collectActiveTodoIDs(items, 5); got != nil {
		t.Fatalf("expected nil when all todos terminal, got %v", got)
	}
	// Limits to max
	manyItems := make([]TodoItem, 10)
	for i := 0; i < 10; i++ {
		manyItems[i] = TodoItem{ID: fmt.Sprintf("t-%d", i), Content: "待执行", Status: TodoStatusPending}
	}
	got := collectActiveTodoIDs(manyItems, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestCollectActiveTodoLinesEmptyOrTerminal(t *testing.T) {
	t.Parallel()

	// Empty items
	if got := collectActiveTodoLines(nil); got != nil {
		t.Fatalf("expected nil for empty items, got %v", got)
	}
	// All terminal
	items := []TodoItem{
		{ID: "t1", Content: "已完成", Status: TodoStatusCompleted},
	}
	if got := collectActiveTodoLines(items); got != nil {
		t.Fatalf("expected nil when all terminal, got %v", got)
	}
}

func TestNormalizePlanStatusCompleted(t *testing.T) {
	t.Parallel()

	if got := NormalizePlanStatus(PlanStatusCompleted); got != PlanStatusCompleted {
		t.Fatalf("NormalizePlanStatus(completed) = %q, want %q", got, PlanStatusCompleted)
	}
}

func TestNormalizePlanArtifactSpecErrorPropagated(t *testing.T) {
	t.Parallel()

	_, err := NormalizePlanArtifact(&PlanArtifact{
		ID:  "plan-err",
		Spec: PlanSpec{
			Goal: "",
			Todos: []TodoItem{
				{ID: "t1", Content: "todo", Dependencies: []string{"unknown"}},
			},
		},
	})
	if err == nil {
		t.Fatal("expected spec validation error")
	}
}

func TestNormalizePlanSpecTodoValidationError(t *testing.T) {
	t.Parallel()

	_, err := NormalizePlanSpec(PlanSpec{
		Goal: "test",
		Todos: []TodoItem{
			{ID: "t1", Content: "todo", Dependencies: []string{"nonexistent"}},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown dependency")
	}
	if !strings.Contains(err.Error(), "unknown dependency") {
		t.Fatalf("error = %v, want contains 'unknown dependency'", err)
	}
}

func TestRenderBulletListEmptyOrBlank(t *testing.T) {
	t.Parallel()

	if got := renderBulletList(nil); got != "" {
		t.Fatalf("expected empty for nil, got %q", got)
	}
	if got := renderBulletList([]string{}); got != "" {
		t.Fatalf("expected empty for empty, got %q", got)
	}
	// All blank items
	if got := renderBulletList([]string{"  ", ""}); got != "" {
		t.Fatalf("expected empty for blank items, got %q", got)
	}
	// Mixed blank and valid
	got := renderBulletList([]string{"step 1", "", "step 3"})
	want := "- step 1\n- step 3"
	if got != want {
		t.Fatalf("renderBulletList = %q, want %q", got, want)
	}
}
