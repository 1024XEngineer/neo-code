---
title: Shell Proxy & Terminal Diagnosis
description: Launch a proxy shell with neocode shell and trigger automated or on-demand terminal error diagnosis using neocode diag.
---

# Shell Proxy & Terminal Diagnosis

NeoCode provides a terminal proxy and diagnosis feature that allows the Agent to observe your terminal activity in real time and trigger automated or manual diagnosis when errors occur.

The workflow is: start a proxy terminal session with `neocode shell`, work normally inside it, and when an error occurs, use `neocode diag` to trigger a diagnosis or enable auto-diagnosis for the Agent to analyze errors proactively.

## Starting a Proxy Shell

Launch a proxy shell session in your terminal:

```bash
neocode shell
```

Once started, you enter a NeoCode-proxied terminal environment. The session registers itself with the NeoCode Gateway as a shell role, allowing subsequent diagnosis commands to locate the target session.

> **Note**: All diagnosis commands (`neocode diag` and its subcommands) depend on an active `neocode shell` session. Start `neocode shell` in one terminal window first, then run diagnosis commands in another.

## Shell Integration

If your shell supports integration, use `--init` to print the initialization script:

```bash
# Print bash init script
neocode shell --init bash

# Print zsh init script
neocode shell --init zsh
```

You can also append the output to your shell config so it loads automatically:

```bash
neocode shell --init bash >> ~/.bashrc
```

## Triggering a One-Shot Diagnosis

When an error appears in the terminal, run the diagnosis command in another terminal window:

```bash
# Trigger a manual diagnosis (both forms are equivalent)
neocode diag
neocode diag diagnose
```

You can pipe error logs directly into the diagnosis:

```bash
cat error.log | neocode diag --session <session-id>
```

Or pass error content with the `--error-log` flag:

```bash
neocode diag --error-log "command not found: xxx"
```

Diagnosis results are streamed to your terminal.

## Interactive Diagnosis Mode (IDM)

Interactive diagnosis mode provides a sandbox environment for multi-turn conversations with the Agent to troubleshoot issues:

```bash
neocode diag -i
```

In IDM, the Agent can read your terminal context and provide diagnostic suggestions in a conversation. To exit IDM:

- Type `exit`
- Press `Ctrl+C` while idle

## Auto-Diagnosis Toggle

You can control whether the Agent automatically triggers diagnosis when terminal errors occur:

| Command | Effect |
|---------|--------|
| `neocode diag auto on` | Enable auto-diagnosis |
| `neocode diag auto off` | Disable auto-diagnosis |
| `neocode diag auto status` | Show current auto-diagnosis status |

```bash
# Enable auto-diagnosis
neocode diag auto on

# Check status
neocode diag auto status
```

To target a specific shell session, use `--session`:

```bash
neocode diag auto on --session <session-id>
```

## Common Issues

### `neocode diag` reports "no neocode shell session found"

**Symptom**

- Running `neocode diag` or `neocode diag -i` shows a session-not-found error

**Likely causes**

- You have not started `neocode shell` yet
- The proxy shell session has exited
- The `NEOCODE_SHELL_SESSION` environment variable is not visible in the other terminal

**Fix**

1. Start `neocode shell` in one terminal window first.
2. Run the diagnosis command in another terminal window.
3. Make sure the proxy shell session is still running.

### `neocode shell` cannot start on this platform

**Symptom**

- Platform not supported or launch failure

**Likely causes**

- The current platform does not support PTY proxying (`neocode shell` currently supports Unix-like systems only)

**Fix**

1. Check your operating system with `uname -s`.
2. Windows support for `neocode shell` is planned for a future release.

### Diagnosis hangs or times out

**Symptom**

- `neocode diag` produces no output for a long time

**Likely causes**

- Gateway is not running or unreachable
- Network issue causing RPC timeout

**Fix**

1. Verify the `neocode shell` session is still running.
2. Check that the Gateway connection is healthy.
3. Retry the command.

## Next steps

- Daily usage patterns: [Daily Use](./daily-use)
- Other troubleshooting: [Troubleshooting](./troubleshooting)
- Configuration options: [Configuration](./configuration)
