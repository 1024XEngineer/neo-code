//go:build ignore
// +build ignore

package cli

import (
	"context"
	"io"

	"neo-code/internal/gateway"
	"neo-code/internal/provider"
)

// Mode 表示 CLI 解析后的执行模式。
type Mode string

const (
	// ModeTUI 表示交互式终端界面模式。
	ModeTUI Mode = "tui"
	// ModeExec 表示无头执行模式。
	ModeExec Mode = "exec"
)

// ExecOutputMode 表示无头执行输出模式。
type ExecOutputMode string

const (
	// ExecOutputStream 表示实时流式输出模式。
	ExecOutputStream ExecOutputMode = "stream"
	// ExecOutputFinal 表示仅输出最终结果模式。
	ExecOutputFinal ExecOutputMode = "final"
)

// Invocation 描述一次 CLI 调用上下文。
type Invocation struct {
	// Argv 是命令行参数序列，不包含可执行文件自身。
	Argv []string
	// Stdin 是标准输入流。
	Stdin io.Reader
	// Stdout 是标准输出流。
	Stdout io.Writer
	// Stderr 是标准错误输出流。
	Stderr io.Writer
	// Workdir 是调用时工作目录。
	Workdir string
}

// ExecRequest 描述无头执行请求。
type ExecRequest struct {
	// Prompt 是无头执行文本输入。
	Prompt string
	// InputParts 是多模态输入分片，语义与 provider.MessagePart 一致。
	InputParts []provider.MessagePart
	// SessionID 是目标会话标识，可为空表示新会话。
	SessionID string
	// Workdir 是本次执行工作目录覆盖值。
	Workdir string
	// OutputMode 是输出模式，默认为流式输出。
	OutputMode ExecOutputMode
}

// DispatchResult 描述 CLI 参数解析结果。
type DispatchResult struct {
	// Mode 是解析出的执行模式。
	Mode Mode
	// Exec 是无头执行请求，仅在 ModeExec 下有效。
	Exec *ExecRequest
}

// GatewayPort 定义 CLI 访问网关的下游端口契约。
type GatewayPort interface {
	// Send 发送一条协议帧到网关。
	// 职责：将 CLI 动作转换为网关协议请求。
	// 输入语义：frame 为已通过 CLI 侧语义校验的请求帧。
	// 并发约束：实现必须支持并发发送并保持同会话顺序语义。
	// 生命周期：在一次 CLI 执行周期内可重复调用。
	// 错误语义：返回连接失败、协议拒绝或下游不可用错误。
	Send(ctx context.Context, frame gateway.MessageFrame) error
	// Events 返回网关事件流。
	// 职责：为无头执行提供事件消费入口。
	// 输入语义：无输入参数。
	// 并发约束：消费模型由实现约束，CLI 按约定单播或扇出消费。
	// 生命周期：连接存续期间持续有效。
	// 错误语义：业务错误通过事件帧承载，不通过该方法返回。
	Events() <-chan gateway.MessageFrame
	// Close 关闭网关连接。
	// 职责：收敛连接与后台接收任务。
	// 输入语义：ctx 控制关闭超时。
	// 并发约束：必须幂等且线程安全。
	// 生命周期：CLI 退出前调用一次。
	// 错误语义：返回关闭超时或资源释放失败错误。
	Close(ctx context.Context) error
}

// CLI 定义命令行入口唯一主契约。
type CLI interface {
	// Parse 解析一次命令调用并产生命令分派结果。
	// 职责：把 argv 解析为稳定模式语义并完成基础参数校验。
	// 输入语义：inv.Argv 为原始参数序列。
	// 并发约束：实现必须支持并发解析调用。
	// 生命周期：每次进程执行调用一次。
	// 错误语义：返回参数非法、子命令未知或组合冲突错误。
	// 语义约束：空子命令 MUST 解析为 ModeTUI；`exec` MUST 解析为 ModeExec；`chat` SHOULD 返回未知子命令提示。
	Parse(ctx context.Context, inv Invocation) (DispatchResult, error)
	// Run 执行一次 CLI 调用。
	// 职责：按分派结果启动 TUI 或执行无头模式并输出结果。
	// 输入语义：inv 包含参数与标准流句柄，gw 为网关端口。
	// 并发约束：同一 CLI 实例内单次调用语义，不应并发重复执行。
	// 生命周期：进程入口调用，直至命令完成或失败退出。
	// 错误语义：返回网关交互失败、执行失败或输出阶段错误。
	Run(ctx context.Context, inv Invocation, gw GatewayPort) error
}
