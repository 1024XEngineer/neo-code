# MCP 配置指南

NeoCode 支持通过 stdio 接入 MCP（Model Context Protocol）server，将外部工具暴露给 Agent 调用。

## 配置位置

在 `~/.neocode/config.yaml` 中添加 `tools.mcp.servers`：

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

## 字段说明

| 字段 | 说明 |
|------|------|
| `id` | server 标识，工具命名空间为 `mcp.<id>.<tool>` |
| `enabled` | 是否启用，只有 `true` 的 server 会在启动时注册 |
| `source` | 传输类型，当前仅支持 `stdio` |
| `stdio.command` | 启动命令（启用时必填） |
| `stdio.args` | 启动参数列表 |
| `stdio.workdir` | 子进程工作目录，支持相对路径 |
| `stdio.start_timeout_sec` | 启动超时（秒） |
| `stdio.call_timeout_sec` | 调用超时（秒） |
| `stdio.restart_backoff_sec` | 重启间隔（秒） |
| `env` | 传给 MCP 子进程的环境变量，推荐用 `value_env` 引用系统变量 |

## 启动行为

- 启动阶段注册所有 `enabled: true` 的 server
- 注册后执行一次 `tools/list` 初始化工具快照
- 若注册失败，启动会报错并中止（fail-fast）

## 验证工具是否可用

启动后让 Agent 列出可用工具：

```
请先列出你当前可用工具的完整名称。
```

确认 `mcp.docs.<tool>` 存在后，发起一次明确调用：

```
请调用 mcp.docs.search，参数 {"query":"hello"}，并返回工具结果。
```

## 排查 `tool not found`

- 检查 `enabled` 是否为 `true`
- 检查 `stdio.command` 是否可执行
- 检查 `env.value_env` 对应的环境变量是否已设置
- 检查 MCP server 是否支持 `tools/list`

## 相关文档

- [配置指南](./configuration)
