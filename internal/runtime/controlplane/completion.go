package controlplane

// CompletionBlockedReason 表示 completion gate 阻塞完成的原因。
type CompletionBlockedReason string

const (
	// CompletionBlockedReasonNone 表示当前不存在阻塞原因。
	CompletionBlockedReasonNone CompletionBlockedReason = ""
	// CompletionBlockedReasonPendingTodo 表示仍存在未完成
	CompletionBlockedReasonPendingTodo CompletionBlockedReason = "pending_todo"
	// CompletionBlockedReasonUnverifiedWrite 表示仍存在未验证写入。
	CompletionBlockedReasonUnverifiedWrite CompletionBlockedReason = "unverified_write"
	// CompletionBlockedReasonPostExecuteClosureRequired 表示刚完成执行后仍需闭环。
	CompletionBlockedReasonPostExecuteClosureRequired CompletionBlockedReason = "post_execute_closure_required"
)

// CompletionState 描述 completion gate 所需的运行事实。
type CompletionState struct {
	HasPendingAgentTodos    bool                    `json:"has_pending_agent_todos"`
	HasUnverifiedWrites     bool                    `json:"has_unverified_writes"`
	TodoOnlyTaskCandidate   bool                    `json:"todo_only_task_candidate,omitempty"`
	TodoStateChanged        bool                    `json:"todo_state_changed,omitempty"`
	TodoStateSatisfied      bool                    `json:"todo_state_satisfied,omitempty"`
	CompletionBlockedReason CompletionBlockedReason `json:"completion_blocked_reason,omitempty"`
}

// EvaluateCompletion 依据当前事实计算是否允许本轮 completed。
func EvaluateCompletion(state CompletionState, assistantHasToolCalls bool) (CompletionState, bool) {
	state.CompletionBlockedReason = CompletionBlockedReasonNone

	if assistantHasToolCalls {
		state.CompletionBlockedReason = CompletionBlockedReasonPostExecuteClosureRequired
		return state, false
	}
	if state.HasPendingAgentTodos {
		state.CompletionBlockedReason = CompletionBlockedReasonPendingTodo
		return state, false
	}
	if state.HasUnverifiedWrites {
		state.CompletionBlockedReason = CompletionBlockedReasonUnverifiedWrite
		return state, false
	}
	return state, true
}
