package context

import "context"

// DefaultBuilder 负责将运行时状态组装为模型可消费的上下文。
type DefaultBuilder struct {
	gitRunner gitCommandRunner
}

// NewBuilder 返回默认上下文构建器实现。
func NewBuilder() Builder {
	return &DefaultBuilder{
		gitRunner: runGitCommand,
	}
}

// Build 构建单轮请求所需的 system prompt 与消息窗口。
// 失败时直接返回错误，不吞掉规则加载或系统状态采集失败。
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

	sections := append([]promptSection{}, defaultSystemPromptSections()...)
	sections = append(sections, renderProjectRulesSection(rules))
	sections = append(sections, renderSystemStateSection(systemState))

	return BuildResult{
		SystemPrompt: composeSystemPrompt(sections...),
		Messages:     trimMessages(input.Messages),
	}, nil
}
