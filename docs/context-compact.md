# Context Compact

## Auto Compact Failure Fallback

- 当 `context.auto_compact.input_token_threshold <= 0` 时，系统会尝试基于当前模型的 `ContextWindow` 自动推导阈值。
- 若当前 provider 选择无效、catalog snapshot 查询失败，或模型窗口元数据缺失，系统会直接回退到 `fallback_input_token_threshold`。
- 自动推导失败不会静默关闭 auto compact；runtime 仍会拿到一个可用的保底阈值。

本文档说明 NeoCode 中 context compact 的配置、执行链路和摘要约定。

## 概览

- runtime 已接入手动 compact、基于 token 阈值的自动 compact，以及 provider 上下文过长后的 `reactive` compact 自动恢复。
- `internal/context/compact` 支持 `manual`、`auto` 与 `reactive` 三种 mode。
- 用户通过 `/compact` 对当前会话执行一次上下文压缩。
- compact 前会先写入完整 transcript，随后生成并校验新的 durable `TaskState` 与 display summary，再回写会话消息。
- compact 的 system prompt 静态说明模板由 `internal/promptasset` 通过 `go:embed` 提供，但 compact user prompt 的元数据块、消息边界和 transcript 渲染仍由代码拼装。

## 配置

compact 相关配置位于：

```yaml
context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 10
    micro_compact_retained_tool_spans: 6
    read_time_max_message_spans: 24
    max_summary_chars: 1200
    micro_compact_disabled: false
  auto_compact:
    enabled: false
    input_token_threshold: 0
    reserve_tokens: 13000
    fallback_input_token_threshold: 100000
```

- `manual_strategy`
  控制手动 compact 的策略，支持 `keep_recent` 和 `full_replace`。
- `manual_keep_recent_messages`
  在 `keep_recent` 模式下保留最近消息数量，并按 tool call 与 tool result 的原子块整体保留。
- `read_time_max_message_spans`
  控制 `context.Builder` 读时 trim 可保留的 message span 上限；该值越大，普通“继续”续跑时越不容易在未触发 compact 前丢掉较早的文件读取结果。
- `micro_compact_retained_tool_spans`
  控制 read-time micro compact 默认保留原始内容的最近可压缩工具块数量；默认值为 `6`，显式配置为更小值时可更积极地回收旧工具结果。
- `max_summary_chars`
  控制 compact summary 的最大字符数。
- `micro_compact_disabled`
  控制是否关闭默认启用的读时 micro compact；设为 `true` 时会回退为仅 trim、不清理旧 tool result。
- `auto_compact.enabled`
  控制是否启用基于 token 阈值的自动压缩；默认关闭。
- `auto_compact.input_token_threshold`
  当会话累计输入 token 数达到此阈值时触发自动压缩；默认 `0`（自动推导），推导失败时回退到 `fallback_input_token_threshold`（默认 `100000`）。

## 自动压缩

当 `auto_compact.enabled` 为 `true` 时，runtime 在每次调用 `context.Builder.Build()` 时将当前 token 累计值传入 Metadata，context 模块通过比较累计值与阈值在 `BuildResult.AutoCompactSuggested` 中返回压缩建议。runtime 读取建议后调用现有 compact 管线执行压缩；token 计数的重置与持久化语义统一见 [Session 持久化设计](./session-persistence-design.md)。

设计原则：
- **context 拥有压缩决策权**，runtime 只做编排执行。
- 每次 `Run()` 调用最多触发一次自动压缩，避免无限循环。
- 压缩成功后 token 计数器重置为零，下一轮不会立即重复触发。

新增工具时，micro compact 策略不再由 `context` 层静态白名单维护，而是由 `internal/tools` 中的工具实现声明。
默认情况下，已注册工具都会参与 micro compact；只有显式声明保留历史结果的工具才会跳过旧结果清理。
默认 pin 仅对 `filesystem_write_file` 与 `filesystem_edit` 这类文件内容修改工具生效，用于保留 `README`、spec/schema、`go.mod`、`package.json` 等关键产物的最近结果；`.env*` 不参与默认 pin，避免敏感内容在上下文中滞留更久。
但 micro compact 只有在当前会话已经建立非空 `TaskState` 时才会生效；没有 durable task state 时，context 仅做 trim，不清理旧 tool result。

## 执行链路

1. TUI 识别 `/compact` 并调用 `runtime.Compact(...)`。
2. runtime 发出 `compact_start` 事件。
3. compact runner 将原始消息写入 transcript（JSONL）。
4. compact runner 根据策略构造归档消息与保留消息，并过滤旧的 `[compact_summary]` 展示摘要，避免“摘要的摘要”。
5. runtime 选择用于生成 summary 的 provider 和 model：
   优先复用会话记录的 `provider` / `model`，缺失时回退到当前配置。
6. summary generator 调用模型生成完整 `task_state` 与 display summary。
7. runner 校验 display summary 结构与长度，必要时截断，并写入 `task_state.last_updated_at`。
8. compact 成功时回写 `session.TaskState` 与会话消息并发出 `compact_applied`；失败时发出 `compact_error`。

其中 `reactive` mode 在 context 包内与 `manual` 复用同一条压缩管线：

1. 先写 transcript。
2. 默认按 `keep_recent` 裁剪可归档历史。
3. 生成并校验 display summary，同时更新 durable `TaskState`。
4. 返回压缩后的消息、`TaskState` 与 transcript 元信息。

当 provider 返回“上下文过长”错误时，runtime 会：

1. 识别 provider 归一化后的 typed error，必要时回退到错误文本匹配。
2. 触发 `compact.Run(mode=reactive)`，并在仍然命中“上下文过长”时继续做逐步降级恢复。
3. 继续复用 `compact_start`、`compact_applied`、`compact_error` 事件，并通过 `trigger_mode=reactive` 区分来源。
4. 每次 `Run()` 最多执行 3 次 reactive compact 降级尝试；每次尝试都会进一步收缩 `manual_keep_recent_messages`，超过上限后返回最后一次 provider 错误。

## 生成协议

compact generator 必须只返回一个 JSON 对象，顶层固定包含：

```json
{
  "task_state": {
    "goal": "",
    "progress": [],
    "open_items": [],
    "next_step": "",
    "blockers": [],
    "key_artifacts": [],
    "decisions": [],
    "user_constraints": []
  },
  "display_summary": "[compact_summary]\n..."
}
```

- `task_state` 表示 compact 之后的完整 durable task state，而不是增量 patch。
- `task_state` 只允许包含固定字段，不允许混入模型自定义键。
- `display_summary` 仍然必须使用 `[compact_summary]` 协议，供人类阅读和后续轮次参考。

上述 JSON 契约与 `[compact_summary]` 格式模板仍由代码注入到 compact system prompt 中，避免在模板文件里复制一份会随实现演进的协议定义。

`display_summary` 必须以如下结构返回：

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

- durable truth 优先进入 `TaskState`，而不是散落在聊天消息里。
- `TaskState` 重点保留目标、已完成进展、未完成事项、下一步、阻塞点、关键工件、决策、用户约束。
- `display_summary` 只保留继续工作最少需要的人类可读信息。
- 默认忽略工具详细输出、重复背景、已解决错误的排查细节。

## 事件

compact 相关 runtime 事件包括：

- `compact_start`
- `compact_applied`
- `compact_error`

`compact_applied` payload 包含：

- `applied`
- `before_chars`
- `before_tokens`
- `after_chars`
- `saved_ratio`
- `trigger_mode`
- `transcript_id`
- `transcript_path`

## Auto Compact 阈值解析

- `context.auto_compact.input_token_threshold > 0` 时，直接使用显式手动阈值。
- `context.auto_compact.input_token_threshold <= 0` 时，系统会对当前选中的 provider/model 做自动推导。
- 自动推导公式为 `resolved_threshold = context_window - reserve_tokens`。
- `reserve_tokens` 默认 `13000`，用于给输出、tool call 和 system prompt 预留缓冲。
- 如果当前模型没有可用的 `ContextWindow`，或窗口值小于等于 `reserve_tokens`，则回退到 `fallback_input_token_threshold`。
- `fallback_input_token_threshold` 默认 `100000`，用于保证主链路在缺少模型窗口元数据时仍可稳定运行。
