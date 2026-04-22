# MCP Configuration

NeoCode supports connecting to MCP (Model Context Protocol) servers via stdio, exposing external tools to the agent.

## Configuration

Add `tools.mcp.servers` to `~/.neocode/config.yaml`:

```yaml
tools:
  mcp:
    servers:
      - id: docs
        enabled: true
        source: stdio
        version: v1
        stdio:
          command: node
          args:
            - ./mcp-server.js
          workdir: ./mcp
          start_timeout_sec: 8
          call_timeout_sec: 20
          restart_backoff_sec: 1
        env:
          - name: MCP_TOKEN
            value_env: MCP_TOKEN
```

## Field reference

| Field | Description |
|-------|-------------|
| `id` | Server identifier. Tools are namespaced as `mcp.<id>.<tool>` |
| `enabled` | Only servers with `true` are registered at startup |
| `source` | Transport type. Currently only `stdio` is supported |
| `stdio.command` | Startup command (required when enabled) |
| `stdio.args` | Startup arguments |
| `stdio.workdir` | Child process working directory (relative paths supported) |
| `stdio.start_timeout_sec` | Startup timeout in seconds |
| `stdio.call_timeout_sec` | Call timeout in seconds |
| `stdio.restart_backoff_sec` | Restart interval in seconds |
| `env` | Environment variables passed to the MCP process. Use `value_env` to reference system variables |

## Startup behavior

- All `enabled: true` servers are registered at startup
- A `tools/list` call is made after registration to snapshot available tools
- If a registered server fails to start, NeoCode exits with an error (fail-fast)

## Verify tool availability

Ask the agent to list its tools:

```
List all the tools you currently have available.
```

Then make a direct call to confirm:

```
Call mcp.docs.search with {"query": "hello"} and return the result.
```

## Troubleshooting `tool not found`

- Check that `enabled` is `true`
- Check that `stdio.command` is executable
- Check that `env.value_env` environment variables are set
- Check that the MCP server supports `tools/list`

## Related

- [Configuration guide](./configuration)
