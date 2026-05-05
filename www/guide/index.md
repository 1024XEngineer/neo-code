---
title: 开始使用
description: NeoCode 用户指南——从安装、核心概念到日常工作流的导航。
---

# 开始使用

NeoCode 是一个在终端里运行的本地 AI 编码助手。它不是云端 SaaS，也不是 IDE 插件；它围绕你的本地工作区、会话上下文、项目规则和工具权限来帮助你读代码、改代码、补测试和写文档。

## 新用户路径

1. [安装与首次运行](./install) — 安装、设 Key、启动、第一条对话
2. [会话、上下文与工作区](./context-session-workspace) — 理解 NeoCode 如何围绕项目和任务工作
3. [Slash 指令](./slash-commands) — 学会本地控制命令
4. [使用示例](./examples) — 复制可用的任务写法

## 核心概念

| 你想了解 | 阅读 |
|---|---|
| `/compact`、`/model` 等命令怎么用 | [Slash 指令](./slash-commands) |
| 工作区、会话和上下文的区别 | [会话、上下文与工作区](./context-session-workspace) |
| 如何让 Agent 遵守项目规则 | [AGENTS.md 项目规则](./agents-md) |
| 记忆、Skills、AGENTS.md、MCP 怎么选 | [能力选择指南](./capability-choice) |

## 仓库 README（快速总览）

- [README（中文）](https://github.com/1024XEngineer/neo-code/blob/main/README.md)
- [README (English)](https://github.com/1024XEngineer/neo-code/blob/main/README.en.md)

## 日常使用

- [日常使用](./daily-use) — 从打开项目到完成任务的工作流
- [工具与权限](./tools-permissions) — Agent 能做什么，审批怎么选
- [配置指南](./configuration) — Provider、模型、Shell、MCP 配置

## 扩展能力

- [Skills 使用](./skills) — 用 `SKILL.md` 固化当前任务的工作方式
- [MCP 工具接入](./mcp) — 把外部工具接入 NeoCode
- [飞书远程接入配置指南](./feishu-remote-setup) — 飞书消息到本地执行的回调与长连接配置
- [Hooks 使用](./hooks) — 在 runtime 生命周期点配置安全 builtin 规则

## 遇到问题

- [排障与常见问题](./troubleshooting)
- [升级与版本检查](./update)

## 进阶与设计

如果你要做外部集成、修改实现或查架构边界，再看 [深入阅读](/reference/)。
