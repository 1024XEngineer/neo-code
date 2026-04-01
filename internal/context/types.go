package context

import (
	"context"

	"neo-code/internal/provider"
)

// Builder builds the provider-facing context for a single model round.
type Builder interface {
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// BuildInput contains the runtime state needed to assemble model context.
type BuildInput struct {
	Messages []provider.Message
	Metadata Metadata
}

// BuildResult is the provider-facing context produced for a single round.
type BuildResult struct {
	SystemPrompt string
	Messages     []provider.Message
}
