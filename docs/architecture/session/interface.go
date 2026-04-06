//go:build ignore
// +build ignore

package session

import (
	"context"
	"time"
)

// Message 是会话消息最小抽象。
type Message struct {
	// Role 是消息角色。
	Role string
	// Content 是消息文本。
	Content string
}

// Session 是当前会话持久化快照。
type Session struct {
	// ID 是会话标识。
	ID string
	// Title 是会话标题。
	Title string
	// Provider 是最近成功运行使用的 provider。
	Provider string
	// Model 是最近成功运行使用的 model。
	Model string
	// CreatedAt 是创建时间。
	CreatedAt time.Time
	// UpdatedAt 是最后更新时间。
	UpdatedAt time.Time
	// Workdir 是会话关联工作目录。
	Workdir string
	// Messages 是会话消息列表。
	Messages []Message
}

// SessionSummary 是会话摘要视图。
type SessionSummary struct {
	// ID 是会话标识。
	ID string
	// Title 是会话标题。
	Title string
	// CreatedAt 是创建时间。
	CreatedAt time.Time
	// UpdatedAt 是最后更新时间。
	UpdatedAt time.Time
}

// Store 是当前会话持久化契约。
type Store interface {
	// Save 持久化一份完整会话快照。
	// 输入语义：session 是待保存会话，不能为空。
	// 并发约束：同一会话写入应串行，避免覆盖冲突。
	// 生命周期：在回合关键节点由 runtime 调用。
	// 错误语义：返回参数非法、序列化失败或 I/O 失败。
	Save(ctx context.Context, session *Session) error
	// Load 按会话标识加载完整会话。
	// 输入语义：id 是会话标识。
	// 并发约束：支持并发读取。
	// 生命周期：会话恢复、切换或运行前加载。
	// 错误语义：返回不存在、反序列化失败或 I/O 错误。
	Load(ctx context.Context, id string) (Session, error)
	// ListSummaries 返回会话摘要列表。
	// 输入语义：ctx 控制读取时限。
	// 并发约束：支持并发读取。
	// 生命周期：会话列表刷新时调用。
	// 错误语义：返回目录读取失败或解析失败。
	ListSummaries(ctx context.Context) ([]SessionSummary, error)
}

// SessionRuntimeStateStore [PROPOSED] 是运行态存储扩展。
type SessionRuntimeStateStore interface {
	// SaveRuntimeState 保存会话运行态数据。
	// 输入语义：sessionID 为会话标识，state 为运行态快照。
	// 并发约束：会话级串行写入。
	// 生命周期：运行中间态需要恢复时调用。
	// 错误语义：返回序列化失败或存储失败。
	SaveRuntimeState(ctx context.Context, sessionID string, state RuntimeState) error
	// LoadRuntimeState 加载会话运行态数据。
	// 输入语义：sessionID 为会话标识。
	// 并发约束：支持并发读取。
	// 生命周期：运行恢复阶段调用。
	// 错误语义：返回不存在或存储读取失败。
	LoadRuntimeState(ctx context.Context, sessionID string) (RuntimeState, error)
}

// ArchiveStore [PROPOSED] 是会话归档存储扩展。
type ArchiveStore interface {
	// ArchiveSession 归档一份会话快照。
	// 输入语义：session 为待归档会话。
	// 并发约束：归档写入应保证原子性。
	// 生命周期：生命周期治理或 compact 联动时调用。
	// 错误语义：返回归档写入失败。
	ArchiveSession(ctx context.Context, session Session) error
}

// RuntimeState [PROPOSED] 描述会话运行态。
type RuntimeState struct {
	// ActiveRunID 是当前活跃运行标识。
	ActiveRunID string
	// LastTerminalEvent 是最近终态事件类型。
	LastTerminalEvent string
}
