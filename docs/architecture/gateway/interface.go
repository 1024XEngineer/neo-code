//go:build ignore
// +build ignore

package gateway

import (
	"context"

	"neo-code/internal/provider"
	"neo-code/internal/runtime"
)

// FrameType 表示网关协议帧类型。
type FrameType string

const (
	// FrameTypeRequest 表示客户端到网关的请求帧。
	FrameTypeRequest FrameType = "request"
	// FrameTypeEvent 表示网关到客户端的事件帧。
	FrameTypeEvent FrameType = "event"
	// FrameTypeError 表示网关到客户端的错误帧。
	FrameTypeError FrameType = "error"
	// FrameTypeAck 表示网关对请求的接收确认帧。
	FrameTypeAck FrameType = "ack"
)

// FrameAction 表示请求动作类型。
type FrameAction string

const (
	// FrameActionRun 表示发起一次运行。
	FrameActionRun FrameAction = "run"
	// FrameActionCompact 表示触发手动压缩。
	FrameActionCompact FrameAction = "compact"
	// FrameActionCancel 表示取消当前活跃运行。
	FrameActionCancel FrameAction = "cancel"
	// FrameActionListSessions 表示获取会话摘要列表。
	FrameActionListSessions FrameAction = "list_sessions"
	// FrameActionLoadSession 表示加载指定会话。
	FrameActionLoadSession FrameAction = "load_session"
	// FrameActionSetSessionWorkdir 表示更新会话工作目录映射。
	FrameActionSetSessionWorkdir FrameAction = "set_session_workdir"
)

// FrameError 表示协议帧中的错误信息。
type FrameError struct {
	// Code 是稳定错误码，供客户端执行分支判断。
	Code string `json:"code"`
	// Message 是可读错误消息。
	Message string `json:"message"`
}

// MessageFrame 是网关与客户端之间的统一通信帧。
type MessageFrame struct {
	// Type 是帧类别。
	Type FrameType `json:"type"`
	// Action 是请求动作，事件帧可为空。
	Action FrameAction `json:"action,omitempty"`
	// RequestID 是客户端请求幂等标识。
	RequestID string `json:"request_id,omitempty"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// InputText 是文本输入内容。
	InputText string `json:"input_text,omitempty"`
	// InputParts 是多模态输入分片，语义与 provider.MessagePart 保持一致。
	InputParts []provider.MessagePart `json:"input_parts,omitempty"`
	// Workdir 是本次请求工作目录覆盖值。
	Workdir string `json:"workdir,omitempty"`
	// Payload 是动作扩展负载或事件负载。
	Payload any `json:"payload,omitempty"`
	// Error 是错误帧负载。
	Error *FrameError `json:"error,omitempty"`
}

// RuntimePort 定义网关访问运行时编排的下游端口契约。
type RuntimePort interface {
	// Run 启动一次运行编排。
	// 职责：接收网关映射后的运行输入并触发编排主循环。
	// 输入语义：input 为网关转换后的运行参数，含会话、运行、多模态输入与工作目录覆盖信息。
	// 并发约束：实现必须支持并发调用，并在会话级保持一致的并发语义。
	// 生命周期：每个客户端 run 请求触发一次调用。
	// 错误语义：返回运行初始化失败、参数非法或上下文取消错误。
	Run(ctx context.Context, input runtime.UserInput) error
	// Compact 对指定会话触发手动压缩。
	// 职责：执行一次独立压缩流程并返回摘要结果。
	// 输入语义：input.SessionID 必填，input.RunID 作为追踪标识。
	// 并发约束：应与 Run 在会话维度互斥，避免并发写入冲突。
	// 生命周期：每次 compact 请求触发一次调用。
	// 错误语义：返回压缩失败、会话不存在或存储失败错误。
	Compact(ctx context.Context, input runtime.CompactInput) (runtime.CompactResult, error)
	// CancelActiveRun 取消当前活跃运行。
	// 职责：向活跃运行传播取消信号。
	// 输入语义：无输入参数。
	// 并发约束：必须幂等且线程安全。
	// 生命周期：可在运行期间重复调用。
	// 错误语义：返回值 false 表示当前无可取消运行。
	CancelActiveRun() bool
	// Events 返回运行时事件流。
	// 职责：为网关提供统一事件订阅入口。
	// 输入语义：无输入参数。
	// 并发约束：消费模型由实现定义，网关需按约定进行单播或扇出。
	// 生命周期：在运行时实例存续期间持续有效。
	// 错误语义：业务错误通过事件负载表达。
	Events() <-chan runtime.RuntimeEvent
	// ListSessions 返回会话摘要列表。
	// 职责：支持客户端会话面板查询。
	// 输入语义：ctx 控制请求超时和取消。
	// 并发约束：必须支持并发读取。
	// 生命周期：可在任意空闲阶段调用。
	// 错误语义：返回存储读取失败或反序列化失败错误。
	ListSessions(ctx context.Context) ([]runtime.SessionSummary, error)
	// LoadSession 加载指定会话详情。
	// 职责：恢复会话完整快照用于后续运行或展示。
	// 输入语义：id 为目标会话标识。
	// 并发约束：必须支持并发读取。
	// 生命周期：会话切换或恢复阶段调用。
	// 错误语义：返回会话不存在、读取失败或解码失败错误。
	LoadSession(ctx context.Context, id string) (runtime.Session, error)
	// SetSessionWorkdir 更新会话工作目录映射。
	// 职责：持久化会话级工作目录变更。
	// 输入语义：sessionID 必填，workdir 为待更新路径。
	// 并发约束：会话级写入应串行化。
	// 生命周期：客户端修改会话工作目录时调用。
	// 错误语义：返回路径非法、会话不存在或保存失败错误。
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (runtime.Session, error)
}

// Gateway 定义网关主契约。
type Gateway interface {
	// Serve 启动网关主循环并绑定运行时端口。
	// 职责：监听客户端连接、解析协议帧、映射为 RuntimePort 调用，并把运行时事件回推给客户端。
	// 输入语义：runtimePort 为网关下游端口，必须在服务周期内保持可用。
	// 并发约束：必须支持多连接并发与多会话并行。
	// 生命周期：进程运行期间启动一次，退出前通过 Close 收敛。
	// 错误语义：返回监听失败、协议处理失败或下游桥接不可恢复错误。
	Serve(ctx context.Context, runtimePort RuntimePort) error
	// Close 优雅关闭网关。
	// 职责：停止接收新连接并收敛现有连接与后台任务。
	// 输入语义：ctx 控制关闭超时。
	// 并发约束：必须幂等且线程安全。
	// 生命周期：服务终止阶段调用。
	// 错误语义：返回资源回收失败或超时错误。
	Close(ctx context.Context) error
}
