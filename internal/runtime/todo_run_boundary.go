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

// shouldResetTodosForUserRun 判断本轮用户输入是否应开启新的 Todo 边界。
// 策略：默认保留旧 Todo，由 prompt 层 stale_todo_reminder 引导模型自行清理；
// 仅当用户输入极少且明确的"全新任务"表达时，才主动清空，避免硬编码过度覆盖。
func shouldResetTodosForUserRun(userGoal string) bool {
	goal := strings.ToLower(strings.TrimSpace(userGoal))
	if goal == "" {
		return false
	}
	goal = strings.TrimRight(goal, " 。.!！?？,，;；~～")
	if goal == "" {
		return false
	}
	return isExplicitNewTaskIntent(goal)
}

// newTaskChineseKeywords 中文明确新任务关键词，仅含完全无歧义的表达。
var newTaskChineseKeywords = []string{"新任务", "换个任务", "换任务", "新需求"}

// newTaskEnglishKeywords 英文明确新任务关键词，仅含完全无歧义的表达。
var newTaskEnglishKeywords = []string{"new task", "different task", "switch task"}

// isExplicitNewTaskIntent 判断标准化后的 goal 是否含明确的新任务意图。
// 默认返回 false，仅匹配极少且高度精确的关键词。
func isExplicitNewTaskIntent(goal string) bool {
	for _, kw := range newTaskChineseKeywords {
		if strings.Contains(goal, kw) {
			return true
		}
	}
	for _, kw := range newTaskEnglishKeywords {
		if goal == kw {
			return true
		}
		if strings.HasPrefix(goal, kw+" ") || strings.HasPrefix(goal, kw+"\t") {
			return true
		}
	}
	return false
}
