package provider

import "context"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Provider hides model vendor protocol differences behind a stable interface.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// StreamingProvider optionally supports incremental text deltas while building the final response.
type StreamingProvider interface {
	Provider
	ChatStream(ctx context.Context, req ChatRequest, onDelta func(delta string) error) (ChatResponse, error)
}

// ChatRequest is the canonical chat payload used by Runtime.
type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []ToolSpec
	Stream   bool
}

// ChatResponse is the normalized provider response.
type ChatResponse struct {
	Message      Message
	FinishReason string
	Usage        Usage
}

// Message is the shared conversation message structure.
type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolCall describes a single model-requested tool invocation.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolSpec is the normalized schema passed to the provider.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Usage contains token accounting when available.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}
