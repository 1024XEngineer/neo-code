package internalcompact

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// MessageSpan 描述一段不可拆分的消息区间，并携带是否需要保护的尾部语义。
type MessageSpan struct {
	Start        int
	End          int
	MessageCount int
	Protected    bool
}

// BuildMessageSpans 按工具调用原子块构建消息分段，并保护最后一条明确用户指令所在分段。
func BuildMessageSpans(messages []providertypes.Message) []MessageSpan {
	spans := make([]MessageSpan, 0, len(messages))
	for i := 0; i < len(messages); {
		start := i
		end := i + 1
		if messages[start].Role == providertypes.RoleAssistant && len(messages[start].ToolCalls) > 0 {
			for end < len(messages) && messages[end].Role == providertypes.RoleTool {
				end++
			}
		}

		spans = append(spans, MessageSpan{
			Start:        start,
			End:          end,
			MessageCount: end - start,
		})
		i = end
	}

	if lastUserIndex := lastExplicitUserMessageIndex(messages); lastUserIndex >= 0 {
		markSpanProtected(spans, lastUserIndex)
	}
	return spans
}

// ProtectedTailStart 返回必须原样保留的受保护尾部起点。
func ProtectedTailStart(spans []MessageSpan) (int, bool) {
	for _, span := range spans {
		if span.Protected {
			return span.Start, true
		}
	}
	return 0, false
}

// RetainedStartForKeepRecentMessages 计算按消息数保留最近上下文时的起始位置，并尊重受保护尾部。
func RetainedStartForKeepRecentMessages(spans []MessageSpan, keepMessages int) int {
	if len(spans) == 0 {
		return 0
	}

	retainedStart := spans[0].Start
	retainedMessages := 0
	for index := len(spans) - 1; index >= 0; index-- {
		retainedMessages += spans[index].MessageCount
		retainedStart = spans[index].Start
		if retainedMessages >= keepMessages {
			break
		}
	}

	if protectedStart, ok := ProtectedTailStart(spans); ok && protectedStart < retainedStart {
		retainedStart = protectedStart
	}
	return retainedStart
}

// lastExplicitUserMessageIndex 返回最后一条显式用户消息的位置，用于保护最近明确指令。
func lastExplicitUserMessageIndex(messages []providertypes.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == providertypes.RoleUser && hasExplicitUserInput(messages[index].Parts) {
			return index
		}
	}
	return -1
}

// hasExplicitUserInput 判断用户消息是否包含显式输入（非空文本或图片）。
func hasExplicitUserInput(parts []providertypes.ContentPart) bool {
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		case providertypes.ContentPartImage:
			if part.Image != nil {
				return true
			}
		}
	}
	return false
}

// markSpanProtected 将包含目标消息的分段标记为受保护分段。
func markSpanProtected(spans []MessageSpan, messageIndex int) {
	for index := range spans {
		if messageIndex >= spans[index].Start && messageIndex < spans[index].End {
			spans[index].Protected = true
			return
		}
	}
}
