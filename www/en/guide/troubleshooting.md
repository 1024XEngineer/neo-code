---
title: Troubleshooting
description: Diagnose NeoCode startup, auth, config, approval, MCP, and long-session issues by symptom.
---

# Troubleshooting

This page is organized by symptom, likely causes, and practical fixes.

## 1) `neocode` command not found

### Symptom

- Terminal says `command not found`
- Windows says the executable is not recognized

### Likely causes

- Installation did not finish
- Install directory is not in `PATH`
- Current terminal has not loaded updated environment variables

### Fix

1. Rerun the installer from [Install & First Run](./install).
2. Close and reopen the terminal.
3. Run `neocode version`.

## 2) API key is set but auth still fails

### Symptom

- Model requests return `unauthorized`, `invalid api key`, or similar auth errors

### Likely causes

- The key was set in a different terminal session
- Active provider does not match the environment variable you set
- The key expired or lacks access to the selected model

### Fix

1. Use `/provider` to confirm the active provider.
2. Check the environment variable table in [Configuration](./configuration).
3. Set the variable in the same terminal that launches NeoCode, then restart NeoCode.

## 3) Provider or model switch does not apply

### Symptom

- You switched provider or model, but responses still look unchanged
- Model list is empty or requests fail after switching

### Likely causes

- Current session still carries old task context
- API key for the new provider is missing
- Selected model is unavailable for your account

### Fix

1. Confirm the current choices with `/provider` and `/model`.
2. Run `/compact` to reduce old context impact.
3. Start a new session and retry the same prompt.

## 4) Too many permission prompts

### Symptom

- File edits or command runs repeatedly ask for approval
- You thought you allowed an action, but NeoCode asks again

### Likely causes

- The current session has not approved similar requests with `Allow session`
- Tool arguments changed, so it is a new operation
- The action is risky enough to require confirmation

### Fix

1. Use `Allow session` for stable, trusted repeated actions.
2. For unknown repositories or risky commands, review each prompt and use `Allow once` only for clearly safe single requests.
3. Use `Reject` when the path, command, or scope is not what you expect.
4. When unsure, ask the agent to explain the command and its impact first.

See [Tools & Permissions](./tools-permissions) for details.

## 5) Long sessions drift or degrade

### Symptom

- Responses repeat
- The agent forgets recent instructions
- New tasks still reference old task context

### Likely causes

- The session is too long
- History contains too much noise
- The new task is unrelated to the previous task

### Fix

1. Run `/compact`.
2. If it still drifts, start a new session.
3. For unrelated work, start a new session and restate the goal.

## 6) MCP tool is unavailable

### Symptom

- The agent cannot see your MCP tool
- The tool appears but calls fail

### Likely causes

- MCP server is not enabled
- Startup command, working directory, or environment variable is wrong
- Tool arguments do not match the MCP server requirements

### Fix

1. Confirm `enabled: true`.
2. Confirm required environment variables are set in the same terminal.
3. Follow the verification steps in [MCP Tools](./mcp): list tools first, then try one simple call.

## 7) External integration cannot connect

External integrations usually depend on a local service being running, listening on the expected address, and using the expected auth setup.

Normal chat usage does not require this. If you are building an external integration, start from [Gateway Reference](/reference/gateway).

## Still blocked?

- Return to [Install & First Run](./install) and retry the minimal path
- Check [Configuration](./configuration) against your active provider
- Read [Tools & Permissions](./tools-permissions) for approval behavior
