package context

import (
	"neo-code/internal/context/internalcompact"
	"neo-code/internal/provider"
)

const maxRetainedMessageSpans = 10

// trimMessages 按消息分段裁剪上下文，并始终保护最近一条明确用户指令所在尾部。
func trimMessages(messages []provider.Message) []provider.Message {
	spans := internalcompact.BuildMessageSpans(messages)
	if len(spans) <= maxRetainedMessageSpans {
		return append([]provider.Message(nil), messages...)
	}

	start := spans[len(spans)-maxRetainedMessageSpans].Start
	if protectedStart, ok := internalcompact.ProtectedTailStart(spans); ok && protectedStart < start {
		start = protectedStart
	}
	return append([]provider.Message(nil), messages[start:]...)
}
