# Session 持久化设计

## 模块职责与收口边界
- `internal/session`：承载会话领域模型、存储抽象与 JSON 持久化实现，是唯一的会话持久化实现归属层
- `internal/runtime`：只依赖 `internal/session` 提供的抽象与模型，负责会话保存时机与主循环编排，不再维护会话存储实现细节
- `internal/tui`：仅消费 runtime 暴露的会话数据，不直接执行会话持久化

## 存储策略
NeoCode 在 MVP 阶段使用 JSON 文件持久化 Session，以保持本地优先、易于调试和跨平台可移植。

## 数据模型
- `Session`：完整消息历史以及 `id`、`title`、`updated_at`、`token_input_total`、`token_output_total` 等元信息
- `Summary`：用于侧边栏的轻量摘要结构（原 `SessionSummary` 命名已统一收口为 `Summary`）

### Token 持久化
- `token_input_total` 和 `token_output_total` 分别记录会话累计输入和输出 token。
- 使用 `omitempty` 标签，确保旧版 JSON 文件正常加载（零值不序列化）。
- runtime 在每次 provider 调用后更新 session 的 token 字段，随 session save 一起持久化。
- 会话加载时，runtime 从 session 恢复 token 计数器；新建会话时计数器清零。
- 自动压缩成功后 token 计数器重置为零，并持久化到 session。

## 加载策略
- `ListSummaries` 只读取渲染侧边栏所需的基础信息
- `Load` 仅在用户真正进入某个会话时读取完整消息历史
- `Save` 通过临时文件原子写入完整 Session

## 命名策略
- 新会话默认展示为 `New Session`
- 一旦持久化，runtime 会根据首轮用户消息生成简短标题

## 并发约束
- `internal/session` 中的 Store 实现必须自行保护共享访问
- 真正的保存时机由 runtime 决定，TUI 不负责直接触发磁盘写入

## 兼容性与演进说明
- 会话持久化能力已从 runtime 侧实现中彻底收口到 `internal/session`
- 新增会话存储实现时，应优先在 `internal/session` 内扩展并通过接口注入 runtime，避免跨层实现

## 工作区隔离

- session 现按工作区隔离存储，目录规则为 `~/.neocode/projects/<workspace-hash>/sessions/`
- 工作区哈希基于启动时确定的工作区根目录生成，而不是基于 `session.Workdir`
- `/cwd` 现改为进程级工作区切换：成功时会拉起一个新的 `--workdir` 进程，并进入目标工作区自己的 session 分桶
- `session.Workdir` 仍表示历史会话中记录的执行目录字段，但 `/cwd` 不再通过当前进程内更新该字段来模拟工作区切换
- 旧的全局 `~/.neocode/sessions/` 开发期数据不迁移、不回读
