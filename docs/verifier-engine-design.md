# Verifier Engine Design

## Verifier 接口
- `FinalVerifier{Name, VerifyFinal}`
- 输入为 `FinalVerifyInput`（session/run/task/workdir/messages/todos/runtime state/config）。
- 输出为 `VerificationResult`（status/reason/error_class/evidence）。

## Verifier 分类
- P0：`todo_convergence`
- P1：`file_exists`、`content_match`、`command_success`、`git_diff`
- P1 代码任务：`build/test/lint/typecheck`（命令驱动）

## Orchestrator 流程
- 按策略解析 verifier 列表并顺序执行。
- 汇总 `VerificationGateDecision{Passed, Reason, Results}`。

## 聚合规则
- 任一 `fail` -> `verification_failed`
- 否则任一 `hard_block` -> `todo_waiting_external` 或 `todo_not_converged`
- 否则任一 `soft_block` -> `todo_not_converged`
- 全部 `pass` -> `accepted`

## Task policy 映射
- `unknown`: todo_convergence
- `create_file/docs`: todo_convergence + file_exists + content_match
- `config`: todo_convergence + file_exists + content_match + command_success
- `edit_code/fix_bug/refactor`: todo_convergence + git_diff + build/test/lint/typecheck(按策略启停)

