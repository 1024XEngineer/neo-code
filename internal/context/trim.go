package context

import "neo-code/internal/provider"

const maxContextTurns = 10

// trimMessages 将消息窗口裁剪为最近 N 个“对话跨度”。
// 当 assistant 含 tool_calls 时，会把后续连续 tool 结果视为同一跨度，避免打断调用链。
func trimMessages(messages []provider.Message) []provider.Message {
	if len(messages) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	type span struct {
		start int
		end   int
	}

	spans := make([]span, 0, len(messages))
	for i := 0; i < len(messages); {
		start := i
		i++

		if messages[start].Role == provider.RoleAssistant && len(messages[start].ToolCalls) > 0 {
			for i < len(messages) && messages[i].Role == provider.RoleTool {
				i++
			}
		}

		spans = append(spans, span{start: start, end: i})
	}

	if len(spans) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	start := spans[len(spans)-maxContextTurns].start
	return append([]provider.Message(nil), messages[start:]...)
}
