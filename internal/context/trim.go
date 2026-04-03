package context

import "neo-code/internal/provider"

const maxContextTurns = 10

// trimMessages 将历史消息裁剪到最近 maxContextTurns 个“对话片段”。
// 片段定义：普通单条消息算一个片段；assistant(tool_calls)+后续tool结果算一个整体片段。
// 这样可以在压缩上下文时尽量保持 tool_call 与 tool_result 的配对完整性。
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

		// assistant 触发工具后，连续的 tool 消息都归属到同一片段。
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

	// 仅保留最近 N 个片段，从片段起点切割，避免破坏消息对齐关系。
	start := spans[len(spans)-maxContextTurns].start
	return append([]provider.Message(nil), messages[start:]...)
}
