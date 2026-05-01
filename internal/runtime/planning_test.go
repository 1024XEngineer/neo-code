package runtime

import (
	"reflect"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestResolvePlanningStageForStateRespectsPlanningEnabled(t *testing.T) {
	t.Parallel()

	session := newRuntimeSession("session-plan-stage")
	state := newRunState("run-plan-stage", session)
	if got := resolvePlanningStageForState(&state); got != "" {
		t.Fatalf("resolvePlanningStageForState() = %q, want empty when planning disabled", got)
	}

	state.planningEnabled = true
	if got := resolvePlanningStageForState(&state); got != planStageBuildExecute {
		t.Fatalf("resolvePlanningStageForState() = %q, want %q", got, planStageBuildExecute)
	}

	state.session.AgentMode = agentsession.AgentModePlan
	if got := resolvePlanningStageForState(&state); got != planStagePlan {
		t.Fatalf("resolvePlanningStageForState() = %q, want %q", got, planStagePlan)
	}
}

func TestApplyRequestedAgentMode(t *testing.T) {
	t.Parallel()

	session := agentsession.New("mode switch")
	session.AgentMode = ""

	if changed := applyRequestedAgentMode(&session, ""); !changed {
		t.Fatalf("expected empty request to initialize default mode")
	}
	if session.AgentMode != agentsession.AgentModeBuild {
		t.Fatalf("AgentMode = %q, want build", session.AgentMode)
	}
	if changed := applyRequestedAgentMode(&session, "plan"); !changed {
		t.Fatalf("expected explicit mode switch to report changed")
	}
	if session.AgentMode != agentsession.AgentModePlan {
		t.Fatalf("AgentMode = %q, want plan", session.AgentMode)
	}
	if changed := applyRequestedAgentMode(&session, "PLAN"); changed {
		t.Fatalf("expected normalized duplicate mode switch to report unchanged")
	}
}

func TestMaybeParsePlanTurnOutput(t *testing.T) {
	t.Parallel()

	message := providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart(`{
  "plan_spec": {
    "goal": "实现 plan/build 模式",
    "steps": ["扩展 session", "过滤工具"],
    "verify": ["build 保留 verify"],
    "todos": [{"id":"todo-1","content":"扩展 session","status":"pending"}]
  },
  "summary_candidate": {
    "goal": "实现 plan/build 模式",
    "key_steps": ["扩展 session"],
    "constraints": [],
    "verify": ["build 保留 verify"],
    "active_todo_ids": ["todo-1"]
  }
}`),
		},
	}

	output, ok, err := maybeParsePlanTurnOutput(message)
	if err != nil {
		t.Fatalf("maybeParsePlanTurnOutput() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected plan JSON to be detected")
	}
	if output.PlanSpec.Goal != "实现 plan/build 模式" {
		t.Fatalf("PlanSpec.Goal = %q", output.PlanSpec.Goal)
	}
	if len(output.PlanSpec.Todos) != 1 || output.PlanSpec.Todos[0].ID != "todo-1" {
		t.Fatalf("PlanSpec.Todos = %+v", output.PlanSpec.Todos)
	}
	if output.DisplayText != "" {
		t.Fatalf("DisplayText = %q, want empty", output.DisplayText)
	}
}

func TestMaybeParsePlanTurnOutputAllowsNaturalLanguage(t *testing.T) {
	t.Parallel()

	output, ok, err := maybeParsePlanTurnOutput(providertypes.Message{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("Here is an analysis without a structured plan.")},
	})
	if err != nil {
		t.Fatalf("maybeParsePlanTurnOutput() error = %v", err)
	}
	if ok {
		t.Fatalf("expected natural-language response not to be treated as a plan: %+v", output)
	}
}

func TestMaybeParsePlanTurnOutputIgnoresBraceTextAndKeepsExplanation(t *testing.T) {
	t.Parallel()

	output, ok, err := maybeParsePlanTurnOutput(providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("We should avoid `{broken}` examples in docs.\n\n" +
				"{\"plan_spec\":{\"goal\":\"ship plan\",\"steps\":[\"step\"],\"verify\":[\"test\"]}}\n\n" +
				"Then execute the plan in small steps."),
		},
	})
	if err != nil {
		t.Fatalf("maybeParsePlanTurnOutput() error = %v", err)
	}
	if !ok {
		t.Fatal("expected plan JSON to be detected")
	}
	want := "We should avoid `{broken}` examples in docs.\n\nThen execute the plan in small steps."
	if output.DisplayText != want {
		t.Fatalf("DisplayText = %q, want %q", output.DisplayText, want)
	}
}

func TestMaybeParsePlanTurnOutputFallsBackWhenSummaryIsInvalid(t *testing.T) {
	t.Parallel()

	output, ok, err := maybeParsePlanTurnOutput(providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(`{
  "plan_spec": {
    "goal": "ship plan",
    "steps": ["step one", "step two"],
    "verify": ["go test ./internal/runtime"],
    "todos": [{"id":"todo-1","content":"step one","status":"pending"}]
  },
  "summary_candidate": {
    "goal": ["bad type"],
    "key_steps": "invalid",
    "verify": ["broken"],
    "active_todo_ids": "todo-1"
  }
}`)},
	})
	if err != nil {
		t.Fatalf("maybeParsePlanTurnOutput() error = %v", err)
	}
	if !ok {
		t.Fatal("expected plan JSON to be detected")
	}
	plan, err := buildPlanArtifact(nil, output)
	if err != nil {
		t.Fatalf("buildPlanArtifact() error = %v", err)
	}
	want := agentsession.BuildSummaryView(output.PlanSpec)
	if !reflect.DeepEqual(plan.Summary, want) {
		t.Fatalf("Summary = %+v, want %+v", plan.Summary, want)
	}
}

func TestMaybeParsePlanTurnOutputTreatsInvalidPlanSpecAsPlainText(t *testing.T) {
	t.Parallel()

	output, ok, err := maybeParsePlanTurnOutput(providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(`{
  "plan_spec": {
    "goal": "",
    "steps": ["step"],
    "verify": ["test"]
  }
}
Explanation still continues.`)},
	})
	if err != nil {
		t.Fatalf("maybeParsePlanTurnOutput() error = %v", err)
	}
	if ok {
		t.Fatalf("expected invalid plan_spec to be ignored, got %+v", output)
	}
}

func TestMaybeParseCompletionTurnOutput(t *testing.T) {
	t.Parallel()

	completed, err := maybeParseCompletionTurnOutput(providertypes.Message{
		Role: providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{
			providertypes.NewTextPart("{\"task_completion\":{\"completed\":true}}\n任务已经完成。"),
		},
	})
	if err != nil {
		t.Fatalf("maybeParseCompletionTurnOutput() error = %v", err)
	}
	if !completed {
		t.Fatal("expected structured completion signal to be detected")
	}

	completed, err = maybeParseCompletionTurnOutput(providertypes.Message{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("plain answer only")},
	})
	if err != nil {
		t.Fatalf("maybeParseCompletionTurnOutput() natural language error = %v", err)
	}
	if completed {
		t.Fatal("expected natural language without completion JSON not to signal completion")
	}
}

func TestMaybeParseCompletionTurnOutputIgnoresInvalidStructuredReply(t *testing.T) {
	t.Parallel()

	completed, err := maybeParseCompletionTurnOutput(providertypes.Message{
		Role:  providertypes.RoleAssistant,
		Parts: []providertypes.ContentPart{providertypes.NewTextPart(`{"task_completion":"done"}`)},
	})
	if err != nil {
		t.Fatalf("maybeParseCompletionTurnOutput() error = %v", err)
	}
	if completed {
		t.Fatal("expected invalid completion payload to be ignored")
	}
}

func TestExtractPlanningJSONObjectIfPresent(t *testing.T) {
	t.Parallel()

	text := "preface\n{\"example\":true}\n{\"plan_spec\":{\"goal\":\"x\",\"steps\":[\"s\"],\"verify\":[\"v\"]},\"summary_candidate\":{\"goal\":\"x\",\"key_steps\":[\"s\"],\"constraints\":[],\"verify\":[\"v\"],\"active_todo_ids\":[]}}\ntrailing"
	got, ok := extractPlanningJSONObjectIfPresent(text, "plan_spec")
	if !ok {
		t.Fatalf("expected JSON object to be detected")
	}
	if !strings.HasPrefix(got.Text, "{") || !strings.Contains(got.Text, "\"plan_spec\"") {
		t.Fatalf("extractPlanningJSONObjectIfPresent() = %q", got.Text)
	}
}

func TestExtractPlanningJSONObjectIfPresentWithoutJSON(t *testing.T) {
	t.Parallel()

	got, ok := extractPlanningJSONObjectIfPresent("plain text only", "plan_spec")
	if ok || got.Text != "" {
		t.Fatalf("expected no JSON result, got ok=%v text=%q", ok, got.Text)
	}
}

func TestBuildPlanArtifact(t *testing.T) {
	t.Parallel()

	current := &agentsession.PlanArtifact{
		ID:        "plan-1",
		Revision:  2,
		Status:    agentsession.PlanStatusDraft,
		CreatedAt: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
		Spec: agentsession.PlanSpec{
			Goal:   "旧计划",
			Steps:  []string{"旧步骤"},
			Verify: []string{"旧验证"},
		},
	}
	output := planTurnOutput{
		PlanSpec: agentsession.PlanSpec{
			Goal:   "新计划",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
			Todos: []agentsession.TodoItem{
				{ID: "todo-1", Content: "待办", Status: agentsession.TodoStatusPending},
			},
		},
		SummaryCandidate: summaryCandidate{
			Goal:          "新计划",
			KeySteps:      []string{"步骤一"},
			Verify:        []string{"验证一"},
			ActiveTodoIDs: []string{"todo-1"},
		},
	}

	plan, err := buildPlanArtifact(current, output)
	if err != nil {
		t.Fatalf("buildPlanArtifact() error = %v", err)
	}
	if plan.ID != "plan-1" {
		t.Fatalf("ID = %q, want %q", plan.ID, "plan-1")
	}
	if plan.Revision != 3 {
		t.Fatalf("Revision = %d, want 3", plan.Revision)
	}
	if plan.Status != agentsession.PlanStatusDraft {
		t.Fatalf("Status = %q, want %q", plan.Status, agentsession.PlanStatusDraft)
	}
	if !plan.CreatedAt.Equal(current.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", plan.CreatedAt, current.CreatedAt)
	}
	if plan.Summary.Goal != "新计划" {
		t.Fatalf("Summary.Goal = %q", plan.Summary.Goal)
	}
}

func TestMarkCurrentPlanCompleted(t *testing.T) {
	t.Parallel()

	session := agentsession.New("plan state")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-1",
		Revision: 1,
		Status:   agentsession.PlanStatusDraft,
		Spec: agentsession.PlanSpec{
			Goal:   "执行当前计划",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	}
	if !markCurrentPlanCompleted(&session, true) {
		t.Fatalf("expected draft plan with completion signal to transition to completed")
	}
	if session.CurrentPlan.Status != agentsession.PlanStatusCompleted {
		t.Fatalf("Status = %q, want completed", session.CurrentPlan.Status)
	}
	if !session.PlanCompletionPendingFullReview {
		t.Fatal("expected completed plan to request one final full-plan review turn")
	}
	if markCurrentPlanCompleted(&session, true) {
		t.Fatalf("expected completed plan not to transition again")
	}

	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-2",
		Revision: 1,
		Status:   agentsession.PlanStatusDraft,
		Spec: agentsession.PlanSpec{
			Goal:   "草案计划",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	}
	if markCurrentPlanCompleted(&session, false) {
		t.Fatalf("expected missing completion signal to keep plan unfinished")
	}
}

func TestPlanningNeedsFullPlan(t *testing.T) {
	t.Parallel()

	state := newRunState("run-full-plan-check", agentsession.New("plan"))
	state.session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-1",
		Revision: 2,
		Status:   agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Goal:   "Use full plan when alignment is pending",
			Steps:  []string{"align plan"},
			Verify: []string{"go test ./internal/runtime"},
			Todos: []agentsession.TodoItem{
				{ID: "todo-1", Content: "align plan", Status: agentsession.TodoStatusPending},
			},
		},
		Summary: agentsession.SummaryView{
			Goal:          "Use full plan when alignment is pending",
			KeySteps:      []string{"align plan"},
			Verify:        []string{"go test ./internal/runtime"},
			ActiveTodoIDs: []string{"todo-1"},
		},
	}
	if !planningNeedsFullPlan(&state) {
		t.Fatalf("expected newer revision to require full plan")
	}

	state.session.LastFullPlanRevision = 2
	if planningNeedsFullPlan(&state) {
		t.Fatalf("expected aligned revision to use summary view only")
	}

	state.session.PlanApprovalPendingFullAlign = true
	if !planningNeedsFullPlan(&state) {
		t.Fatalf("expected approval alignment flag to require full plan")
	}
	state.session.PlanApprovalPendingFullAlign = false

	state.session.PlanCompletionPendingFullReview = true
	state.session.CurrentPlan.Status = agentsession.PlanStatusCompleted
	if !planningNeedsFullPlan(&state) {
		t.Fatalf("expected completion review flag to require full plan even for completed plan")
	}
	state.session.PlanCompletionPendingFullReview = false
	if planningNeedsFullPlan(&state) {
		t.Fatalf("expected completed plan without review flag to stay summary-only")
	}

	state.session.CurrentPlan.Status = agentsession.PlanStatusApproved
	state.session.CurrentPlan.Summary = agentsession.SummaryView{}
	if !planningNeedsFullPlan(&state) {
		t.Fatalf("expected unusable summary view to require full plan")
	}
}

func TestApproveCurrentPlan(t *testing.T) {
	t.Parallel()

	session := agentsession.New("approve plan")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-approve",
		Revision: 3,
		Status:   agentsession.PlanStatusDraft,
		Spec: agentsession.PlanSpec{
			Goal:   "批准当前计划",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	}
	if err := approveCurrentPlan(&session, "plan-approve", 3); err != nil {
		t.Fatalf("approveCurrentPlan() error = %v", err)
	}
	if session.CurrentPlan.Status != agentsession.PlanStatusApproved {
		t.Fatalf("Status = %q, want approved", session.CurrentPlan.Status)
	}
	if !session.PlanApprovalPendingFullAlign {
		t.Fatal("expected approval to schedule a full-plan alignment")
	}
}

func TestRememberFullPlanRevisionClearsAlignmentFlags(t *testing.T) {
	t.Parallel()

	session := agentsession.New("remember full plan")
	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-align",
		Revision: 2,
		Status:   agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Goal:   "完成全文对齐",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	}
	session.PlanApprovalPendingFullAlign = true
	session.PlanCompletionPendingFullReview = true
	session.PlanContextDirty = true
	session.PlanRestorePendingAlign = true

	if !rememberFullPlanRevision(&session) {
		t.Fatal("expected full-plan alignment to update revision state")
	}
	if session.LastFullPlanRevision != 2 {
		t.Fatalf("LastFullPlanRevision = %d, want 2", session.LastFullPlanRevision)
	}
	if session.PlanApprovalPendingFullAlign || session.PlanCompletionPendingFullReview ||
		session.PlanContextDirty || session.PlanRestorePendingAlign {
		t.Fatalf("expected one-shot alignment flags to be cleared, got %+v", session)
	}
}

func TestMarkCurrentPlanRestorePendingAndContextDirty(t *testing.T) {
	t.Parallel()

	session := agentsession.New("mark restore/context dirty")
	if markCurrentPlanRestorePending(&session) {
		t.Fatal("expected false when current plan is missing")
	}
	if markCurrentPlanContextDirty(&session) {
		t.Fatal("expected false when current plan is missing")
	}

	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-restore",
		Revision: 1,
		Status:   agentsession.PlanStatusApproved,
		Spec: agentsession.PlanSpec{
			Goal:   "restore full plan",
			Steps:  []string{"step one"},
			Verify: []string{"verify one"},
		},
	}
	if !markCurrentPlanRestorePending(&session) {
		t.Fatal("expected first restore mark to succeed")
	}
	if markCurrentPlanRestorePending(&session) {
		t.Fatal("expected duplicated restore mark to be ignored")
	}
	if !markCurrentPlanContextDirty(&session) {
		t.Fatal("expected first context dirty mark to succeed")
	}
	if markCurrentPlanContextDirty(&session) {
		t.Fatal("expected duplicated context dirty mark to be ignored")
	}

	session.CurrentPlan.Status = agentsession.PlanStatusCompleted
	session.PlanCompletionPendingFullReview = false
	session.PlanRestorePendingAlign = false
	session.PlanContextDirty = false
	if markCurrentPlanRestorePending(&session) {
		t.Fatal("expected completed plan without full review pending not to mark restore align")
	}
	if markCurrentPlanContextDirty(&session) {
		t.Fatal("expected completed plan without full review pending not to mark context dirty")
	}
}

func TestApplyCurrentPlanRevisionNilGuards(t *testing.T) {
	t.Parallel()

	session := agentsession.New("apply plan revision nil guards")
	plan := &agentsession.PlanArtifact{ID: "plan-1", Revision: 1}
	if applyCurrentPlanRevision(nil, plan) {
		t.Fatal("expected nil session to return false")
	}
	if applyCurrentPlanRevision(&session, nil) {
		t.Fatal("expected nil plan to return false")
	}
}

func TestApproveCurrentPlanValidationErrors(t *testing.T) {
	t.Parallel()

	session := agentsession.New("approve validation")
	if err := approveCurrentPlan(&session, "plan-1", 1); err == nil {
		t.Fatal("expected error when current plan does not exist")
	}

	session.CurrentPlan = &agentsession.PlanArtifact{
		ID:       "plan-1",
		Revision: 2,
		Status:   agentsession.PlanStatusDraft,
		Spec: agentsession.PlanSpec{
			Goal:   "审批校验",
			Steps:  []string{"步骤一"},
			Verify: []string{"验证一"},
		},
	}

	if err := approveCurrentPlan(&session, "plan-2", 2); err == nil {
		t.Fatal("expected id mismatch error")
	}
	if err := approveCurrentPlan(&session, "plan-1", 1); err == nil {
		t.Fatal("expected revision mismatch error")
	}

	session.CurrentPlan.Status = agentsession.PlanStatusApproved
	if err := approveCurrentPlan(&session, "plan-1", 2); err == nil {
		t.Fatal("expected status mismatch error")
	}
}
