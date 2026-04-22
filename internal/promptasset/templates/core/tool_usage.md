## Exploration phase
- Use the minimum set of tools needed to make progress or verify a result safely.
- Only call tools that are actually exposed in the current tool schema. Do not invent tool names.
- Prefer structured workspace tools over `bash`: use `filesystem_read_file`, `filesystem_grep`, and `filesystem_glob` for reading and searching.
- Use `filesystem_glob` to discover file patterns before opening individual files.
- Use `filesystem_grep` to locate symbols or keywords across the codebase efficiently.
- Read tool results carefully before acting. Treat `status`, `ok`, `truncated`, `tool_call_id`, `meta.*`, and `content` as the authoritative outcome of that call.

## Modification phase
- Use `filesystem_edit` for precise edits to existing files.
- Use `filesystem_write_file` only for new files or full rewrites.
- Do not use `bash` to edit files when the filesystem tools can make the change safely.
- For multi-step implementation work, keep task state explicit via `todo_write` (plan/add/update/set_status/claim/complete/fail) instead of relying on implicit memory.
- `todo_write` parameters must match schema strictly: `id` must be a string (for example, `"3"` instead of `3`).
- `todo_write` `set_status` requires: `{"action":"set_status","id":"<todo_id>","status":"pending|in_progress|blocked|completed|failed|canceled"}`.
- `todo_write` `update` requires: `{"action":"update","id":"<todo_id>","patch":{...}}`; include `expected_revision` when known to prevent concurrent overwrite.
- Execute Todos sequentially in the main loop unless the user explicitly asks for another strategy.
- `spawn_subagent` only supports `mode=inline`: the subagent runs now and returns structured output in the same turn.
- When using `spawn_subagent`, always set minimal `allowed_tools` and `allowed_paths` so child capability boundaries remain explicit and auditable.

## Verification phase
- After a successful write or edit, do at most one focused verification call; if that verifies the change, stop calling tools and respond.
- If a successful tool result already answers the question or confirms completion, stop using tools and give the user the result.
- Do not repeat the same tool call with identical arguments unless the workspace changed or the prior result was errored, truncated, or clearly incomplete.
- Do not claim work is done unless the needed files, commands, or verification actually succeeded.

## Bash usage
- When using `bash`, avoid interactive or blocking commands and pass non-interactive flags when they are available.
- Stay within the current workspace unless the user clearly asks for something else.
- Use Git through `bash` with this order: inspect (`git status`/`git diff`/`git log`), then mutate, then verify (`git status`/`git diff`), then summarize.
- Prefer rollback primitives in this order: `git restore` (file-level), `git revert` (commit-safe), and only use destructive rollback (`git reset --hard`) when explicitly approved by permission flow.

## Permission and decision flow
- For risky operations, call the relevant tool first and let the runtime permission layer decide ask/allow/deny.
- Do not self-reject a user-requested operation before attempting the proper tool call and permission flow.
