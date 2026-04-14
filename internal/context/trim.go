package context

import (
	"neo-code/internal/config"
	"neo-code/internal/context/internalcompact"
	providertypes "neo-code/internal/provider/types"
)

// trimMessages 按消息分段裁剪上下文，并始终保护最后一条明确用户指令所在尾部。
func trimMessages(messages []providertypes.Message, maxRetainedMessageSpans int) []providertypes.Message {
	if maxRetainedMessageSpans <= 0 {
		maxRetainedMessageSpans = config.DefaultCompactReadTimeMaxMessageSpans
	}

	spans := internalcompact.BuildMessageSpans(messages)
	if len(spans) <= maxRetainedMessageSpans {
		return append([]providertypes.Message(nil), messages...)
	}

	start := spans[len(spans)-maxRetainedMessageSpans].Start
	if protectedStart, ok := internalcompact.ProtectedTailStart(spans); ok && protectedStart < start {
		start = protectedStart
	}
	return append([]providertypes.Message(nil), messages[start:]...)
}
