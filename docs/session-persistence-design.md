# Session 持久化设计

## 模块职责与边界

- `internal/session` 是会话领域模型、SQLite 存储实现和资产持久化的唯一归属层。
- `internal/runtime` 只决定何时创建会话、追加消息、更新会话头和替换 transcript，不关心底层表结构。
- `internal/tui` 只消费 runtime 暴露的会话数据，不直接读取数据库或资产文件。

## 存储策略

NeoCode 当前使用工作区级 SQLite 数据库持久化会话，不再使用 `session.json` 文件。

- 数据库路径：`~/.neocode/projects/<workspace-hash>/session.db`
- 资产目录：`~/.neocode/projects/<workspace-hash>/assets/<session-id>/<asset-id>.bin`
- 工作区哈希基于启动时确定的工作区根目录生成
- `session.Workdir` 记录该会话最近一次运行实际使用的目录，但不参与分桶
- 开发阶段遗留的旧 `sessions/` JSON 目录不迁移、不回读、不兼容

SQLite 初始化固定使用以下 PRAGMA：

- `journal_mode = WAL`
- `synchronous = NORMAL`
- `foreign_keys = ON`
- `busy_timeout = 5000`
- `user_version = 1`

## 数据模型

### sessions

会话头保存摘要和 durable 状态：

- `id`
- `title`
- `created_at_ms`
- `updated_at_ms`
- `provider`
- `model`
- `workdir`
- `task_state_json`
- `todos_json`
- `activated_skills_json`
- `token_input_total`
- `token_output_total`
- `last_seq`
- `message_count`

### messages

消息正文按行存储，一条消息对应一行：

- `session_id`
- `seq`
- `role`
- `parts_json`
- `tool_calls_json`
- `tool_call_id`
- `is_error`
- `tool_metadata_json`
- `created_at_ms`

### session_assets

资产元数据入库，二进制内容落盘：

- `id`
- `session_id`
- `mime_type`
- `size_bytes`
- `relative_path`
- `created_at_ms`

## 运行时读写语义

### 创建会话

runtime 在新会话开始时调用 `CreateSession`，只写入一条空会话头，不写消息正文。

### 追加消息

runtime 在以下时机调用 `AppendMessages`：

- 用户消息提交后
- assistant 完整回复后
- 每个 tool result 完成后

一次调用会在同一事务内完成两件事：

- 追加 1..N 条消息
- 更新会话头上的 `updated_at`、`provider`、`model`、`workdir`、token 增量和消息计数

因此常规写入不再与历史消息总量线性耦合。

### 更新会话头

runtime 在以下场景调用 `UpdateSessionState`：

- workdir 变更
- task_state 变更
- todo 列表变更
- skill 激活状态变更
- assistant 本轮没有正文，但 provider/model 或 token 统计发生变化

该操作不写消息，只覆盖会话头字段。

### 替换 transcript

compact 成功后，runtime 调用 `ReplaceTranscript`，在单事务内：

- 删除该会话原有全部消息
- 按新顺序写回 compact 后的消息
- 同步更新 `task_state`、token 统计、provider/model/workdir 和消息计数

这是低频路径，允许重写整段 transcript。

### 加载会话

- `ListSummaries` 只查询 `sessions` 表，并按 `updated_at` 倒序返回摘要
- `LoadSession` 先读取会话头，再按 `seq` 顺序加载消息并组装完整 `Session`

## Token 持久化

- runtime 在 assistant 调用完成后累计输入和输出 token
- `AppendMessages` 可以原子地追加消息并累加 token
- `UpdateSessionState` 和 `ReplaceTranscript` 可以直接覆盖 token 总量
- compact 成功后，runtime 会将 token 总量重置为 0 并持久化

## TaskState 与 Todo

- `TaskState` 是 compact 与多轮续航依赖的 durable summary
- `Todo` 是结构化任务状态，独立持久化在 `sessions.todos_json`
- 二者都属于会话头，不写入 `messages` 表
- context 构建时优先读取 `TaskState`、`Todo`、最近消息和必要工具结果

## 并发约束

- SQLite 负责单工作区数据库的一致性和事务边界
- runtime 继续通过会话锁串行化同一 session 的关键写入路径
- 不同 session 可以并行运行

## 演进约束

- 新增持久化行为时，优先扩展 `internal/session.Store` 的意图型接口
- 不要把 SQL、事务或表结构细节泄漏到 `runtime`、`tui` 或其他上层模块
- 如需进一步优化读路径，应继续在 `internal/session` 内演进，而不是重新引入文件级快照保存
