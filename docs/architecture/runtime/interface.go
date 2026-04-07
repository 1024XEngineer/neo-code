//go:build ignore
// +build ignore

package runtime

import (
	"context"
	"time"

	"neo-code/internal/provider"
)

// EventType 表示运行时事件类型。
type EventType string

const (
	// EventRunStart 表示运行开始。
	EventRunStart EventType = "run_start"
	// EventRunProgress 表示运行过程事件。
	EventRunProgress EventType = "run_progress"
	// EventRunDone 表示运行成功结束。
	EventRunDone EventType = "run_done"
	// EventRunError 表示运行失败结束。
	EventRunError EventType = "run_error"
	// EventRunCanceled 表示运行被取消。
	EventRunCanceled EventType = "run_canceled"
	// EventPermissionRequest 表示权限审批请求。
	EventPermissionRequest EventType = "permission_request"
	// EventPermissionResolved 表示权限审批结果。
	EventPermissionResolved EventType = "permission_resolved"
	// EventCompactStart 表示压缩开始。
	EventCompactStart EventType = "compact_start"
	// EventCompactDone 表示压缩完成。
	EventCompactDone EventType = "compact_done"
	// EventCompactError 表示压缩失败。
	EventCompactError EventType = "compact_error"
)

// RuntimeEvent 是运行时对外广播的事件信封。
type RuntimeEvent struct {
	// Type 是事件类型。
	Type EventType
	// RunID 是运行标识。
	RunID string
	// SessionID 是会话标识。
	SessionID string
	// Payload 是事件负载。
	Payload any
}

// Session 是会话快照。
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
	Messages []provider.Message
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

// UserInput 是一次运行请求输入。
type UserInput struct {
	// SessionID 为空时表示创建新会话。
	SessionID string
	// RunID 是本次请求的幂等标识。
	RunID string
	// Content 是用户输入文本。
	Content string
	// InputParts 是多模态输入分片，若非空优先于 Content 参与组装。
	InputParts []provider.MessagePart
	// Workdir 是本次运行工作目录覆盖值。
	Workdir string
}

// CompactInput 是手动 compact 请求输入。
type CompactInput struct {
	// SessionID 是目标会话标识。
	SessionID string
	// RunID 是触发本次 compact 的运行标识。
	RunID string
}

// CompactResult 是 compact 执行结果摘要。
type CompactResult struct {
	// Applied 表示是否发生实际压缩。
	Applied bool
	// BeforeChars 是压缩前字符数。
	BeforeChars int
	// AfterChars 是压缩后字符数。
	AfterChars int
	// SavedRatio 是节省比例。
	SavedRatio float64
	// TriggerMode 是触发模式。
	TriggerMode string
	// TranscriptID 是压缩转录标识。
	TranscriptID string
	// TranscriptPath 是压缩转录路径。
	TranscriptPath string
}

// CompactStartPayload 是 compact 开始事件负载。
type CompactStartPayload struct {
	// TriggerMode 是触发模式。
	TriggerMode string `json:"trigger_mode"`
}

// CompactDonePayload 是 compact 完成事件负载。
type CompactDonePayload struct {
	// Applied 表示是否发生实际压缩。
	Applied bool `json:"applied"`
	// BeforeChars 是压缩前字符数。
	BeforeChars int `json:"before_chars"`
	// AfterChars 是压缩后字符数。
	AfterChars int `json:"after_chars"`
	// SavedRatio 是节省比例。
	SavedRatio float64 `json:"saved_ratio"`
	// TriggerMode 是触发模式。
	TriggerMode string `json:"trigger_mode"`
	// TranscriptID 是压缩转录标识。
	TranscriptID string `json:"transcript_id"`
	// TranscriptPath 是压缩转录路径。
	TranscriptPath string `json:"transcript_path"`
}

// CompactErrorPayload 是 compact 失败事件负载。
type CompactErrorPayload struct {
	// TriggerMode 是触发模式。
	TriggerMode string `json:"trigger_mode"`
	// Message 是错误信息。
	Message string `json:"message"`
}

// PermissionRequestPayload 是审批请求事件负载。
type PermissionRequestPayload struct {
	// ToolName 是工具名称。
	ToolName string `json:"tool_name"`
	// Reason 是请求审批原因。
	Reason string `json:"reason"`
}

// PermissionResolvedPayload 是审批结果事件负载。
type PermissionResolvedPayload struct {
	// ToolName 是工具名称。
	ToolName string `json:"tool_name"`
	// Decision 是审批结果，例如 approved/denied。
	Decision string `json:"decision"`
}

// Orchestrator 定义运行时编排主契约。
type Orchestrator interface {
	// Run 启动一次完整编排回合。
	// 职责：推进用户输入到终态事件。
	// 输入语义：input 提供会话标识、运行标识、文本或多模态输入以及工作目录覆盖。
	// 并发约束：同一实例下 Run 与 Compact 必须串行执行。
	// 生命周期：从接收输入到发出终态事件结束。
	// 错误语义：返回该回合终态错误，取消时返回 context.Canceled。
	Run(ctx context.Context, input UserInput) error
	// Compact 对指定会话执行手动上下文压缩。
	// 职责：触发一次手动 compact 并回写会话。
	// 输入语义：input.SessionID 必填。
	// 并发约束：与 Run 互斥，避免并发改写会话。
	// 生命周期：一次 compact_start 到 compact_done/compact_error。
	// 错误语义：返回压缩失败或会话保存失败。
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	// CancelActiveRun 取消当前活跃运行。
	// 职责：向当前活跃 run 下发取消信号。
	// 输入语义：无。
	// 并发约束：线程安全且幂等。
	// 生命周期：只影响当前活跃 run。
	// 错误语义：返回值表示是否命中可取消运行。
	CancelActiveRun() bool
	// Events 返回 runtime 事件通道。
	// 职责：向上游输出运行过程与终态事件。
	// 输入语义：无。
	// 并发约束：默认单消费者，多消费者需上层扇出。
	// 生命周期：runtime 生命周期内持续可读。
	// 错误语义：业务错误通过事件负载表达。
	Events() <-chan RuntimeEvent
	// ListSessions 返回会话摘要列表。
	// 职责：提供会话面板所需摘要数据。
	// 输入语义：ctx 控制读取时限。
	// 并发约束：支持并发读取。
	// 生命周期：可在任意空闲阶段调用。
	// 错误语义：返回存储读取失败或反序列化失败。
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	// LoadSession 加载指定会话详情。
	// 职责：恢复目标会话完整状态。
	// 输入语义：id 为会话标识。
	// 并发约束：支持并发读取。
	// 生命周期：切换会话或启动回合前调用。
	// 错误语义：返回会话不存在或存储读取错误。
	LoadSession(ctx context.Context, id string) (Session, error)
	// SetSessionWorkdir 更新会话工作目录映射。
	// 职责：设置并持久化会话级 workdir。
	// 输入语义：sessionID 必填，workdir 支持相对路径输入。
	// 并发约束：会话级写入应串行。
	// 生命周期：用户切换会话工作目录时调用。
	// 错误语义：返回路径非法、会话不存在或保存失败。
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (Session, error)
	// TryEmitTerminal 尝试提交候选终态事件。
	// 职责：保证单个 run_id 仅输出一个终态。
	// 输入语义：runID 为运行标识，eventType 为候选终态。
	// 并发约束：必须线程安全，支持并发竞争。
	// 生命周期：run 结束后应释放内部状态。
	// 错误语义：返回 false 表示被已提交终态抑制。
	TryEmitTerminal(runID string, eventType EventType) bool
	// AcquireReactiveRetry 消耗指定 run 的单次自动重试资格。
	// 职责：避免上下文过长场景出现无限重试循环。
	// 输入语义：runID 为运行标识。
	// 并发约束：线程安全且可并发调用。
	// 生命周期：同一 runID 仅允许成功一次。
	// 错误语义：返回 false 表示资格已用尽。
	AcquireReactiveRetry(runID string) bool
}
