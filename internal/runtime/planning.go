package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/controlplane"
	agentsession "neo-code/internal/session"
)

const (
	planStagePlan         = "plan"
	planStageBuildExecute = "build_execute"
)

type summaryCandidate struct {
	Goal          string   `json:"goal"`
	KeySteps      []string `json:"key_steps"`
	Constraints   []string `json:"constraints"`
	Verify        []string `json:"verify"`
	ActiveTodoIDs []string `json:"active_todo_ids"`
}

type planTurnOutput struct {
	PlanSpec         agentsession.PlanSpec `json:"plan_spec"`
	SummaryCandidate summaryCandidate      `json:"summary_candidate"`
}

type taskCompletionSignal struct {
	Completed bool `json:"completed"`
}

type completionTurnOutput struct {
	TaskCompletion taskCompletionSignal `json:"task_completion"`
}

// resolvePlanningStage 根据当前会话模式映射出活动的 planning stage。
func resolvePlanningStage(session agentsession.Session) string {
	if agentsession.NormalizeAgentMode(session.AgentMode) == agentsession.AgentModePlan {
		return planStagePlan
	}
	return planStageBuildExecute
}

// resolvePlanningStageForState 在需要时启用 plan/build 上下文链路。
func resolvePlanningStageForState(state *runState) string {
	if state == nil || !state.planningEnabled {
		return ""
	}
	return resolvePlanningStage(state.session)
}

// applyRequestedAgentMode 将显式请求的 mode 回写到会话状态中。
func applyRequestedAgentMode(session *agentsession.Session, requested string) bool {
	if session == nil {
		return false
	}
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		if session.AgentMode == "" {
			session.AgentMode = agentsession.AgentModeBuild
			return true
		}
		session.AgentMode = agentsession.NormalizeAgentMode(session.AgentMode)
		return false
	}
	next := agentsession.NormalizeAgentMode(agentsession.AgentMode(trimmed))
	if session.AgentMode == next {
		return false
	}
	session.AgentMode = next
	return true
}

// isReadOnlyPlanningStage 标记只允许只读工具的 planning stage，目前仅 plan 模式受限。
func isReadOnlyPlanningStage(stage string) bool {
	return stage == planStagePlan
}

// baseRunStateForPlanningStage 为 planning stage 选择初始运行态，确保规划阶段仍落在 RunStatePlan。
func baseRunStateForPlanningStage(stage string) controlplane.RunState {
	return controlplane.RunStatePlan
}

// planningNeedsFullPlan 判断当前回合是否需要注入完整计划正文。
func planningNeedsFullPlan(state *runState) bool {
	if state == nil || state.session.CurrentPlan == nil {
		return false
	}
	if state.session.CurrentPlan.Status == agentsession.PlanStatusCompleted &&
		!state.session.PlanCompletionPendingFullReview {
		return false
	}
	if !summaryViewUsable(state.session.CurrentPlan.Summary) {
		return true
	}
	if state.session.CurrentPlan.Revision > state.session.LastFullPlanRevision {
		return true
	}
	return state.session.PlanApprovalPendingFullAlign ||
		state.session.PlanCompletionPendingFullReview ||
		state.session.PlanContextDirty ||
		state.session.PlanRestorePendingAlign
}

func summaryViewUsable(summary agentsession.SummaryView) bool {
	return strings.TrimSpace(summary.Goal) != "" &&
		len(summary.KeySteps) > 0 &&
		len(summary.Verify) > 0
}

func normalizeSummaryCandidate(candidate summaryCandidate) agentsession.SummaryView {
	return agentsession.SummaryView{
		Goal:          strings.TrimSpace(candidate.Goal),
		KeySteps:      append([]string(nil), candidate.KeySteps...),
		Constraints:   append([]string(nil), candidate.Constraints...),
		Verify:        append([]string(nil), candidate.Verify...),
		ActiveTodoIDs: append([]string(nil), candidate.ActiveTodoIDs...),
	}
}

// maybeParsePlanTurnOutput 仅在 assistant 实际输出 planning JSON 时解析计划载荷。
func maybeParsePlanTurnOutput(message providertypes.Message) (planTurnOutput, bool, error) {
	text := strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts))
	if text == "" {
		return planTurnOutput{}, false, nil
	}
	jsonText, ok, err := extractPlanningJSONObjectIfPresent(text)
	if err != nil {
		return planTurnOutput{}, false, err
	}
	if !ok {
		return planTurnOutput{}, false, nil
	}
	var output planTurnOutput
	if err := json.Unmarshal([]byte(jsonText), &output); err != nil {
		return planTurnOutput{}, false, fmt.Errorf("runtime: decode planning json: %w", err)
	}
	spec, err := agentsession.NormalizePlanSpec(output.PlanSpec)
	if err != nil {
		return planTurnOutput{}, false, err
	}
	output.PlanSpec = spec
	return output, true, nil
}

// maybeParseCompletionTurnOutput 仅在 assistant 明确输出结构化完成信号时返回完成标记。
func maybeParseCompletionTurnOutput(message providertypes.Message) (bool, error) {
	text := strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts))
	if text == "" || !strings.Contains(text, `"task_completion"`) {
		return false, nil
	}
	jsonText, ok, err := extractPlanningJSONObjectIfPresent(text)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	var output completionTurnOutput
	if err := json.Unmarshal([]byte(jsonText), &output); err != nil {
		return false, fmt.Errorf("runtime: decode completion json: %w", err)
	}
	return output.TaskCompletion.Completed, nil
}

// extractPlanningJSONObjectIfPresent 在文本中提取首个配平的 JSON 对象。
func extractPlanningJSONObjectIfPresent(text string) (string, bool, error) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return "", false, nil
	}
	for {
		candidate, err := extractJSONObjectCandidate(text, start)
		if err == nil {
			return candidate, true, nil
		}
		next := strings.IndexByte(text[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return "", false, fmt.Errorf("runtime: planning response does not contain a valid JSON object")
}

func buildPlanArtifact(current *agentsession.PlanArtifact, output planTurnOutput) (*agentsession.PlanArtifact, error) {
	now := time.Now().UTC()
	revision := 1
	planID := agentsession.NewID("plan")
	createdAt := now
	if current != nil {
		planID = strings.TrimSpace(current.ID)
		if planID == "" {
			planID = agentsession.NewID("plan")
		}
		revision = current.Revision + 1
		if revision <= 0 {
			revision = 1
		}
		if !current.CreatedAt.IsZero() {
			createdAt = current.CreatedAt.UTC()
		}
	}

	summary := agentsession.NormalizeSummaryView(normalizeSummaryCandidate(output.SummaryCandidate), output.PlanSpec)
	plan, err := agentsession.NormalizePlanArtifact(&agentsession.PlanArtifact{
		ID:        planID,
		Revision:  revision,
		Status:    agentsession.PlanStatusDraft,
		Spec:      output.PlanSpec,
		Summary:   summary,
		CreatedAt: createdAt,
		UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// applyCurrentPlanRevision 用新 revision 替换当前计划，并清理旧 revision 遗留的对齐状态。
func applyCurrentPlanRevision(session *agentsession.Session, plan *agentsession.PlanArtifact) bool {
	if session == nil || plan == nil {
		return false
	}
	session.CurrentPlan = plan
	session.PlanApprovalPendingFullAlign = false
	session.PlanCompletionPendingFullReview = false
	session.PlanContextDirty = false
	session.PlanRestorePendingAlign = false
	return true
}

// markCurrentPlanRestorePending 为已加载的活动计划设置一次恢复后全文对齐标记。
func markCurrentPlanRestorePending(session *agentsession.Session) bool {
	if session == nil || session.CurrentPlan == nil {
		return false
	}
	if session.CurrentPlan.Status == agentsession.PlanStatusCompleted &&
		!session.PlanCompletionPendingFullReview {
		return false
	}
	if session.PlanRestorePendingAlign {
		return false
	}
	session.PlanRestorePendingAlign = true
	return true
}

// markCurrentPlanContextDirty 在 compact 成功后标记当前计划需要重新做一次全文对齐。
func markCurrentPlanContextDirty(session *agentsession.Session) bool {
	if session == nil || session.CurrentPlan == nil {
		return false
	}
	if session.CurrentPlan.Status == agentsession.PlanStatusCompleted &&
		!session.PlanCompletionPendingFullReview {
		return false
	}
	if session.PlanContextDirty {
		return false
	}
	session.PlanContextDirty = true
	return true
}

// rememberFullPlanRevision 记录最近一次已完整注入的计划 revision，并清理一次性对齐标记。
func rememberFullPlanRevision(session *agentsession.Session) bool {
	if session == nil || session.CurrentPlan == nil {
		return false
	}
	changed := false
	if session.CurrentPlan.Revision > session.LastFullPlanRevision {
		session.LastFullPlanRevision = session.CurrentPlan.Revision
		changed = true
	}
	if session.PlanApprovalPendingFullAlign {
		session.PlanApprovalPendingFullAlign = false
		changed = true
	}
	if session.PlanCompletionPendingFullReview {
		session.PlanCompletionPendingFullReview = false
		changed = true
	}
	if session.PlanContextDirty {
		session.PlanContextDirty = false
		changed = true
	}
	if session.PlanRestorePendingAlign {
		session.PlanRestorePendingAlign = false
		changed = true
	}
	return changed
}

// approveCurrentPlan 显式批准当前 draft revision，并安排下一轮做一次完整计划对齐。
func approveCurrentPlan(session *agentsession.Session, planID string, revision int) error {
	if session == nil || session.CurrentPlan == nil {
		return fmt.Errorf("runtime: current plan does not exist")
	}
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(session.CurrentPlan.ID) != strings.TrimSpace(planID) {
		return fmt.Errorf("runtime: current plan id does not match")
	}
	if revision <= 0 || session.CurrentPlan.Revision != revision {
		return fmt.Errorf("runtime: current plan revision does not match")
	}
	if session.CurrentPlan.Status != agentsession.PlanStatusDraft {
		return fmt.Errorf("runtime: current plan status %q cannot be approved", session.CurrentPlan.Status)
	}
	session.CurrentPlan = session.CurrentPlan.Clone()
	session.CurrentPlan.Status = agentsession.PlanStatusApproved
	session.CurrentPlan.UpdatedAt = time.Now().UTC()
	session.PlanApprovalPendingFullAlign = true
	session.PlanCompletionPendingFullReview = false
	return nil
}

// markCurrentPlanCompleted 在结构化完成信号和验收同时通过后推进计划完成态。
func markCurrentPlanCompleted(session *agentsession.Session, completionSignaled bool) bool {
	if session == nil || session.CurrentPlan == nil {
		return false
	}
	if !completionSignaled {
		return false
	}
	if session.CurrentPlan.Status == agentsession.PlanStatusCompleted {
		return false
	}
	session.CurrentPlan = session.CurrentPlan.Clone()
	session.CurrentPlan.Status = agentsession.PlanStatusCompleted
	session.CurrentPlan.UpdatedAt = time.Now().UTC()
	session.PlanApprovalPendingFullAlign = false
	session.PlanCompletionPendingFullReview = true
	return true
}
