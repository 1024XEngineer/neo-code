package context

import (
	"strings"
	"unicode/utf8"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

const (
	recentWindowAbsoluteMessageLimit = 24
	recentWindowToolContentCharLimit = 600
)

// ProjectToolMessagesForModel 原地投影 tool 消息，复用主链路对模型可见的只读格式化规则。
func ProjectToolMessagesForModel(messages []providertypes.Message) []providertypes.Message {
	for i := range messages {
		message := messages[i]
		if !isProjectableToolMessage(message) {
			continue
		}
		messages[i].Parts = []providertypes.ContentPart{providertypes.NewTextPart(tools.FormatToolMessageForModel(message))}
		messages[i].ToolMetadata = nil
	}
	return messages
}

// BuildRecentMessagesForModel 从会话尾部构造 provider-safe 的最近消息窗口，避免保留非法 tool call 片段。
func BuildRecentMessagesForModel(messages []providertypes.Message, limit int) []providertypes.Message {
	if len(messages) == 0 || limit <= 0 {
		return nil
	}

	keep := make([]bool, len(messages))
	anchors := 0
	keptMessages := 0
	maxMessages := recentWindowMessageBudget(limit)

	markSpan := func(span []int) bool {
		if len(span) == 0 || keptMessages+len(span) > maxMessages {
			return false
		}
		for _, spanIndex := range span {
			keep[spanIndex] = true
		}
		keptMessages += len(span)
		anchors++
		return true
	}

	for index := len(messages) - 1; index >= 0 && anchors < limit; index-- {
		message := messages[index]
		if message.Role == providertypes.RoleTool {
			continue
		}

		if message.Role == providertypes.RoleAssistant && len(message.ToolCalls) > 0 {
			markSpan(matchedToolCallSpan(messages, index))
			continue
		}

		if !markSpan([]int{index}) {
			break
		}
	}

	selected := make([]providertypes.Message, 0, limit)
	for index, message := range messages {
		if !keep[index] {
			continue
		}
		selected = append(selected, message)
	}
	if len(selected) == 0 {
		return nil
	}

	return sanitizeRecentWindowToolMessages(ProjectToolMessagesForModel(cloneContextMessages(selected)))
}

// matchedToolCallSpan 返回 assistant tool call 与其完整 tool 响应组成的合法窗口下标集合。
func matchedToolCallSpan(messages []providertypes.Message, assistantIndex int) []int {
	if assistantIndex < 0 || assistantIndex >= len(messages) {
		return nil
	}

	message := messages[assistantIndex]
	if message.Role != providertypes.RoleAssistant || len(message.ToolCalls) == 0 {
		return nil
	}

	required := make(map[string]struct{}, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			return nil
		}
		required[callID] = struct{}{}
	}
	if len(required) == 0 {
		return nil
	}

	matched := make(map[string]struct{}, len(required))
	toolIndexes := make([]int, 0, len(required))
	for index := assistantIndex + 1; index < len(messages); index++ {
		toolMessage := messages[index]
		if toolMessage.Role != providertypes.RoleTool {
			break
		}
		if !isProjectableToolMessage(toolMessage) {
			continue
		}

		callID := strings.TrimSpace(toolMessage.ToolCallID)
		if _, ok := required[callID]; !ok {
			continue
		}
		if _, exists := matched[callID]; exists {
			continue
		}
		matched[callID] = struct{}{}
		toolIndexes = append(toolIndexes, index)
	}

	if len(matched) != len(required) {
		return nil
	}

	span := make([]int, 0, len(toolIndexes)+1)
	span = append(span, assistantIndex)
	span = append(span, toolIndexes...)
	return span
}

// isProjectableToolMessage 判断 tool 消息是否满足“可注入且可投影”条件。
func isProjectableToolMessage(message providertypes.Message) bool {
	return isInjectableToolMessage(message) && len(message.ToolMetadata) > 0
}

// isInjectableToolMessage 判断 tool 消息是否仍适合作为模型可见上下文继续注入。
func isInjectableToolMessage(message providertypes.Message) bool {
	if message.Role != providertypes.RoleTool {
		return false
	}
	content := strings.TrimSpace(renderDisplayParts(message.Parts))
	if content == microCompactClearedMessage {
		return false
	}
	return content != "" || len(message.ToolMetadata) > 0
}

// recentWindowMessageBudget 计算 recent window 可保留的消息总数硬上限，避免窗口体积失控。
func recentWindowMessageBudget(limit int) int {
	if limit <= 0 {
		return 0
	}
	budget := limit * 2
	if budget < limit {
		budget = limit
	}
	if budget > recentWindowAbsoluteMessageLimit {
		budget = recentWindowAbsoluteMessageLimit
	}
	return budget
}

// sanitizeRecentWindowToolMessages 缩减 tool 消息内容，降低 memo 提取链路对原始工具输出的暴露面。
func sanitizeRecentWindowToolMessages(messages []providertypes.Message) []providertypes.Message {
	for index := range messages {
		message := messages[index]
		if message.Role != providertypes.RoleTool {
			continue
		}
		messages[index].Parts = []providertypes.ContentPart{providertypes.NewTextPart(sanitizeProjectedToolContent(renderTranscriptParts(message.Parts)))}
	}
	return messages
}

// sanitizeProjectedToolContent 将投影后的 tool content 限制为固定长度摘要，避免注入完整原始输出。
func sanitizeProjectedToolContent(content string) string {
	const contentMarker = "\ncontent:\n"

	index := strings.Index(content, contentMarker)
	if index < 0 {
		return sanitizeRawToolContent(content)
	}

	prefix := strings.TrimRight(content[:index], "\n")
	body := strings.TrimSpace(content[index+len(contentMarker):])
	if body == "" {
		return prefix
	}

	limited, truncated := truncateUTF8(body, recentWindowToolContentCharLimit)
	lines := []string{prefix, "content_excerpt:", limited}
	if truncated {
		lines = append(lines, "[content truncated for memo extraction]")
	}
	return strings.Join(lines, "\n")
}

// sanitizeRawToolContent 对未命中投影标记的 tool 文本做保底摘要化，避免透传完整原始输出。
func sanitizeRawToolContent(content string) string {
	body := strings.TrimSpace(content)
	if body == "" {
		return ""
	}
	limited, truncated := truncateUTF8(body, recentWindowToolContentCharLimit)
	if !truncated {
		return body
	}
	return strings.Join([]string{
		"content_excerpt:",
		limited,
		"[content truncated for memo extraction]",
	}, "\n")
}

// truncateUTF8 按 rune 数量截断字符串，返回截断后的文本及是否发生截断。
func truncateUTF8(text string, maxRunes int) (string, bool) {
	if maxRunes <= 0 || text == "" {
		return "", text != ""
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text, false
	}

	count := 0
	for index := range text {
		if count == maxRunes {
			return text[:index], true
		}
		count++
	}
	return text, false
}
