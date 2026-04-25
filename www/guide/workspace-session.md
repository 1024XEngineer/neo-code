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

当会话越来越长时，上下文会占用越来越多的 token 预算。NeoCode 提供三种压缩机制：

### 手动压缩

```text
/compact
```

手动压缩将当前会话历史替换为摘要，保留继续完成任务所需的上下文。压缩策略由 `context.compact.manual_strategy` 控制：

- `keep_recent`：保留最近 N 条消息 + 历史摘要（默认）
- `full_replace`：用完整摘要替换所有历史

### Micro Compact（读时压缩）

每次 Runtime 读取上下文时，自动对较早的工具调用结果进行压缩：

- 保留最近 N 个工具块的原始内容（由 `context.compact.micro_compact_retained_tool_spans` 控制，默认 6）
- 更早的工具块只保留摘要
- `memo_recall` 和 `memo_list` 的结果不参与 micro compact（策略为 `preserve_history`）

Micro Compact 默认启用，可通过 `context.compact.micro_compact_disabled: true` 关闭。

### Reactive Compact（预算触发压缩）

当输入 token 超过预算时，Runtime 会自动触发 reactive compact：

- 预算由 `context.budget.prompt_budget` 控制
- `prompt_budget > 0` 时直接使用该值作为输入预算上限
- `prompt_budget = 0`（默认）时，自动从模型窗口推导输入预算，预留 `reserve_tokens` 给输出和 system prompt
- 单次 Run 内最多触发 `max_reactive_compacts` 次 reactive compact（默认 3 次）

如果 reactive compact 后仍超预算，Run 会因预算不足而终止。

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
