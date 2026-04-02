package context

import "context"

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	gitRunner gitCommandRunner
}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	return &DefaultBuilder{
		gitRunner: runGitCommand,
	}
}

// Build assembles the provider-facing context for the current round.
func (b *DefaultBuilder) Build(ctx context.Context, input BuildInput) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	rules, err := loadProjectRules(ctx, input.Metadata.Workdir)
	if err != nil {
		return BuildResult{}, err
	}

	systemState, err := collectSystemState(ctx, input.Metadata, b.gitRunner)
	if err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		SystemPrompt: composeSystemPrompt(
			defaultSystemPrompt(),
			renderProjectRulesSection(rules),
			renderSystemStateSection(systemState),
		),
		Messages: trimMessages(input.Messages),
	}, nil
}
