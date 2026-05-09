package runtime

import (
	"context"
	"strings"

	"neo-code/internal/promptasset"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/acceptgate"
	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

const missingCompletionSignalLimit = 6

// completionProtocolReminderForStreak 根据连续缺失完成信号的次数返回对应协议提示。
func completionProtocolReminderForStreak(streak int) string {
	if streak >= missingCompletionSignalLimit-1 {
		return promptasset.CompletionProtocolFinalReminder()
	}
	return promptasset.CompletionProtocolReminder()
}

// evaluateAcceptGate 从运行态提取事实快照，并执行最终 Accept Gate。
func (s *Service) evaluateAcceptGate(ctx context.Context, state *runState, assistantMessage providertypes.Message) acceptgate.Report {
	if state == nil {
		return acceptgate.Evaluate(ctx, acceptgate.Input{})
	}
	state.mu.Lock()
	var planVerify agentsession.AcceptChecks
	var currentPlan *agentsession.PlanArtifact
	if state.session.CurrentPlan != nil {
		currentPlan = state.session.CurrentPlan.Clone()
		planVerify = currentPlan.Summary.Verify.Clone()
		if len(planVerify) == 0 {
			planVerify = currentPlan.Spec.Verify.Clone()
		}
	}
	todos := selectPlanOwnedTodos(currentPlan, cloneTodosForPersistence(state.session.Todos))
	factsSnapshot := runtimefacts.RuntimeFacts{}
	if state.factsCollector != nil {
		factsSnapshot = state.factsCollector.Snapshot()
	}
	state.mu.Unlock()

	return acceptgate.Evaluate(ctx, acceptgate.Input{
		PlanVerify:        planVerify,
		Facts:             factsSnapshot,
		Todos:             todos,
		LastAssistantText: renderAssistantTextWithoutCompletion(assistantMessage),
	})
}

// selectPlanOwnedTodos 只把当前计划拥有的 todo 交给终态验收，避免无 plan 的 chat/read-only 被旧 todo 污染。
func selectPlanOwnedTodos(plan *agentsession.PlanArtifact, todos []agentsession.TodoItem) []agentsession.TodoItem {
	if plan == nil || len(todos) == 0 {
		return nil
	}
	owned := make(map[string]struct{})
	for _, id := range plan.Summary.ActiveTodoIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			owned[id] = struct{}{}
		}
	}
	for _, todo := range plan.Spec.Todos {
		id := strings.TrimSpace(todo.ID)
		if id != "" {
			owned[id] = struct{}{}
		}
	}
	selected := make([]agentsession.TodoItem, 0, len(todos))
	for _, todo := range todos {
		if _, ok := owned[strings.TrimSpace(todo.ID)]; ok {
			selected = append(selected, todo)
			continue
		}
		if isPostPlanRequiredTodo(plan, todo) {
			selected = append(selected, todo)
		}
	}
	return selected
}

// isPostPlanRequiredTodo 判断计划执行期新增的必需 todo 是否应纳入当前计划验收。
func isPostPlanRequiredTodo(plan *agentsession.PlanArtifact, todo agentsession.TodoItem) bool {
	if plan == nil || !todo.RequiredValue() || todo.Status.IsTerminal() {
		return false
	}
	if plan.CreatedAt.IsZero() || todo.CreatedAt.IsZero() {
		return false
	}
	return !todo.CreatedAt.Before(plan.CreatedAt)
}

// emitAcceptGateReport 将 Accept Gate 报告发布为统一 acceptance_decided 事件。
func (s *Service) emitAcceptGateReport(state *runState, report acceptgate.Report) {
	status := string(acceptgate.OutcomeFailed)
	if report.Outcome == acceptgate.OutcomeAccepted {
		status = string(acceptgate.OutcomeAccepted)
	}
	s.emitRunScopedOptional(EventAcceptanceDecided, state, AcceptanceDecidedPayload{
		Status:     status,
		StopReason: report.StopReason,
		Summary:    report.Summary,
		Results:    append([]acceptgate.CheckResult(nil), report.Results...),
	})
}

func renderAssistantTextWithoutCompletion(message providertypes.Message) string {
	text := strings.TrimSpace(renderPartsForVerification(message.Parts))
	if text == "" {
		return ""
	}
	candidate, ok := extractPlanningJSONObjectIfPresent(text, "task_completion")
	if !ok {
		return text
	}
	return strings.TrimSpace(stripPlanningJSONObjectText(text, candidate))
}

// stripCompletionSignalFromAssistantMessage 移除仅供 runtime 控制使用的 task_completion JSON，保留用户可见回复。
func stripCompletionSignalFromAssistantMessage(message providertypes.Message) providertypes.Message {
	text := renderAssistantTextWithoutCompletion(message)
	if strings.TrimSpace(text) == strings.TrimSpace(renderPartsForVerification(message.Parts)) {
		return message
	}
	message.Parts = []providertypes.ContentPart{providertypes.NewTextPart(text)}
	return message
}
