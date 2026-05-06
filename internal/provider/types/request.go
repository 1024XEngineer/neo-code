package types

import (
	"context"
	"io"
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
	Model              string             `json:"model"`
	SystemPrompt       string             `json:"system_prompt"`
	Messages           []Message          `json:"messages"`
	Tools              []ToolSpec         `json:"tools,omitempty"`
	ThinkingConfig     *ThinkingConfig    `json:"thinking_config,omitempty"`
	SessionAssetReader SessionAssetReader `json:"-"`
}
