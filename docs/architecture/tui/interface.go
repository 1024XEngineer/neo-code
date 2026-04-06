//go:build ignore
// +build ignore

package tui

import "context"

// RuntimeClient 是 TUI 需要的最小 runtime 能力集合。
type RuntimeClient interface {
	// Run 发送一次用户输入并启动运行。
	// 输入语义：sessionID/runID/content 由 TUI 侧组装。
	// 并发约束：应与 runtime 的串行策略保持一致。
	// 生命周期：每次提交触发一次运行。
	// 错误语义：返回运行启动失败或终态错误。
	Run(ctx context.Context, sessionID string, runID string, content string, workdir string) error
	// CancelActiveRun 取消当前活跃运行。
	// 输入语义：无。
	// 并发约束：线程安全。
	// 生命周期：用户主动中断时调用。
	// 错误语义：返回值表示是否命中可取消运行。
	CancelActiveRun() bool
}

// EventSubscriber 是 TUI 的事件订阅契约。
type EventSubscriber interface {
	// Events 返回只读事件流。
	// 输入语义：无。
	// 并发约束：若多消费者需上层扇出。
	// 生命周期：应用生命周期内持续有效。
	// 错误语义：错误通过事件载荷传递。
	Events() <-chan any
}
