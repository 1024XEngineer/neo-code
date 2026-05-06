package chatcompletions

// 以下类型定义了 OpenAI Chat Completions API 的请求和响应结构体，
// 仅在 openai-compatible chat_completions 协议实现内部及其适配层使用。

// Request 表示 /chat/completions 端点的请求体。
type Request struct {
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	Stream          bool             `json:"stream"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
}

// Message 表示 OpenAI 协议中的消息格式。
type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

// MessageContentPart 表示多模态消息的单个部分。
type MessageContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 表示图片 URL 对象。
type ImageURL struct {
	URL string `json:"url"`
}

// ToolDefinition 表示工具定义的 OpenAI 格式。
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition 表示函数描述的 OpenAI 格式。
type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall 表示响应中工具调用的 OpenAI 格式。
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 表示函数调用参数的 OpenAI 格式。
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
