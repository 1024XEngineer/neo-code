# Session 持久化设计
## 存储策略
NeoCode 在 MVP 阶段使用 JSON 文件持久化 Session，以保持本地优先、易于调试和跨平台可移植。

## 数据模型
- `Session`：完整消息历史以及 `id`、`title`、`updated_at` 等元信息
- `SessionSummary`：用于侧边栏的轻量摘要结构

## 加载策略
- `ListSummaries` 只读取渲染侧边栏所需的基础信息
- `Load` 仅在用户真正进入某个会话时读取完整消息历史
- `Save` 通过临时文件原子写入完整 Session

## 命名策略
- 新会话默认展示为 `Draft`
- 一旦持久化，runtime 会根据首轮用户消息生成简短标题

## 并发约束
- SessionStore 实现必须自行保护共享访问
- 真正的保存时机由 runtime 决定，TUI 不负责直接触发磁盘写入
