package context

import (
	"context"

	"neo-code/internal/provider"
)

// Builder 定义上下文构建契约：将运行时状态转换为模型请求上下文。
type Builder interface {
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// BuildInput 是构建上下文所需输入。
type BuildInput struct {
	Messages []provider.Message
	Metadata Metadata
}

// BuildResult 是传给 provider 的上下文结果。
type BuildResult struct {
	SystemPrompt string
	Messages     []provider.Message
}
