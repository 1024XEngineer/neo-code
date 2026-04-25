---
title: 深入阅读
description: 汇总 NeoCode 仓库中已有设计文档，并说明它们分别适合在什么场景下阅读。
---

# 深入阅读

这一页不复制仓库里所有设计文档，而是告诉你什么时候该看哪一份原始文档。

## 运行时与上下文

- [Runtime / Provider 事件流](https://github.com/1024XEngineer/neo-code/blob/main/docs/runtime-provider-event-flow.md)
  - 适合在你想理解 Provider 流式响应怎样进入 Runtime、事件怎样发给 UI 时阅读。
- [Runtime 收尾流程](https://github.com/1024XEngineer/neo-code/blob/main/docs/runtime-finalization-flow.md)
  - 适合在你要理解任务收尾决策、验证拦截和停止条件优先级时阅读。
- [停止条件与决策优先级](https://github.com/1024XEngineer/neo-code/blob/main/docs/stop-reason-and-decision-priority.md)
  - 适合在你要理解 Runtime 何时停止推理、各停止条件的优先级关系时阅读。
- [Context Compact 说明](https://github.com/1024XEngineer/neo-code/blob/main/docs/context-compact.md)
  - 适合在你要调整压缩策略、自动压缩阈值或理解 micro compact 时阅读。
- [Session 持久化设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/session-persistence-design.md)
  - 适合在你要理解会话恢复、状态落盘和会话模型边界时阅读。
- [Session Todo 设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/session-todo-design.md)
  - 适合在你要理解 Todo 状态机、依赖关系和 schema 迁移时阅读。
- [Todo Schema 迁移](https://github.com/1024XEngineer/neo-code/blob/main/docs/todo-schema-migration.md)
  - 适合在你要理解 Todo schema 版本升级和向后兼容策略时阅读。
- [Repository 设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/repository-design.md)
  - 适合在你要理解仓库级事实发现、检索和安全策略时阅读。

## Gateway 与安全

- [Gateway 详细设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/gateway-detailed-design.md)
  - 适合在你关注 JSON-RPC、ACL、Silent Auth、网络访问面与流式中继时阅读。
- [Gateway RPC API](https://github.com/1024XEngineer/neo-code/blob/main/docs/gateway-rpc-api.md)
  - 适合在你要对接 Gateway JSON-RPC 接口时阅读。
- [Gateway 错误目录](https://github.com/1024XEngineer/neo-code/blob/main/docs/gateway-error-catalog.md)
  - 适合在你要排查 Gateway 错误码和错误分类时阅读。
- [Gateway 兼容性](https://github.com/1024XEngineer/neo-code/blob/main/docs/gateway-compatibility.md)
  - 适合在你要理解 Gateway 版本兼容策略时阅读。
- [Tools 与 TUI 集成](https://github.com/1024XEngineer/neo-code/blob/main/docs/tools-and-tui-integration.md)
  - 适合在你要理解工具调用如何经过 Runtime / TUI 协同展示时阅读。
- [兼容性与回退生命周期](https://github.com/1024XEngineer/neo-code/blob/main/docs/compatibility-fallback-lifecycle.md)
  - 适合在你要理解功能回退和兼容策略时阅读。

## 验证器与任务验收

- [验证器引擎设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/verifier-engine-design.md)
  - 适合在你要理解验证器编排、结果聚合和拦截机制时阅读。
- [验证器配置与策略](https://github.com/1024XEngineer/neo-code/blob/main/docs/verifier-configuration-and-policy.md)
  - 适合在你要调整验证器启用/禁用、超时和失败策略时阅读。
- [任务验收设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/task-acceptance-design.md)
  - 适合在你要理解任务完成判定和验收流程时阅读。

## 配置与扩展

- [配置指南](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/configuration.md)
  - 这里是主配置和 custom provider 的事实来源。
- [扩展 Provider](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/adding-providers.md)
  - 适合在你要新增内置 Provider 或接入 OpenAI-compatible 网关时阅读。
- [ModelScope Provider 配置](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/modelscope-provider-setup.md)
  - 适合在你要配置 ModelScope Provider 时阅读。
- [MCP 配置指南](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/mcp-configuration.md)
  - 适合在你准备配置 MCP server 时阅读。
- [Gateway 集成指南](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/gateway-integration-guide.md)
  - 适合在你要将外部客户端对接 Gateway 时阅读。
- [Config 管理详细设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/config-management-detail-design.md)
  - 适合在你要改配置加载、校验和选择修正逻辑时阅读。

## Skills 与其他主题

- [Skills 设计与使用](https://github.com/1024XEngineer/neo-code/blob/main/docs/skills-system-design.md)
  - 适合在你要理解 Skills 的发现、激活和执行边界时阅读。
- [Provider Schema 策略](https://github.com/1024XEngineer/neo-code/blob/main/docs/provider-schema-strategy.md)
  - 适合在你关注 Provider schema、请求格式和兼容策略时阅读。
- [更新与升级](https://github.com/1024XEngineer/neo-code/blob/main/docs/guides/update.md)
  - 适合在你要核对静默更新行为时阅读。

## 建议

如果你只是想使用 NeoCode，请优先返回 [开始使用](/guide/)；只有当你需要修改实现、排查边界或补测试时，再打开这些设计文档。
