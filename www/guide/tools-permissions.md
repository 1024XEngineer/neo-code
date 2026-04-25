---
title: 工具与权限
description: NeoCode 内置工具列表、权限审批流程、Bash 语义分类与文件系统沙箱规则。
---

# 工具与权限

NeoCode 的所有可被模型调用的能力都收敛在 `internal/tools` 中，通过统一的 schema + execute 协议注册。用户与工具的交互界面主要是权限审批——高风险操作需要你确认后才执行。

## 内置工具一览

当前代码注册了 13 个内置工具：

| 工具名 | 用途 | 需要审批 |
|--------|------|----------|
| `filesystem_read_file` | 读取工作区内文件 | 否（只读） |
| `filesystem_write_file` | 写入文件，自动创建父目录 | 是 |
| `filesystem_grep` | 正则或文本搜索 | 否（只读） |
| `filesystem_glob` | Glob 模式匹配文件路径 | 否（只读） |
| `filesystem_edit` | 精确替换文件中的代码块 | 是 |
| `bash` | 执行 Shell 命令 | 视语义分类而定 |
| `webfetch` | 抓取网页内容 | 否（只读，但有安全限制） |
| `todo_write` | 管理会话 Todo 状态与依赖 | 否 |
| `spawn_subagent` | 启动子代理 | 否 |
| `memo_remember` | 保存持久记忆 | 否 |
| `memo_recall` | 按关键词检索记忆 | 否 |
| `memo_list` | 列出持久记忆索引 | 否 |
| `memo_remove` | 按关键词删除记忆 | 否 |

此外，通过 MCP stdio 配置可注册外部工具，命名空间为 `mcp.<server-id>.<tool>`。MCP 工具同样经过统一权限检查。

## 权限审批流程

当模型请求执行一个需要审批的工具时，TUI 会弹出权限确认界面：

```text
◆ NEO wants to run: filesystem_write_file
  path: src/main.go
  content: (428 bytes)

  [Allow] [Deny] [Ask]
```

三种决策的含义：

- **Allow**：本次允许执行，且记住决策——后续相同操作不再询问
- **Deny**：拒绝本次执行
- **Ask**：每次都询问（默认行为）

权限决策会按会话记忆保存，同一会话内对相同操作的重复请求会自动应用之前的决策。

### Full Access 模式

如果你信任当前工作区的所有操作，可以启用 Full Access 模式：

- 启用后，所有工具审批自动通过，不再弹出确认界面
- 适合在受控环境或自动化场景下使用
- 在 TUI 中通过快捷键 `!` 触发 Full Access 风险提示并确认

::: warning
Full Access 模式会跳过所有权限审批，包括破坏性操作。请确保你了解风险后再启用。
:::

## Bash 语义分类

Bash 工具不是简单地"执行命令"——它会对命令进行语义解析，根据分类走不同的审批策略：

| 分类 | 含义 | 审批策略 | 示例 |
|------|------|----------|------|
| `read_only` | 只读操作 | 自动放行 | `git status`、`git log`、`ls` |
| `local_mutation` | 本地变更 | 需要审批 | `git commit`、`go build` |
| `remote_op` | 远端交互 | 需要审批 | `git push`、`git fetch` |
| `destructive` | 破坏性操作 | 需要审批 | `git reset --hard`、`git checkout .` |
| `unknown` | 无法确认语义 | 需要审批 | 复合命令、解析失败的命令 |

### Git 只读白名单

对于 Git 只读命令，NeoCode 会进一步做环境隔离：

- 只允许白名单内的子命令自动放行：`status`、`log`、`show`、`diff`、`rev-parse`、`describe`、`branch --list`、`remote -v`
- 自动注入 `GIT_CONFIG_NOSYSTEM=1`、`GIT_TERMINAL_PROMPT=0` 等环境变量覆盖，防止 Git 配置触发外部程序
- 如果命令包含 `-c` 配置标志、输出重定向（`--output`）或复合命令（`&&`、`||`），会降级为 `unknown`，需要审批

### 非 Git 命令

非 Git 命令（如 `go test`、`npm install`）默认分类为 `unknown`，需要审批。但你可以通过 Allow 决策让后续相同命令自动通过。

## 文件系统沙箱

所有文件操作工具都受工作区沙箱约束：

- 操作目标必须在 `--workdir` 或 `/cwd` 指定的工作区内
- 拒绝路径穿越（如 `../../../etc/passwd`）
- 拒绝符号链接逃逸（符号链接指向工作区外会被拦截）
- 相对路径会自动基于工作区根解析为绝对路径

## WebFetch 安全限制

WebFetch 工具有独立的安全约束：

- 禁止自动重定向（返回最后一条重定向响应，不跟随）
- 限制响应大小（默认 256 KiB，可通过 `tools.webfetch.max_response_bytes` 配置）
- 限制内容类型（默认只允许 `text/html`、`text/plain`、`application/json`，可通过 `tools.webfetch.supported_content_types` 配置）

## 继续阅读

- 子代理与验证器：看 [子代理与验证器](./subagent-verification)
- 配置工具参数：看 [配置入口](./configuration)
- 工具设计细节：看 [深入阅读](/reference/)
