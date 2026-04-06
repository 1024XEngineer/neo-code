//go:build ignore
// +build ignore

package provider

import "context"

// ChatRequest 是统一模型请求。
type ChatRequest struct {
	// Model 是模型标识。
	Model string
	// SystemPrompt 是系统提示词。
	SystemPrompt string
}

// ChatResponse 是统一模型响应。
type ChatResponse struct {
	// FinishReason 是结束原因。
	FinishReason string
}

// StreamEvent 是流式事件。
type StreamEvent struct {
	// Type 是事件类型。
	Type string
	// Text 是文本增量。
	Text string
}

// Provider 是模型供应商适配接口。
type Provider interface {
	// Chat 执行一次模型调用并写入流式事件。
	// 输入语义：req 是统一请求信封，events 用于流式回传。
	// 并发约束：实现应支持多请求并发，单请求事件顺序稳定。
	// 生命周期：一次调用对应一次完整响应。
	// 错误语义：返回可归一化错误，供 runtime 做重试或终止决策。
	Chat(ctx context.Context, req ChatRequest, events chan<- StreamEvent) (ChatResponse, error)
}

// ProviderError [CURRENT] 是 provider 统一错误。
type ProviderError struct {
	// Code 是错误分类。
	Code string
	// Retryable 表示是否可重试。
	Retryable bool
}

// ErrorClassifier [PROPOSED] 提供错误归一化能力。
type ErrorClassifier interface {
	// IsContextOverflow 判断错误是否属于上下文过长。
	// 输入语义：err 是 provider 返回的原始错误。
	// 并发约束：无状态实现应支持并发调用。
	// 生命周期：在 runtime 处理 provider 错误时调用。
	// 错误语义：返回 false 表示未命中，不抛出二次错误。
	IsContextOverflow(err error) bool
}
