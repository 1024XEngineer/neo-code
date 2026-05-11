package context

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	stablePromptSources  []promptSectionSource
	dynamicPromptSources []promptSectionSource
	promptSources        []promptSectionSource
	trimPolicy           messageTrimPolicy
	microCompactCfg      MicroCompactConfig
}

// newStablePromptSources 返回稳定提示词来源列表，适合作为缓存前缀。
// extra 中的非 nil SectionSource 也会追加到 stable 中（如 memo 持久记忆索引）。
func newStablePromptSources(extra ...SectionSource) []promptSectionSource {
	sources := []promptSectionSource{
		corePromptSource{},
		newRulesPromptSource(nil),
	}
	for _, src := range extra {
		if src != nil {
			sources = append(sources, src)
		}
	}
	return sources
}

// newDynamicPromptSources 返回动态提示词来源列表，随任务进度、会话状态变化。
func newDynamicPromptSources() []promptSectionSource {
	return []promptSectionSource{
		capabilitiesSource{},
		taskStateSource{},
		planModeContextSource{},
		todosSource{},
		skillPromptSource{},
		repositoryContextSource{},
		&systemStateSource{},
	}
}

// NewConfiguredBuilder 基于聚合配置和可选 SectionSource 列表构建上下文构建器，是推荐的统一构造入口。
// cfg.PinChecker 为 nil 时自动使用默认 pin checker；sources 中 nil 元素会被跳过。
func NewConfiguredBuilder(cfg MicroCompactConfig, sources ...SectionSource) Builder {
	if cfg.PinChecker == nil {
		cfg.PinChecker = NewDefaultPinChecker()
	}
	return &DefaultBuilder{
		stablePromptSources:  newStablePromptSources(sources...),
		dynamicPromptSources: newDynamicPromptSources(),
		trimPolicy:           spanMessageTrimPolicy{},
		microCompactCfg:      cfg,
	}
}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	return NewConfiguredBuilder(MicroCompactConfig{})
}

// NewBuilderWithToolPolicies 返回带工具 micro compact 策略源的默认上下文构建器。
//
// Deprecated: 使用 NewConfiguredBuilder 替代。
func NewBuilderWithToolPolicies(policies MicroCompactPolicySource) Builder {
	return NewConfiguredBuilder(MicroCompactConfig{Policies: policies})
}

// NewBuilderWithToolPoliciesAndSummarizers 返回带工具策略与内容摘要器的上下文构建器。
//
// Deprecated: 使用 NewConfiguredBuilder 替代。
func NewBuilderWithToolPoliciesAndSummarizers(policies MicroCompactPolicySource, summarizers MicroCompactSummarizerSource) Builder {
	return NewConfiguredBuilder(MicroCompactConfig{Policies: policies, Summarizers: summarizers})
}

// NewBuilderWithMemo 返回带记忆注入能力的上下文构建器。
// memoSource 为 nil 时等价于 NewBuilderWithToolPolicies。
//
// Deprecated: 使用 NewConfiguredBuilder 替代。
func NewBuilderWithMemo(policies MicroCompactPolicySource, memoSource SectionSource) Builder {
	return NewConfiguredBuilder(MicroCompactConfig{Policies: policies}, memoSource)
}

// NewBuilderWithMemoAndSummarizers 返回带记忆注入与内容摘要器的上下文构建器。
//
// Deprecated: 使用 NewConfiguredBuilder 替代。
func NewBuilderWithMemoAndSummarizers(policies MicroCompactPolicySource, summarizers MicroCompactSummarizerSource, memoSource SectionSource) Builder {
	return NewConfiguredBuilder(MicroCompactConfig{Policies: policies, Summarizers: summarizers}, memoSource)
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

	stableSources := b.stablePromptSources
	dynamicSources := b.dynamicPromptSources

	// 兼容旧构造方式：如果新字段未设置但旧 promptSources 有内容，回退到旧单列表。
	if len(stableSources) == 0 && len(dynamicSources) == 0 && len(b.promptSources) > 0 {
		stableSources = b.promptSources
	}

	var stablePrompt, dynamicPrompt string
	if stableSources != nil {
		stableSections, err := collectPromptSections(ctx, input, stableSources)
		if err != nil {
			return BuildResult{}, err
		}
		stablePrompt = composeSystemPrompt(stableSections...)
	}
	if dynamicSources != nil {
		dynamicSections, err := collectPromptSections(ctx, input, dynamicSources)
		if err != nil {
			return BuildResult{}, err
		}
		dynamicPrompt = composeSystemPrompt(dynamicSections...)
	}

	systemPrompt := joinSystemPromptParts(stablePrompt, dynamicPrompt)

	trimPolicy := b.trimPolicy
	if trimPolicy == nil {
		trimPolicy = spanMessageTrimPolicy{}
	}
	pinChecker := b.microCompactCfg.PinChecker
	if pinChecker == nil {
		pinChecker = NewDefaultPinChecker()
	}

	return BuildResult{
		SystemPrompt:        systemPrompt,
		StableSystemPrompt:  stablePrompt,
		DynamicSystemPrompt: dynamicPrompt,
		Messages: applyReadTimeContextProjection(
			trimPolicy.Trim(input.Messages, input.Compact),
			input.TaskState,
			input.Compact,
			b.microCompactCfg.Policies,
			b.microCompactCfg.Summarizers,
			pinChecker,
		),
	}, nil
}

// applyReadTimeContextProjection 负责在 provider 读取路径上应用只读上下文投影，避免改写原始会话消息。
func applyReadTimeContextProjection(
	messages []providertypes.Message,
	taskState agentsession.TaskState,
	options CompactOptions,
	policies MicroCompactPolicySource,
	summarizers MicroCompactSummarizerSource,
	pinChecker MicroCompactPinChecker,
) []providertypes.Message {
	projectedMessages := cloneContextMessages(messages)
	if options.DisableMicroCompact || !taskState.Established() {
		return ProjectToolMessagesForModel(projectedMessages)
	}

	projectedMessages = microCompactMessagesWithPolicies(
		projectedMessages,
		policies,
		options.MicroCompactRetainedToolSpans,
		summarizers,
		pinChecker,
	)
	return ProjectToolMessagesForModel(projectedMessages)
}
