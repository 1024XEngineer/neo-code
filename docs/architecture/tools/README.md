# Tools 模块

## 角色定位

Tools 是模型可调用能力的统一执行边界，负责 schema 暴露、参数校验、权限检查与执行结果收敛。

## [CURRENT] 实现基线

- runtime 通过 `tools.Manager` 获取可用 schema 和执行工具调用。
- `DefaultManager` 串联权限网关、工作区沙箱与具体执行器。
- 工具权限命中 ask/deny 时由 runtime 转为权限事件。

## [PROPOSED] 目标态

- 增加 `SubAgentOrchestrator` 作为子任务隔离扩展契约。
- 审批流升级为“请求-等待用户决策-回执”的交互式闭环。

## 上下游边界

- 上游：runtime。
- 下游：内置工具执行器与权限/沙箱组件。
- 约束：tools 不直接调用 provider，不直接写会话文件。

## 联调注意事项

- 当前 ask 场景不是交互式等待流程，客户端不要假设存在阻塞确认。
