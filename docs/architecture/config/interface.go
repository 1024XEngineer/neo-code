//go:build ignore
// +build ignore

package config

import "context"

// Config 是运行配置快照。
type Config struct {
	// SelectedProvider 是当前选中 provider。
	SelectedProvider string
	// CurrentModel 是当前模型。
	CurrentModel string
	// Workdir 是默认工作目录。
	Workdir string
}

// Registry [PROPOSED] 是统一配置读写契约。
type Registry interface {
	// Snapshot 返回当前配置快照。
	// 输入语义：ctx 控制读取超时。
	// 并发约束：支持并发读，写时序列化。
	// 生命周期：Run/Compact 前按需调用。
	// 错误语义：返回配置加载失败或校验失败。
	Snapshot(ctx context.Context) (Config, error)
	// Update 以事务方式更新配置。
	// 输入语义：mutate 在配置副本上执行变更。
	// 并发约束：串行化写入，避免丢写。
	// 生命周期：配置变更入口。
	// 错误语义：返回校验失败或持久化失败。
	Update(ctx context.Context, mutate func(*Config) error) error
	// Watch 注册配置变更监听。
	// 输入语义：fn 是变更回调函数。
	// 并发约束：回调不得阻塞写锁。
	// 生命周期：返回 cancel 用于注销监听。
	// 错误语义：无错误返回，回调异常由实现方记录。
	Watch(fn func(Config)) (cancel func())
}

// ManagerLike [CURRENT] 对齐当前 manager 的稳定能力。
type ManagerLike interface {
	// Get 返回当前内存配置快照。
	// 输入语义：无。
	// 并发约束：线程安全，允许并发读取。
	// 生命周期：运行时任意阶段可调用。
	// 错误语义：不返回错误，依赖初始化成功。
	Get() Config
	// Update 更新配置并持久化。
	// 输入语义：mutate 在副本上执行变更。
	// 并发约束：写入串行。
	// 生命周期：配置编辑路径调用。
	// 错误语义：返回校验失败与保存失败。
	Update(ctx context.Context, mutate func(*Config) error) error
}
