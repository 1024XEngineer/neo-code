---
title: 为什么选择 NeoCode
description: 从本地运行、终端原生、安全优先、可扩展等维度，说明 NeoCode 与其他 AI 编码工具的差异。
---

# 为什么选择 NeoCode

NeoCode 是一个在终端里运行的本地 AI 编码助手。它不是云端 SaaS，不是浏览器插件，也不是 IDE 内嵌面板——它是一个你可以完全掌控的命令行工具。

## 完全本地运行

NeoCode 的模型调用走你本地的 Provider 配置，不经过任何第三方中转服务器：

- API Key 只从系统环境变量读取，不写入配置文件
- 代码和工作区数据留在你的机器上
- 不需要注册账号，不需要登录云端服务
- 模型请求直接发往你配置的 Provider 端点（OpenAI、Gemini、ModelScope 等）

如果你使用企业内部网关或本地模型服务，流量同样只在你控制的网络内流转。

## 终端原生体验

NeoCode 基于 Go 和 Bubble Tea 构建，是一个真正的终端 TUI 应用：

- 无需浏览器，直接在你的 shell 工作流中运行
- Slash 命令（`/help`、`/compact`、`/provider` 等）与终端操作习惯一致
- `& git status` 这样的本地命令前缀可以无缝嵌入会话上下文
- 启动即用，退出即走，不残留后台进程

## 多模型灵活切换

NeoCode 内置 5 个 Provider，支持自定义接入：

| 内置 Provider | 环境变量 |
| --- | --- |
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `openll` | `AI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |
| `modelscope` | `MODELSCOPE_API_KEY` |

如果你使用兼容 OpenAI 接口的企业网关或本地模型服务，只需创建一个 `provider.yaml` 即可接入，无需改代码。

在 TUI 中通过 `/provider` 和 `/model` 随时切换，切换结果自动保存。

## ReAct 推理闭环

NeoCode 围绕 ReAct（Reason-Act-Observe）循环工作：

`用户输入 → Agent 推理 → 调用工具 → 获取结果 → 继续推理 → UI 展示`

这个闭环不是简单的单轮问答，而是多轮推理编排：

- **预算门禁**：发送前估算输入 token，超预算时自动压缩或停止，避免无效请求
- **熔断保护**：连续无进展或重复调用同一工具参数时，注入纠偏提示或终止运行
- **上下文压缩**：`/compact` 手动压缩或自动压缩，保留任务状态摘要而非丢弃历史

## 安全优先

NeoCode 在多个层级内置了安全机制：

- **工具权限审批**：高风险操作（如文件写入、bash 执行）需要用户确认，支持 ask/deny/allow 决策
- **Bash 语义治理**：bash 命令会被分类为 read_only / local_mutation / remote_op / destructive / unknown，不同分类走不同审批策略；Git 操作有独立白名单
- **文件系统沙箱**：文件操作限制在工作区内，拒绝路径穿越和符号链接逃逸
- **Gateway Origin 校验**：网络访问面仅允许 localhost / 127.0.0.1 / [::1] / app:// 来源，跨站请求返回 403
- **WebFetch 安全**：禁止自动重定向，限制响应大小和内容类型

## 会话持久化与恢复

NeoCode 使用工作区级 SQLite 数据库持久化会话：

- 会话切换：`/session` 在不同任务间切换
- 跨会话记忆：`/remember` 保存偏好和项目事实，`/memo` 查看记忆索引
- 上下文压缩：长会话通过 `/compact` 压缩，保留任务状态摘要继续续航
- 工作区隔离：`--workdir` 和 `/cwd` 控制工具访问范围和会话隔离

## 可扩展能力层

NeoCode 提供三层可扩展能力，都不改变主执行链路：

### Skills 提示层

Skills 是"能力提示层"，影响 Context 注入和工具排序优先级，但不改变工具执行入口和权限决策。

```text
/skills          查看可用 Skills
/skill use <id>  激活某个 Skill
/skill off <id>  停用某个 Skill
/skill active    查看已激活 Skills
```

### MCP stdio 接入

通过 `config.yaml` 的 `tools.mcp.servers` 注册 MCP server，将外部工具暴露给 Agent 调用，支持 allowlist/denylist 暴露过滤。

### 子代理编排

内置 `spawn_subagent` 工具支持三种角色：

- **researcher**：检索与分析
- **coder**：实现与修复
- **reviewer**：审查与验收

子代理在受控预算（步数 + 超时）内独立运行，结果回灌主会话。

### 验收验证器

任务完成前可触发验证器检查，当前内置：

- `file_exists`：文件是否存在
- `content_match`：文件内容是否匹配
- `todo_convergence`：Todo 是否收敛
- `build`：构建是否通过
- `test`：测试是否通过
- `lint`：Lint 是否通过
- `typecheck`：类型检查是否通过
- `command_success`：命令是否成功
- `git_diff`：Git 变更是否符合预期

## 不适合什么场景

NeoCode 当前不追求以下方向：

- 不做 IDE 内嵌面板——它是终端工具
- 不做云端协作平台——它是本地单用户工具
- 不做向量检索或 embedding——仓库级检索走文本和符号匹配
- 不做 LSP 集成——代码理解依赖模型推理和工具调用

## 接下来

- 想先跑起来：看 [安装与运行](./install)
- 想了解完整能力列表：看 [首次上手](./quick-start)
- 想理解配置和 Provider：看 [配置入口](./configuration)
