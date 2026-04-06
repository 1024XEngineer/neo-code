# Provider 模块

## 角色定位

Provider 负责抹平不同模型厂商协议差异，对 runtime 提供统一 `Chat` 与流式事件契约。

## [CURRENT] 实现基线

- 当前核心接口：`Provider.Chat(ctx, req, events)`。
- 输出统一结构：`ChatResponse` 与 `StreamEvent`。
- 已有统一错误载体 `ProviderError`，支持重试语义。

## [PROPOSED] 目标态

- 增加上下文过长错误归一化接口 `IsContextOverflow`。
- 补充 `context_overflow` 语义错误码，为 runtime reactive 自动恢复提供稳定判定。

## 上下游边界

- 上游：runtime。
- 下游：具体 provider driver（openai 等）。
- 约束：provider 不处理工具执行，不持久化会话。

## 联调注意事项

- 当前主链未自动使用“上下文过长”分类触发 reactive。
- 文档中的 `ErrorClassifier` 属于目标态接口。
