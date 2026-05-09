package session

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// RepairIncompleteToolCallTail 检查 transcript 末尾是否存在未闭合的 assistant tool_calls，
// 若缺少紧随其后的完整 tool 结果块，则截断该 assistant 消息及其后续残缺消息。
func RepairIncompleteToolCallTail(messages []providertypes.Message) ([]providertypes.Message, bool) {
	if len(messages) == 0 {
		return nil, false
	}

	for index := 0; index < len(messages); {
		spanEnd, complete, hasToolCalls := transcriptToolSpan(messages, index)
		if !hasToolCalls {
			index++
			continue
		}
		if !complete {
			return cloneTranscriptPrefix(messages[:index]), true
		}
		index = spanEnd + 1
	}

	return cloneTranscriptPrefix(messages), false
}

// TrimMessagesToLimitPreservingToolSpans 保留 transcript 的最近消息，同时避免从 assistant tool_calls
// 与其对应 tool 结果的原子块中间截断。
func TrimMessagesToLimitPreservingToolSpans(messages []providertypes.Message, limit int) []providertypes.Message {
	if limit <= 0 || len(messages) <= limit {
		return cloneTranscriptPrefix(messages)
	}

	start := len(messages) - limit
	start = advanceToTranscriptBoundary(messages, start)
	if start >= len(messages) {
		return nil
	}
	return cloneTranscriptPrefix(messages[start:])
}

// TrimPrefixCountPreservingToolSpans 计算在删除 transcript 前缀时的安全删除数量，
// 保证不会把 assistant tool_calls 与其紧随的 tool 结果块从中间切断。
func TrimPrefixCountPreservingToolSpans(messages []providertypes.Message, deleteCount int) int {
	if deleteCount <= 0 || len(messages) == 0 {
		return 0
	}
	if deleteCount >= len(messages) {
		return len(messages)
	}
	return advanceToTranscriptBoundary(messages, deleteCount)
}

// advanceToTranscriptBoundary 将起始位置推进到安全边界；如果当前位置落在 tool span 中间，
// 则整体跳过该 span，避免保留残缺的 tool 结果或 assistant tool_calls。
func advanceToTranscriptBoundary(messages []providertypes.Message, start int) int {
	if start <= 0 {
		return 0
	}
	if start >= len(messages) {
		return len(messages)
	}

	for index := 0; index < len(messages); {
		spanEnd, _, hasToolCalls := transcriptToolSpan(messages, index)
		if !hasToolCalls {
			index++
			continue
		}
		if start == index {
			return start
		}
		if start > index && start <= spanEnd {
			start = spanEnd + 1
			if start >= len(messages) {
				return len(messages)
			}
		}
		index = spanEnd + 1
	}
	return start
}

// transcriptToolSpan 识别从 assistant tool_calls 开始的连续 tool 结果块，
// 返回 span 结束位置、是否完整闭合，以及当前位置是否为 tool_calls 起点。
func transcriptToolSpan(messages []providertypes.Message, start int) (end int, complete bool, hasToolCalls bool) {
	if start < 0 || start >= len(messages) {
		return start, true, false
	}

	message := messages[start]
	if strings.TrimSpace(message.Role) != providertypes.RoleAssistant || len(message.ToolCalls) == 0 {
		return start, true, false
	}

	expected := make(map[string]struct{}, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			return start, false, true
		}
		expected[callID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(expected))
	end = start
	for index := start + 1; index < len(messages); index++ {
		next := messages[index]
		if strings.TrimSpace(next.Role) != providertypes.RoleTool {
			break
		}
		callID := strings.TrimSpace(next.ToolCallID)
		if _, ok := expected[callID]; !ok {
			break
		}
		if _, duplicated := seen[callID]; duplicated {
			break
		}
		seen[callID] = struct{}{}
		end = index
		if len(seen) == len(expected) {
			return end, true, true
		}
	}

	return end, false, true
}

// cloneTranscriptPrefix 深拷贝 transcript 切片，避免调用方共享底层消息结构。
func cloneTranscriptPrefix(messages []providertypes.Message) []providertypes.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]providertypes.Message, len(messages))
	for index, message := range messages {
		cloned[index] = cloneMessage(message)
	}
	return cloned
}
