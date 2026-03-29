package provider

import "context"

type Provider interface {
	Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error)
}

type StreamEventType string

const (
	// StreamEventTextDelta 表示模型输出的文本片段。
	StreamEventTextDelta StreamEventType = "text_delta"
	// StreamEventToolCallStart 表示模型开始请求工具调用，TUI 可据此展示过渡提示。
	StreamEventToolCallStart StreamEventType = "tool_call_start"
)

// StreamEvent 表示 provider 驱动层向 runtime 推送的流式事件。
type StreamEvent struct {
	Type       StreamEventType
	Text       string // 文本片段（text_delta 时使用）
	ToolName   string // 工具名称（tool_call_start 时使用）
	ToolCallID string // 工具调用 ID（tool_call_start 时使用）
}
