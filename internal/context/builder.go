package context

import "context"

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	promptSources []promptSectionSource
	trimPolicy    messageTrimPolicy
}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	systemSource := &systemStateSource{gitRunner: runGitCommand}
	return &DefaultBuilder{
		promptSources: []promptSectionSource{
			corePromptSource{},
			&projectRulesSource{},
			systemSource,
		},
		trimPolicy: spanMessageTrimPolicy{},
	}
}

// Build assembles the provider-facing context for the current round.
func (b *DefaultBuilder) Build(ctx context.Context, input BuildInput) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	sections := make([]promptSection, 0, len(b.promptSources)+1)
	for _, source := range b.promptSources {
		sourceSections, err := source.Sections(ctx, input)
		if err != nil {
			return BuildResult{}, err
		}
		sections = append(sections, sourceSections...)
	}

	trimPolicy := b.trimPolicy
	if trimPolicy == nil {
		trimPolicy = spanMessageTrimPolicy{}
	}

	return BuildResult{
		SystemPrompt: composeSystemPrompt(sections...),
		Messages:     trimPolicy.Trim(input.Messages),
	}, nil
}
