package runtime

import (
	"context"
	"reflect"
	"time"

	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

// resetTodosForUserRun 清空新用户 Run 的当前 Todo 状态，避免上一任务遗留的 open todo 阻塞本轮验收。
func (s *Service) resetTodosForUserRun(ctx context.Context, state *runState) error {
	if s == nil || state == nil {
		return nil
	}

	state.mu.Lock()
	currentTodos := cloneTodosForPersistence(state.session.Todos)
	nextTodos, reason := todosForUserRunBoundary(state.session, currentTodos)
	if reflect.DeepEqual(currentTodos, nextTodos) {
		state.mu.Unlock()
		return nil
	}
	state.session.Todos = nextTodos
	state.session.UpdatedAt = time.Now()
	if state.factsCollector != nil {
		state.factsCollector.ApplyTodoSnapshot(todoSummaryLikeForItems(nextTodos))
	}
	sessionSnapshot := cloneSessionForPersistence(state.session)
	state.mu.Unlock()

	if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(sessionSnapshot)); err != nil {
		return err
	}

	payload := buildTodoEventPayload(state, "reset", reason)
	s.emitRunScoped(ctx, EventTodoSnapshotUpdated, state, payload)
	s.emitRuntimeSnapshotUpdated(ctx, state, "todo_reset")
	return nil
}

// todosForUserRunBoundary 返回新 Run 应继承的 todo 集合；active plan 只继承 plan-owned todo。
func todosForUserRunBoundary(session agentsession.Session, todos []agentsession.TodoItem) ([]agentsession.TodoItem, string) {
	if shouldResetTodosForUserRun(session) {
		return nil, "new_user_run"
	}
	return selectPlanOwnedTodos(session.CurrentPlan, todos), "plan_owned_prune"
}

// shouldResetTodosForUserRun 根据 PlanArtifact 生命周期判断本轮是否开启新的 Todo 边界。
func shouldResetTodosForUserRun(session agentsession.Session) bool {
	if session.CurrentPlan == nil {
		return true
	}
	switch agentsession.NormalizePlanStatus(session.CurrentPlan.Status) {
	case agentsession.PlanStatusDraft, agentsession.PlanStatusApproved:
		return false
	case agentsession.PlanStatusCompleted:
		return true
	default:
		return true
	}
}

// todoSummaryLikeForItems 将保留后的 todo 列表压缩成事实层需要的计数。
func todoSummaryLikeForItems(items []agentsession.TodoItem) runtimefacts.TodoSummaryLike {
	var summary runtimefacts.TodoSummaryLike
	for _, item := range items {
		if !item.RequiredValue() {
			continue
		}
		if item.Status.IsTerminal() {
			if item.Status == agentsession.TodoStatusFailed {
				summary.RequiredFailed++
			} else {
				summary.RequiredCompleted++
			}
			continue
		}
		summary.RequiredOpen++
	}
	return summary
}
