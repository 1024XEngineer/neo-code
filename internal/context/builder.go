package context

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// DefaultBuilder preserves the current runtime context-building behavior.
type DefaultBuilder struct {
	promptSources           []promptSectionSource
	trimPolicy              messageTrimPolicy
	microCompactPolicies    MicroCompactPolicySource
	microCompactSummarizers MicroCompactSummarizerSource
	microCompactPinChecker  MicroCompactPinChecker
}

// newDefaultBuilder 统一构建默认上下文构建器，避免多个构造函数重复装配相同依赖。
func newDefaultBuilder(
	policies MicroCompactPolicySource,
	summarizers MicroCompactSummarizerSource,
	memoSource SectionSource,
) Builder {
	return &DefaultBuilder{
		promptSources:           newPromptSources(memoSource),
		trimPolicy:              spanMessageTrimPolicy{},
		microCompactPolicies:    policies,
		microCompactSummarizers: summarizers,
		microCompactPinChecker:  NewDefaultPinChecker(),
	}
}

// newPromptSources 组装系统提示词来源列表，并按约定将 memoSource 插入到 systemState 之前。
func newPromptSources(memoSource SectionSource) []promptSectionSource {
	sources := []promptSectionSource{
		corePromptSource{},
		&projectRulesSource{},
		taskStateSource{},
		todosSource{},
		skillPromptSource{},
	}
	if memoSource != nil {
		sources = append(sources, memoSource)
	}
	return append(sources, &systemStateSource{gitRunner: runGitCommand})
}

// NewBuilder returns the default context builder implementation.
func NewBuilder() Builder {
	return NewBuilderWithToolPolicies(nil)
}

// NewBuilderWithToolPolicies 返回带工具 micro compact 策略源的默认上下文构建器。
func NewBuilderWithToolPolicies(policies MicroCompactPolicySource) Builder {
	return newDefaultBuilder(policies, nil, nil)
}

// NewBuilderWithToolPoliciesAndSummarizers 返回带工具策略与内容摘要器的上下文构建器。
func NewBuilderWithToolPoliciesAndSummarizers(policies MicroCompactPolicySource, summarizers MicroCompactSummarizerSource) Builder {
	return newDefaultBuilder(policies, summarizers, nil)
}

// NewBuilderWithMemo 返回带记忆注入能力的上下文构建器。
// memoSource 为 nil 时等价于 NewBuilderWithToolPolicies。
func NewBuilderWithMemo(policies MicroCompactPolicySource, memoSource SectionSource) Builder {
	return NewBuilderWithMemoAndSummarizers(policies, nil, memoSource)
}

// NewBuilderWithMemoAndSummarizers 返回带记忆注入与内容摘要器的上下文构建器。
func NewBuilderWithMemoAndSummarizers(policies MicroCompactPolicySource, summarizers MicroCompactSummarizerSource, memoSource SectionSource) Builder {
	return newDefaultBuilder(policies, summarizers, memoSource)
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
	pinChecker := b.microCompactPinChecker
	if pinChecker == nil {
		pinChecker = NewDefaultPinChecker()
	}

	shouldAutoCompact := input.Compact.AutoCompactThreshold > 0 &&
		input.Metadata.SessionInputTokens >= input.Compact.AutoCompactThreshold

	return BuildResult{
		SystemPrompt:         composeSystemPrompt(sections...),
		Messages:             applyReadTimeContextProjection(trimPolicy.Trim(input.Messages, input.Compact), input.TaskState, input.Compact, b.microCompactPolicies, b.microCompactSummarizers, pinChecker),
		AutoCompactSuggested: shouldAutoCompact,
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
	if options.DisableMicroCompact || !taskState.Established() {
		return ProjectToolMessagesForModel(cloneContextMessages(messages))
	} else {
		return ProjectToolMessagesForModel(
			microCompactMessagesWithPolicies(messages, policies, options.MicroCompactRetainedToolSpans, summarizers, pinChecker),
		)
	}
}
