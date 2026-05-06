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
	DisplayText      string                `json:"-"`
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
	candidate, ok := extractPlanningJSONObjectIfPresent(text, "plan_spec")
	if !ok {
		return planTurnOutput{}, false, nil
	}
	output, err := decodePlanTurnOutput(candidate.Text)
	if err != nil {
		return planTurnOutput{}, false, nil
	}
	output.DisplayText = stripPlanningJSONObjectText(text, candidate)
	return output, true, nil
}

// maybeParseCompletionTurnOutput 仅在 assistant 明确输出结构化完成信号时返回完成标记。
func maybeParseCompletionTurnOutput(message providertypes.Message) (bool, error) {
	text := strings.TrimSpace(partsrender.RenderDisplayParts(message.Parts))
	if text == "" || !strings.Contains(text, `"task_completion"`) {
		return false, nil
	}
	candidate, ok := extractPlanningJSONObjectIfPresent(text, "task_completion")
	if !ok {
		return false, nil
	}
	var output completionTurnOutput
	if err := json.Unmarshal([]byte(candidate.Text), &output); err != nil {
		return false, nil
	}
	return output.TaskCompletion.Completed, nil
}

// extractPlanningJSONObjectIfPresent 在文本中提取首个配平的 JSON 对象。
type extractedPlanningJSONObject struct {
	Text  string
	Start int
	End   int
}

// decodePlanTurnOutput 按 planning 契约解析计划输出，并为摘要回退保留空间。
func decodePlanTurnOutput(jsonText string) (planTurnOutput, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonText), &payload); err != nil {
		return planTurnOutput{}, fmt.Errorf("runtime: decode planning json: %w", err)
	}

	planSpecRaw, ok := payload["plan_spec"]
	if !ok {
		return planTurnOutput{}, fmt.Errorf("runtime: planning json missing plan_spec")
	}

	var spec agentsession.PlanSpec
	if err := json.Unmarshal(planSpecRaw, &spec); err != nil {
		return planTurnOutput{}, fmt.Errorf("runtime: decode planning json plan_spec: %w", err)
	}
	spec, err := agentsession.NormalizePlanSpec(spec)
	if err != nil {
		return planTurnOutput{}, err
	}

	output := planTurnOutput{PlanSpec: spec}
	summaryRaw, ok := payload["summary_candidate"]
	if !ok || len(summaryRaw) == 0 || string(summaryRaw) == "null" {
		return output, nil
	}

	var candidate summaryCandidate
	if err := json.Unmarshal(summaryRaw, &candidate); err == nil {
		output.SummaryCandidate = candidate
	}
	return output, nil
}

// stripPlanningJSONObjectText 从原始回复中移除结构化 JSON，并尽量保留自然段落间距。
func stripPlanningJSONObjectText(text string, candidate extractedPlanningJSONObject) string {
	before := strings.TrimSpace(text[:candidate.Start])
	after := strings.TrimSpace(text[candidate.End:])
	switch {
	case before == "":
		return after
	case after == "":
		return before
	default:
		return strings.TrimSpace(before + "\n\n" + after)
	}
}

// extractPlanningJSONObjectIfPresent 在文本中提取首个满足指定顶层键契约的 JSON 对象。
func extractPlanningJSONObjectIfPresent(text string, requiredKey string) (extractedPlanningJSONObject, bool) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return extractedPlanningJSONObject{}, false
	}
	for {
		candidate, end, err := extractJSONObjectCandidateRange(text, start)
		if err == nil && jsonObjectContainsTopLevelKey(candidate, requiredKey) {
			return extractedPlanningJSONObject{
				Text:  candidate,
				Start: start,
				End:   end,
			}, true
		}
		next := strings.IndexByte(text[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return extractedPlanningJSONObject{}, false
}

// jsonObjectContainsTopLevelKey 判断候选 JSON 对象是否包含指定顶层键。
func jsonObjectContainsTopLevelKey(text string, key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return false
	}
	_, ok := payload[key]
	return ok
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
// resolvePlanDisplayText 优先保留模型对计划的额外说明文本，缺失时回退为规范计划正文。
// resolvePlanDisplayText 优先保留模型对计划的额外说明文本，缺失时回退为规范计划正文。
func resolvePlanDisplayText(output planTurnOutput, spec agentsession.PlanSpec) string {
	display := strings.TrimSpace(output.DisplayText)
	if display != "" {
		return display
	}
	return strings.TrimSpace(agentsession.RenderPlanContent(spec))
}

func applyCurrentPlanRevision(session *agentsession.Session, plan *agentsession.PlanArtifact) bool {
	if session == nil || plan == nil {
		return false
	}
	// 新 revision 覆盖时，仅取消旧 plan 明确引用的非终态 todo
	if oldPlan := session.CurrentPlan; oldPlan != nil && oldPlan.Revision < plan.Revision {
		agentsession.CancelTodosByIDs(session.Todos, oldPlan.Summary.ActiveTodoIDs)
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
