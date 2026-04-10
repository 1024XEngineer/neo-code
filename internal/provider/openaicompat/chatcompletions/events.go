package chatcompletions

import (
	"context"

	providertypes "neo-code/internal/provider/types"
)

// EmitTextDelta 发送文本增量事件，空文本时跳过。
func EmitTextDelta(ctx context.Context, events chan<- providertypes.StreamEvent, text string) error {
	if text == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewTextDeltaStreamEvent(text))
}

// EmitToolCallStart 发送工具调用开始事件，空名称时跳过。
func EmitToolCallStart(ctx context.Context, events chan<- providertypes.StreamEvent, index int, id, name string) error {
	if name == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewToolCallStartStreamEvent(index, id, name))
}

// EmitToolCallDelta 发送工具调用参数增量事件。
// id 为工具调用 ID，由上游 MergeToolCallDelta 从累积状态中传入。
func EmitToolCallDelta(ctx context.Context, events chan<- providertypes.StreamEvent, index int, id, argumentsDelta string) error {
	if argumentsDelta == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewToolCallDeltaStreamEvent(index, id, argumentsDelta))
}

// EmitMessageDone 发送消息完成事件。
func EmitMessageDone(ctx context.Context, events chan<- providertypes.StreamEvent, finishReason string, usage *providertypes.Usage) error {
	event := providertypes.NewMessageDoneStreamEvent(finishReason, usage)
	if ctx == nil || ctx.Err() == nil {
		return emitStreamEvent(ctx, events, event)
	}
	if events == nil {
		return nil
	}

	select {
	case events <- event:
		return nil
	default:
		return nil
	}
}

// FlushDataLines 逐行处理缓冲的 data lines，每行作为独立 payload 交给 processChunk。
// OpenAI 的 SSE 实际按单行 JSON 发送，因此逐行处理更可靠。
func FlushDataLines(dataLines []string, processChunk func(string) error) error {
	for _, line := range dataLines {
		if err := processChunk(line); err != nil {
			return err
		}
	}
	return nil
}

// emitStreamEvent 通过 channel 安全发送流式事件，支持上下文取消和 nil channel 保护。
func emitStreamEvent(ctx context.Context, events chan<- providertypes.StreamEvent, event providertypes.StreamEvent) error {
	if events == nil {
		return nil
	}

	select {
	case events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
