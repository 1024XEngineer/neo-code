package tools

import (
	"context"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
)

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any
	MicroCompactPolicy() MicroCompactPolicy
	Execute(ctx context.Context, call ToolCallInput) (ToolResult, error)
}

// ChunkEmitter 是工具执行过程中向上游发送流式分片的回调。
// 并发语义：
// - 单次 Execute 内允许调用 0 次或多次；
// - 同一次 Execute 内默认要求串行调用，工具实现不应并发调用同一个 emitter；
// - 若工具确需跨 goroutine 使用，必须自行保证顺序、同步与上游消费方的并发安全契约；
// - 调用方若返回非 nil error，工具应停止后续分片发送并尽快中止执行。
// 内存语义：
// - 回调返回后不得继续持有传入的 chunk 引用，若需异步使用必须先复制。
type ChunkEmitter func(chunk []byte) error

type ToolCallInput struct {
	ID            string
	Name          string
	Arguments     []byte
	SessionID     string
	Workdir       string
	WorkspacePlan *security.WorkspaceExecutionPlan
	// EmitChunk 为流式分片回调，语义见 ChunkEmitter 注释。
	EmitChunk ChunkEmitter
}

type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
	Metadata   map[string]any
}

type ToolSpec = providertypes.ToolSpec
