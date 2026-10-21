package types

import (
	"context"
	"io"
	"strings"
)

// SessionAssetReader 定义 provider 请求阶段读取会话附件内容的最小能力。
type SessionAssetReader interface {
	Open(ctx context.Context, assetID string) (io.ReadCloser, string, error)
}

// ThinkingConfig 表示 runtime 向 provider 传递的抽象 thinking 控制指令，由各 adapter 翻译为厂商特定参数。
type ThinkingConfig struct {
	Enabled      bool   `json:"enabled"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Effort       string `json:"effort,omitempty"`
}

// GenerateRequest 是 provider.Generate() 的请求参数。
type GenerateRequest struct {
	Model string `json:"model"`
	// SystemPrompt 是渲染完成的完整系统提示，由稳定前缀与易变尾部按序拼接而成。
	SystemPrompt string `json:"system_prompt"`
	// SystemStablePrefix 是 SystemPrompt 中长期稳定的缓存前缀部分。
	// 约束：必须是 SystemPrompt 的真实前缀；不满足时 adapter 必须丢弃该提示，不得打缓存标记。
	SystemStablePrefix string             `json:"system_stable_prefix,omitempty"`
	Messages           []Message          `json:"messages"`
	Tools              []ToolSpec         `json:"tools,omitempty"`
	ThinkingConfig     *ThinkingConfig    `json:"thinking_config,omitempty"`
	SessionAssetReader SessionAssetReader `json:"-"`
}

// ResolvedStablePrefix 返回可用于缓存打标的稳定前缀。
// 当提示为空、或不是 SystemPrompt 的真实前缀时返回空串，调用方据此降级为不打标。
func (r GenerateRequest) ResolvedStablePrefix() string {
	if r.SystemStablePrefix == "" {
		return ""
	}
	if !strings.HasPrefix(r.SystemPrompt, r.SystemStablePrefix) {
		return ""
	}
	return r.SystemStablePrefix
}

// VolatileSystemSuffix 返回系统提示中稳定前缀之后的部分，按原样保留分隔符与尾部内容。
// 与 ResolvedStablePrefix 拼接后严格等于 SystemPrompt，保证分段发送不改变实际文本。
func (r GenerateRequest) VolatileSystemSuffix() string {
	prefix := r.ResolvedStablePrefix()
	if prefix == "" {
		return r.SystemPrompt
	}
	return r.SystemPrompt[len(prefix):]
}
