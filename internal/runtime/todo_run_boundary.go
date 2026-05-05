package runtime

import (
	"context"
	"strings"
	"time"

	runtimefacts "neo-code/internal/runtime/facts"
)

// resetTodosForUserRun 清空新用户 Run 的当前 Todo 状态，避免上一任务遗留的 open todo 阻塞本轮验收。
func (s *Service) resetTodosForUserRun(ctx context.Context, state *runState) error {
	if s == nil || state == nil {
		return nil
	}
	if !shouldResetTodosForUserRun(state.userGoal) {
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

// shouldResetTodosForUserRun 判断本轮用户输入是否应开启新的 Todo 边界，续做类输入保留旧 Todo。
func shouldResetTodosForUserRun(userGoal string) bool {
	goal := strings.ToLower(strings.TrimSpace(userGoal))
	if goal == "" {
		return false
	}
	switch goal {
	case "continue", "继续", "继续执行", "继续任务", "继续上一个任务":
		return false
	default:
		return true
	}
}
