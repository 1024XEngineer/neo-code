package runtime

import (
	"context"

	agentsession "neo-code/internal/session"
)

const todoBootstrapRequiredReason = "todo_bootstrap_required"

const todoBootstrapRequiredReminder = `[Runtime Control]

todo_bootstrap_required: This build run has no current plan and no active todos.

Before project analysis, documentation writing, code changes, multi-step debugging, or verification work, call todo_write with action=plan or action=add to create required todos for this run.

Do not update or complete old todo IDs that are not present in the current Todo State.`

// maybeAppendTodoBootstrapReminder 在 direct build 缺少 plan/todo 时注入一次结构化提醒。
func (s *Service) maybeAppendTodoBootstrapReminder(ctx context.Context, state *runState) error {
	if !shouldInjectTodoBootstrapReminder(state) {
		return nil
	}
	return s.appendSystemMessageAndSave(ctx, state, todoBootstrapRequiredReminder)
}

// shouldInjectTodoBootstrapReminder 判断本轮 build 是否需要先创建当前 run 的 todo。
func shouldInjectTodoBootstrapReminder(state *runState) bool {
	if state == nil || state.disableTools || !state.planningEnabled {
		return false
	}
	state.mu.Lock()
	session := state.session
	state.mu.Unlock()

	if agentsession.NormalizeAgentMode(session.AgentMode) != agentsession.AgentModeBuild {
		return false
	}
	if hasActivePlanForTodoBootstrap(session.CurrentPlan) || len(session.Todos) > 0 {
		return false
	}
	return true
}

// hasActivePlanForTodoBootstrap 判断当前 plan 是否仍可为 build 继承 todo 所有权。
func hasActivePlanForTodoBootstrap(plan *agentsession.PlanArtifact) bool {
	if plan == nil {
		return false
	}
	switch agentsession.NormalizePlanStatus(plan.Status) {
	case agentsession.PlanStatusDraft, agentsession.PlanStatusApproved:
		return true
	default:
		return false
	}
}

const planBootstrapRequiredReason = "plan_bootstrap_required"

const planBootstrapRequiredReminder = `[Runtime Control]

plan_bootstrap_required: You are in plan mode but no current plan exists.

Before research, analysis, or conversational response, you MUST complete the following:

1. Research the codebase as needed using read-only tools.
2. Output a JSON object containing "plan_spec" and "summary_candidate" that defines the current plan.
3. plan_spec.todos must be non-empty — include major actionable items with unique IDs and status "pending".

Do not end this turn without producing a plan.`

// maybeAppendPlanBootstrapReminder 在 plan 模式缺少 CurrentPlan 时注入一次结构化提醒。
func (s *Service) maybeAppendPlanBootstrapReminder(ctx context.Context, state *runState) error {
	if !shouldInjectPlanBootstrapReminder(state) {
		return nil
	}
	return s.appendSystemMessageAndSave(ctx, state, planBootstrapRequiredReminder)
}

// shouldInjectPlanBootstrapReminder 判断本轮 plan 模式是否需要先创建 plan。
func shouldInjectPlanBootstrapReminder(state *runState) bool {
	if state == nil || state.disableTools || !state.planningEnabled {
		return false
	}
	if resolvePlanningStageForState(state) != planStagePlan {
		return false
	}
	state.mu.Lock()
	plan := state.session.CurrentPlan
	state.mu.Unlock()
	return plan == nil
}
