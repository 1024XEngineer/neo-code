---
title: 日常使用
description: 从打开项目到完成任务，按 NeoCode 的真实工作流组织日常操作。
---

# 日常使用

NeoCode 的日常使用通常是：打开项目、描述目标、观察 Agent 行动、确认权限、查看结果、必要时继续或压缩上下文。

## 终端与浏览器

NeoCode 默认在终端中运行。如果你更喜欢图形界面，可以通过 `neocode web` 启动浏览器 Web UI，功能与终端一致，详见 [Web UI 使用指南](./web-ui)。

## 打开项目

启动时指定工作区：

```bash
neocode -w /path/to/project
```

工作区决定 NeoCode 能读取、搜索、编辑和执行命令的项目范围。切换到另一个项目时，建议新建会话，避免旧上下文混入。

## 描述任务

直接用自然语言描述目标。复杂任务建议先让 NeoCode 阅读相关代码并给方案，再决定是否实现。

```text
请先阅读配置加载相关代码，给出最小修复方案。暂时不要改文件。
```

确认方案后再继续：

```text
请按刚才的方案实现，并运行相关测试。
```

好的任务描述通常包含：

- 目标：要修什么、加什么、解释什么
- 范围：只看哪些文件或模块，哪些不要动
- 验证：希望运行哪个测试或构建命令

## 观察执行过程

NeoCode 会在界面中展示 Agent 的回复、工具调用和执行状态。读取文件、搜索内容通常会自动执行；写文件或运行有风险的命令时，会出现权限确认。

常用按键：

| 按键 | 作用 |
|---|---|
| `Enter` | 发送输入 |
| `Ctrl+J` | 输入换行 |
| `Ctrl+W` | 取消当前 Agent 任务 |
| `Ctrl+Q` | 打开 Slash 指令帮助 |
| `Ctrl+N` | 新建会话 |
| `Ctrl+O` | 打开工作区选择 |
| `Ctrl+F` | 切换 Full Access 提示 |
| `Ctrl+L` | 打开日志视图 |
| `Tab` / `Shift+Tab` | 切换面板焦点 |

## 确认权限

当 NeoCode 请求写文件、抓取非默认放行的外部域名，或执行有风险的命令时，会弹出权限确认。

| 选择 | 适合场景 |
|---|---|
| `Allow once` | 只批准本次请求，适合单次写入或仍想逐项确认 |
| `Allow session` | 批准当前会话内相似请求，适合已确认安全的重复操作 |
| `Reject` | 拒绝本次请求，适合路径不对、命令危险或范围失控 |

确认界面支持快捷键：`y=once`、`a=session`、`n=reject`。

更详细的风险判断见 [工具与权限](./tools-permissions)。

## 继续、压缩或新建会话

| 场景 | 建议 |
|---|---|
| 继续同一个 bug、功能或文档任务 | 继续当前会话 |
| 对话很长，回答开始重复或混入旧任务 | 执行 `/compact` |
| 开始不相关的新任务 | `Ctrl+N` 新建会话 |
| 切换到另一个项目 | 新建会话并切换工作区 |

压缩后建议补一句当前目标：

```text
继续刚才的文档更新，目标是补齐 Slash 指令和 AGENTS.md 的用户说明。
```

## 常用 Slash 指令

| 命令 | 作用 |
|---|---|
| `/help` | 查看所有 Slash 指令 |
| `/session` | 切换会话 |
| `/compact` | 压缩当前会话上下文 |
| `/provider` | 切换 Provider |
| `/provider add` | 添加自定义 Provider |
| `/model` | 切换模型 |
| `/memo` | 查看记忆 |
| `/remember <text>` | 保存长期偏好或稳定事实 |
| `/forget <keyword>` | 删除匹配的记忆 |
| `/skills` | 查看可用 Skills |
| `/skill use <id>` | 启用 Skill |
| `/skill off <id>` | 停用 Skill |
| `/skill active` | 查看已启用 Skills |
| `/clear` | 清空当前输入草稿 |
| `/exit` | 退出 NeoCode |

完整说明见 [Slash 指令](./slash-commands)。

## 下一步

- 想理解会话和上下文：[会话、上下文与工作区](./context-session-workspace)
- 想让 Agent 遵守项目规则：[AGENTS.md 项目规则](./agents-md)
- 想判断记忆、Skills、MCP 怎么选：[能力选择指南](./capability-choice)
- 想看可复制任务示例：[使用示例](./examples)
