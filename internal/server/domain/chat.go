package domain

import (
	"context"
)

type ChatRequest struct {
	Messages []Message
	Model    string
	Tools    []ToolSchema
}

type ChatGateway interface {
	Send(ctx context.Context, req *ChatRequest) (<-chan ChatEvent, error)
}

type ChatProvider interface {
	GetModelName() string
	Chat(ctx context.Context, messages []Message, tools []ToolSchema) (<-chan ChatEvent, error)
}

type Message struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []ChatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type ChatEventType string

const (
	ChatEventDelta    ChatEventType = "delta"
	ChatEventToolCall ChatEventType = "tool_call"
	ChatEventDone     ChatEventType = "done"
)

type ChatEvent struct {
	Type     ChatEventType
	Content  string
	ToolCall *ChatToolCall
}

type ChatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function ChatToolCallFunction `json:"function"`
}

type ChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
