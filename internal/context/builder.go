package context

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	promptSources   []promptSectionSource
	trimPolicy      messageTrimPolicy
	microCompactCfg MicroCompactConfig
}

// newPromptSources 组装系统提示词来源列表，将额外 SectionSource 插入到 systemState 之前。
// nil 元素会被跳过，不会影响来源顺序。
func newPromptSources(extra ...SectionSource) []promptSectionSource {
	sources := []promptSectionSource{
		corePromptSource{},
		&projectRulesSource{},
		taskStateSource{},
		todosSource{},
		skillPromptSource{},
	}
	for _, src := range extra {
		if src != nil {
			sources = append(sources, src)
		}
	}
	sources = append(sources, repositoryContextSource{})
	return append(sources, &systemStateSource{})
}

// NewConfiguredBuilder 基于聚合配置和可选 SectionSource 列表构建上下文构建器，是推荐的统一构造入口。
// cfg.PinChecker 为 nil 时自动使用默认 pin checker；sources 中 nil 元素会被跳过。
func NewConfiguredBuilder(cfg MicroCompactConfig, sources ...SectionSource) Builder {
	if cfg.PinChecker == nil {
		cfg.PinChecker = NewDefaultPinChecker()
	}
	return &DefaultBuilder{
		promptSources:   newPromptSources(sources...),
		trimPolicy:      spanMessageTrimPolicy{},
		microCompactCfg: cfg,
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
	pinChecker := b.microCompactCfg.PinChecker
	if pinChecker == nil {
		pinChecker = NewDefaultPinChecker()
	}

	return BuildResult{
		SystemPrompt: composeSystemPrompt(sections...),
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
