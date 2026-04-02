# Tools 与 TUI 集成设计
## 工具契约
每个工具都应提供：
- 合法且稳定的工具名
- 面向模型的简明描述
- 类 JSON Schema 的参数定义
- 接收 `context.Context` 和结构化输入的 `Execute` 方法

## Registry 职责
- 以名字注册工具
- 向 Provider 返回可消费的工具 schema 列表
- 根据工具名分发执行，并把失败规范化为可回灌给模型的 ToolResult

## 当前工具集
- `filesystem_read_file`
- `filesystem_write_file`
- `filesystem_grep`
- `filesystem_glob`
- `filesystem_edit`
- `bash`
- `webfetch`

## TUI 集成方式
- 本地配置操作统一通过 Slash Command 完成，例如 Base URL、API Key 和模型选择
- `/compact` 提供手动上下文压缩入口，触发 runtime 的 manual compact 流程
- TUI 对 compact 仅做轻量可观测展示：显示 `compact_done` / `compact_error` 关键信息，避免事件刷屏
- runtime 事件以内联形式渲染到 transcript 中，而不是单独拆出控制台面板
- 工具开始和结束事件会以轻量提示插入聊天流，使交互更沉浸

## 交互原则
Composer 是唯一的控制入口。只要某个功能本质上是在修改本地 Agent 状态，优先通过 Slash Command 发现和触发，而不是继续叠加额外快捷键。
