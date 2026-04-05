package compact

import (
	"unicode/utf8"

	"neo-code/internal/provider"
)

// cloneMessages 深拷贝消息切片，避免后续规划或摘要阶段共享底层数据。
func cloneMessages(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]provider.Message, 0, len(messages))
	for _, message := range messages {
		next := message
		next.ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
		out = append(out, next)
	}
	return out
}

// countMessageChars 以 rune 数量统计消息体积，用于 compact 前后指标计算。
func countMessageChars(messages []provider.Message) int {
	total := 0
	for _, message := range messages {
		total += utf8.RuneCountInString(message.Role)
		total += utf8.RuneCountInString(message.Content)
		total += utf8.RuneCountInString(message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += utf8.RuneCountInString(call.ID)
			total += utf8.RuneCountInString(call.Name)
			total += utf8.RuneCountInString(call.Arguments)
		}
	}
	return total
}
