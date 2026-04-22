---
title: 首次上手
description: 第一次使用 NeoCode 时最常见的提问方式、Slash 命令和会话入口。
---

# 首次上手

## 最快的起步方式

完成安装并配置好环境变量后，直接启动：

```bash
go run ./cmd/neocode
```

第一次进入会话时，你可以直接输入自然语言任务，例如：

```text
请先阅读当前项目目录结构并给出模块职责摘要
帮我在 internal/runtime 下定位与 tool result 回灌相关逻辑
```

## 常用 Slash 命令

以下命令来自当前 TUI 实现，适合第一次上手时直接记住：

| 命令 | 作用 |
| --- | --- |
| `/help` | 查看 Slash 命令帮助 |
| `/clear` | 清空当前草稿会话展示 |
| `/compact` | 压缩当前会话上下文 |
| `/status` | 查看当前会话与运行状态 |
| `/provider` | 打开 Provider 选择器 |
| `/provider add` | 添加自定义 Provider |
| `/model` | 打开 Model 选择器 |
| `/session` | 切换到其他会话 |
| `/cwd [path]` | 查看或设置当前会话工作区 |
| `/memo` | 查看持久记忆索引 |
| `/remember <text>` | 写入持久记忆 |
| `/forget <keyword>` | 删除匹配关键词的记忆 |
| `/skills` | 查看当前可用 Skills |
| `/skill use <id>` | 激活某个 Skill |
| `/skill off <id>` | 停用某个 Skill |
| `/skill active` | 查看当前会话已激活的 Skills |
| `/exit` | 退出 NeoCode |

## Provider 和 Model

当前 README 与配置文档里明确列出的内置 Provider 包括：

- `openai`
- `gemini`
- `openll`
- `qiniu`

你可以通过交互入口切换：

- `/provider`
- `/model`

也可以在配置文件中保存上一次的选择状态，详细见 [配置入口](./configuration)。

## 本地命令执行

如果你需要在当前工作区里直接运行一个本地命令，可以使用：

```text
& git status
```

这适合把终端里的简单检查与会话任务放在同一个上下文里处理。

## 什么时候继续往下看

- 想了解配置文件和自定义 Provider：看 [配置入口](./configuration)
- 想理解工作区、会话切换和压缩：看 [工作区与会话](./workspace-session)
- 想启用记忆和 Skills：看 [记忆与 Skills](./memo-skills)
