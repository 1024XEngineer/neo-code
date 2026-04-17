# Session Todo 设计说明

本文档补充说明 `internal/session` 中 Todo 的数据模型、持久化语义和边界约束。

## 设计目标

- Todo 归属于 `Session`，不单独引入新的持久化子系统
- Todo 只表示结构化待办状态，不替代 `TaskState`
- Todo 的校验、规范化和基础增删改查统一收敛在 `internal/session`

## 数据模型

`Session` 包含 `Todos []TodoItem` 字段。

单个 `TodoItem` 当前包含：

- `id`
- `content`
- `status`
- `dependencies`
- `priority`
- `owner_type`
- `owner_id`
- `artifacts`
- `failure_reason`
- `revision`
- `created_at`
- `updated_at`

其中 `status` 当前固定为：

- `pending`
- `in_progress`
- `completed`
- `failed`

## 持久化语义

- Todo 跟随会话头一起保存在 SQLite `sessions.todos_json`
- runtime 修改 Todo 时只调用 `UpdateSessionState`，不会写入 `messages` 表
- `LoadSession` 时会把 `todos_json` 还原为完整 `[]TodoItem`

## 规范化与校验

写入前会统一执行 Todo 校验和规范化，包括：

- `id`、`content` 去空白
- 空状态收敛为 `pending`
- `dependencies` 去空白、去重并保持顺序
- 拒绝重复 ID
- 拒绝自依赖
- 拒绝引用不存在的依赖项
- 使用 `revision` 保障更新时的乐观并发校验

## 与 TaskState 的关系

- `TaskState` 仍是 runtime/context 用于 compact 和续航的 durable summary
- `Todo` 是更细粒度的结构化执行状态
- `Todo` 不直接拼入模型消息历史
- 如需让 `TaskState` 汇总 Todo，应在 runtime/context 层显式投影，而不是复用同一个字段
