# 架构概览

NeoCode 是一个基于 Go 实现的本地 AI 编码助手，主链路为：

**用户输入 → Agent 推理 → 调用工具 → 获取结果 → 继续推理 → UI 展示**

## 核心层级

| 层级 | 职责 |
|------|------|
| TUI（`internal/tui`） | 终端界面，使用 Bubble Tea 构建，负责展示和输入 |
| Runtime（`internal/runtime`） | ReAct 主循环，编排 Agent 推理、工具调用和状态管理 |
| Provider（`internal/provider`） | 模型服务适配器，将厂商差异收敛在此层 |
| Tools（`internal/tools`） | 工具实现，文件操作、Bash 执行、WebFetch 等 |
| Session（`internal/session`） | 会话持久化，JSON 存储 |
| Config（`internal/config`） | 配置加载与校验 |

## 设计原则

- **层间单向依赖**：TUI 只调用 Runtime，Runtime 只调用 Provider 和 Tool Manager
- **厂商差异隔离**：模型协议差异（OpenAI / Gemini / Anthropic）收敛在 `internal/provider`，不泄漏到上层
- **工具能力集中**：所有可被模型调用的能力进入 `internal/tools`，不散落在其他层
- **状态统一管理**：会话状态、消息历史、工具调用记录由 `runtime` 统一管理

## 相关文档

- [配置指南](../guides/configuration)
- [切换模型](../guides/providers)
