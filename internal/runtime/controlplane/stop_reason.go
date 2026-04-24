package controlplane

// StopReason 表示一次 Run 的最终硬停止原因。
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
	// StopReasonRetryExhausted 表示 todo 重试次数已耗尽。
	StopReasonRetryExhausted StopReason = "retry_exhausted"
	// StopReasonVerificationFailed 表示验证器明确失败。
	StopReasonVerificationFailed StopReason = "verification_failed"
	// StopReasonAccepted 表示 completion/verification 双门控均通过并完成收尾。
	StopReasonAccepted StopReason = "accepted"
	// StopReasonTodoNotConverged 表示 required todo 尚未收敛。
	StopReasonTodoNotConverged StopReason = "todo_not_converged"
	// StopReasonTodoWaitingExternal 表示 required todo 等待外部输入。
	StopReasonTodoWaitingExternal StopReason = "todo_waiting_external"
	// StopReasonNoProgressAfterFinalIntercept 表示 final 连续拦截但无进展。
	StopReasonNoProgressAfterFinalIntercept StopReason = "no_progress_after_final_intercept"
	// StopReasonMaxTurnExceededWithUnconvergedTodos 表示 max turn + todo 未收敛。
	StopReasonMaxTurnExceededWithUnconvergedTodos StopReason = "max_turn_exceeded_with_unconverged_todos"
	// StopReasonMaxTurnExceededWithFailedVerification 表示 max turn + verification 未通过。
	StopReasonMaxTurnExceededWithFailedVerification StopReason = "max_turn_exceeded_with_failed_verification"
	// StopReasonVerificationConfigMissing 表示 verifier 必需配置缺失。
	StopReasonVerificationConfigMissing StopReason = "verification_config_missing"
	// StopReasonVerificationExecutionDenied 表示 verifier 命令被执行策略拒绝。
	StopReasonVerificationExecutionDenied StopReason = "verification_execution_denied"
	// StopReasonVerificationExecutionError 表示 verifier 命令执行异常。
	StopReasonVerificationExecutionError StopReason = "verification_execution_error"
	// StopReasonCompatibilityFallback 表示走了兼容回退路径。
	StopReasonCompatibilityFallback StopReason = "compatibility_fallback"

	// StopReasonMaxTurnsReached 兼容旧命名，语义等价于 max_turn_exceeded。
	StopReasonMaxTurnsReached StopReason = StopReasonMaxTurnExceeded
	// StopReasonCompleted 兼容旧命名，语义等价于 accepted。
	StopReasonCompleted StopReason = StopReasonAccepted
)
