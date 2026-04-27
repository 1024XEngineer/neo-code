---
title: Skills
description: Use SKILL.md files to codify workflow guidance for the current NeoCode session.
---

# Skills

Skills are reusable workflow instructions. They do not add tools or bypass approvals; they tell the agent how to approach a class of tasks.

## When to use Skills

| Goal | Recommendation |
|---|---|
| Make code reviews follow a checklist | Use a Skill |
| Make the agent read specific docs before editing | Use a Skill |
| Require a specific output format for this task | Use a Skill |
| Add a real callable external tool | Use [MCP](./mcp) |
| Save long-term preferences or project facts | Use `/remember` memory |

## Where Skills live

Skills are loaded in this order (project overrides global):

```text
<workspace>/.neocode/skills/
~/.neocode/skills/
```

If `~/.neocode/skills/` does not exist, NeoCode falls back to `~/.codex/skills/`.

Recommended layout:

```text
<workspace>/.neocode/skills/go-review/SKILL.md
~/.neocode/skills/go-review/SKILL.md
```

If the same `id` exists in both project and global roots, the project version is used.

## Create a Skill

Example:

```md
---
id: go-review
name: Go Review
description: Review Go changes for correctness, boundaries, and tests.
---

# Go Review

## Instruction

Read the related implementation and tests before reviewing. Focus on behavior regressions, error handling, edge cases, and test gaps. Output risks first, then a short summary.
```

Common fields:

| Field | Description |
|---|---|
| `id` | Skill identifier |
| `name` | Display name |
| `description` | Short description shown in lists |

The most important part is `Instruction`: state what to do first, what to focus on, and what output you expect.

## Activate and deactivate

Use these commands in NeoCode:

```text
/skills                  # View available Skills
/skill use go-review     # Activate a Skill
/skill off go-review     # Deactivate a Skill
/skill active            # View active Skills
```

`/skill use <id>` affects only the current session. Use memory for long-term preferences that should apply every time.

In `/skills` output, the `source` field shows the source layer (for example `project/local` or `global/local`) so overrides are visible.

## Write a useful Instruction

Avoid:

```md
## Instruction

Please review more carefully.
```

Prefer:

```md
## Instruction

Read the related implementation and tests first. Output high-risk issues first, then test gaps, then a short summary. Do not request unrelated refactors.
```

## Skills vs memory vs MCP

| Capability | Solves | Adds tools |
|---|---|---|
| Skills | Current task workflow and output constraints | No |
| Memory | Long-term preferences and project facts | No |
| MCP | External callable tools | Yes |

## Common issues

### `/skills` does not show my Skill

Check:

- The file is under `~/.neocode/skills/`
- The filename is `SKILL.md`
- Frontmatter is valid YAML
- `id` is not duplicated

### Skill is active but has little effect

Make `Instruction` more specific. Define the reading order, focus areas, and output structure.

### Can a Skill authorize tools?

No. File writes, command execution, and other sensitive actions still use the normal approval flow.

## Next steps

- Connect external tools: [MCP Tools](./mcp)
- Save long-term preferences: [Daily Use](./daily-use)
- Understand permission boundaries: [Tools & Permissions](./tools-permissions)
