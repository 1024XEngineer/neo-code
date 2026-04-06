# Session 模块

## 角色定位

Session 模块定义会话状态的持久化边界，负责：

- 会话完整快照保存（消息、标题、时间戳、模型元信息）。
- 会话加载与摘要列表读取。
- 会话级元信息承载（如 provider/model、运行期 workdir 关联）。

## [CURRENT] 实现基线

- 当前基线类型位于 runtime：`Session`、`SessionSummary`、`Store`、`JSONSessionStore`。
- 当前持久化语义：基于 JSON 文件落盘，按会话维度读写。
- 当前 runtime 依赖 session 契约做统一状态管理，不在 TUI 散落保存聊天状态。

### [CURRENT] 关键行为

- `Save`：整会话覆盖保存，使用临时文件替换保证写入完整性。
- `Load`：按 `session_id` 读取完整会话。
- `ListSummaries`：读取摘要并按 `updated_at` 倒序。
- `Workdir`：会话运行期映射由 runtime 维护，会话结构保留关联语义。

## [PROPOSED] 目标态扩展

- `[PROPOSED][NOT IMPLEMENTED YET]` 会话运行态拆分：将易变运行态与持久快照解耦。
- `[PROPOSED][NOT IMPLEMENTED YET]` 可插拔存储后端：文件、SQLite、远端服务。
- `[PROPOSED][NOT IMPLEMENTED YET]` 会话归档与 compact transcript 联动，支持生命周期治理。
- `[PROPOSED][NOT IMPLEMENTED YET]` 统一可观测字段（例如 token 汇总、最后终态事件）。

## 与 Runtime 的接口关系

- Runtime 只通过 Session 契约读写状态：`Save/Load/ListSummaries`。
- Runtime 负责决定写入时机与一致性边界；Session 只负责存储语义。
- TUI 不应直接写 session 存储，避免出现双写与时序冲突。

## 联调注意事项

- 当前会话数据写入是“回合内关键节点保存”，不是每个 UI 字符输入实时持久化。
- `compact` 触发前后会发生会话消息重写，消费者需按事件时序刷新视图。
- 当前没有独立的“运行态存储接口”，相关能力属于目标态。

## 迁移来源

- `docs/session-persistence-design.md` 的持久化策略与边界说明，逐步迁移到本模块文档与契约文件。
