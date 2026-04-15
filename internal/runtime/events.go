package runtime

import (
	"time"

	"neo-code/internal/runtime/controlplane"
)

// EventType identifies the kind of runtime event emitted during a run.
type EventType string

// RuntimeEvent is emitted by the runtime to report progress and terminal states
// for a specific run. RunID is provided by the caller and is echoed back on all
// events so upper layers can ignore stale events from older runs.
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

// BudgetCheckedPayload 为预算检查预留事件负载。
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

// LedgerReconciledPayload 为账本对账预留事件负载。
type LedgerReconciledPayload struct {
	Note string `json:"note,omitempty"`
}

// PermissionRequestPayload 描述一次需要审批的权限请求上下文。
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

// PermissionResolvedPayload 描述权限请求被运行时处理后的最终状态。
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

// SessionSkillEventPayload 描述一次会话级 skill 状态变化或缺失提示。
type SessionSkillEventPayload struct {
	SkillID string `json:"skill_id"`
}

const (
	// EventUserMessage is emitted after the user input has been accepted and saved.
	EventUserMessage EventType = "user_message"
	// EventAgentChunk carries streamed assistant text.
	EventAgentChunk EventType = "agent_chunk"
	// EventAgentDone is emitted when the assistant finishes normally.
	EventAgentDone EventType = "agent_done"
	// EventToolStart is emitted before a tool call begins execution.
	EventToolStart EventType = "tool_start"
	// EventToolResult is emitted after a tool call finishes and its result is saved.
	EventToolResult EventType = "tool_result"
	// EventToolChunk carries streamed tool output.
	EventToolChunk EventType = "tool_chunk"
	// EventRunCanceled is emitted once when the root run context is canceled.
	EventRunCanceled EventType = "run_canceled"
	// EventError is emitted for terminal runtime errors other than cancellation.
	EventError EventType = "error"
	// EventToolCallThinking is emitted when the model decides to call a tool,
	// before the tool execution begins. TUI can show a transitional indicator.
	EventToolCallThinking EventType = "tool_call_thinking"
	// EventProviderRetry is emitted when runtime retries a provider call due to
	// a retryable error (e.g. 429, 5xx). Payload is a human-readable message.
	EventProviderRetry EventType = "provider_retry"
	// EventPermissionRequested 表示一次权限审批请求。
	EventPermissionRequested EventType = "permission_requested"
	// EventPermissionResolved is emitted when runtime resolves a permission request or denial.
	EventPermissionResolved EventType = "permission_resolved"
	// EventCompactStart is emitted when a compact cycle starts.
	EventCompactStart EventType = "compact_start"
	// EventCompactApplied 表示一次 compact 已成功应用或校验完成。
	EventCompactApplied EventType = "compact_applied"
	// EventCompactError is emitted when compact fails.
	EventCompactError EventType = "compact_error"
	// EventTokenUsage is emitted after each provider response with token statistics.
	EventTokenUsage EventType = "token_usage"
	// EventSkillActivated 表示会话成功激活了一个 skill。
	EventSkillActivated EventType = "skill_activated"
	// EventSkillDeactivated 表示会话成功停用了一个 skill。
	EventSkillDeactivated EventType = "skill_deactivated"
	// EventSkillMissing 表示运行时发现会话记录的 skill 已无法解析。
	EventSkillMissing EventType = "skill_missing"
	// EventPhaseChanged 表示显式 phase 迁移。
	EventPhaseChanged EventType = "phase_changed"
	// EventProgressEvaluated 表示 progress 评估结果。
	EventProgressEvaluated EventType = "progress_evaluated"
	// EventStopReasonDecided 表示唯一停止原因已决议。
	EventStopReasonDecided EventType = "stop_reason_decided"
)

// TokenUsagePayload carries token usage statistics for a single provider turn.
type TokenUsagePayload struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	SessionInputTokens  int `json:"session_input_tokens"`
	SessionOutputTokens int `json:"session_output_tokens"`
}
