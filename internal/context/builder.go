package context

import "context"

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct{}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	return &DefaultBuilder{}
}

// Build assembles the provider-facing context for the current round.
func (b *DefaultBuilder) Build(ctx context.Context, input BuildInput) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		SystemPrompt: defaultSystemPrompt(),
		Messages:     trimMessages(input.Messages),
	}, nil
}
