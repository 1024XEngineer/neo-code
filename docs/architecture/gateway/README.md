# Gateway 模块

## 角色定位

Gateway 是 Runtime 的协议适配层，面向 TUI/CLI/Web 暴露 REST/WS 入口。

## [CURRENT] 实现基线

- 当前仓库尚无稳定 Gateway 实现。
- 当前主链路仍是 `TUI/CLI -> Runtime` 直接调用。

## [PROPOSED][NOT IMPLEMENTED YET] 目标态

- 统一入口：`TUI/CLI/Web -> Gateway -> Runtime`。
- 提供无状态 REST 接口（会话、配置、compact、token 估算）。
- 提供 WS 流式接口用于对话事件、权限审批、中间状态推送。

## 上下游边界

- 上游：各类客户端。
- 下游：runtime。
- 约束：Gateway 不承载业务编排，只做协议转换与会话连接管理。

## 联调注意事项

- 当前禁止把 Gateway 能力当成已实现事实。
- 相关字段与事件协议以 runtime 当前行为为基线，目标态逐步演进。
