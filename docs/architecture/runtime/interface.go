//go:build ignore
// +build ignore

package runtime

import "context"

// EventType 表示运行时事件类型。
type EventType string

// RuntimeEvent 是 runtime 对外广播的事件。
type RuntimeEvent struct {
	// Type 是事件类型。
	Type EventType
	// RunID 是运行标识。
	RunID string
	// SessionID 是会话标识。
	SessionID string
	// Payload 是事件负载。
	Payload any
}

// UserInput 是一次运行请求输入。
type UserInput struct {
	// SessionID 为空时表示创建新会话。
	SessionID string
	// RunID 是本次请求的幂等标识。
	RunID string
	// Content 是用户输入文本。
	Content string
	// Workdir 是本次运行工作目录覆盖值。
	Workdir string
}

// CompactInput 是手动 compact 请求输入。
type CompactInput struct {
	// SessionID 是目标会话标识。
	SessionID string
	// RunID 是触发本次 compact 的运行标识。
	RunID string
}

// CompactResult 是 compact 执行结果摘要。
type CompactResult struct {
	// Applied 表示是否发生实际压缩。
	Applied bool
	// TriggerMode 表示触发模式。
	TriggerMode string
}

// Runtime 定义编排层核心接口。
type Runtime interface {
	// Run 启动一次完整编排回合。
	// 输入语义：input 提供用户文本、会话标识和运行标识。
	// 并发约束：同一实例下 Run 与 Compact 必须串行执行。
	// 生命周期：从接收输入到发出终态事件结束。
	// 错误语义：返回该回合终态错误，取消时返回 context.Canceled。
	Run(ctx context.Context, input UserInput) error
	// Compact 对指定会话执行手动上下文压缩。
	// 输入语义：input.SessionID 必填。
	// 并发约束：与 Run 互斥，避免并发改写会话。
	// 生命周期：一次 compact_start 到 compact_done/compact_error。
	// 错误语义：返回压缩失败或会话保存失败。
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	// CancelActiveRun 取消当前活跃运行。
	// 输入语义：无。
	// 并发约束：线程安全且幂等。
	// 生命周期：只影响当前活跃 run。
	// 错误语义：返回值表示是否命中可取消运行。
	CancelActiveRun() bool
	// Events 返回 runtime 事件通道。
	// 输入语义：无。
	// 并发约束：默认单消费者，多消费者需上层扇出。
	// 生命周期：runtime 生命周期内持续可读。
	// 错误语义：业务错误通过事件载荷表达。
	Events() <-chan RuntimeEvent
}

// TerminalEventGate [PROPOSED] 管理终态事件唯一性。
type TerminalEventGate interface {
	// TryEmit 尝试提交候选终态事件。
	// 输入语义：runID 为运行标识，eventType 为候选终态。
	// 并发约束：必须线程安全，支持并发竞争。
	// 生命周期：run 结束后应释放内部状态。
	// 错误语义：返回 false 表示被已提交终态抑制。
	TryEmit(runID string, eventType EventType) bool
}

// ReactiveRetryGate [PROPOSED] 管理 reactive 自动重试门禁。
type ReactiveRetryGate interface {
	// Acquire 消耗指定 run 的单次自动重试资格。
	// 输入语义：runID 为运行标识。
	// 并发约束：线程安全且可并发调用。
	// 生命周期：同一 runID 仅允许成功一次。
	// 错误语义：返回 false 表示资格已用尽。
	Acquire(runID string) bool
}
