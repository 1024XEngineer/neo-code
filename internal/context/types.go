package context

import (
	"context"

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
	Messages     []providertypes.Message
	TaskState    agentsession.TaskState
	Todos        []agentsession.TodoItem
	ActiveSkills []skills.Skill
	Metadata     Metadata
	Compact      CompactOptions
}

// BuildResult is the provider-facing context produced for a single round.
type BuildResult struct {
	SystemPrompt         string
	Messages             []providertypes.Message
	AutoCompactSuggested bool
}

// MicroCompactPolicySource 定义 context 读取工具 micro compact 策略的最小依赖。
type MicroCompactPolicySource interface {
	MicroCompactPolicy(name string) tools.MicroCompactPolicy
}

// CompactOptions controls read-time compact behavior inside the context builder.
type CompactOptions struct {
	DisableMicroCompact           bool
	AutoCompactThreshold          int
	MicroCompactRetainedToolSpans int
	ReadTimeMaxMessageSpans       int
}
