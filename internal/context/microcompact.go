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
)

// microCompactMessages 对裁剪后的消息做只读投影式微压缩，仅清理旧工具结果内容。
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
		if !hasCompactableToolContent(cloned, span, compactableIDs) {
			continue
		}
		if retainedCompactableSpans < retainedToolSpans {
			retainedCompactableSpans++
			continue
		}

		for messageIndex := span.Start + 1; messageIndex < span.End; messageIndex++ {
			if shouldClearToolMessage(cloned[messageIndex], compactableIDs) {
				summary := summarizeOrClear(cloned[messageIndex], toolNames, summarizers)
				cloned[messageIndex].Parts = []providertypes.ContentPart{providertypes.NewTextPart(summary)}
			}
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

// hasCompactableToolContent 判断工具块中是否存在会影响保留预算的有效工具结果内容。
func hasCompactableToolContent(messages []providertypes.Message, span internalcompact.MessageSpan, compactableIDs map[string]struct{}) bool {
	for messageIndex := span.Start + 1; messageIndex < span.End; messageIndex++ {
		if shouldClearToolMessage(messages[messageIndex], compactableIDs) {
			return true
		}
	}
	return false
}

// shouldClearToolMessage 判断一条 tool 消息是否满足旧结果清理条件。
func shouldClearToolMessage(message providertypes.Message, compactableIDs map[string]struct{}) bool {
	if message.Role != providertypes.RoleTool || message.IsError {
		return false
	}
	if compactableIDs == nil {
		return false
	}
	if _, ok := compactableIDs[strings.TrimSpace(message.ToolCallID)]; !ok {
		return false
	}

	content := strings.TrimSpace(renderDisplayParts(message.Parts))
	return content != "" && content != microCompactClearedMessage
}

// summarizeOrClear 为单条可压缩工具消息生成摘要或回退到默认清除占位。
func summarizeOrClear(
	message providertypes.Message,
	toolNames map[string]string,
	summarizers MicroCompactSummarizerSource,
) string {
	if summarizers == nil {
		return microCompactClearedMessage
	}

	toolName, ok := toolNames[strings.TrimSpace(message.ToolCallID)]
	if !ok {
		return microCompactClearedMessage
	}

	summarizer := summarizers.MicroCompactSummarizer(toolName)
	if summarizer == nil {
		return microCompactClearedMessage
	}

	content := renderDisplayParts(message.Parts)
	summary := summarizer(content, message.ToolMetadata, message.IsError)
	if summary == "" {
		return microCompactClearedMessage
	}
	return summary
}
