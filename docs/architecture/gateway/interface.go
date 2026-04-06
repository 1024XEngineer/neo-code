//go:build ignore
// +build ignore

package gateway

import "context"

// MessageFrame 是网关与客户端通信帧。
type MessageFrame struct {
	// Type 是消息类型，例如 run_start、runtime_event。
	Type string `json:"type"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// Payload 是业务负载。
	Payload any `json:"payload,omitempty"`
}

// ChatGateway 定义网关生命周期契约。
type ChatGateway interface {
	// Start 启动网关服务。
	// 输入语义：ctx 控制服务生命周期。
	// 并发约束：需支持多连接并发。
	// 生命周期：进程启动阶段调用。
	// 错误语义：返回监听失败或依赖初始化失败。
	Start(ctx context.Context) error
	// Close 关闭网关并回收连接资源。
	// 输入语义：ctx 控制关闭超时。
	// 并发约束：需幂等。
	// 生命周期：进程退出阶段调用。
	// 错误语义：返回未完成关闭或清理失败。
	Close(ctx context.Context) error
}

// RuntimeBridge 定义网关调用 runtime 的适配契约。
type RuntimeBridge interface {
	// SubmitRun 提交一次运行请求。
	// 输入语义：frame 为已经过协议校验的请求帧。
	// 并发约束：同一会话请求应保持顺序语义。
	// 生命周期：每次用户输入触发一次提交。
	// 错误语义：返回请求校验失败或运行启动失败。
	SubmitRun(ctx context.Context, frame MessageFrame) error
}
