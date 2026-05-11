---
title: Hooks 使用指南
description: 通过 runtime hooks 配置可观测规则，支持 builtin 与 http observe。
---

# Hooks 使用指南

Hooks 是给 NeoCode 加“运行规则”和“状态回调”的方式。

它不是任意脚本执行器。当前只支持两类安全配置：

- `builtin + sync`：做提醒/检查（例如调用 `bash` 前提醒）
- `http + observe`：把运行状态推送到本地组件（例如桌宠、状态面板）

如果你第一次接触，可以先记住下面一句话：

`builtin` 用来“约束行为”，`http observe` 用来“对外广播状态”。

## 什么时候用 Hooks

| 需求 | 建议 |
|---|---|
| 每次都想提醒模型遵守同一条习惯 | 用 `add_context_note` |
| 调用某些工具时想打提示 | 用 `warn_on_tool_call` |
| 完成前需要一个文件存在 | 用 `require_file_exists` |
| 想把运行状态推送给本地组件（桌宠/看板） | 用 `kind: http` + `mode: observe` |
| 想执行自定义脚本 | 当前不支持（见下文） |

## 配置放在哪里

全局（对你所有项目生效）：

```text
~/.neocode/config.yaml
```

项目级（只对当前工作区生效）：

```text
<workspace>/.neocode/hooks.yaml
```

## 快速开始（推荐直接复制）

下面这份示例同时包含 `builtin` 与 `http observe`：

```yaml
runtime:
  hooks:
    enabled: true
    user_hooks_enabled: true
    items:
      - id: user-context-note
        enabled: true
        point: user_prompt_submit
        scope: user
        kind: builtin
        mode: sync
        handler: add_context_note
        params:
          note: "优先最小改动，避免无说明的大范围重构。"

      - id: user-warn-bash
        enabled: true
        point: before_tool_call
        scope: user
        kind: builtin
        mode: sync
        handler: warn_on_tool_call
        params:
          tool_names: ["bash"]
          message: "执行 bash 前请确认命令不会破坏工作区。"

      - id: user-http-observe
        enabled: true
        point: after_tool_result
        scope: user
        kind: http
        mode: observe
        timeout_sec: 2
        failure_policy: warn_only
        params:
          url: "http://127.0.0.1:3101/hook"
          method: POST
          headers:
            X-Source: neocode
          include_metadata: true
```

## 字段怎么填（通俗版）

### 1. `builtin + sync`

| 字段 | 必填 | 说明 |
|---|---|---|
| `kind` | 是 | 固定 `builtin` |
| `mode` | 是 | 固定 `sync` |
| `handler` | 是 | `add_context_note` / `warn_on_tool_call` / `require_file_exists` |
| `params` | 视 handler 而定 | 比如 `tool_name`、`note`、`path` |

### 2. `http + observe`

| 字段 | 必填 | 说明 |
|---|---|---|
| `kind` | 是 | 固定 `http` |
| `mode` | 是 | 固定 `observe` |
| `params.url` | 是 | 绝对 `http/https` 地址 |
| `params.method` | 否 | 默认 `POST` |
| `params.headers` | 否 | 自定义请求头 |
| `params.include_metadata` | 否 | 是否带上 metadata（默认 `true`） |

注意：

- `http observe` 只做观测回调，不会阻断主链。
- `failure_policy` 不能用 `fail_closed`，建议用 `warn_only`。

## 支持哪些 builtin handler

| handler | 作用 | 常见点位 |
|---|---|---|
| `add_context_note` | 给本轮任务补充一条规则说明 | `user_prompt_submit` |
| `warn_on_tool_call` | 调用指定工具时给提醒 | `before_tool_call` |
| `require_file_exists` | 完成前检查文件是否存在 | `before_completion_decision` |

## 仓库级 hooks（repo）示例

在 `<workspace>/.neocode/hooks.yaml`：

```yaml
hooks:
  items:
    - id: require-readme-before-final
      enabled: true
      point: before_completion_decision
      scope: repo
      kind: builtin
      mode: sync
      handler: require_file_exists
      params:
        path: "README.md"
        message: "请先补齐 README.md。"
```

## 回调会发什么（http observe）

回调 body 是 JSON，常见字段如下：

```json
{
  "hook_id": "user-http-observe",
  "point": "after_tool_result",
  "scope": "user",
  "kind": "http",
  "mode": "observe",
  "run_id": "run_xxx",
  "session_id": "session_xxx",
  "triggered_at": "2026-05-11T08:00:00Z",
  "metadata": {
    "tool_name": "bash",
    "stop_reason": "accepted"
  }
}
```

你可以把它理解成“在某个生命周期点位，NeoCode 主动推一条状态通知给你”。

## repo hooks 为什么有时不生效

repo hooks 需要 workspace 先被信任（trusted）。

trust 文件路径：

```text
~/.neocode/trusted-workspaces.json
```

最小示例（绝对路径）：

```json
{
  "version": 1,
  "workspaces": [
    "/absolute/path/to/workspace"
  ]
}
```

如果文件缺失、格式错误或路径不在列表里，repo hooks 会被跳过。这是安全设计，不是 bug。

## 怎么确认生效了

1. 执行一次会触发 Hook 的任务  
2. 在日志视图看事件（`Ctrl+L`）

常见事件：

- `hook_started`
- `hook_finished`
- `hook_failed`
- `repo_hooks_skipped_untrusted`

示例：

```text
hook_finished source=user point=user_prompt_submit hook_id=user-context-note message="优先最小改动..."
```

如果是 `http observe`，还可以在你的回调服务日志里看到 `POST /hook` 的请求记录。

## Clawd 桌宠接入

已经单独整理为实战文档，直接看：

- [NeoCode x Clawd 桌宠接入示例](./hooks-clawd-integration)

## 你需要知道的限制

- 用户 hooks 支持 `kind: builtin`（`mode: sync`）和 `kind: http`（`mode: observe`）。
- `command` / `prompt` / `agent` 暂不支持。
- 配置字段是 `params`，不是 `with`。
- Hooks 主要用于“补充规则、提醒、检查”，不是最终裁决器。

如果配置了不支持的 kind，会看到类似报错：

```text
external hook kind "command" is not supported in P6-lite; only builtin/http-observe hooks are enabled
```
