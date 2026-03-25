# NeoCode TUI 架构说明

## 核心设计

当前 TUI 仍然基于 Bubble Tea 的 TEA 模式，但重构后职责边界已经从旧版的 `app + infra` 结构调整为 `bootstrap + core + state + components + services`：

- 状态驱动：界面输出由 `core.Model` 持有的状态决定。
- 异步更新：模型响应、工具执行、记忆刷新都通过 `tea.Cmd` 回流到 `Update`。
- 依赖收口：TUI 对后端实现的依赖统一集中在 `internal/tui/services/`，`core` 和 `components` 不直接触碰 `internal/server/...`。
- 本地组装：当前 TUI 默认通过本地 `service + provider + repository + tools` 组装聊天能力，而不是通过独立的 gRPC/HTTP 客户端。

---

## 五层结构

### 入口层 - `cmd/tui/`

- 解析命令行参数，目前支持 `--workspace`。
- 负责启动前准备：设置终端 UTF-8、准备工作区、交互式检查 API Key、加载配置与人设。
- 调用 `bootstrap.NewProgram(...)` 构建 Bubble Tea Program 并运行。

### 启动层 - `internal/tui/bootstrap/`

- `setup.go` 负责工作区解析、配置文件初始化、API Key 校验前的交互式引导。
- `runtime.go` 负责创建 `services.ChatClient`，再注入 `core.NewModel(...)`。
- 这一层是依赖装配点，后续如果要切换成远端 API 客户端，也应该优先在这里替换实现。

### 状态机层 - `internal/tui/core/`

- `model.go` 定义顶层 `Model`，聚合 UI 状态、聊天状态、Bubble 组件实例和流式通道。
- `update.go` 负责按键处理、命令解析、消息流更新、工具调用闭环、记忆清理与模型切换。
- `view.go` 负责顶层布局，组合状态栏、聊天区、帮助区、输入区。
- `msg.go` 定义流式输出、工具结果、帮助切换等内部消息。

### 纯状态层 - `internal/tui/state/`

- `ui_state.go` 保存窗口尺寸、当前模式、自动滚动等纯 UI 状态。
- `chat_state.go` 保存消息历史、当前模型、记忆统计、命令历史、工作区和配置状态。
- 状态层只保留结构体定义，不承载业务流程。

### 组件与适配层

#### 视图组件 - `internal/tui/components/`

- 负责状态栏、输入框、帮助面板、消息列表、代码块高亮等纯渲染逻辑。
- 输入是基础数据或轻量结构，输出是渲染后的字符串。
- 不发起请求，不修改全局状态。

#### 服务适配 - `internal/tui/services/`

- `api_client.go` 当前不是网络客户端，而是本地聊天适配器：直接组装 `internal/server/service`、`internal/server/infra/provider`、`internal/server/infra/repository` 和 `internal/server/infra/tools`。
- 同时负责工作区根目录、提供商/模型规范化、工具调用封装、记忆统计等 TUI 依赖的运行时能力。
- `core` 只依赖这里暴露的接口和数据结构，不关心底层是本地实现还是远程实现。

---

## 当前数据流

以“用户输入一条消息并触发工具调用”为例：

1. 用户在输入框编辑内容，`core/update.go` 处理按键并更新 `textarea` 状态。
2. 按下 `F5` 或 `F8` 后，`handleSubmit()` 将用户消息写入 `chat_state`，然后触发 `streamResponse(...)`。
3. `services.ChatClient.Chat(...)` 启动本地聊天服务，流式返回模型输出。
4. 聊天服务会把角色提示、记忆上下文、任务上下文，以及由 `internal/server/infra/tools/*.go` 的 `Definition()` 渲染出的 tool schema 一并注入到 system context。
5. `StreamChunkMsg` 持续追加 assistant 内容；`StreamDoneMsg` 在流结束时检查最后一条 assistant 消息是否是工具调用 JSON。
6. 若检测到 `{"tool":"...","params":{...}}`，TUI 会通过 `services.ExecuteToolCall(...)` 执行工具，并把工具结果重新注入为 system 上下文。
7. 模型基于新的上下文继续生成，直到得到最终自然语言回复。

---

## 当前目录结构

```text
internal/tui/
├── bootstrap/           # 启动准备与依赖装配
│   ├── runtime.go
│   └── setup.go
├── components/          # 纯渲染组件
│   ├── code_block.go
│   ├── help.go
│   ├── input_box.go
│   ├── message_list.go
│   └── statusbar.go
├── core/                # Bubble Tea 状态机
│   ├── model.go
│   ├── msg.go
│   ├── update.go
│   └── view.go
├── services/            # 本地服务适配与运行时能力
│   ├── api_client.go
│   └── runtime_services.go
└── state/               # 纯状态定义
    ├── chat_state.go
    └── ui_state.go
```

---

## 约束建议

1. `core` 不直接引用 `internal/server/...`，所有后端能力统一经 `services` 暴露。
2. `components` 只做渲染，不做状态修改和副作用。
3. `state` 只放结构体，避免把流程控制重新塞回状态层。
4. 与模型、工具、工作区、记忆相关的新能力，优先落在 `services` 或 `bootstrap`，不要堆进 `cmd/tui/main.go`。
5. 如果未来切换到远端 API，优先替换 `services.ChatClient` 实现，尽量不改 `core` 的 Update/View 逻辑。
