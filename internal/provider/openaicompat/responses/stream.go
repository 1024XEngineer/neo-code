package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/sse"
	providertypes "neo-code/internal/provider/types"
)

// ConsumeStream 消费 Responses SSE 响应并发出统一流式事件。
func ConsumeStream(
	ctx context.Context,
	body io.Reader,
	events chan<- providertypes.StreamEvent,
) error {
	reader := sse.NewBoundedReader(body)
	var (
		finishReason     string
		usage            providertypes.Usage
		done             bool
		toolCalls        = make(map[int]*providertypes.ToolCall)
		itemToolCallMap  = make(map[string]int)
		nextToolCallSlot int
	)

	dataLines := make([]string, 0, 4)

	processChunk := func(payload string) error {
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return fmt.Errorf("%sdecode stream chunk: %w", errorPrefix, err)
		}
		if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
			return errors.New(strings.TrimSpace(event.Error.Message))
		}

		switch strings.TrimSpace(event.Type) {
		case "response.output_text.delta":
			if err := provider.EmitTextDelta(ctx, events, event.Delta); err != nil {
				return err
			}
		case "response.function_call_arguments.delta":
			toolIndex := resolveToolCallIndex(event.OutputIndex, event.ItemID, itemToolCallMap, &nextToolCallSlot)
			delta := responseToolCallDelta{
				Index: toolIndex,
				ID:    strings.TrimSpace(event.ItemID),
				Function: responseFunctionCall{
					Arguments: event.Delta,
				},
				ArgumentsMode: responseToolCallArgumentsMergeAppend,
			}
			if err := mergeToolCallDelta(ctx, events, toolCalls, delta); err != nil {
				return err
			}
		case "response.output_item.added", "response.output_item.done":
			if event.Item == nil || strings.TrimSpace(event.Item.Type) != "function_call" {
				return nil
			}
			argumentsMode := responseToolCallArgumentsMergeAppend
			if strings.TrimSpace(event.Type) == "response.output_item.done" {
				argumentsMode = responseToolCallArgumentsMergeReplace
			}
			toolIndex := resolveToolCallIndex(event.OutputIndex, event.Item.ID, itemToolCallMap, &nextToolCallSlot)
			toolCallID := strings.TrimSpace(event.Item.CallID)
			if toolCallID == "" {
				toolCallID = strings.TrimSpace(event.Item.ID)
			}
			delta := responseToolCallDelta{
				Index: toolIndex,
				ID:    toolCallID,
				Function: responseFunctionCall{
					Name:      strings.TrimSpace(event.Item.Name),
					Arguments: event.Item.Arguments,
				},
				ArgumentsMode: argumentsMode,
			}
			if err := mergeToolCallDelta(ctx, events, toolCalls, delta); err != nil {
				return err
			}
		case "response.completed":
			done = true
			extractUsage(&usage, event.Response)
			finishReason = resolveFinishReason("completed", event.Response)
		case "response.incomplete":
			done = true
			extractUsage(&usage, event.Response)
			finishReason = resolveFinishReason("incomplete", event.Response)
		case "response.failed":
			if event.Response != nil && event.Response.Error != nil && strings.TrimSpace(event.Response.Error.Message) != "" {
				return errors.New(strings.TrimSpace(event.Response.Error.Message))
			}
			return errors.New("response failed")
		case "error":
			if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
				return errors.New(strings.TrimSpace(event.Error.Message))
			}
			return errors.New("response stream error")
		default:
			return nil
		}
		return nil
	}

	finishStream := func() error {
		reason := strings.TrimSpace(finishReason)
		if reason == "" {
			reason = "stop"
		}
		return provider.EmitMessageDone(ctx, events, reason, &usage)
	}

	flushPendingData := func() error {
		defer func() { dataLines = dataLines[:0] }()
		return provider.FlushDataLines(dataLines, processChunk)
	}

	for {
		if !done {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		line, err := reader.ReadLine()
		if err != nil && !errors.Is(err, io.EOF) {
			if done {
				if flushErr := flushPendingData(); flushErr != nil {
					return flushErr
				}
				return finishStream()
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			if strings.TrimSpace(finishReason) != "" {
				return finishStream()
			}
			return fmt.Errorf("%w: %w", provider.ErrStreamInterrupted, err)
		}

		switch {
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				if flushErr := flushPendingData(); flushErr != nil {
					return flushErr
				}
				done = true
			} else {
				dataLines = append(dataLines, data)
			}
		case line == "":
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			if done {
				return finishStream()
			}
		case strings.HasPrefix(line, ":"):
			// SSE 注释/心跳行，忽略。
		}

		if errors.Is(err, io.EOF) {
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			if done {
				return finishStream()
			}
			if strings.TrimSpace(finishReason) != "" {
				return finishStream()
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("%w: missing [DONE] marker before EOF", provider.ErrStreamInterrupted)
		}
	}
}

type responseFunctionCall struct {
	Name      string
	Arguments string
}

type responseToolCallDelta struct {
	Index         int
	ID            string
	Function      responseFunctionCall
	ArgumentsMode responseToolCallArgumentsMergeMode
}

type responseToolCallArgumentsMergeMode int

const (
	responseToolCallArgumentsMergeAppend responseToolCallArgumentsMergeMode = iota
	responseToolCallArgumentsMergeReplace
)

// mergeToolCallDelta 将单个 tool call 增量合并到累积状态，并在必要时发出统一事件。
func mergeToolCallDelta(
	ctx context.Context,
	events chan<- providertypes.StreamEvent,
	toolCalls map[int]*providertypes.ToolCall,
	delta responseToolCallDelta,
) error {
	call, exists := toolCalls[delta.Index]
	if !exists {
		call = &providertypes.ToolCall{}
		toolCalls[delta.Index] = call
	}

	hadName := strings.TrimSpace(call.Name) != ""
	if id := strings.TrimSpace(delta.ID); id != "" {
		call.ID = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.Name = name
	}

	if !hadName && strings.TrimSpace(call.Name) != "" {
		if err := provider.EmitToolCallStart(ctx, events, delta.Index, call.ID, call.Name); err != nil {
			return err
		}
	}

	if args := delta.Function.Arguments; args != "" {
		switch delta.ArgumentsMode {
		case responseToolCallArgumentsMergeReplace:
			if call.Arguments == args {
				return nil
			}
			emitDelta := args
			if strings.HasPrefix(args, call.Arguments) {
				emitDelta = strings.TrimPrefix(args, call.Arguments)
			}
			call.Arguments = args
			if emitDelta != "" {
				if err := provider.EmitToolCallDelta(ctx, events, delta.Index, call.ID, emitDelta); err != nil {
					return err
				}
			}
		default:
			if strings.HasSuffix(call.Arguments, args) {
				return nil
			}
			call.Arguments += args
			if err := provider.EmitToolCallDelta(ctx, events, delta.Index, call.ID, args); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveToolCallIndex 维护 Responses 流中的 tool_call 索引，优先复用 output_index。
func resolveToolCallIndex(outputIndex *int, itemID string, byItemID map[string]int, next *int) int {
	if outputIndex != nil && *outputIndex >= 0 {
		index := *outputIndex
		if trimmed := strings.TrimSpace(itemID); trimmed != "" {
			byItemID[trimmed] = index
		}
		return index
	}

	if trimmed := strings.TrimSpace(itemID); trimmed != "" {
		if index, ok := byItemID[trimmed]; ok {
			return index
		}
		index := *next
		byItemID[trimmed] = index
		*next = *next + 1
		return index
	}

	index := *next
	*next = *next + 1
	return index
}

// extractUsage 将 Responses usage 覆盖到统一 token 统计结构。
func extractUsage(usage *providertypes.Usage, response *streamResponse) {
	if response == nil || response.Usage == nil {
		return
	}
	*usage = providertypes.Usage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
}

// resolveFinishReason 将 Responses 状态映射为统一 finish_reason。
func resolveFinishReason(eventType string, response *streamResponse) string {
	normalizedEventType := strings.ToLower(strings.TrimSpace(eventType))
	normalizedStatus := ""
	if response != nil {
		normalizedStatus = strings.ToLower(strings.TrimSpace(response.Status))
	}
	if normalizedStatus == "" {
		normalizedStatus = normalizedEventType
	}

	switch normalizedStatus {
	case "completed":
		return "stop"
	case "cancelled", "canceled":
		return "cancelled"
	case "failed":
		return "error"
	case "incomplete":
		reason := ""
		if response != nil && response.IncompleteDetails != nil {
			reason = strings.ToLower(strings.TrimSpace(response.IncompleteDetails.Reason))
		}
		switch reason {
		case "max_output_tokens":
			return "length"
		case "content_filter":
			return "content_filter"
		case "":
			return "length"
		default:
			return reason
		}
	default:
		return ""
	}
}
