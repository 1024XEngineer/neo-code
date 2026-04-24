package context

import (
	"context"

	"neo-code/internal/context/repository"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

// Builder builds the provider-facing context for a single model round.
type Builder interface {
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// BuildInput contains the runtime state needed to assemble model context.
type BuildInput struct {
	Messages          []providertypes.Message
	TaskState         agentsession.TaskState
	Todos             []agentsession.TodoItem
	ActiveSkills      []skills.Skill
	RepositorySummary *RepositorySummarySection
	Repository        RepositoryContext
	Metadata          Metadata
	Compact           CompactOptions
}

// BuildResult is the provider-facing context produced for a single round.
type BuildResult struct {
	SystemPrompt string
	Messages     []providertypes.Message
}

// RepositorySummarySection 承载 runtime 已决策好的最小 repository summary 投影。
type RepositorySummarySection struct {
	InGitRepo bool
	Branch    string
	Dirty     bool
	Ahead     int
	Behind    int
}

// RepositoryContext 承载 runtime 已决策好的 repository 事实投影，供 context 只读渲染。
type RepositoryContext struct {
	ChangedFiles *RepositoryChangedFilesSection
	Retrieval    *RepositoryRetrievalSection
}

// RepositoryChangedFilesSection 描述当前轮允许注入的变更文件摘要。
type RepositoryChangedFilesSection struct {
	Files         []repository.ChangedFile
	Truncated     bool
	ReturnedCount int
	TotalCount    int
}

// RepositoryRetrievalSection 描述当前轮允许注入的定向检索结果。
type RepositoryRetrievalSection struct {
	Hits      []repository.RetrievalHit
	Truncated bool
	Mode      string
	Query     string
}

// MicroCompactPolicySource 定义 context 读取工具 micro compact 策略的最小依赖。
type MicroCompactPolicySource interface {
	MicroCompactPolicy(name string) tools.MicroCompactPolicy
}

// MicroCompactSummarizerSource 定义 context 查找按工具内容摘要器的最小依赖。
type MicroCompactSummarizerSource interface {
	MicroCompactSummarizer(name string) tools.ContentSummarizer
}

// MicroCompactPinChecker 定义上下文层判断单个工具结果是否应钉住（不参与微压缩）的接口。
type MicroCompactPinChecker interface {
	ShouldPin(toolName string, metadata map[string]string) bool
}

// MicroCompactConfig 聚合微压缩所需的三个依赖源，简化 Builder 构造参数。
// 三个子接口仍各自遵循接口隔离原则；MicroCompactConfig 仅作为构造时的参数打包容器。
type MicroCompactConfig struct {
	Policies    MicroCompactPolicySource
	Summarizers MicroCompactSummarizerSource
	PinChecker  MicroCompactPinChecker
}

// CompactOptions controls read-time compact behavior inside the context builder.
type CompactOptions struct {
	DisableMicroCompact           bool
	MicroCompactRetainedToolSpans int
	ReadTimeMaxMessageSpans       int
}
