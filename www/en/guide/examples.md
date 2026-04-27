---
title: Usage Examples
description: Copyable NeoCode prompts that state goal, scope, and verification clearly.
---

# Usage Examples

Paste these examples into a NeoCode session and replace `{...}` with real paths, logs, or requirements.

## 1. Understand an unfamiliar project

```text
Please read the current project directory structure, explain what each main directory is responsible for, and point out which files I should read first to understand the main flow.
```

## 2. Read project rules first

```text
Please read the visible AGENTS.md and README in the current workspace, then summarize the collaboration rules you should follow.
```

## 3. Plan before editing

```text
I want to add this feature: {feature description}.
Please read the related code first, then propose the smallest implementation plan, files to change, and verification command. Do not edit files yet.
```

Then:

```text
Please implement that plan, keep the existing code style, and add necessary tests.
```

## 4. Find and fix a bug

```text
I see the following failure. Please locate the root cause and propose a fix:

{test log}
```

Then:

```text
Please apply the proposed fix and run the smallest relevant tests. If tests fail, analyze and continue fixing.
```

For deletes, Git resets, or broad rewrites, inspect the requested action first. Use `Allow once` only for a clearly safe single request; use `Reject` if the path, command, or scope is not what you expect.

## 5. Add tests

```text
Please add unit tests for {function name} in {source file}. Cover the happy path, empty input, and error return. Put the tests in {test file}.
```

## 6. Save a long-term preference

```text
/remember I prefer reading tests before editing code
/memo
```

Memory is for stable preferences, not secrets or one-off task instructions.

## 7. Activate a Skill

```text
/skills
/skill use go-review
```

Then:

```text
Please review the current changes. Prioritize behavior regressions, edge cases, and missing tests.
```

## 8. Continue after compaction

```text
/compact
```

Then restate the current goal:

```text
Continue the previous fix. The current goal is to make go test ./internal/config pass with minimal changes.
```

## Next steps

- Permissions: [Tools & Permissions](./tools-permissions)
- Slash commands: [Slash Commands](./slash-commands)
- Configuration: [Configuration](./configuration)
- Daily workflow: [Daily Use](./daily-use)
