//go:build ignore
// +build ignore

package tui

import (
	"context"

	"neo-code/internal/gateway"
	"neo-code/internal/provider"
)

// ComposeRunInput 描述 TUI 组装运行请求所需输入。
type ComposeRunInput struct {
	// SessionID 是目标会话标识，可为空表示新会话。
	SessionID string
	// RunID 是本次运行标识，可为空由下游生成。
	RunID string
	// InputText 是文本输入。
	InputText string
	// InputParts 是多模态输入分片，语义与 provider.MessagePart 一致。
	InputParts []provider.MessagePart
	// Workdir 是本次运行工作目录覆盖值。
	Workdir string
}

// GatewayFacade 定义 TUI 访问网关的单一端口契约。
type GatewayFacade interface {
	// Send 发送请求帧到网关。
	// 职责：把 TUI 动作转换为网关协议请求。
	// 输入语义：frame 为符合网关协议的请求帧。
	// 并发约束：必须支持并发发送并保证同会话顺序语义。
	// 生命周期：TUI 会话生命周期内可重复调用。
	// 错误语义：返回连接失败、协议拒绝或下游不可用错误。
	Send(ctx context.Context, frame gateway.MessageFrame) error
	// Events 返回网关事件流。
	// 职责：为 TUI 提供统一事件订阅入口。
	// 输入语义：无输入参数。
	// 并发约束：默认单消费语义，多消费者需上层扇出。
	// 生命周期：连接有效期内持续可读。
	// 错误语义：业务错误通过事件帧承载，不通过该方法返回。
	Events() <-chan gateway.MessageFrame
	// Close 关闭网关连接。
	// 职责：收敛连接与后台事件接收任务。
	// 输入语义：ctx 控制关闭超时。
	// 并发约束：必须幂等且线程安全。
	// 生命周期：TUI 退出前调用。
	// 错误语义：返回超时或资源释放失败错误。
	Close(ctx context.Context) error
}

// TUI 定义终端界面唯一主契约。
type TUI interface {
	// Run 启动 TUI 主循环。
	// 职责：驱动输入采集、视图渲染、网关通信与事件消费闭环。
	// 输入语义：gw 为网关门面，必须在整个主循环期间可用。
	// 并发约束：单实例单主循环语义，不应并发重复启动。
	// 生命周期：从界面启动到用户退出。
	// 错误语义：返回初始化失败、网关连接失败或主循环异常。
	Run(ctx context.Context, gw GatewayFacade) error
	// BuildRunFrame 组装运行请求帧。
	// 职责：把输入框文本与附件转换为标准 run 请求帧。
	// 输入语义：input 同时支持文本与多模态分片输入。
	// 并发约束：纯组装逻辑，应支持并发调用。
	// 生命周期：每次用户提交消息时调用。
	// 错误语义：返回输入非法、附件不合规或组装失败错误。
	BuildRunFrame(input ComposeRunInput) (gateway.MessageFrame, error)
	// BuildCompactFrame 组装 compact 请求帧。
	// 职责：生成手动压缩请求。
	// 输入语义：sessionID 必填，runID 可选。
	// 并发约束：纯组装逻辑，应支持并发调用。
	// 生命周期：用户触发 compact 命令时调用。
	// 错误语义：返回参数非法错误。
	BuildCompactFrame(sessionID string, runID string) (gateway.MessageFrame, error)
	// BuildCancelFrame 组装 cancel 请求帧。
	// 职责：生成取消活跃运行请求。
	// 输入语义：runID 可选，用于增强追踪。
	// 并发约束：纯组装逻辑，应支持并发调用。
	// 生命周期：用户触发取消时调用。
	// 错误语义：返回参数非法错误。
	BuildCancelFrame(runID string) (gateway.MessageFrame, error)
}
