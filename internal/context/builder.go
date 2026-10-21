package context

import (
	"context"
)

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	stablePromptSources  []promptSectionSource
	dynamicPromptSources []promptSectionSource
	trimPolicy           messageTrimPolicy
}

// newStablePromptSources 返回稳定提示词来源列表，构成 prompt 缓存的可复用前缀。
// 只有"进程生命周期内不变、且随工作区规则文件变化"的来源才允许进入本列表：
// 任何随会话写入变化的来源一旦进入稳定段，都会让整个缓存前缀失效。
func newStablePromptSources() []promptSectionSource {
	return []promptSectionSource{
		corePromptSource{},
		newRulesPromptSource(nil),
	}
}

// newDynamicPromptSources 返回动态提示词来源列表，随任务进度、会话状态变化。
// extra 来源（如 memo 索引、会话记忆）排在最前：它们随写入变化，属于动态段而非缓存前缀。
func newDynamicPromptSources(extra ...SectionSource) []promptSectionSource {
	sources := make([]promptSectionSource, 0, len(extra)+7)
	for _, src := range extra {
		if src != nil {
			sources = append(sources, src)
		}
	}
	return append(sources,
		capabilitiesSource{},
		taskStateSource{},
		planModeContextSource{},
		todosSource{},
		skillPromptSource{},
		repositoryContextSource{},
		&systemStateSource{},
	)
}

// NewConfiguredBuilder 基于可选 SectionSource 列表构建上下文构建器，是推荐的统一构造入口。
// 传入的来源统一归入动态段，避免易变内容污染缓存前缀。
func NewConfiguredBuilder(sources ...SectionSource) Builder {
	return &DefaultBuilder{
		stablePromptSources:  newStablePromptSources(),
		dynamicPromptSources: newDynamicPromptSources(sources...),
		trimPolicy:           spanMessageTrimPolicy{},
	}
}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	return NewConfiguredBuilder()
}

// collectPromptSections 遍历 promptSectionSource 列表并收集所有 sections。
func collectPromptSections(ctx context.Context, input BuildInput, sources []promptSectionSource) ([]promptSection, error) {
	sections := make([]promptSection, 0, len(sources))
	for _, source := range sources {
		sourceSections, err := source.Sections(ctx, input)
		if err != nil {
			return nil, err
		}
		sections = append(sections, sourceSections...)
	}
	return sections, nil
}

// Build assembles the provider-facing context for the current round.
func (b *DefaultBuilder) Build(ctx context.Context, input BuildInput) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	stableSections, err := collectPromptSections(ctx, input, b.stablePromptSources)
	if err != nil {
		return BuildResult{}, err
	}
	stablePrompt := composeSystemPrompt(stableSections...)

	dynamicSections, err := collectPromptSections(ctx, input, b.dynamicPromptSources)
	if err != nil {
		return BuildResult{}, err
	}
	dynamicPrompt := composeSystemPrompt(dynamicSections...)

	systemPrompt := joinSystemPromptParts(stablePrompt, dynamicPrompt)

	trimPolicy := b.trimPolicy
	if trimPolicy == nil {
		trimPolicy = spanMessageTrimPolicy{}
	}

	return BuildResult{
		SystemPrompt:        systemPrompt,
		StableSystemPrompt:  stablePrompt,
		DynamicSystemPrompt: dynamicPrompt,
		Messages: projectReadTimeMessagesForModel(
			trimPolicy.Trim(input.Messages, input.Compact),
		),
	}, nil
}
