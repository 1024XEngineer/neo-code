package runtime

import (
	"context"
	"time"

	runtimefacts "neo-code/internal/runtime/facts"
	agentsession "neo-code/internal/session"
)

// resetTodosForUserRun 清空新用户 Run 的当前 Todo 状态，避免上一任务遗留的 open todo 阻塞本轮验收。
func (s *Service) resetTodosForUserRun(ctx context.Context, state *runState) error {
	if s == nil || state == nil {
		return nil
	}
	if !shouldResetTodosForUserRun(state.session) {
		return nil
	}

	state.mu.Lock()
	if len(state.session.Todos) == 0 {
		state.mu.Unlock()
		return nil
	}
	state.session.Todos = nil
	state.session.UpdatedAt = time.Now()
	if state.factsCollector != nil {
		state.factsCollector.ApplyTodoSnapshot(runtimefacts.TodoSummaryLike{})
	}
	sessionSnapshot := cloneSessionForPersistence(state.session)
	state.mu.Unlock()

	if err := s.sessionStore.UpdateSessionState(ctx, sessionStateInputFromSession(sessionSnapshot)); err != nil {
		return err
	}

	payload := buildTodoEventPayload(state, "reset", "new_user_run")
	s.emitRunScoped(ctx, EventTodoSnapshotUpdated, state, payload)
	s.emitRuntimeSnapshotUpdated(ctx, state, "todo_reset")
	return nil
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
