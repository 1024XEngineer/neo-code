---
title: Daily Use
description: Daily NeoCode workflow from opening a project to finishing a task.
---

# Daily Use

A normal NeoCode workflow is: open a project, describe the goal, watch agent activity, approve risky actions, review the result, then continue, compact, or start a new session.

## Open a project

Start in a workspace:

```bash
neocode --workdir /path/to/project
```

View or switch workspace inside NeoCode:

```text
/cwd
/cwd /path/to/project
```

The workspace controls which project NeoCode can read, search, edit, and run commands in. When switching projects, start a new session to avoid mixing context.

## Describe the task

Use natural language for tasks. For complex work, ask NeoCode to inspect the code and propose a plan before editing.

```text
Please read the configuration loading code and propose the smallest fix. Do not edit files yet.
```

Then continue:

```text
Please implement that plan and run the related tests.
```

Good prompts include the goal, scope, and verification command.

## Useful keys

| Key | Action |
|---|---|
| `Enter` | Send input |
| `Ctrl+J` | New line |
| `Ctrl+W` | Cancel current agent task |
| `Ctrl+Q` | Open slash command help |
| `Ctrl+N` | New session |
| `Ctrl+O` | Open workspace selector |
| `Ctrl+F` | Full Access prompt |
| `Ctrl+L` | Log viewer |
| `Tab` / `Shift+Tab` | Move focus between panels |

## Approvals

NeoCode asks before file writes, non-default external web domains, and risky commands.

| Choice | Best for |
|---|---|
| `Allow once` | Approve only this request, useful for a single edit or step-by-step review |
| `Allow session` | Approve similar requests in the current session, useful for trusted repeated operations |
| `Reject` | Block this request when the path, command, or scope is wrong |

The prompt supports shortcuts: `y=once`, `a=session`, `n=reject`.

See [Tools & Permissions](./tools-permissions) for details.

## Continue, compact, or start fresh

| Scenario | Recommendation |
|---|---|
| Same bug, feature, or docs task | Continue current session |
| Long conversation starts drifting | Run `/compact` |
| New unrelated task | Use `Ctrl+N` |
| Different project | New session and switch workspace |

After compacting, restate the current goal briefly.

## Common slash commands

| Command | Purpose |
|---|---|
| `/help` | Show slash commands |
| `/cwd [path]` | View or switch workspace |
| `/session` | Switch session |
| `/compact` | Compact current session context |
| `/provider` | Switch provider |
| `/provider add` | Add a custom provider |
| `/model` | Switch model |
| `/memo` | View memory |
| `/remember <text>` | Save a long-term preference or fact |
| `/forget <keyword>` | Delete matching memory |
| `/skills` | View Skills |
| `/skill use <id>` | Activate a Skill |
| `/skill off <id>` | Deactivate a Skill |
| `/skill active` | Show active Skills |
| `/clear` | Clear current draft |
| `/exit` | Exit NeoCode |

Full details: [Slash Commands](./slash-commands).

## Next steps

- Sessions and context: [Sessions, Context, and Workspace](./context-session-workspace)
- Project rules: [AGENTS.md Project Rules](./agents-md)
- Capability choices: [Capability Guide](./capability-choice)
- Copyable prompts: [Usage Examples](./examples)
