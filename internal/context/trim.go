package context

import (
	"strings"

	"neo-code/internal/provider"
)

const maxContextTurns = 10

type trimStrategyInput struct {
	messages []provider.Message
	index    messageRelationIndex
	limit    int
}

type messageRelationIndex struct {
	assistantByToolCallID map[string]int
	toolsByToolCallID     map[string][]int
	rootIndexes           []int
}

func trimMessages(messages []provider.Message) []provider.Message {
	return trimMessagesWithStrategy(messages, selectRecentRootIndexes)
}

func trimMessagesWithStrategy(
	messages []provider.Message,
	selectRoots func(input trimStrategyInput) []int,
) []provider.Message {
	if len(messages) == 0 {
		return nil
	}

	index := buildMessageRelationIndex(messages)
	roots := selectRoots(trimStrategyInput{
		messages: messages,
		index:    index,
		limit:    maxContextTurns,
	})
	if len(roots) == 0 {
		return nil
	}

	return retainMessageClosure(messages, index, roots)
}

func buildMessageRelationIndex(messages []provider.Message) messageRelationIndex {
	index := messageRelationIndex{
		assistantByToolCallID: make(map[string]int),
		toolsByToolCallID:     make(map[string][]int),
	}

	for messageIndex, message := range messages {
		if message.Role != provider.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		for _, call := range message.ToolCalls {
			toolCallID := strings.TrimSpace(call.ID)
			if toolCallID == "" {
				continue
			}
			index.assistantByToolCallID[toolCallID] = messageIndex
		}
	}

	for messageIndex, message := range messages {
		if message.Role != provider.RoleTool {
			continue
		}
		toolCallID := strings.TrimSpace(message.ToolCallID)
		if toolCallID == "" {
			continue
		}
		index.toolsByToolCallID[toolCallID] = append(index.toolsByToolCallID[toolCallID], messageIndex)
	}

	index.rootIndexes = buildRootIndexes(messages, index)
	return index
}

func buildRootIndexes(messages []provider.Message, index messageRelationIndex) []int {
	roots := make([]int, 0, len(messages))
	for messageIndex := range messages {
		if index.isAssociatedToolMessage(messageIndex, messages) {
			continue
		}
		roots = append(roots, messageIndex)
	}
	return roots
}

func selectRecentRootIndexes(input trimStrategyInput) []int {
	if input.limit <= 0 || len(input.index.rootIndexes) == 0 {
		return nil
	}
	if len(input.index.rootIndexes) <= input.limit {
		return append([]int(nil), input.index.rootIndexes...)
	}
	return append([]int(nil), input.index.rootIndexes[len(input.index.rootIndexes)-input.limit:]...)
}

func retainMessageClosure(
	messages []provider.Message,
	index messageRelationIndex,
	initialIndexes []int,
) []provider.Message {
	if len(messages) == 0 || len(initialIndexes) == 0 {
		return nil
	}

	keep := make(map[int]struct{}, len(initialIndexes))
	queue := append([]int(nil), initialIndexes...)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current < 0 || current >= len(messages) {
			continue
		}
		if _, exists := keep[current]; exists {
			continue
		}

		keep[current] = struct{}{}
		for _, linked := range index.linkedIndexes(current, messages) {
			if _, exists := keep[linked]; exists {
				continue
			}
			queue = append(queue, linked)
		}
	}

	if len(keep) == len(messages) {
		return cloneMessages(messages)
	}

	trimmed := make([]provider.Message, 0, len(keep))
	for messageIndex, message := range messages {
		if _, exists := keep[messageIndex]; !exists {
			continue
		}
		trimmed = append(trimmed, cloneMessage(message))
	}
	return trimmed
}

func (index messageRelationIndex) isAssociatedToolMessage(messageIndex int, messages []provider.Message) bool {
	if messageIndex < 0 || messageIndex >= len(messages) {
		return false
	}

	message := messages[messageIndex]
	if message.Role != provider.RoleTool {
		return false
	}

	toolCallID := strings.TrimSpace(message.ToolCallID)
	if toolCallID == "" {
		return false
	}

	_, exists := index.assistantByToolCallID[toolCallID]
	return exists
}

func (index messageRelationIndex) linkedIndexes(messageIndex int, messages []provider.Message) []int {
	if messageIndex < 0 || messageIndex >= len(messages) {
		return nil
	}

	message := messages[messageIndex]
	switch message.Role {
	case provider.RoleAssistant:
		return index.toolIndexesForAssistant(message)
	case provider.RoleTool:
		toolCallID := strings.TrimSpace(message.ToolCallID)
		if toolCallID == "" {
			return nil
		}
		assistantIndex, exists := index.assistantByToolCallID[toolCallID]
		if !exists {
			return nil
		}
		return []int{assistantIndex}
	default:
		return nil
	}
}

func (index messageRelationIndex) toolIndexesForAssistant(message provider.Message) []int {
	if len(message.ToolCalls) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, len(message.ToolCalls))
	linked := make([]int, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		toolCallID := strings.TrimSpace(call.ID)
		if toolCallID == "" {
			continue
		}
		for _, toolIndex := range index.toolsByToolCallID[toolCallID] {
			if _, exists := seen[toolIndex]; exists {
				continue
			}
			seen[toolIndex] = struct{}{}
			linked = append(linked, toolIndex)
		}
	}
	return linked
}

func cloneMessages(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]provider.Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message provider.Message) provider.Message {
	cloned := message
	if len(message.ToolCalls) > 0 {
		cloned.ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
	}
	return cloned
}
