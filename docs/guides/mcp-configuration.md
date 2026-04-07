# MCP 配置指南（stdio）

本文档说明如何在 NeoCode 中通过配置注册 MCP server，并验证 `mcp.<server>.<tool>` 能力是否可用。

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

- `id`：server 稳定标识，用于工具命名空间（`mcp.<id>.<tool>`）。
- `enabled`：是否启用该 server；仅 `true` 的 server 会在启动时注册。
- `source`：传输类型，当前仅支持 `stdio`。
- `version`：可选版本字段，用于可观测和后续策略命中。
- `stdio.command`：启动命令（必填，启用时）。
- `stdio.args`：启动参数列表。
- `stdio.workdir`：子进程工作目录，支持相对路径（相对主 `workdir` 解析）。
- `stdio.start_timeout_sec` / `call_timeout_sec` / `restart_backoff_sec`：可选秒级超时与重试参数。
- `env`：传给 MCP 子进程的环境变量列表。
  - 每项必须配置 `value` 或 `value_env` 其中之一。
  - 推荐使用 `value_env` 引用系统环境变量，避免在 YAML 中写明文敏感信息。

## 启动行为

- 启动阶段会注册所有 `enabled: true` 的 server。
- 注册后会执行一次 `tools/list` 初始化工具快照。
- 若启用的 server 注册失败，启动会报错并中止（fail-fast）。

## 功能测试建议

1. 启动应用后让 Agent 列出工具：
   - `请先列出你当前可用工具的完整名称。`
2. 检查是否存在 `mcp.docs.<tool>`。
3. 发起一次明确调用：
   - `请调用 mcp.docs.search，参数 {\"query\":\"hello\"}，并返回工具结果。`

若返回 `tool not found`，优先检查：
- `enabled` 是否为 `true`；
- `stdio.command` 是否可执行；
- `env.value_env` 对应环境变量是否存在；
- MCP server 是否支持 `tools/list`。
