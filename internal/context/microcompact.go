package context

import (
	"strings"

	"neo-code/internal/context/internalcompact"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

const (
	// microCompactClearedMessage 是旧工具结果被读时微压缩后的占位符文本。
	microCompactClearedMessage = "[Old tool result content cleared]"
	// defaultMicroCompactRetainedToolSpans 定义 micro compact 默认保留原始内容的最近可压缩工具块数量。
	defaultMicroCompactRetainedToolSpans = 2
	// microCompactSummaryMaxRunes 是摘要回灌到上下文前允许的最大 rune 数量。
	microCompactSummaryMaxRunes = 200
)

// microCompactMessages 对裁剪后的消息做只读投影式微压缩，优先摘要旧工具结果，失败时回退清理占位。
func microCompactMessages(messages []providertypes.Message) []providertypes.Message {
	return microCompactMessagesWithPolicies(messages, nil, 0, nil)
}

// microCompactMessagesWithPolicies 按工具策略对裁剪后的消息做只读投影式微压缩。
func microCompactMessagesWithPolicies(messages []providertypes.Message, policies MicroCompactPolicySource, retainedToolSpans int, summarizers MicroCompactSummarizerSource) []providertypes.Message {
	if retainedToolSpans <= 0 {
		retainedToolSpans = defaultMicroCompactRetainedToolSpans
	}

	cloned := cloneContextMessages(messages)
	if len(cloned) == 0 {
		return cloned
	}

	spans := internalcompact.BuildMessageSpans(cloned)
	protectedStart, hasProtectedTail := internalcompact.ProtectedTailStart(spans)
	retainedCompactableSpans := 0

	for spanIndex := len(spans) - 1; spanIndex >= 0; spanIndex-- {
		span := spans[spanIndex]
		if hasProtectedTail && span.Start >= protectedStart {
			continue
		}
		if !isToolCallSpan(cloned, span) {
			continue
		}

		compactableIDs, toolNames := compactableToolCallIDs(cloned[span.Start].ToolCalls, policies)
		if len(compactableIDs) == 0 {
			continue
		}
		if retainedCompactableSpans < retainedToolSpans {
			if hasCompactableToolMessage(cloned, span, compactableIDs) {
				retainedCompactableSpans++
			}
			continue
		}

		compactableContents := compactableToolMessageContents(cloned, span, compactableIDs)
		if len(compactableContents) == 0 {
			continue
		}

		for messageIndex := span.Start + 1; messageIndex < span.End; messageIndex++ {
			content, ok := compactableContents[messageIndex]
			if !ok {
				continue
			}
			summary := summarizeOrClear(cloned[messageIndex], content, toolNames, summarizers)
			cloned[messageIndex].Parts = []providertypes.ContentPart{providertypes.NewTextPart(summary)}
		}
	}

	return cloned
}

// cloneContextMessages 深拷贝消息切片，避免读时投影污染 runtime 持有的原始会话消息。
func cloneContextMessages(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]providertypes.Message, 0, len(messages))
	for _, message := range messages {
		next := message
		next.ToolCalls = append([]providertypes.ToolCall(nil), message.ToolCalls...)
		if len(message.ToolMetadata) > 0 {
			next.ToolMetadata = make(map[string]string, len(message.ToolMetadata))
			for key, value := range message.ToolMetadata {
				next.ToolMetadata[key] = value
			}
		}
		cloned = append(cloned, next)
	}
	return cloned
}

// isToolCallSpan 判断当前 span 是否是由 assistant tool call 起始的原子工具块。
func isToolCallSpan(messages []providertypes.Message, span internalcompact.MessageSpan) bool {
	if span.Start < 0 || span.Start >= len(messages) {
		return false
	}
	message := messages[span.Start]
	return message.Role == providertypes.RoleAssistant && len(message.ToolCalls) > 0
}

// compactableToolCallIDs 返回 assistant tool call 中可参与微压缩的调用 ID 集合及对应的工具名映射。
func compactableToolCallIDs(calls []providertypes.ToolCall, policies MicroCompactPolicySource) (map[string]struct{}, map[string]string) {
	if len(calls) == 0 {
		return nil, nil
	}

	ids := make(map[string]struct{}, len(calls))
	toolNames := make(map[string]string, len(calls))
	for _, call := range calls {
		toolName := strings.TrimSpace(call.Name)
		if !toolParticipatesInMicroCompact(toolName, policies) {
			continue
		}
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			continue
		}
		ids[callID] = struct{}{}
		toolNames[callID] = toolName
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, toolNames
}

// toolParticipatesInMicroCompact 判断工具是否应参与 micro compact；未知工具默认视为可压缩。
func toolParticipatesInMicroCompact(toolName string, policies MicroCompactPolicySource) bool {
	if policies == nil {
		return true
	}
	return policies.MicroCompactPolicy(toolName) != tools.MicroCompactPolicyPreserveHistory
}

// compactableToolMessageContents 收集工具块中可压缩消息的渲染内容，避免重复渲染。
func compactableToolMessageContents(messages []providertypes.Message, span internalcompact.MessageSpan, compactableIDs map[string]struct{}) map[int]string {
	var contents map[int]string
	for messageIndex := span.Start + 1; messageIndex < span.End; messageIndex++ {
		content, ok := compactableToolMessageContent(messages[messageIndex], compactableIDs)
		if !ok {
			continue
		}
		if contents == nil {
			contents = make(map[int]string)
		}
		contents[messageIndex] = content
	}
	return contents
}

// hasCompactableToolMessage 判断工具块中是否存在至少一条可压缩的工具消息。
func hasCompactableToolMessage(messages []providertypes.Message, span internalcompact.MessageSpan, compactableIDs map[string]struct{}) bool {
	for messageIndex := span.Start + 1; messageIndex < span.End; messageIndex++ {
		if _, ok := compactableToolMessageContent(messages[messageIndex], compactableIDs); ok {
			return true
		}
	}
	return false
}

// compactableToolMessageContent 判断 tool 消息是否可压缩，并返回渲染后的内容文本。
func compactableToolMessageContent(message providertypes.Message, compactableIDs map[string]struct{}) (string, bool) {
	if message.Role != providertypes.RoleTool || message.IsError {
		return "", false
	}
	callID := strings.TrimSpace(message.ToolCallID)
	if _, ok := compactableIDs[callID]; !ok {
		return "", false
	}

	content := strings.TrimSpace(renderDisplayParts(message.Parts))
	if content == "" || content == microCompactClearedMessage {
		return "", false
	}
	return content, true
}

// summarizeOrClear 为单条可压缩工具消息生成摘要或回退到默认清除占位。
func summarizeOrClear(
	message providertypes.Message,
	content string,
	toolNames map[string]string,
	summarizers MicroCompactSummarizerSource,
) string {
	if summarizers == nil {
		return microCompactClearedMessage
	}

	callID := strings.TrimSpace(message.ToolCallID)
	toolName, ok := toolNames[callID]
	if !ok {
		return microCompactClearedMessage
	}

	summarizer := summarizers.MicroCompactSummarizer(toolName)
	if summarizer == nil {
		return microCompactClearedMessage
	}

	summary := summarizer(content, message.ToolMetadata, message.IsError)
	if summary == "" {
		return microCompactClearedMessage
	}
	summary = sanitizeMicroCompactSummary(summary)
	if summary == "" {
		return microCompactClearedMessage
	}
	return summary
}

// sanitizeMicroCompactSummary 对 summarizer 输出做最终净化与限长，避免把不安全文本直接回灌上下文。
func sanitizeMicroCompactSummary(summary string) string {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}

	clean := strings.TrimSpace(b.String())
	if clean == "" {
		return ""
	}
	return truncateSummaryRunes(clean, microCompactSummaryMaxRunes)
}

// truncateSummaryRunes 按 rune 数量截断摘要，超限时追加 "..."。
func truncateSummaryRunes(summary string, maxRunes int) string {
	if maxRunes <= 0 || summary == "" {
		return summary
	}

	runes := []rune(summary)
	if len(runes) <= maxRunes {
		return summary
	}
	return string(runes[:maxRunes]) + "..."
}
