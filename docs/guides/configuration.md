# 配置指南

本文说明 NeoCode 当前真实生效的配置结构与约束。

## 配置文件位置

主配置文件：

```text
~/.neocode/config.yaml
```

自定义 provider 目录：

```text
~/.neocode/providers/<provider-name>/provider.yaml
```

## `config.yaml` 示例

```yaml
selected_provider: openai
current_model: gpt-5.4
shell: bash
tool_timeout_sec: 20
generate_start_timeout_sec: 90

runtime:
  max_no_progress_streak: 5
  max_repeat_cycle_streak: 3
  max_turns: 90
  hooks:
    enabled: true
    user_hooks_enabled: true
    default_timeout_sec: 2
    default_failure_policy: warn_only
    items:
      - id: warn-bash
        enabled: true
        point: before_tool_call
        scope: user
        kind: builtin
        mode: sync
        handler: warn_on_tool_call
        priority: 100
        params:
          tool_name: bash
          message: "bash tool is invoked"
  assets:
    max_session_asset_bytes: 20971520
    max_session_assets_total_bytes: 20971520

tools:
  webfetch:
    max_response_bytes: 262144
    supported_content_types:
      - text/html
      - text/plain
      - application/json

context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 10
    micro_compact_retained_tool_spans: 6
    read_time_max_message_spans: 24
    max_summary_chars: 1200
    micro_compact_disabled: false
  budget:
    prompt_budget: 0
    reserve_tokens: 13000
    fallback_prompt_budget: 100000
    max_reactive_compacts: 3
```

## 基础字段

| 字段 | 说明 |
|------|------|
| `selected_provider` | 当前选中的 provider 名称 |
| `current_model` | 当前选中的模型 ID |
| `shell` | 默认 shell；Windows 默认 `powershell`，其他平台默认 `bash` |
| `tool_timeout_sec` | 工具执行超时秒数 |

## `context` 字段

### `context.compact`

| 字段 | 说明 |
|------|------|
| `context.compact.manual_strategy` | `/compact` 手动压缩策略，支持 `keep_recent` / `full_replace` |
| `context.compact.manual_keep_recent_messages` | `keep_recent` 下保留的最近消息数 |
| `context.compact.micro_compact_retained_tool_spans` | read-time micro compact 默认保留原始内容的最近工具块数量 |
| `context.compact.read_time_max_message_spans` | context 构建时保留的 message span 上限 |
| `context.compact.max_summary_chars` | compact summary 最大字符数 |
| `context.compact.micro_compact_disabled` | 是否关闭默认启用的 micro compact |

### `context.budget`

| 字段 | 说明 |
|------|------|
| `context.budget.prompt_budget` | 显式输入预算；`> 0` 时直接使用，`0` 表示自动推导 |
| `context.budget.reserve_tokens` | 自动推导输入预算时，从模型窗口中预留给输出、tool call、system prompt 的缓冲 |
| `context.budget.fallback_prompt_budget` | 模型窗口不可用或推导失败时使用的保底输入预算 |
| `context.budget.max_reactive_compacts` | 单次 `Run()` 内允许的 reactive compact 最大次数 |

## `runtime` 字段

| 字段 | 说明 |
|------|------|
| `runtime.max_no_progress_streak` | 连续“无进展”轮次提醒阈值，默认 `5`；达到 `limit-1` 起会向模型注入纠偏提示，不会直接终止运行 |
| `runtime.max_repeat_cycle_streak` | 连续“重复调用同一工具参数”提醒阈值，默认 `3`；达到阈值后触发重复循环提醒，不会直接终止运行 |
| `runtime.max_turns` | 单次 Run 的最大推理轮数上限，默认 `40`；达到上限后直接终止并返回明确 stop reason |
| `runtime.hooks.enabled` | hooks 总开关；关闭后不执行 runtime hooks |
| `runtime.hooks.user_hooks_enabled` | user hooks 开关；关闭后不加载 `runtime.hooks.items` |
| `runtime.hooks.default_timeout_sec` | user hook 默认超时秒数，需 `> 0` |
| `runtime.hooks.default_failure_policy` | 默认失败策略，支持 `warn_only` / `fail_open` / `fail_closed` |
| `runtime.hooks.items` | user builtin hooks 列表；仅支持 `scope=user`、`kind=builtin`、`mode=sync` |
| `runtime.assets.max_session_asset_bytes` | 单个 `session_asset` 最大原始字节数，默认 `20971520`（20 MiB）；`0` 或未配置时回退默认值 |
| `runtime.assets.max_session_assets_total_bytes` | 单次请求可携带的 `session_asset` 原始总字节上限，默认 `20971520`（20 MiB）；`0` 或未配置时回退默认值 |

### `runtime.hooks.items` 字段约束

| 字段 | 说明 |
|------|------|
| `id` | hook 唯一标识，同一配置文件内不可重复 |
| `enabled` | 是否启用该 hook，默认 `true` |
| `point` | 支持 `before_tool_call` / `after_tool_result` / `before_completion_decision` / `before_permission_decision` / `after_tool_failure` / `session_start` / `session_end` / `user_prompt_submit` / `pre_compact` / `post_compact` / `subagent_start` / `subagent_stop` |
| `scope` | P2 固定为 `user` |
| `kind` | P2 固定为 `builtin` |
| `mode` | P2 固定为 `sync` |
| `handler` | 仅支持 `require_file_exists` / `warn_on_tool_call` / `add_context_note` |
| `priority` | 同一 hook point 内执行优先级，数值越大越先执行 |
| `timeout_sec` | 覆盖默认超时；未配置时继承 `runtime.hooks.default_timeout_sec` |
| `failure_policy` | 覆盖默认失败策略；未配置时继承 `runtime.hooks.default_failure_policy` |
| `params` | handler 参数；不同 handler 使用不同键 |

> 注意：`warn_only` 在 runtime 内部映射为 `fail_open`，表示记录失败但不阻断主链。

### Repo Hooks（P3）

仓库级 hooks 文件路径固定为：

```text
<workspace>/.neocode/hooks.yaml
```

执行受 trust gate 控制，默认不执行。只有当 workspace 出现在 `~/.neocode/trusted-workspaces.json` 中时才会加载。

`hooks.yaml` 示例：

```yaml
hooks:
  items:
    - id: repo-readme-check
      enabled: true
      point: before_completion_decision
      scope: repo
      kind: builtin
      mode: sync
      handler: require_file_exists
      params:
        path: README.md
        message: "请先补齐 README.md"
```

trust store 示例：

```json
{
  "version": 1,
  "workspaces": [
    "/absolute/path/to/workspace"
  ]
}
```

约束说明：

- `runtime.hooks.enabled=false` 会关闭全部 hooks（internal/user/repo）。
- repo hooks 仅支持 builtin 子集（上述 points + 3 个 handlers）。
- 执行顺序固定：`internal -> user -> repo`。
- 跨来源同 ID 允许并存；同来源内重复 ID 会报错。
- trust store 缺失/空文件/损坏 JSON/结构错误时，按 untrusted 处理并发出 `repo_hooks_trust_store_invalid` 事件，不阻断启动。
- `before_permission_decision` / `pre_compact` / `subagent_start` 不允许 user/repo 挂载，配置会 fail-fast。

## Budget 解析规则

NeoCode 已不再使用旧的 `auto_compact` 阈值语义，当前统一使用 `context.budget`：

1. `context.budget.prompt_budget > 0` 时，直接使用显式预算。
2. `context.budget.prompt_budget <= 0` 时，系统尝试基于当前 provider/model 的 `ContextWindow` 自动推导。
3. 自动推导公式为：

```text
prompt_budget = context_window - reserve_tokens
```

4. 如果当前 provider 选择无效、catalog snapshot 查询失败、模型缺少可用 `ContextWindow`，或 `ContextWindow <= reserve_tokens`，则回退到 `fallback_prompt_budget`。

## 配置结构升级

启动装配阶段会在严格解析 `config.yaml` 前执行一次 preflight 结构升级：

- 仅当检测到 `context.auto_compact` 时，自动迁移为 `context.budget`。
- 迁移前会写入 `config.yaml.bak`，原配置内容保留在备份中。
- 如果旧配置显式 `context.auto_compact.enabled: false`，迁移仍会执行，并记录说明：
  `旧 context.auto_compact.enabled 已废弃，新预算门禁不可关闭`。
- 如果 `context.auto_compact` 与 `context.budget` 同时存在，程序会直接报错，避免猜测覆盖用户配置。
- 主解析器仍只接受当前结构；迁移完成后不会在运行时兼容旧字段。

打包用户不需要额外执行迁移命令。`neocode migrate context-budget` 仅用于提前检查或手动修复配置文件。

## 预算闭环

当前发送链路采用固定闭环：

```text
BuildRequest -> FreezeSnapshot -> EstimateInput -> DecideBudget -> (allow | compact | stop)
```

规则如下：

- provider 发送前一定先做输入 token estimate。
- 如果 estimate 没超过 `prompt_budget`，本轮允许发送。
- 如果 estimate 首次超预算，先执行一次 `proactive` compact，然后重建请求并重新估算。
- 如果 compact 后仍超预算且 `gate_policy=gateable`，停止当前 run，并产出 `STOP_BUDGET_EXCEEDED`。
- 如果 compact 后仍超预算但 `gate_policy=advisory`，不直接硬停，继续发送请求。
- 如果 provider 返回 `context_too_long`，runtime 会进入 `reactive` compact 恢复链路，并重新进入同一预算闭环。

## provider 策略

NeoCode 采用 “builtin provider + custom provider” 双来源模型。

### builtin provider

内置 provider 定义于：

```text
internal/config/provider.go
```

当前内置 provider：

- `openai`
- `gemini`
- `qiniu`
- `modelscope`

### custom provider

自定义 provider 通过单独文件声明，而不是写入 `config.yaml`：

```yaml
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: discover
base_url: https://llm.example.com/v1
chat_api_mode: chat_completions
chat_endpoint_path: /chat/completions
discovery_endpoint_path: /models
generate_max_retries: 5
generate_idle_timeout_sec: 300
```

新增的生成链路控制字段含义如下：

- `generate_max_retries`：额外重试次数，不含首次尝试；未填写时默认使用 `5`，显式填写 `0` 表示关闭生成重试，且必须 `<= 20`。
- `generate_start_timeout_sec`：写在 `config.yaml` 顶层，从发请求到收到首个有效流 payload 的最长等待窗口；`<= 0` 时回退默认值 `90`。
- `generate_idle_timeout_sec`：首包后连续没有任何新 payload 的最长空闲窗口；`<= 0` 时回退默认值 `300`。

启动时会自动把缺失的 `generate_start_timeout_sec` 规范化写回 `config.yaml`，避免磁盘配置与运行时默认值不一致。

## 不写入 `config.yaml` 的字段

以下内容不允许写入主配置文件：

- `providers`
- `provider_overrides`
- `workdir`
- `default_workdir`
- `base_url`
- `api_key_env`
- `models`

如果这些字段出现在 `config.yaml` 中，加载会直接失败，不会自动迁移或清理。

## 环境变量

API Key 只从系统环境变量读取。

常见映射：

| Provider | 环境变量 |
|----------|----------|
| `openai` | `OPENAI_API_KEY` |
| `gemini` | `GEMINI_API_KEY` |
| `qiniu` | `QINIU_API_KEY` |
| `modelscope` | `MODELSCOPE_API_KEY` |

示例：

```bash
export OPENAI_API_KEY="sk-..."
export GEMINI_API_KEY="AI..."
export MODELSCOPE_API_KEY="ms-..."
```

Windows PowerShell：

```powershell
$env:OPENAI_API_KEY = "sk-..."
$env:GEMINI_API_KEY = "AI..."
$env:MODELSCOPE_API_KEY = "ms-..."
```

## CLI 运行参数覆盖

工作目录不写入 `config.yaml`，只通过启动参数覆盖：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

说明：

- `--workdir` 只影响当前进程
- 不会回写到 `config.yaml`
- 工具根目录与 session 隔离都会使用该工作区

## 常见错误

### 旧字段被拒绝

如果在 `config.yaml` 中看到如下字段：

- `workdir`
- `default_workdir`
- `providers`
- `provider_overrides`

当前版本会直接报未知字段或结构不匹配错误。处理方式是手动删除旧字段，而不是等待程序自动兼容。

`context.auto_compact` 是例外：如果配置中只存在旧预算块，启动 preflight 会自动迁移为 `context.budget`；如果新旧预算块同时存在，则需要手动合并后再启动。

### API Key 未设置

报错示例：

```text
config: environment variable OPENAI_API_KEY is empty
```

先在当前 shell 中设置对应环境变量，再启动 NeoCode。
