# Runtime Hooks 设计说明

本文记录 NeoCode runtime hooks 的当前实现边界与约束，确保配置、运行时行为与可观测性一致。

## 当前阶段

当前已实现能力：

- P0：hooks core（registry / executor / timeout / panic recover / failure policy / hook events）
- P1：接入 `before_tool_call`、`after_tool_result`、`before_completion_decision`
- P2：全局 user builtin hooks（`runtime.hooks`）

当前未实现能力：

- repo hooks（P3）
- async / async_rewake（P5）
- command/http/prompt/agent hooks（P6）

## P2 user hooks 边界

P2 仅支持：

- `scope=user`
- `kind=builtin`
- `mode=sync`
- 挂载点：`before_tool_call`、`after_tool_result`、`before_completion_decision`
- handler：`require_file_exists`、`warn_on_tool_call`、`add_context_note`

P2 明确不支持：

- user hook 修改 tool 输入或 tool result
- user hook 直接写入 provider-facing prompt

## 安全模型

### 上下文裁剪

user hook 接收的 `HookContext` 经过白名单裁剪，仅保留最小必要字段：

- `run_id` / `session_id`
- `point` / `tool_call_id` / `tool_name`
- `is_error` / `error_class`
- `result_content_preview` / `result_metadata_present`
- `execution_error`
- `workdir`

不会暴露：

- API key / capability token
- service 指针与 provider 客户端对象
- 原始工具参数明文

### 路径约束

`require_file_exists` 对 `params.path` 强制执行工作目录边界检查：

- 相对路径按当前运行 workdir 解析
- 绝对路径必须位于 workdir 内
- symlink 路径会进行 realpath 校验，禁止绕过

## 可观测性

runtime 会透传 hooks 生命周期事件：

- `hook_started`
- `hook_finished`
- `hook_failed`
- `hook_blocked`

`hook_finished/hook_failed` 包含 `message` 字段，用于承载 warning/note 文本。  
user hook 的 `message` 同时进入 runtime 的 annotation buffer（运行态内存缓冲），用于后续观测与诊断。

## 失败策略

配置层支持：

- `warn_only`
- `fail_open`
- `fail_closed`

运行时映射：

- `warn_only` -> `fail_open`
- `fail_open` -> `fail_open`
- `fail_closed` -> `fail_closed`

其中 `warn_only/fail_open` 不阻断主链，仅记录失败；`fail_closed` 触发阻断。
