package context

import (
	"context"

	"neo-code/internal/rules"
)

// promptSectionSource 约束单个 prompt section 来源的最小能力，避免 Builder 持有具体细节。
type promptSectionSource interface {
	Sections(ctx context.Context, input BuildInput) ([]promptSection, error)
}

// SectionSource 是 promptSectionSource 的导出版本，允许外部包实现并注入额外的 prompt section。
type SectionSource = promptSectionSource

// corePromptSource 只负责提供固定核心 system prompt sections。
type corePromptSource struct{}

// Sections 返回当前轮次都需要注入的固定核心提示。
func (corePromptSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]promptSection(nil), defaultSystemPromptSections()...), nil
}

// rulesPromptSource 负责加载并渲染项目与全局规则。
type rulesPromptSource struct {
	loader rules.Loader
}

// newRulesPromptSource 创建默认规则 section source。
func newRulesPromptSource(loader rules.Loader) *rulesPromptSource {
	if loader == nil {
		loader = rules.NewLoader("")
	}
	return &rulesPromptSource{loader: loader}
}

// systemStateSource 只负责收集并渲染运行时系统摘要。
type systemStateSource struct{}

// Sections 汇总 workdir、shell、provider、model 与 git 摘要信息。
func (s *systemStateSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	systemState, err := collectSystemState(ctx, input.Metadata, input.RepositorySummary)
	if err != nil {
		return nil, err
	}
	return []promptSection{renderSystemStateSection(systemState)}, nil
}
