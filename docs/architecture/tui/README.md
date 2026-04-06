# TUI 模块

## 角色定位

TUI 负责交互与渲染，消费 runtime 事件并展示会话、工具执行状态、错误与结果。

## [CURRENT] 实现基线

- 基于 Bubble Tea 状态机实现。
- 当前直接依赖 runtime 接口（非网关模式）。
- 接口设计收敛为单一能力入口：发送请求与接收事件统一在同一契约中。

## [PROPOSED] 目标态

- 通过 Gateway 进行协议通信，TUI 与 runtime 进程解耦。
- 审批流升级为可交互确认。
- 即使迁移到 Gateway，TUI 侧仍保持“单一接口门面”设计，不拆分为发送/订阅两套默认接口。

## 上下游边界

- 上游：用户输入。
- 下游：runtime（当前）/gateway（目标态）。
- 约束：TUI 不直接执行工具，不直接调用 provider。

## 联调注意事项

- 当前事件协议按 runtime 现实现解码，尤其 `compact_start` 为字符串 payload。
- 当前 ask 场景是 runtime 内部顺序事件，不是交互式等待。
