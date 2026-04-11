//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

package config

import "context"

// ProviderConfig 是 provider 配置项。
type ProviderConfig struct {
	// Name 是 provider 名称。
	Name string
	// Driver 是 provider 驱动类型。
	Driver string
	// BaseURL 是 provider 基础地址。
	BaseURL string
	// Model 是默认模型。
	Model string
	// APIKeyEnv 是 API Key 环境变量名。
	APIKeyEnv string
}

// ResolvedProviderConfig 是补齐密钥后的 provider 配置。
type ResolvedProviderConfig struct {
	// ProviderConfig 是基础配置。
	ProviderConfig ProviderConfig
	// APIKey 是解析后的密钥值。
	APIKey string
}

// Config 是运行配置快照。
type Config struct {
	// Providers 是可用 provider 列表。
	Providers []ProviderConfig
	// SelectedProvider 是当前选中 provider。
	SelectedProvider string
	// CurrentModel 是当前模型。
	CurrentModel string
	// Workdir 是默认工作目录。
	Workdir string
	// Shell 是默认 shell。
	Shell string
	// MaxLoops 是最大回合数。
	MaxLoops int
	// ToolTimeoutSec 是工具默认超时秒数。
	ToolTimeoutSec int
}

// Registry 是配置模块主契约。
type Registry interface {
	// Snapshot 返回当前配置快照。
	// 职责：提供统一只读配置视图供上游消费。
	// 输入语义：ctx 控制读取超时与取消。
	// 并发约束：支持并发读，读路径不得阻塞写路径过久。
	// 生命周期：运行时任意阶段可调用，常用于每轮编排前读取。
	// 错误语义：返回加载失败、校验失败或存储读取失败。
	Snapshot(ctx context.Context) (Config, error)
	// Update 以事务方式更新配置并持久化。
	// 职责：统一配置变更入口，保证变更-校验-持久化原子语义。
	// 输入语义：mutate 在配置副本上执行变更逻辑。
	// 并发约束：写入应串行，避免并发丢写。
	// 生命周期：配置修改路径调用。
	// 错误语义：返回 mutate 失败、校验失败或持久化失败。
	Update(ctx context.Context, mutate func(*Config) error) error
	// Watch 注册配置变更监听。
	// 职责：向上游推送配置变更事件，减少轮询。
	// 输入语义：fn 是变更回调函数。
	// 并发约束：回调执行不得阻塞内部写锁。
	// 生命周期：返回 cancel 用于注销监听。
	// 错误语义：不直接返回错误，回调异常由实现方记录。
	Watch(fn func(Config)) (cancel func())
}

// ManagerLike 是兼容层接口，用于迁移期适配历史调用路径。
type ManagerLike interface {
	// Load 从持久化介质加载配置并更新内存快照。
	// 职责：初始化或重新装载配置。
	// 输入语义：ctx 控制读取超时。
	// 并发约束：写入路径串行。
	// 生命周期：启动阶段或显式重载时调用。
	// 错误语义：返回解析失败、校验失败或 I/O 失败。
	Load(ctx context.Context) (Config, error)
	// Reload 重新加载配置。
	// 职责：等价于一次重载流程。
	// 输入语义：ctx 控制重载时限。
	// 并发约束：与其他写操作串行。
	// 生命周期：运行中热重载入口。
	// 错误语义：返回加载或校验失败。
	Reload(ctx context.Context) (Config, error)
	// Get 返回当前内存配置快照。
	// 职责：提供只读配置视图。
	// 输入语义：无。
	// 并发约束：线程安全，允许并发读取。
	// 生命周期：运行时任意阶段可调用。
	// 错误语义：不返回错误，依赖初始化成功。
	Get() Config
	// Save 将当前内存快照写回持久化介质。
	// 职责：显式持久化当前配置。
	// 输入语义：ctx 控制保存时限。
	// 并发约束：写入串行。
	// 生命周期：配置确认落盘时调用。
	// 错误语义：返回序列化失败或 I/O 失败。
	Save(ctx context.Context) error
	// Update 以事务方式修改配置并持久化。
	// 职责：统一配置变更入口。
	// 输入语义：mutate 在配置副本上执行变更。
	// 并发约束：写入串行，避免丢写。
	// 生命周期：配置编辑路径调用。
	// 错误语义：返回校验失败或保存失败。
	Update(ctx context.Context, mutate func(*Config) error) error
	// SelectedProvider 返回当前选中 provider 配置。
	// 职责：读取当前 provider 基础配置。
	// 输入语义：无。
	// 并发约束：线程安全读取。
	// 生命周期：provider 构建前调用。
	// 错误语义：返回选中项不存在或配置非法。
	SelectedProvider() (ProviderConfig, error)
	// ResolvedSelectedProvider 返回补齐密钥的选中 provider 配置。
	// 职责：向下游提供可直接调用的 provider 配置。
	// 输入语义：无。
	// 并发约束：线程安全读取。
	// 生命周期：provider 构建阶段调用。
	// 错误语义：返回环境变量缺失、配置非法等错误。
	ResolvedSelectedProvider() (ResolvedProviderConfig, error)
	// BaseDir 返回配置根目录。
	// 职责：暴露配置存储目录。
	// 输入语义：无。
	// 并发约束：线程安全读取。
	// 生命周期：初始化与诊断场景调用。
	// 错误语义：无。
	BaseDir() string
	// ConfigPath 返回配置文件路径。
	// 职责：暴露配置文件绝对路径。
	// 输入语义：无。
	// 并发约束：线程安全读取。
	// 生命周期：诊断与编辑场景调用。
	// 错误语义：无。
	ConfigPath() string
}