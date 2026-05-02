package controlplane

// StopReason 表示一次 Run 的最终终止原因。
type StopReason string

const (
	// StopReasonUserInterrupt 表示运行被用户或上层上下文中断。
	StopReasonUserInterrupt StopReason = "user_interrupt"
	// StopReasonFatalError 表示出现不可恢复错误。
	StopReasonFatalError StopReason = "fatal_error"
	// StopReasonBudgetExceeded 表示预算闭环判定本轮请求无法继续发送。
	StopReasonBudgetExceeded StopReason = "budget_exceeded"
	// StopReasonMaxTurnExceeded 表示达到运行轮次上限并主动终止。
	StopReasonMaxTurnExceeded StopReason = "max_turn_exceeded"
	// StopReasonVerificationFailed 表示 verifier 明确失败。
	StopReasonVerificationFailed StopReason = "verification_failed"
	// StopReasonAccepted 表示 completion gate 与 verifier gate 均通过并完成收尾。
	StopReasonAccepted StopReason = "accepted"
	// StopReasonTodoNotConverged 表示 required todo 尚未收敛。
	StopReasonTodoNotConverged StopReason = "todo_not_converged"
	// StopReasonTodoWaitingExternal 表示 required todo 仍在等待外部条件。
	StopReasonTodoWaitingExternal StopReason = "todo_waiting_external"
	// StopReasonNoProgressAfterFinalIntercept 表示 final 连续被拦截且没有新进展。
	StopReasonNoProgressAfterFinalIntercept StopReason = "no_progress_after_final_intercept"
	// StopReasonMaxTurnExceededWithUnconvergedTodos 表示达到最大轮次时 todo 仍未收敛。
	StopReasonMaxTurnExceededWithUnconvergedTodos StopReason = "max_turn_exceeded_with_unconverged_todos"
	// StopReasonMaxTurnExceededWithFailedVerification 表示达到最大轮次时 verifier 已失败。
	StopReasonMaxTurnExceededWithFailedVerification StopReason = "max_turn_exceeded_with_failed_verification"
	// StopReasonVerificationConfigMissing 表示 verifier 依赖的配置缺失或非法。
	StopReasonVerificationConfigMissing StopReason = "verification_config_missing"
	// StopReasonVerificationExecutionDenied 表示 verifier 命令被执行策略拒绝。
	StopReasonVerificationExecutionDenied StopReason = "verification_execution_denied"
	// StopReasonVerificationExecutionError 表示 verifier 命令执行异常。
	StopReasonVerificationExecutionError StopReason = "verification_execution_error"
	// StopReasonRequiredTodoFailed 表示 required todo 已进入失败终态。
	StopReasonRequiredTodoFailed StopReason = "required_todo_failed"
)
