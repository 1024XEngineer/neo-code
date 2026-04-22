---
title: 工作区与会话
description: 解释 --workdir、/cwd、/session、/compact 以及会话持久化的使用边界。
---

# 工作区与会话

## `--workdir` 和 `/cwd` 的区别

NeoCode 当前同时提供启动参数和会话内命令：

- `--workdir`：只影响当前进程启动时的工作区，不回写配置文件
- `/cwd [path]`：在当前会话里查看或切换工作区

启动参数示例：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

会话内命令示例：

```text
/cwd
/cwd /path/to/workspace
```

## 会话切换

当前 TUI 中存在 `/session` 命令，用来切换到其他会话。配合会话持久化，适合把不同任务拆开管理，而不是把所有内容堆在一个长会话里。

## 上下文压缩

当会话越来越长时，可以使用：

```text
/compact
```

当前实现支持：

- 手动压缩策略
- 自动压缩相关阈值配置
- 读时 micro compact

压缩策略的目标是保留继续完成任务所需的上下文，而不是让 UI 自己保存散落状态。

## 会话为什么重要

NeoCode 把这些状态优先放在 Runtime / Session 层，而不是散在 UI：

- 消息历史
- 工具调用记录
- token 累积
- 激活的 Skills
- 记忆提取与回放相关内容

这也是为什么工作区、会话和压缩是一起看的。

## 何时使用多个会话

建议切分会话的情况：

- 你在不同仓库或不同工作区之间来回切换
- 一个会话已经积累了很多无关上下文
- 你想让某个任务的记忆、压缩和工具轨迹保持独立

## 继续阅读

- 记忆和 Skills：看 [记忆与 Skills](./memo-skills)
- 配置压缩阈值：看 [配置入口](./configuration)
