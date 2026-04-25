# 架构概览

NeoCode 是一个基于 Go 实现的本地 AI 编码助手，主链路为：

**用户输入 → Agent 推理 → 调用工具 → 获取结果 → 继续推理 → UI 展示**

## 核心层级

| 层级 | 职责 |
|------|------|
| TUI（`internal/tui`） | 终端界面，使用 Bubble Tea 构建，负责展示、输入和 Slash 命令 |
| Gateway（`internal/gateway`） | IPC / 网络接入、鉴权、ACL 和流式中继 |
| Runtime（`internal/runtime`） | ReAct 主循环、tool result 回灌、停止条件、事件派发、预算门禁 |
| Provider（`internal/provider`） | 模型服务适配器，将厂商差异收敛在此层（OpenAI / Gemini / Anthropic） |
| Tools（`internal/tools`） | 工具实现与注册，文件操作、Bash 执行、WebFetch、MCP 接入等 |
| Session（`internal/session`） | 会话持久化，SQLite 存储 |
| Config（`internal/config`） | 配置加载、校验与状态管理 |
| Security（`internal/security`） | 权限审批、能力策略与工作区安全 |
| Subagent（`internal/subagent`） | 子代理角色策略、执行约束与输出契约 |
| Context（`internal/context`） | Prompt 构建与上下文裁剪 |
| Repository（`internal/context/repository`） | 仓库级事实发现与检索 |

## 内置工具集

当前代码注册了以下内置工具：

| 工具名 | 职责 |
|--------|------|
| `filesystem_read_file` | 读取工作区内文件 |
| `filesystem_write_file` | 写入文件，自动创建父目录 |
| `filesystem_grep` | 正则或文本搜索 |
| `filesystem_glob` | Glob 模式匹配文件路径 |
| `filesystem_edit` | 精确替换文件中的代码块 |
| `bash` | 执行 Shell 命令，带超时和语义治理 |
| `webfetch` | 抓取网页内容，带内容类型过滤和大小限制 |
| `todo_write` | 管理会话 Todo 状态与依赖 |
| `spawn_subagent` | 启动子代理（researcher / coder / reviewer） |
| `memo_remember` | 保存持久记忆 |
| `memo_recall` | 按关键词检索记忆 |
| `memo_list` | 列出持久记忆索引 |
| `memo_remove` | 按关键词删除记忆 |

此外，通过 MCP stdio 配置可注册外部工具，命名空间为 `mcp.<server-id>.<tool>`。

## 验收验证器

Runtime 在任务完成前可触发验证器检查：

- `file_exists`：文件是否存在
- `content_match`：文件内容是否匹配
- `todo_convergence`：Todo 是否收敛
- `build`：构建是否通过
- `test`：测试是否通过
- `lint`：Lint 是否通过
- `typecheck`：类型检查是否通过
- `command_success`：命令是否成功
- `git_diff`：Git 变更是否符合预期

## 设计原则

- **层间单向依赖**：TUI 只调用 Runtime，Runtime 只调用 Provider 和 Tool Manager
- **厂商差异隔离**：模型协议差异收敛在 `internal/provider`，不泄漏到上层
- **工具能力集中**：所有可被模型调用的能力进入 `internal/tools`，不散落在其他层
- **状态统一管理**：会话状态、消息历史、工具调用记录由 Runtime / Session 统一管理
- **安全边界不可绕过**：Skills 不提供权限豁免，MCP 工具仍经过统一权限检查

## 相关文档

- [配置指南](../guides/configuration)
- [切换模型](../guides/providers)
