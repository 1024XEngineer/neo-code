package provider

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	IsError    bool       `json:"is_error,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type ChatRequest struct {
	Model        string     `json:"model"`
	SystemPrompt string     `json:"system_prompt"`
	Messages     []Message  `json:"messages"`
	Tools        []ToolSpec `json:"tools,omitempty"`
}

type ChatResponse struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
	Usage        Usage   `json:"usage"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
