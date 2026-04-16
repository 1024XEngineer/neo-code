package runtime

import (
	"time"

	"neo-code/internal/runtime/controlplane"
)

// EventType 标识 runtime 事件类型。
type EventType string

// RuntimeEvent 是 runtime 对外发送的统一事件结构。
type RuntimeEvent struct {
	Type           EventType
	RunID          string
	SessionID      string
	Turn           int
	Phase          string
	Timestamp      time.Time
	PayloadVersion int
	Payload        any
}

// PhaseChangedPayload 描述 phase 迁移。
type PhaseChangedPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// BudgetCheckedPayload 为预算检查预留负载。
type BudgetCheckedPayload struct {
	Note string `json:"note,omitempty"`
}

// ProgressEvaluatedPayload 汇总 progress 控制面的评估结果。
type ProgressEvaluatedPayload struct {
	Score controlplane.ProgressScore `json:"score"`
}

// StopReasonDecidedPayload 承载唯一停止原因决议结果。
type StopReasonDecidedPayload struct {
	Reason controlplane.StopReason `json:"reason"`
	Detail string                  `json:"detail,omitempty"`
}

// LedgerReconciledPayload 为账本对账预留负载。
type LedgerReconciledPayload struct {
	Note string `json:"note,omitempty"`
}

// PermissionRequestPayload 描述一次权限请求。
type PermissionRequestPayload struct {
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
}

// PermissionResolvedPayload 描述权限请求被处理后的状态。
type PermissionResolvedPayload struct {
	RequestID     string
	ToolCallID    string
	ToolName      string
	ToolCategory  string
	ActionType    string
	Operation     string
	TargetType    string
	Target        string
	Decision      string
	Reason        string
	RuleID        string
	RememberScope string
	ResolvedAs    string
}

// SessionSkillEventPayload 描述会话级 skill 变更事件。
type SessionSkillEventPayload struct {
	SkillID string `json:"skill_id"`
}

// TodoEventPayload 描述 todo_write 相关事件。
type TodoEventPayload struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

const (
	// EventUserMessage 表示用户消息已写入会话。
	EventUserMessage EventType = "user_message"
	// EventAgentChunk 表示 assistant 流式文本分片。
	EventAgentChunk EventType = "agent_chunk"
	// EventAgentDone 表示 assistant 正常结束。
	EventAgentDone EventType = "agent_done"
	// EventToolStart 表示工具开始执行。
	EventToolStart EventType = "tool_start"
	// EventToolResult 表示工具执行完成并写回会话。
	EventToolResult EventType = "tool_result"
	// EventToolChunk 表示工具流式输出分片。
	EventToolChunk EventType = "tool_chunk"
	// EventRunCanceled 表示运行被取消。
	EventRunCanceled EventType = "run_canceled"
	// EventError 表示运行出现终止错误。
	EventError EventType = "error"
	// EventToolCallThinking 表示模型发起工具调用思考阶段。
	EventToolCallThinking EventType = "tool_call_thinking"
	// EventProviderRetry 表示 provider 调用重试。
	EventProviderRetry EventType = "provider_retry"
	// EventPermissionRequested 表示发起权限请求。
	EventPermissionRequested EventType = "permission_requested"
	// EventPermissionResolved 表示权限请求已决议。
	EventPermissionResolved EventType = "permission_resolved"
	// EventCompactStart 表示 compact 开始。
	EventCompactStart EventType = "compact_start"
	// EventCompactApplied 表示 compact 成功应用。
	EventCompactApplied EventType = "compact_applied"
	// EventCompactError 表示 compact 失败。
	EventCompactError EventType = "compact_error"
	// EventTokenUsage 表示 token 用量上报。
	EventTokenUsage EventType = "token_usage"
	// EventSkillActivated 表示 skill 激活。
	EventSkillActivated EventType = "skill_activated"
	// EventSkillDeactivated 表示 skill 停用。
	EventSkillDeactivated EventType = "skill_deactivated"
	// EventSkillMissing 表示会话记录的 skill 丢失。
	EventSkillMissing EventType = "skill_missing"
	// EventPhaseChanged 表示运行 phase 迁移。
	EventPhaseChanged EventType = "phase_changed"
	// EventProgressEvaluated 表示 progress 评估完成。
	EventProgressEvaluated EventType = "progress_evaluated"
	// EventStopReasonDecided 表示 stop reason 已决议。
	EventStopReasonDecided EventType = "stop_reason_decided"
	// EventTodoUpdated 表示 todo_write 成功更新。
	EventTodoUpdated EventType = "todo_updated"
	// EventTodoConflict 表示 todo_write 触发冲突类错误。
	EventTodoConflict EventType = "todo_conflict"
	// EventTodoSummaryInjected 表示本轮上下文注入了 Todo 摘要。
	EventTodoSummaryInjected EventType = "todo_summary_injected"
)

// TokenUsagePayload 承载单轮 token 用量统计。
type TokenUsagePayload struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	SessionInputTokens  int `json:"session_input_tokens"`
	SessionOutputTokens int `json:"session_output_tokens"`
}
