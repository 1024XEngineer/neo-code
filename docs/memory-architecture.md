# 记忆架构

本文档描述 NeoCode 作为 AI 编码助手的目标记忆设计。

## 设计目标

- 持久保存项目级规则，避免模型只能从聊天历史中被动猜测。
- 持久保存有价值的长期编码记忆，例如用户偏好、代码事实和修复经验。
- 保存短期工作态，让助手能够在中断后继续当前任务。
- 让每一层记忆都可检查、可替换、可独立演进。

## 分层设计

### 1. 显式项目记忆

从工作区中的约定文件加载，例如：

- `AGENTS.md`
- `CLAUDE.md`
- `.neocode/memory.md`
- `NEOCODE.md`

这类文件被视为项目的显式权威说明，在自动推断记忆之前优先注入上下文。

### 2. 结构化自动记忆

以结构化条目的形式持久化，例如：

- `user_preference`
- `project_rule`
- `code_fact`
- `fix_recipe`
- `session_memory`

提取策略支持：

- `rule`
- `llm`
- `auto`

### 3. 工作会话记忆

按工作区持久化，用于恢复当前工作状态，例如：

- 当前任务
- 最近完成的动作
- 当前进行中的事项
- 下一步计划
- 最近涉及的文件
- 最近对话轮次

## 上下文注入优先级

Prompt 拼装顺序应为：

1. role prompt
2. explicit project memory
3. working memory
4. todo context
5. recalled structured memory

## 验证要求

- 显式项目记忆文件只能从当前激活的工作区加载。
- 项目记忆文件缺失时不应导致流程失败。
- 工作记忆仍应按工作区维度恢复。
- 当 `memory.extractor: auto` 的 LLM 提取失败时，应回退到规则提取。
- `go test ./...` 需要通过。
