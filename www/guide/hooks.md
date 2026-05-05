---
title: Hooks 使用指南
description: 通过安全 builtin hooks 在 runtime 生命周期点配置用户/项目规则。
---

# Hooks 使用指南

Hooks 是给 NeoCode 加“运行规则”的方式。

它不是任意脚本执行器。你只能在固定点位配置内置能力（builtin handler）。

如果你是第一次接触，可以把它理解成三件事：

1. 在任务开始时给模型补一条规则（例如“优先最小改动”）  
2. 在调用某些工具时给提醒（例如调用 `bash` 前提醒）  
3. 在任务完成前做简单检查（例如 `README.md` 必须存在）

## 什么时候用 Hooks

| 需求 | 建议 |
|---|---|
| 每次都想提醒模型遵守同一条习惯 | 用 `add_context_note` |
| 调用某些工具时想打提示 | 用 `warn_on_tool_call` |
| 完成前需要一个文件存在 | 用 `require_file_exists` |
| 想执行自定义脚本或 HTTP 回调 | 当前不支持（见下文） |

## 配置放在哪里

全局（对你所有项目生效）：

```text
~/.neocode/config.yaml
```

项目级（只对当前工作区生效）：

```text
<workspace>/.neocode/hooks.yaml
```

## 先抄可用模板

### A. 全局规则（推荐先用）

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
```

### B. 项目完成前检查

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

## 支持哪些内置能力

当前只支持 3 个 builtin handler：

| handler | 作用 | 常见点位 |
|---|---|---|
| `add_context_note` | 给本轮任务补充一条规则说明 | `user_prompt_submit` |
| `warn_on_tool_call` | 调用指定工具时给提醒 | `before_tool_call` |
| `require_file_exists` | 完成前检查文件是否存在 | `before_completion_decision` |

字段说明（最常用）：

- `params.note` / `params.message`：`add_context_note` 文案
- `params.tool_name` 或 `params.tool_names`：`warn_on_tool_call` 目标工具
- `params.path`：`require_file_exists` 要检查的文件

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

## 你需要知道的限制

- 目前只支持 `kind: builtin`。
- `command` / `http` / `prompt` / `agent` 暂不支持。
- `mode` 对用户配置仅支持 `sync`。
- 配置字段是 `params`，不是 `with`。
- Hooks 主要用于“补充规则、提醒、检查”，不是最终裁决器。

如果配置了不支持的 kind，会看到类似报错：

```text
external hook kind "command" is not supported in P6-lite; only builtin hooks are enabled
```
