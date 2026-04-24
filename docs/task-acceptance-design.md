# Task Acceptance Design

## 背景问题
- 旧流程中，模型输出 final 且无 tool call 时，runtime 可能直接完成。
- 这会导致“文本 final”与“任务真实完成”混淆。

## 为什么模型 final 不能直接等于完成
- final 仅代表模型主观结束意图，不代表 required todo、文件产物或验证命令已满足。
- 真实完成必须由 runtime 验收层裁决。

## completion / verification / acceptance 区分
- `completion_gate`：判断当前回合是否可尝试收尾（必要非充分）。
- `verification_gate`：由 verifier engine 判断任务是否满足验收条件。
- `acceptance_decision`：聚合两者输出 `accepted/continue/incomplete/failed`。

## 双门控模型
- `completed = completion_gate.passed && verification_gate.passed`
- 任一门未通过都不能直接 `agent_done`。

## 状态机
- provider final -> `beforeAcceptFinal` -> verification -> acceptance_decided
- `accepted` -> `agent_done`
- `continue` -> 注入系统提醒继续推理
- `incomplete/failed` -> 结束 run 并输出 stop reason

## StopReason 设计
- stop reason 由 controlplane decider 统一输出。
- 新增 `verification_failed`、`todo_not_converged`、`retry_exhausted` 等枚举。

## 与 todo / subagent / runtime 的关系
- todo 是 verifier 输入，不直接决定终态。
- subagent 完成不等于主任务完成，仍需通过 verifier gate。
- runtime 只消费 decider 输出，不再平行判定终态。

## decider 单一裁决层
- 终态只由 decider 输出。
- events / TUI / persistence 统一消费 decider 决议。

