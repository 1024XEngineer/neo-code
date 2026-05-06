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
// 识别策略：去掉尾部标点 → 中文用前缀匹配，英文用单词边界匹配，覆盖
// "继续修这个" / "continue with X" / "接着做" / "继续。" / "Continue!" / "keep going" 等常见变体。
func shouldResetTodosForUserRun(userGoal string) bool {
	goal := strings.ToLower(strings.TrimSpace(userGoal))
	if goal == "" {
		return false
	}
	goal = strings.TrimRight(goal, " 。.!！?？,，;；~～")
	if goal == "" {
		return false
	}
	if isContinuationIntent(goal) {
		return false
	}
	return true
}

// continuationChinesePrefixes 中文续做关键词，落到 strings.HasPrefix 直接匹配。
var continuationChinesePrefixes = []string{"继续", "接着", "续做", "再继续", "再来"}

// continuationEnglishPrefixes 英文续做关键词，要求精确匹配或后跟空格（避免 "keep it simple" 误命中）。
var continuationEnglishPrefixes = []string{"continue", "keep going", "keep doing", "go on", "resume", "carry on"}

// isContinuationIntent 判断标准化后的 goal 是否含续做意图。
func isContinuationIntent(goal string) bool {
	for _, kw := range continuationChinesePrefixes {
		if strings.HasPrefix(goal, kw) {
			return true
		}
	}
	for _, kw := range continuationEnglishPrefixes {
		if goal == kw {
			return true
		}
		if strings.HasPrefix(goal, kw+" ") || strings.HasPrefix(goal, kw+"\t") {
			return true
		}
	}
	return false
}
