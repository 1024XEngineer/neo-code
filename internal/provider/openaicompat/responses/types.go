package responses

// Request 表示 /responses 端点请求体。
type Request struct {
	Model        string           `json:"model"`
	Instructions string           `json:"instructions,omitempty"`
	Input        []InputItem      `json:"input"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	ToolChoice   string           `json:"tool_choice,omitempty"`
	Stream       bool             `json:"stream"`
	// PromptCacheKey 是缓存路由键，作用是让相同前缀的请求尽量落到同一后端，
	// 提升自动前缀缓存的命中率；为空时不发送该字段。
	PromptCacheKey string `json:"prompt_cache_key,omitempty"`
}

// InputItem 表示 Responses API 中输入项（消息、函数调用、函数调用输出）。
type InputItem struct {
	Type      string             `json:"type,omitempty"`
	Role      string             `json:"role,omitempty"`
	Content   []InputContentPart `json:"content,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
	Output    string             `json:"output,omitempty"`
}

// InputContentPart 表示输入消息中的多模态片段。
type InputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ToolDefinition 表示工具定义的 Responses 协议结构。
type ToolDefinition struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// streamEvent 表示 Responses SSE 的统一事件结构。
type streamEvent struct {
	Type        string           `json:"type"`
	Delta       string           `json:"delta,omitempty"`
	ItemID      string           `json:"item_id,omitempty"`
	OutputIndex *int             `json:"output_index,omitempty"`
	Item        *streamEventItem `json:"item,omitempty"`
	Response    *streamResponse  `json:"response,omitempty"`
	Error       *streamError     `json:"error,omitempty"`
}

// streamEventItem 表示 output item 事件中的 item 字段。
type streamEventItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// streamResponse 表示 completed/incomplete/failed 事件中的 response 字段。
type streamResponse struct {
	Status            string                   `json:"status,omitempty"`
	Usage             *streamUsage             `json:"usage,omitempty"`
	IncompleteDetails *streamIncompleteDetails `json:"incomplete_details,omitempty"`
	Error             *streamError             `json:"error,omitempty"`
}

// streamUsage 表示 Responses 事件中的 usage 字段。
type streamUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// streamIncompleteDetails 表示不完整结束时的补充信息。
type streamIncompleteDetails struct {
	Reason string `json:"reason,omitempty"`
}

// streamError 表示 Responses 事件中的错误结构。
type streamError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
