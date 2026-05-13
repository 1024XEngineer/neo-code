---
title: Hooks Guide
description: Configure runtime hooks with safe builtin handlers and HTTP observe callbacks.
---

# Hooks Guide

Hooks let you add runtime rules and observability callbacks to NeoCode.

Today, two user-facing types are supported:

- `builtin + sync`: guardrails and reminders
- `http + observe`: event callbacks to local components (like desktop pets or status dashboards)

## When to use hooks

| Need | Use |
|---|---|
| Add stable instruction every run | `add_context_note` |
| Warn before specific tool calls | `warn_on_tool_call` |
| Require file existence before completion | `require_file_exists` |
| Push lifecycle events to another app | `kind: http` + `mode: observe` |

## Config locations

Global config:

```text
~/.neocode/config.yaml
```

Repo-level config:

```text
<workspace>/.neocode/hooks.yaml
```

## Quick template

```yaml
runtime:
  hooks:
    enabled: true
    user_hooks_enabled: true
    items:
      - id: user-note
        enabled: true
        point: user_prompt_submit
        scope: user
        kind: builtin
        mode: sync
        handler: add_context_note
        params:
          note: "Prefer minimal safe changes."

      - id: user-warn-bash
        enabled: true
        point: before_tool_call
        scope: user
        kind: builtin
        mode: sync
        handler: warn_on_tool_call
        params:
          tool_name: bash
          message: "Confirm the bash command is safe before execution."

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
          include_metadata: false
```

## HTTP observe fields

| Field | Required | Notes |
|---|---|---|
| `kind` | Yes | Must be `http` |
| `mode` | Yes | Must be `observe` |
| `params.url` | Yes | Absolute `http/https` URL |
| `params.method` | No | Default `POST` |
| `params.headers` | No | Custom request headers |
| `params.include_metadata` | No | Include metadata payload (`false` by default) |

Notes:

- `http observe` is non-blocking by design.
- `failure_policy: fail_closed` is not allowed for `http observe`.
- Even when enabled, sensitive fields (`result_content_preview`, `execution_error`) are stripped from callback metadata.
- `params.url` is restricted to loopback hosts (`localhost`, `127.0.0.1`, `::1`) to prevent accidental public exfiltration.

## Callback payload

NeoCode sends JSON similar to:

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

## Clawd integration

See the step-by-step walkthrough:

- [NeoCode x Clawd Integration Example](./hooks-clawd-integration)

## Current limits

- Supported for user hooks: `builtin/sync` and `http/observe`
- Not supported yet: `command`, `prompt`, `agent`
- Hook params use `params` (not `with`)
