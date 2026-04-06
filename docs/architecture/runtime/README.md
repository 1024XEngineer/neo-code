# Runtime 模块

## 角色定位

Runtime 是 NeoCode 的编排中枢，负责把一次用户输入推进到终态事件，并协调 Context、Provider、Tools、Config、Session。

## [CURRENT] 实现基线

- 对外接口：`Run`、`Compact`、`CancelActiveRun`、`Events`、`ListSessions`、`LoadSession`、`SetSessionWorkdir`。
- 并发策略：`Run` 与 `Compact` 串行化，避免会话并发写入。
- 事件语义：`permission_request` 与 `permission_resolved` 在 ask 场景下由 runtime 内部顺序发送。
- compact 事件：`compact_start` 当前 payload 是字符串；`compact_done` / `compact_error` 为结构化。

## [PROPOSED] 目标态

- 接入 Gateway 后，Runtime 保持 Go 接口并作为网关后的唯一编排服务。
- 引入 `TerminalEventGate`，确保每个 `run_id` 仅一个终态事件。
- 引入 `ReactiveRetryGate`，实现上下文过长错误触发的单次 reactive 自动重试。

## 上下游边界

- 上游：`tui`、`cli`（当前），`gateway`（目标态）。
- 下游：`context.Builder`、`provider.Provider`、`tools.Manager`、`config.Manager`、`session.Store`。
- 约束：上游禁止直连 provider/tools；状态持久化由 runtime 统一调度。

## 联调注意事项

- 当前联调必须按 `compact_start:string` 解码。
- 审批事件当前不是交互式等待用户决策。
- reactive 自动恢复尚未接入主链，不能按已实现能力依赖。
