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

	rules, systemState, err := b.collectPromptState(ctx, input.Metadata)
	if err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		SystemPrompt: composeSystemPrompt(buildPromptSections(input, rules, systemState)...),
		Messages:     trimMessages(input.Messages),
	}, nil
}

func (b *DefaultBuilder) collectPromptState(ctx context.Context, metadata Metadata) ([]ruleDocument, SystemState, error) {
	rules, err := loadProjectRules(ctx, metadata.Workdir)
	if err != nil {
		return nil, SystemState{}, err
	}

	systemState, err := collectSystemState(ctx, metadata, b.gitRunner)
	if err != nil {
		return nil, SystemState{}, err
	}

	return rules, systemState, nil
}

func buildPromptSections(input BuildInput, rules []ruleDocument, systemState SystemState) []promptSection {
	sections := append([]promptSection{}, defaultSystemPromptSections()...)
	sections = append(sections, buildDynamicPromptSections(input)...)
	sections = append(sections, renderProjectRulesSection(rules))
	sections = append(sections, renderSystemStateSection(systemState))
	return sections
}

func buildDynamicPromptSections(BuildInput) []promptSection {
	return nil
}
