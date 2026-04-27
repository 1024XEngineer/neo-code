---
title: Tools & Permissions
description: What the agent can do and how to choose Allow once, Allow session, or Reject.
---

# Tools & Permissions

NeoCode uses tools to interact with your project. Read-only actions usually run automatically. File writes, edits, and risky commands ask for approval.

## What the agent can do

| Capability | Tool | Usually asks? |
|---|---|---|
| Read files | `filesystem_read_file` | No |
| Search file content | `filesystem_grep` | No |
| Search file paths | `filesystem_glob` | No |
| Write files | `filesystem_write_file` | Yes |
| Edit files | `filesystem_edit` | Yes |
| Run commands | `bash` | Depends on risk |
| Fetch web pages | `webfetch` | Depends on domain policy |
| Manage task list | `todo_write` | No |
| Manage memory | `memo_*` | No |
| Start subagents | `spawn_subagent` | No |

MCP tools use names like `mcp.<server-id>.<tool>`. See [MCP Tools](./mcp).

## Approval choices

```text
Permission request: filesystem_write_file (write_file)
Target: src/main.go

Use Up/Down to choose, Enter to confirm (shortcuts: y=once, a=session, n=reject)
> Allow once    - Approve this request once
  Allow session - Approve similar requests for this session
  Reject        - Reject this request
```

| Choice | Meaning | Best for |
|---|---|---|
| `Allow once` | Approve only this request | One-off writes, a single test command, or step-by-step review |
| `Allow session` | Approve similar requests for the current session | Confirmed safe repeated edits or test runs |
| `Reject` | Block this request | Wrong path, risky command, or uncontrolled scope |

## How to decide

| Scenario | Recommendation |
|---|---|
| Reading and searching files | Usually allow |
| Small code or test edits | Check paths first, then use `Allow once`; use `Allow session` for trusted repeated operations |
| Existing test command | Usually allow |
| Deletes, Git reset, broad rewrites | Request an explanation first, then usually approve only a clearly safe single request with `Allow once` |
| Secrets or local config | `Reject` |

## WebFetch Domain Policy

`webfetch` fetches HTTP/HTTPS pages. The current recommended policy allows `github.com` and `*.github.com` by default. Other external domains trigger an approval prompt.

The tool also has its own safety boundary: it only supports `http` and `https`, blocks localhost, private-network, link-local, and similar targets, and blocks automatic redirects from bypassing validation. Approval decides whether an external domain may be fetched; the tool still rejects clearly unsafe targets.

## Full Access

`Ctrl+F` opens the Full Access risk prompt. When enabled, tool approvals are auto-approved.

::: warning
Use Full Access only when you understand the task risk, trust the workspace, and accept file or command side effects.
:::

## Command risk

| Category | Examples | Handling |
|---|---|---|
| Read-only | `git status`, `git log`, `ls` | Auto-allow |
| Local changes | `git commit`, `go build` | Needs approval |
| Remote interaction | `git push`, `git fetch` | Needs approval |
| Destructive | `git reset --hard`, `rm` | Needs approval |
| Unknown | Compound commands, parse failures | Needs approval |

## File scope

File operations are limited to the current workspace by default.

```text
/cwd
```

## Next steps

- Daily workflow: [Daily Use](./daily-use)
- Slash commands: [Slash Commands](./slash-commands)
- External tools: [MCP Tools](./mcp)
