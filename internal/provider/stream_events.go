package provider

import (
	"context"

	providertypes "neo-code/internal/provider/types"
)

// EmitTextDelta 发送文本增量事件，空文本直接忽略。
func EmitTextDelta(ctx context.Context, events chan<- providertypes.StreamEvent, text string) error {
	if text == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewTextDeltaStreamEvent(text))
}

// EmitToolCallStart 发送工具调用起始事件，工具名为空时直接忽略。
func EmitToolCallStart(ctx context.Context, events chan<- providertypes.StreamEvent, index int, id, name string) error {
	if name == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewToolCallStartStreamEvent(index, id, name))
}

// EmitToolCallDelta 发送工具调用参数增量事件。
func EmitToolCallDelta(ctx context.Context, events chan<- providertypes.StreamEvent, index int, id, argumentsDelta string) error {
	if argumentsDelta == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewToolCallDeltaStreamEvent(index, id, argumentsDelta))
}

// EmitThinkingDelta 发送思考增量事件，空文本直接忽略。
func EmitThinkingDelta(ctx context.Context, events chan<- providertypes.StreamEvent, text string) error {
	if text == "" {
		return nil
	}
	return emitStreamEvent(ctx, events, providertypes.NewThinkingDeltaStreamEvent(text))
}

// EmitMessageDone 发送消息完成事件，并在上下文取消时做非阻塞兜底。
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

// emitStreamEvent 安全发送流式事件，并支持上下文取消。
func emitStreamEvent(ctx context.Context, events chan<- providertypes.StreamEvent, event providertypes.StreamEvent) error {
	if events == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
