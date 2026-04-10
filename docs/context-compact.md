# Context Compact

本文档说明 NeoCode 中 context compact 的配置、执行链路和摘要约定。

## 概览

- runtime 已接入手动 compact、基于 token 阈值的自动 compact，以及 provider 上下文过长后的 `reactive` compact 自动恢复。
- `internal/context/compact` 支持 `manual` 与 `reactive` 两种 mode。
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
    micro_compact_disabled: false
  auto_compact:
    enabled: false
    input_token_threshold: 100000
```

- `manual_strategy`
  控制手动 compact 的策略，支持 `keep_recent` 和 `full_replace`。
- `manual_keep_recent_messages`
  在 `keep_recent` 模式下保留最近消息数量，并按 tool call 与 tool result 的原子块整体保留。
- `max_summary_chars`
  控制 compact summary 的最大字符数。
- `micro_compact_disabled`
  控制是否关闭默认启用的读时 micro compact；设为 `true` 时会回退为仅 trim、不清理旧 tool result。
- `auto_compact.enabled`
  控制是否启用基于 token 阈值的自动压缩；默认关闭。
- `auto_compact.input_token_threshold`
  当会话累计输入 token 数达到此阈值时触发自动压缩；默认 100000。

## 自动压缩

当 `auto_compact.enabled` 为 `true` 时，runtime 在每次调用 `context.Builder.Build()` 时将当前 token 累计值传入 Metadata，context 模块通过比较累计值与阈值在 `BuildResult.ShouldAutoCompact` 中返回压缩建议。runtime 读取建议后调用现有 compact 管线执行压缩，并在成功后重置 token 计数器。

设计原则：
- **context 拥有压缩决策权**，runtime 只做编排执行。
- 每次 `Run()` 调用最多触发一次自动压缩，避免无限循环。
- 压缩成功后 token 计数器重置为零，下一轮不会立即重复触发。

新增工具时，micro compact 策略不再由 `context` 层静态白名单维护，而是由 `internal/tools` 中的工具实现声明。
默认情况下，已注册工具都会参与 micro compact；只有显式声明保留历史结果的工具才会跳过旧结果清理。

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

其中 `reactive` mode 在 context 包内与 `manual` 复用同一条压缩管线：

1. 先写 transcript。
2. 默认按 `keep_recent` 裁剪可归档历史。
3. 生成并校验 `[compact_summary]`。
4. 返回压缩后的消息与 transcript 元信息。

当 provider 返回“上下文过长”错误时，runtime 会：

1. 识别 provider 归一化后的 typed error，必要时回退到错误文本匹配。
2. 触发一次 `compact.Run(mode=reactive)`。
3. 继续复用 `compact_start`、`compact_done`、`compact_error` 事件，并通过 `trigger_mode=reactive` 区分来源。
4. 每次 `Run()` 最多只执行一次 reactive 重试，避免无限循环。

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

compact 相关 runtime 事件包括：

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
