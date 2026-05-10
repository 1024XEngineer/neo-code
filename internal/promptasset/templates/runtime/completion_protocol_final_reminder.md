[Runtime Control]

You again stopped calling tools without outputting `task_completion`.

This is the final protocol reminder. Missing it again will terminate this run.

Completion retry rule:
Your previous prose may already be visible to the user. Do not duplicate, restate, expand, or re-list prior summaries.

If the task is done:
- Emit the required `task_completion` JSON exactly once.
- After the JSON, write at most one brief final sentence.
- Do not repeat file lists, completed steps, tool results, or previous summaries.

If the task is not done:
- Continue with the next necessary tool call.
- Do not write another prose summary until the work is actually complete.
