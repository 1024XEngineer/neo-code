# Runtime Finalization Flow

## 旧流程
- assistant final 且无 tool call 时，可直接进入完成路径。

## 新流程
- assistant final -> completion gate -> `beforeAcceptFinal` -> verifier engine -> acceptance decision -> decider stop reason -> runtime 终态。

## beforeAcceptFinal 插入点
- 在 runtime 主循环中，`len(tool_calls)==0` 的 final 候选分支。
- 先发 `verification_started`，后执行 acceptance engine。

## 分支行为
- `accepted`: `verification_completed` -> `acceptance_decided` -> `agent_done`
- `continue`: `verification_finished` -> 注入 runtime reminder -> 下一轮
- `incomplete`: `acceptance_decided` + `stop_reason_decided` -> `agent_done`
- `failed`: `verification_failed` + `acceptance_decided` + `stop_reason_decided` -> `agent_done`

## completion_gate vs verification_gate
- completion gate 只控制“能否尝试收尾”。
- verification gate 才决定“是否允许最终完成”。

## decider 位置与真源关系
- decider 在 run 退出时统一发 `stop_reason_decided`。
- acceptance 输出写入 runtime 终态快照，由 decider 统一编码原因。

