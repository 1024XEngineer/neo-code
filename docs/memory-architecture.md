# Memory Architecture

This document describes the target memory design for NeoCode as an AI coding assistant.

## Goals

- Preserve durable project rules without forcing the model to infer them from chat history.
- Preserve useful long-term coding memory such as preferences, code facts, and fix recipes.
- Preserve short-term working state so the assistant can resume where it left off.
- Keep every layer inspectable and replaceable.

## Layers

### 1. Explicit Project Memory

Loaded from workspace files such as:

- `AGENTS.md`
- `CLAUDE.md`
- `.neocode/memory.md`
- `NEOCODE.md`

These files are treated as authoritative project instructions and are injected before inferred memory.

### 2. Structured Auto Memory

Stored as structured memory items:

- `user_preference`
- `project_rule`
- `code_fact`
- `fix_recipe`
- `session_memory`

Extraction can use:

- `rule`
- `llm`
- `auto`

### 3. Working Session Memory

Persisted per workspace and used to resume active work:

- current task
- last completed action
- current in progress
- next step
- recent files
- recent turns

## Injection Priority

Prompt assembly order should be:

1. role prompt
2. explicit project memory
3. working memory
4. todo context
5. recalled structured memory

## Validation

- Explicit project memory files should load only from the active workspace.
- Missing project memory files should not cause failures.
- Working memory should continue to resume per workspace.
- `memory.extractor: auto` should fall back to rule extraction if LLM extraction fails.
- `go test ./...` must pass.
