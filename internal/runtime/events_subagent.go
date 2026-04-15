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
	Delta      string              `json:"delta,omitempty"`
	Error      string              `json:"error,omitempty"`
}

const (
	// EventSubAgentStarted 在子代理任务启动后触发。
	EventSubAgentStarted EventType = "subagent_started"
	// EventSubAgentProgress 在子代理执行每一步后触发。
	EventSubAgentProgress EventType = "subagent_progress"
	// EventSubAgentCompleted 在子代理成功结束后触发。
	EventSubAgentCompleted EventType = "subagent_completed"
	// EventSubAgentFailed 在子代理失败结束后触发。
	EventSubAgentFailed EventType = "subagent_failed"
	// EventSubAgentCanceled 在子代理被取消后触发。
	EventSubAgentCanceled EventType = "subagent_canceled"
)
