//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

package provider

import "context"

// Role 常量定义统一消息角色。
const (
	// RoleSystem 标识系统消息。
	RoleSystem = "system"
	// RoleUser 标识用户消息。
	RoleUser = "user"
	// RoleAssistant 标识助手消息。
	RoleAssistant = "assistant"
	// RoleTool 标识工具结果消息。
	RoleTool = "tool"
)

// Modality 表示统一的内容模态类型。
type Modality string

const (
	// ModalityText 表示文本模态。
	ModalityText Modality = "text"
	// ModalityImage 表示图片模态。
	ModalityImage Modality = "image"
	// ModalityAudio 表示音频模态。
	ModalityAudio Modality = "audio"
	// ModalityFile 表示通用文件模态。
	ModalityFile Modality = "file"
)

// AssetSource 表示附件引用来源。
type AssetSource string

const (
	// AssetSourceSessionAsset 表示会话附件池中的引用。
	AssetSourceSessionAsset AssetSource = "session_asset"
	// AssetSourceLocalURI 表示本地文件 URI 引用。
	AssetSourceLocalURI AssetSource = "local_uri"
	// AssetSourceRemoteURI 表示远端资源 URI 引用。
	AssetSourceRemoteURI AssetSource = "remote_uri"
)

// AssetRef 描述非文本内容的领域级附件引用。
type AssetRef struct {
	// Source 是附件来源类型。
	Source AssetSource `json:"source"`
	// AssetID 是会话附件标识，Source=session_asset 时必填。
	AssetID string `json:"asset_id,omitempty"`
	// URI 是本地或远端资源定位符，Source=local_uri/remote_uri 时必填。
	URI string `json:"uri,omitempty"`
	// MIMEType 是附件内容类型。
	MIMEType string `json:"mime_type"`
	// FileName 是原始文件名，便于审计与展示。
	FileName string `json:"file_name,omitempty"`
	// SizeBytes 是附件大小（字节）。
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// SHA256 是可选内容摘要，用于去重与校验。
	SHA256 string `json:"sha256,omitempty"`
}

// MessagePart 表示消息中的一个分片。
type MessagePart struct {
	// Type 是分片模态类型。
	Type Modality `json:"type"`
	// Text 是文本内容，仅在 text 模态有效。
	Text string `json:"text,omitempty"`
	// Asset 是非文本附件引用，仅在 image/audio/file 模态有效。
	Asset *AssetRef `json:"asset,omitempty"`
}

// Message 表示统一消息结构。
type Message struct {
	// Role 是消息角色。
	Role string `json:"role"`
	// Parts 是按顺序排列的内容分片序列。
	Parts []MessagePart `json:"parts"`
	// ToolCalls 是助手发起的工具调用列表。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 是工具结果回灌时关联的调用标识。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// IsError 表示工具结果是否为错误语义。
	IsError bool `json:"is_error,omitempty"`
}

// ToolCall 表示模型发起的工具调用请求。
type ToolCall struct {
	// ID 是工具调用标识。
	ID string `json:"id"`
	// Name 是工具名称。
	Name string `json:"name"`
	// Arguments 是工具参数 JSON 字符串。
	Arguments string `json:"arguments"`
}

// ToolSpec 表示暴露给模型的可调用工具描述。
type ToolSpec struct {
	// Name 是工具名。
	Name string `json:"name"`
	// Description 是工具说明。
	Description string `json:"description"`
	// Schema 是工具参数 JSON Schema。
	Schema map[string]any `json:"schema"`
}

// GenerateRequest 是模型生成调用统一请求。
type GenerateRequest struct {
	// Model 是模型标识。
	Model string `json:"model"`
	// SystemPrompt 是系统提示词。
	SystemPrompt string `json:"system_prompt"`
	// Messages 是消息序列。
	Messages []Message `json:"messages"`
	// Tools 是可调用工具列表。
	Tools []ToolSpec `json:"tools,omitempty"`
}

// Usage 记录 token 使用统计。
type Usage struct {
	// InputTokens 是输入 token 数。
	InputTokens int `json:"input_tokens"`
	// OutputTokens 是输出 token 数。
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 是总 token 数。
	TotalTokens int `json:"total_tokens"`
}

// StreamEventType 定义流式事件类型。
type StreamEventType string

const (
	// StreamEventTextDelta 表示文本增量。
	StreamEventTextDelta StreamEventType = "text_delta"
	// StreamEventToolCallStart 表示工具调用开始。
	StreamEventToolCallStart StreamEventType = "tool_call_start"
	// StreamEventToolCallDelta 表示工具调用参数增量。
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	// StreamEventMessageDone 表示消息结束。
	StreamEventMessageDone StreamEventType = "message_done"
)

// StreamEvent 是统一流式事件结构。
type StreamEvent struct {
	// Type 是事件类型。
	Type StreamEventType `json:"type"`
	// TextDelta 是文本增量负载。
	TextDelta *TextDeltaPayload `json:"text_delta,omitempty"`
	// ToolCallStart 是工具调用开始负载。
	ToolCallStart *ToolCallStartPayload `json:"tool_call_start,omitempty"`
	// ToolCallDelta 是工具调用参数增量负载。
	ToolCallDelta *ToolCallDeltaPayload `json:"tool_call_delta,omitempty"`
	// MessageDone 是消息结束负载。
	MessageDone *MessageDonePayload `json:"message_done,omitempty"`
}

// TextDeltaPayload 表示文本增量事件负载。
type TextDeltaPayload struct {
	// Text 是增量文本。
	Text string `json:"text"`
}

// ToolCallStartPayload 表示工具调用开始事件负载。
type ToolCallStartPayload struct {
	// Index 是工具调用索引。
	Index int `json:"index"`
	// ID 是工具调用标识。
	ID string `json:"id"`
	// Name 是工具名。
	Name string `json:"name"`
}

// ToolCallDeltaPayload 表示工具调用参数增量事件负载。
type ToolCallDeltaPayload struct {
	// Index 是工具调用索引。
	Index int `json:"index"`
	// ID 是工具调用标识。
	ID string `json:"id"`
	// ArgumentsDelta 是参数增量字符串。
	ArgumentsDelta string `json:"arguments_delta"`
}

// MessageDonePayload 表示消息结束事件负载。
type MessageDonePayload struct {
	// FinishReason 是结束原因。
	FinishReason string `json:"finish_reason"`
	// Usage 是 token 使用统计。
	Usage *Usage `json:"usage"`
}

// ModelCapabilityHints 描述模型能力提示信息。
type ModelCapabilityHints struct {
	// InputModalities 是输入模态支持提示。
	InputModalities map[Modality]bool
	// OutputModalities 是输出模态支持提示。
	OutputModalities map[Modality]bool
	// SupportsStreaming 表示流式能力提示。
	SupportsStreaming bool
	// SupportsToolCall 表示工具调用能力提示。
	SupportsToolCall bool
	// MaxContextTokensHint 是上下文窗口提示值。
	MaxContextTokensHint int
	// MaxOutputTokensHint 是最大输出 token 提示值。
	MaxOutputTokensHint int
}

// ModelDescriptor 描述模型发现元数据。
type ModelDescriptor struct {
	// ID 是模型标识。
	ID string
	// Name 是展示名称。
	Name string
	// Description 是模型说明。
	Description string
	// ProviderName 是所属提供商标识。
	ProviderName string
	// Hints 是模型能力提示。
	Hints ModelCapabilityHints
}

// Provider 定义模型供应商生成接口。
type Provider interface {
	// Generate 执行一次模型生成并写入流式事件。
	// 职责：屏蔽供应商协议差异并输出统一事件序列。
	// 输入语义：req 为统一生成请求，events 为流式回传通道。
	// 并发约束：实现应支持多请求并发，单请求事件顺序必须稳定。
	// 生命周期：一次调用对应一次完整模型响应生命周期。
	// 错误语义：返回统一错误语义，供上游执行重试或终止决策。
	Generate(ctx context.Context, req GenerateRequest, events chan<- StreamEvent) error
}

// ProviderRuntimeConfig 描述驱动构建所需的运行时配置。
type ProviderRuntimeConfig struct {
	// Name 是 provider 实例名。
	Name string
	// Driver 是驱动类型。
	Driver string
	// BaseURL 是供应商基础地址。
	BaseURL string
	// Model 是默认模型标识。
	Model string
	// APIKey 是认证密钥。
	APIKey string
	// TimeoutSec 是请求超时秒数。
	TimeoutSec int
	// MaxTokens 是默认最大输出 token。
	MaxTokens int
}

// DriverBuilder 描述驱动构建函数签名。
type DriverBuilder func(ctx context.Context, cfg ProviderRuntimeConfig) (Provider, error)

// ModelDiscoverer 描述模型发现函数签名。
type ModelDiscoverer func(ctx context.Context, cfg ProviderRuntimeConfig) ([]ModelDescriptor, error)

// DriverDefinition 描述一个可注册驱动。
type DriverDefinition struct {
	// Name 是驱动名称。
	Name string
	// Build 是驱动构建函数。
	Build DriverBuilder
	// Discover 是模型发现函数，可为空。
	Discover ModelDiscoverer
}

// DriverRegistry 定义驱动注册与发现契约。
type DriverRegistry interface {
	// Register 注册一个驱动定义。
	// 职责：将驱动纳入统一管理并避免重复注册。
	// 输入语义：driver 包含名称、构建函数与可选发现函数。
	// 并发约束：实现必须保证并发注册时状态一致。
	// 生命周期：通常在系统启动阶段调用。
	// 错误语义：返回驱动名非法、构建函数缺失或重复注册错误。
	Register(driver DriverDefinition) error
	// Build 根据运行时配置构建 Provider 实例。
	// 职责：按驱动类型解析并创建具体适配器。
	// 输入语义：cfg 提供驱动标识与连接配置。
	// 并发约束：应支持并发构建不同实例。
	// 生命周期：每次需要创建 provider 时调用。
	// 错误语义：返回驱动不存在、配置非法或构建失败错误。
	Build(ctx context.Context, cfg ProviderRuntimeConfig) (Provider, error)
	// DiscoverModels 查询指定驱动可用模型描述列表。
	// 职责：提供统一模型发现入口供上层选择与能力协商。
	// 输入语义：cfg 提供目标驱动与认证信息。
	// 并发约束：应支持并发查询，且不污染注册状态。
	// 生命周期：模型选择、能力探测或缓存刷新时调用。
	// 错误语义：返回驱动不存在、认证失败或远端查询失败错误。
	DiscoverModels(ctx context.Context, cfg ProviderRuntimeConfig) ([]ModelDescriptor, error)
	// Supports 判断驱动类型是否已注册。
	// 职责：快速探测某驱动是否可用。
	// 输入语义：driverType 为驱动标识。
	// 并发约束：并发读取必须线程安全。
	// 生命周期：配置校验与路由决策阶段调用。
	// 错误语义：通过布尔值表达是否支持，不返回错误。
	Supports(driverType string) bool
}

// ProviderErrorCode 表示 provider 错误分类。
type ProviderErrorCode string

const (
	// ErrorCodeAuthFailed 表示认证失败。
	ErrorCodeAuthFailed ProviderErrorCode = "auth_failed"
	// ErrorCodeForbidden 表示权限不足。
	ErrorCodeForbidden ProviderErrorCode = "forbidden"
	// ErrorCodeNotFound 表示资源不存在。
	ErrorCodeNotFound ProviderErrorCode = "not_found"
	// ErrorCodeClient 表示一般客户端错误。
	ErrorCodeClient ProviderErrorCode = "client_error"
	// ErrorCodeRateLimit 表示限流。
	ErrorCodeRateLimit ProviderErrorCode = "rate_limited"
	// ErrorCodeServer 表示服务端错误。
	ErrorCodeServer ProviderErrorCode = "server_error"
	// ErrorCodeTimeout 表示超时。
	ErrorCodeTimeout ProviderErrorCode = "timeout"
	// ErrorCodeNetwork 表示网络错误。
	ErrorCodeNetwork ProviderErrorCode = "network_error"
	// ErrorCodeUnknown 表示未知错误。
	ErrorCodeUnknown ProviderErrorCode = "unknown"
	// ErrorCodeContextOverflow 表示上下文过长。
	ErrorCodeContextOverflow ProviderErrorCode = "context_overflow"
	// ErrorCodeUnsupportedModality 表示输入模态不被模型支持。
	ErrorCodeUnsupportedModality ProviderErrorCode = "unsupported_modality"
)

// ProviderError 是统一错误结构。
type ProviderError struct {
	// StatusCode 是 HTTP 状态码，0 表示非 HTTP 错误。
	StatusCode int
	// Code 是语义化错误分类。
	Code ProviderErrorCode
	// Message 是可读错误信息。
	Message string
	// Retryable 表示是否建议重试。
	Retryable bool
}

// ErrorClassifier 定义错误归一化辅助契约。
type ErrorClassifier interface {
	// IsContextOverflow 判断错误是否属于上下文过长。
	// 职责：为上游重试与压缩分支提供稳定判定依据。
	// 输入语义：err 为 provider 返回的原始错误对象。
	// 并发约束：实现应支持并发调用且无共享可变状态依赖。
	// 生命周期：在错误处理与重试策略评估阶段调用。
	// 错误语义：返回 false 表示未命中，不应抛出二次错误。
	IsContextOverflow(err error) bool
	// IsUnsupportedModality 判断错误是否属于模态不支持。
	// 职责：为上游模型切换或输入降级分支提供稳定判定依据。
	// 输入语义：err 为 provider 返回的原始错误对象。
	// 并发约束：实现应支持并发调用且不依赖共享可变状态。
	// 生命周期：在请求前校验失败或远端返回能力错误后调用。
	// 错误语义：返回 false 表示未命中，不应抛出二次错误。
	IsUnsupportedModality(err error) bool
}