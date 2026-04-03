package context

import (
	"context"

	"neo-code/internal/provider"
)

// Builder builds the provider-facing context for a single model round.
// Builder 抽象“每轮模型调用前”的上下文构建过程。
type Builder interface {
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// BuildInput contains the runtime state needed to assemble model context.
// BuildInput 是构建上下文所需输入，包含消息历史与运行时元信息。
type BuildInput struct {
	Messages []provider.Message
	Metadata Metadata
}

// BuildResult is the provider-facing context produced for a single round.
// BuildResult 是可直接喂给 provider 的上下文结果。
type BuildResult struct {
	SystemPrompt string
	Messages     []provider.Message
}
