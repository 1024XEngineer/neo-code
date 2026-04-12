package context

import (
	"context"
	"os"
	"sync"
	"time"
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

type projectRulesLoader func(ctx context.Context, workdir string) ([]ruleDocument, error)
type ruleFileStat func(path string) (os.FileInfo, error)

type ruleFileSnapshot struct {
	Path    string
	ModTime time.Time
	Size    int64
}

type cachedRuleDocuments struct {
	documents []ruleDocument
	snapshots []ruleFileSnapshot
}

// projectRulesSource 负责发现、缓存并渲染项目规则文件。
type projectRulesSource struct {
	mu        sync.Mutex
	cache     map[string]cachedRuleDocuments
	loadRules projectRulesLoader
	statFile  ruleFileStat
}

// systemStateSource 只负责收集并渲染运行时系统摘要。
type systemStateSource struct {
	gitRunner gitCommandRunner
}

// Sections 汇总 workdir、shell、provider、model 与 git 摘要信息。
func (s *systemStateSource) Sections(ctx context.Context, input BuildInput) ([]promptSection, error) {
	systemState, err := collectSystemState(ctx, input.Metadata, s.gitRunner)
	if err != nil {
		return nil, err
	}
	return []promptSection{renderSystemStateSection(systemState)}, nil
}
