//go:build ignore
// +build ignore

package cli

import "context"

// ProgramBootstrap 是 CLI 启动应用的最小契约。
type ProgramBootstrap interface {
	// Build 构建可运行程序实例。
	// 输入语义：ctx 为应用初始化上下文。
	// 并发约束：通常在启动阶段单次调用。
	// 生命周期：进程启动前调用。
	// 错误语义：返回依赖装配失败或配置校验失败。
	Build(ctx context.Context) (Program, error)
}

// Program 是可运行程序抽象。
type Program interface {
	// Run 启动主循环并阻塞到退出。
	// 输入语义：无。
	// 并发约束：单次运行语义，不应重复并发调用。
	// 生命周期：进程主生命周期。
	// 错误语义：返回运行期异常。
	Run() error
}

// CommandRouter [PROPOSED] 是未来子命令分发契约。
type CommandRouter interface {
	// Execute 执行指定子命令。
	// 输入语义：name 为子命令名，args 为参数数组。
	// 并发约束：命令执行应串行化。
	// 生命周期：每次 CLI 调用执行一次。
	// 错误语义：返回参数错误、命令不存在或执行失败。
	Execute(ctx context.Context, name string, args []string) error
}
