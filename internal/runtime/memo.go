package runtime

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

// triggerMemoExtraction 在 Run 结束后异步触发记忆提取，避免阻塞主闭环。
func (s *Service) triggerMemoExtraction(sessionID string, messages []providertypes.Message, skip bool) {
	if s == nil || s.memoExtractor == nil || len(messages) == 0 {
		return
	}
	if skip {
		return
	}

	s.memoExtractor.Schedule(sessionID, cloneMessages(messages))
}

// runBoundaryMessagesForMemo 返回当前 run 边界内的消息切片，供自动记忆提取使用。
func runBoundaryMessagesForMemo(state *runState) []providertypes.Message {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return cloneMessages(state.memoRunMessages)
}

// appendMemoRunMessage 记录当前 run 内已成功写入 transcript 的消息，作为自动记忆提取边界。
func appendMemoRunMessage(state *runState, message providertypes.Message) {
	if state == nil || message.IsEmpty() {
		return
	}
	cloned := cloneMessages([]providertypes.Message{message})
	if len(cloned) == 0 {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.memoRunMessages = append(state.memoRunMessages, cloned[0])
}

// isSuccessfulRememberToolCall 判断工具调用是否成功完成显式记忆写入。
func isSuccessfulRememberToolCall(callName string, result tools.ToolResult, execErr error) bool {
	if execErr != nil || result.IsError {
		return false
	}
	return strings.TrimSpace(callName) == tools.ToolNameMemoRemember
}
