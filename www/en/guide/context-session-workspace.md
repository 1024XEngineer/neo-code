---
title: Sessions, Context, and Workspace
description: Understand how NeoCode works with projects, tasks, and conversation history.
---

# Sessions, Context, and Workspace

NeoCode daily use revolves around workspace, session, and context.

The workspace decides which project the agent can inspect. A session stores one continuous task. Context is the information the agent can use for the next answer.

## Workspace

Launch NeoCode in a project:

```bash
neocode -w /path/to/project
```

View or switch workspace:

```text
/cwd
/cwd /path/to/project
```

Prefer one workspace per session. When switching projects, start a new session.

## Sessions

A session stores conversation history, tool results, and task progress for one continuous task.

```text
/session
```

Use `Ctrl+N` to create a new session.

## Context

Context includes your latest input, relevant history, project rules, memory, active Skills, task state, and project summaries.

When a session becomes long and old details start affecting answers, run:

```text
/compact
```

After compaction, restate the active goal:

```text
Continue the previous fix. The goal is to make go test ./internal/runtime pass.
```

## Continue or start fresh

| Scenario | Recommendation |
|---|---|
| Same bug or feature | Continue current session |
| Plan first, then implement | Continue current session |
| New unrelated task | Start a new session |
| Different project | Start a new session and switch workspace |
| Old context is distracting | Try `/compact`, then start fresh if needed |

## Next steps

- Local commands: [Slash Commands](./slash-commands)
- Project rules: [AGENTS.md Project Rules](./agents-md)
- Daily workflow: [Daily Use](./daily-use)
