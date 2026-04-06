# Context 模块

## 角色定位

Context 负责把 runtime 会话状态加工为 provider 可消费的系统提示词和消息上下文。

## [CURRENT] 实现基线

- 稳定入口：`Builder.Build(ctx, BuildInput)`。
- 当前输入结构：`BuildInput{Messages, Metadata}`。
- 由 context 统一拼装核心 system prompt、`AGENTS.md` 规则和系统状态摘要。

## [PROPOSED] 目标态

- 在保持单入口 Build 不变的前提下，扩展可选输入：`LoopState`、`TokenBudget`、`WorkspaceMap`、`TaskScope`。
- 支持动态压缩策略与子任务隔离信息透传。

## 上下游边界

- 上游：runtime。
- 下游：provider（仅消费 BuildResult，不感知上下文来源）。
- 约束：context 不直接执行工具、不直接做会话持久化。

## 联调注意事项

- 当前联调只能依赖 `Messages + Metadata`。
- `TokenBudget` 等扩展字段尚未落地，不能按已实现调用。
