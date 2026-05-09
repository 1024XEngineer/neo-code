# Stop Reason And Decision Priority

## StopReason 集合
- `user_interrupt`
- `fatal_error`
- `budget_exceeded`
- `max_turn_exceeded`
- `verification_failed`
- `accepted`
- `missing_completion_signal`
- `accept_check_failed`
- `todo_not_converged`
- `todo_waiting_external`
- `repeat_cycle`
- `max_turn_exceeded_with_unconverged_todos`
- `max_turn_exceeded_with_failed_verification`
- `verification_config_missing`
- `verification_execution_denied`
- `verification_execution_error`

## 决策优先级
- controlplane decider 仍负责输出唯一 stop reason。
- 通用优先级保持为：`user_interrupt` > `fatal_error` > `budget_exceeded` > `max_turn_exceeded` > `verification_failed` > `accepted`。
- final acceptance 只根据 completion gate、verifier gate 与 terminal decision 规则产出结果，不再额外注入 todo retry 旁路。

## 与 ErrorClass 的关系
- `StopReason` 表达“为什么这次 run 结束”。
- `ErrorClass` 只表达 verifier 失败的领域分类，例如 `env_missing`、`execution_denied`、`timeout`。
- `pass` 结果不得携带 `ErrorClass`。
