# NeoCode 架构说明

本文档只描述当前 `main` 分支已经落地的主链路与模块边界，同时标注少量已经预留、但尚未成为默认执行路径的扩展接口，避免把未来方案误写成当前事实。

## 当前主链路

当前可运行闭环保持为：

`用户输入 -> TUI / CLI -> Runtime -> Provider / Tools -> Runtime -> TUI 展示`

其中：

- `internal/tui` 负责终端交互、状态展示与命令入口。
- `internal/cli` 负责命令行启动参数与应用入口。
- `internal/runtime` 负责 ReAct 循环、事件派发、工具回灌、压缩触发与停止条件。
- `internal/provider` 负责模型生成、事件转换、模型目录发现与驱动差异收敛。
- `internal/tools` 负责工具注册、权限决策、工作区沙箱衔接与统一执行协议。
- `internal/session` 负责会话模型与 JSON 持久化。
- `internal/context` 负责提示词装配、消息裁剪、micro compact 与自动压缩决策。
- `internal/config` 负责配置加载、校验、provider/model 选择与工作目录配置。

## 已预留但未成为默认入口的能力

### Gateway

仓库已经为未来的跨进程客户端、HTTP / WebSocket 网关和远端 UI 预留了协议边界与设计空间，但当前默认主链路仍是 `TUI / CLI -> Runtime`，不会强制经过独立 Gateway。

当前约束如下：

- Gateway 属于后续扩展入口，不是当前默认调用边界。
- 预留字段与协议概念可以保留，但新增语义时必须同步补充校验规则、测试和接入位置说明。
- 如果某个网关字段、动作或端口还未接入主链路，文档必须明确标注“预留”或“扩展”，不能写成当前事实。

### 多模态输入

当前 provider 层已经为文本加图片输入、多模型能力发现与目录缓存保留了协议能力，用于后续附件、会话资产与多协议模型支持；这部分属于已规划的扩展面，不应因为当前调用路径尚少而删除字段。

## 设计原则

- 文档优先描述当前实现，再描述已保留的扩展面。
- 未来能力若尚未接入主链路，必须明确标注“预留”或“扩展”，不能写成当前默认事实。
- 预留接口可以暂未被默认路径使用，但必须说明用途、边界与接入位置。
- 如果某个抽象与当前 `main` 的真实实现冲突，以 `main` 代码为准修正文档与命名。

## 相关文档

- `docs/runtime-provider-event-flow.md`
- `docs/context-compact.md`
- `docs/session-persistence-design.md`
- `docs/tools-and-tui-integration.md`
