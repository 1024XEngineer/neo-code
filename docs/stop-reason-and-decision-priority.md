# Stop Reason And Decision Priority

## StopReason 全集
- `user_interrupt`
- `fatal_error`
- `budget_exceeded`
- `max_turn_exceeded`
- `retry_exhausted`
- `verification_failed`
- `accepted`
- `todo_not_converged`
- `todo_waiting_external`
- `no_progress_after_final_intercept`
- `max_turn_exceeded_with_unconverged_todos`
- `max_turn_exceeded_with_failed_verification`
- `verification_config_missing`
- `verification_execution_denied`
- `verification_execution_error`
- `compatibility_fallback`

## 优先级
- `user_interrupt` > `fatal_error` > `budget_exceeded` > `max_turn_exceeded` > `retry_exhausted` > `verification_failed` > `accepted`

## 决议互斥关系
- decider 返回单一 stop reason。
- acceptance/verifier 只提供输入，不直接终裁。

## 与 ErrorClass 的关系
- `ErrorClass` 只描述失败分类（compile/test/lint/type/timeout/permission 等）。
- stop reason 描述终止归因；error class 描述失败类型，二者不重复表达。

