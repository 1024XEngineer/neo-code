//go:build ignore
// +build ignore

package tui

import "context"

// RuntimeEvent 是 TUI 侧消费的运行事件抽象。
type RuntimeEvent struct {
	// Type 是事件类型。
	Type string
	// RunID 是运行标识。
	RunID string
	// SessionID 是会话标识。
	SessionID string
	// Payload 是事件负载。
	Payload any
}

// SessionSummary 是会话摘要视图。
type SessionSummary struct {
	// ID 是会话标识。
	ID string
	// Title 是会话标题。
	Title string
}

// Session 是会话详情视图。
type Session struct {
	// ID 是会话标识。
	ID string
	// Title 是会话标题。
	Title string
}

// RuntimeFacade 是 TUI 使用的单一运行时接口。
type RuntimeFacade interface {
	// Run 提交一次用户输入并启动运行。
	// 输入语义：sessionID 可为空表示新建会话，runID 由调用方生成。
	// 并发约束：应遵守 runtime 的串行运行语义。
	// 生命周期：每次用户发送消息触发一次。
	// 错误语义：返回运行启动失败或运行终态错误。
	Run(ctx context.Context, sessionID string, runID string, content string, workdir string) error
	// CancelActiveRun 取消当前活跃运行。
	// 输入语义：无。
	// 并发约束：线程安全且幂等。
	// 生命周期：用户中断时调用。
	// 错误语义：返回值表示是否命中可取消运行。
	CancelActiveRun() bool
	// Events 返回运行事件流。
	// 输入语义：无。
	// 并发约束：默认单消费者，多消费者需上层扇出。
	// 生命周期：TUI 生命周期内持续订阅。
	// 错误语义：错误通过事件负载表达。
	Events() <-chan RuntimeEvent
	// ListSessions 返回会话摘要列表。
	// 输入语义：ctx 控制读取时限。
	// 并发约束：支持并发读取。
	// 生命周期：会话面板刷新时调用。
	// 错误语义：返回存储读取失败。
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	// LoadSession 加载指定会话详情。
	// 输入语义：sessionID 为会话标识。
	// 并发约束：支持并发读取。
	// 生命周期：切换会话时调用。
	// 错误语义：返回会话不存在或反序列化错误。
	LoadSession(ctx context.Context, sessionID string) (Session, error)
	// SetSessionWorkdir 更新会话工作目录映射。
	// 输入语义：sessionID 必填，workdir 支持相对路径。
	// 并发约束：会话级写入应串行。
	// 生命周期：用户切换会话工作目录时调用。
	// 错误语义：返回路径非法或会话不存在错误。
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (Session, error)
	// Compact 对指定会话执行手动上下文压缩。
	// 输入语义：sessionID 为目标会话。
	// 并发约束：与运行回合互斥。
	// 生命周期：用户显式触发 /compact 时调用。
	// 错误语义：返回压缩执行失败或持久化失败。
	Compact(ctx context.Context, sessionID string, runID string) error
}
