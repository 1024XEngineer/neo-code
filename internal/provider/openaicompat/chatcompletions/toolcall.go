package chatcompletions

import (
	"context"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// MergeToolCallDelta 将单个 tool call delta 累积到 toolCalls map 中，
// 并在名称或参数首次出现时发出对应的增量事件。
func MergeToolCallDelta(
	ctx context.Context,
	events chan<- providertypes.StreamEvent,
	toolCalls map[int]*providertypes.ToolCall,
	delta ToolCallDelta,
) error {
	call, exists := toolCalls[delta.Index]
	if !exists {
		call = &providertypes.ToolCall{}
		toolCalls[delta.Index] = call
	}

	hadName := strings.TrimSpace(call.Name) != "" // 记录是否已知工具名，用于避免重复发 start 事件。

	if id := strings.TrimSpace(delta.ID); id != "" {
		call.ID = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.Name = name
	}

	if !hadName && strings.TrimSpace(call.Name) != "" {
		if err := EmitToolCallStart(ctx, events, delta.Index, call.ID, call.Name); err != nil {
			return err
		}
	}

	if args := delta.Function.Arguments; args != "" {
		call.Arguments += args
		if err := EmitToolCallDelta(ctx, events, delta.Index, call.ID, args); err != nil {
			return err
		}
	}
	return nil
}
