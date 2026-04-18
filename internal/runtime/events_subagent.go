package runtime

import "neo-code/internal/subagent"

// EventPermissionRequest 为兼容旧事件名保留，语义等同 EventPermissionRequested。
const EventPermissionRequest EventType = EventPermissionRequested

// EventCompactDone 为兼容旧事件名保留，语义等同 EventCompactApplied。
const EventCompactDone EventType = EventCompactApplied

// SubAgentEventPayload 描述子代理执行生命周期的事件载荷。
type SubAgentEventPayload struct {
	Role       subagent.Role       `json:"role"`
	TaskID     string              `json:"task_id"`
	State      subagent.State      `json:"state"`
	StopReason subagent.StopReason `json:"stop_reason,omitempty"`
	Step       int                 `json:"step,omitempty"`
	Reason     string              `json:"reason,omitempty"`
	Delta      string              `json:"delta,omitempty"`
	Error      string              `json:"error,omitempty"`
}

// SubAgentToolCallEventPayload 描述子代理工具调用事件载荷。
type SubAgentToolCallEventPayload struct {
	Role      subagent.Role `json:"role"`
	TaskID    string        `json:"task_id"`
	ToolName  string        `json:"tool_name"`
	Decision  string        `json:"decision"`
	ElapsedMS int64         `json:"elapsed_ms"`
	Truncated bool          `json:"truncated"`
	Error     string        `json:"error,omitempty"`
}

const (
	// EventSubAgentStarted 在子代理任务启动后触发。
	EventSubAgentStarted EventType = "subagent_started"
	// EventSubAgentProgress 在子代理执行每一步后触发。
	EventSubAgentProgress EventType = "subagent_progress"
	// EventSubAgentRetried 在子代理任务进入重试后触发。
	EventSubAgentRetried EventType = "subagent_retried"
	// EventSubAgentBlocked 在子代理任务被阻塞（依赖或退避）时触发。
	EventSubAgentBlocked EventType = "subagent_blocked"
	// EventSubAgentCompleted 在子代理成功结束后触发。
	EventSubAgentCompleted EventType = "subagent_completed"
	// EventSubAgentFailed 在子代理失败结束后触发。
	EventSubAgentFailed EventType = "subagent_failed"
	// EventSubAgentCanceled 在子代理被取消后触发。
	EventSubAgentCanceled EventType = "subagent_canceled"
	// EventSubAgentFinished 在一次调度轮次结束后触发。
	EventSubAgentFinished EventType = "subagent_finished"
	// EventSubAgentToolCallStarted 在子代理发起工具调用时触发。
	EventSubAgentToolCallStarted EventType = "subagent_tool_call_started"
	// EventSubAgentToolCallResult 在子代理工具调用返回后触发。
	EventSubAgentToolCallResult EventType = "subagent_tool_call_result"
	// EventSubAgentToolCallDenied 在子代理工具调用被权限拒绝时触发。
	EventSubAgentToolCallDenied EventType = "subagent_tool_call_denied"
)
