# TUI 模块

## 角色定位

TUI 负责交互与渲染，消费 runtime 事件并展示会话、工具执行状态、错误与结果。

## [CURRENT] 实现基线

- 基于 Bubble Tea 状态机实现。
- 当前直接依赖 runtime 接口（非网关模式）。
- Provider 选择能力通过 `ProviderController` 注入。

## [PROPOSED] 目标态

- 通过 Gateway 进行协议通信，TUI 与 runtime 进程解耦。
- 审批流改为可交互确认，避免仅显示顺序事件。

## 上下游边界

- 上游：用户输入。
- 下游：runtime（当前）/gateway（目标态）。
- 约束：TUI 不直接执行工具，不直接调用 provider。

## 联调注意事项

- 当前事件协议应按 runtime 现实现解码，尤其 `compact_start` 为字符串。
