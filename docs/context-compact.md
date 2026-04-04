# Context Compact

本文档说明 NeoCode 中 manual compact 的配置、执行链路和摘要约定。

## 概览

- 当前仅支持手动触发的 compact，不包含自动 compact。
- 用户通过 `/compact` 对当前会话执行一次上下文压缩。
- compact 前会先写入完整 transcript，随后生成并校验 compact summary，再回写会话消息。

## 配置

compact 相关配置位于：

```yaml
context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 10
    max_summary_chars: 1200
```

- `manual_strategy`
  控制手动 compact 的策略，支持 `keep_recent` 和 `full_replace`。
- `manual_keep_recent_messages`
  在 `keep_recent` 模式下保留最近消息数量，并按 tool call 与 tool result 的原子块整体保留。
- `max_summary_chars`
  控制 compact summary 的最大字符数。

## 执行链路

1. TUI 识别 `/compact` 并调用 `runtime.Compact(...)`。
2. runtime 发出 `compact_start` 事件。
3. compact runner 将原始消息写入 transcript（JSONL）。
4. compact runner 根据策略构造归档消息与保留消息。
5. runtime 选择用于生成 summary 的 provider 和 model：
   优先复用会话记录的 `provider` / `model`，缺失时回退到当前配置。
6. summary generator 调用模型生成语义摘要。
7. runner 校验摘要结构与长度，必要时截断。
8. compact 成功时回写会话消息并发出 `compact_done`；失败时发出 `compact_error`。

## 摘要协议

compact summary 必须以如下结构返回：

```text
[compact_summary]

done:
- ...

in_progress:
- ...

decisions:
- ...

code_changes:
- ...

constraints:
- ...
```

- 必须包含固定起始标记 `[compact_summary]`。
- 必须包含 `done`、`in_progress`、`decisions`、`code_changes`、`constraints` 五个 section。
- 每个 section 至少包含一条非空 bullet。

## 保留原则

- 优先保留已完成事项及结果。
- 保留仍在进行中的状态、关键决策及原因、关键代码改动、用户约束。
- 默认忽略工具详细输出、重复背景、已解决错误的排查细节。

## 事件

manual compact 相关 runtime 事件包括：

- `compact_start`
- `compact_done`
- `compact_error`

`compact_done` payload 包含：

- `applied`
- `before_chars`
- `after_chars`
- `saved_ratio`
- `trigger_mode`
- `transcript_id`
- `transcript_path`
