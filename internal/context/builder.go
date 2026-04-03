package context

import "context"

// DefaultBuilder preserves the current runtime context-building behavior.
// DefaultBuilder 是 Context 构建器的默认实现，负责把运行时状态整理成模型可消费的输入。
type DefaultBuilder struct {
	// gitRunner 用于采集 git 状态，保留为可注入函数，便于测试替换。
	gitRunner gitCommandRunner
}

// NewBuilder returns the default context builder implementation.
// NewBuilder 返回默认构建器；当前项目的上下文构建统一从这里进入。
func NewBuilder() Builder {
	return &DefaultBuilder{
		gitRunner: runGitCommand,
	}
}

// Build assembles the provider-facing context for the current round.
// Build 按固定顺序构建本轮上下文：
// 1) 检查上下文取消状态；
// 2) 读取项目规则（如 AGENTS.md）；
// 3) 收集系统态（workdir/shell/provider/model/git）；
// 4) 拼装系统提示词并裁剪历史消息。
func (b *DefaultBuilder) Build(ctx context.Context, input BuildInput) (BuildResult, error) {
	if err := ctx.Err(); err != nil {
		return BuildResult{}, err
	}

	// 项目规则会按目录层级汇总并受长度预算限制，避免 system prompt 失控增长。
	rules, err := loadProjectRules(ctx, input.Metadata.Workdir)
	if err != nil {
		return BuildResult{}, err
	}

	// 系统态为模型提供当前执行环境信息，帮助其做出更准确决策。
	systemState, err := collectSystemState(ctx, input.Metadata, b.gitRunner)
	if err != nil {
		return BuildResult{}, err
	}

	// 先放默认约束，再追加项目规则和系统态，确保提示结构稳定可预测。
	sections := append([]promptSection{}, defaultSystemPromptSections()...)
	sections = append(sections, renderProjectRulesSection(rules))
	sections = append(sections, renderSystemStateSection(systemState))

	return BuildResult{
		// Messages 会进行窗口裁剪，避免超长历史影响模型性能与成本。
		SystemPrompt: composeSystemPrompt(sections...),
		Messages:     trimMessages(input.Messages),
	}, nil
}
