# NeoCode TUI v2 — 代码实现与工程推进

> 版本：v3.0 | 日期：2026-05-18 | 父文档：[架构导航](./tui-v2-architecture-hub.md)

---

## What — 本指南解决什么问题

本指南告诉开发者 **如何在 `internal/tui-v2/` 下实际编写 TUI v2 代码**。具体来说：

- **当前代码状况如何？** 对 TUI v1 的代码量、模块职责和架构合规状态做诚实评估，作为 v2 的起点参考。
- **v2 的目录结构是什么？** 每个包和文件的职责、每个组件的边界。
- **组件如何拆分？** 从 v1 的 7603 行单体 update.go 到 v2 的独立 `tea.Model` 组件的拆分原则和约束。
- **按什么顺序实施？** 四个 Phase 的优先级、依赖关系和交付物。

**边界**：本指南关注代码组织和工程推进，不涉及视觉设计（见 [UI/UX 规范](./tui-v2-ui-ux-design.md)）和数据契约（见 [数据/契约](./tui-v2-data-and-gateway.md)）。

## Why — 为什么采用这种工程方案

### 为什么每个组件必须是独立的 tea.Model

v1 的核心问题是 `App` struct 承载了几乎所有状态和渲染逻辑——`update.go` 7603 行、`view.go` 985 行、`app.go` 458 行。这导致：

1. **修改风险高**：改一处可能影响十处。因为所有状态字段都在一个 struct 上，无法编译器验证"这个组件只用这 3 个字段"。
2. **测试困难**：无法单独测试 Stream 渲染逻辑、Prompt 状态切换、Inspector 数据更新——必须启动整个 App。
3. **代码理解成本高**：新人要理解 Stream 的渲染逻辑需要通读 7603 行 Update，找到所有 `case StreamEntry` 相关分支。

Bubble Tea 的 `tea.Model` 接口天然支持组件化：每个组件有独立的 `Init()`、`Update()`、`View()`，App 只负责路由消息和垂直拼接。

### 为什么单文件要控制在 800 行以内

这不是教条。7603 行的 `update.go` 是一个实证：当一个文件超过 2000 行时，开发者倾向于"在这个文件末尾继续追加"，而非思考"这个逻辑属于哪个组件"。800 行是一个心理阈值——超过它，人会开始失去对文件全貌的把握。

### 为什么复用 v1 的 services 层

`internal/tui/services/` 中的 `Runtime` 接口和 `RemoteRuntimeAdapter` 已经是干净的抽象——它们封装了 Gateway JSON-RPC 通信细节，提供了 TUI 可以消费的 Go 接口。重写它们不会带来架构收益，只会增加维护负担。v2 的差异化价值在 UI 层，不在通信层。

---

## How — 具体实施指南

### 1. 当前代码现状（TUI v1 参考）

#### 1.1 模块职责

| 模块 | 代码位置 | 当前职责 | TUI v2 对应 |
|------|---------|---------|------------|
| **TUI v1** | `internal/tui/` | Bubble Tea 交互界面 | 保留不动 |
| **TUI v2** | `internal/tui-v2/` | 新实现目标 | 待创建 |
| **TUI v1 Core App** | `internal/tui/core/app/` | App Model、Update (7603行)、View (985行)、Styles、KeyMap | v2 拆分为独立组件 |
| **TUI Services** | `internal/tui/services/` | Runtime 接口、RemoteRuntimeAdapter、事件流解码 | v2 可复用或独立实现 |
| **Gateway** | `internal/gateway/` | JSON-RPC、RPC 分发、事件中继、传输、ACL | v1/v2 共用 |
| **Gateway Protocol** | `internal/gateway/protocol/` | RPC 方法定义、参数结构体、错误码 | v1/v2 共用 |
| **Gateway Client** | `internal/gateway/client/` | GatewayRPCClient：连接管理、自动重连 | v1/v2 共用 |
| **Runtime** | `internal/runtime/` | ReAct 循环编排、事件发射、权限审批 | TUI 不直接访问 |
| **Session** | `internal/session/` | 会话领域模型、SQLite 持久化 | TUI 通过 Gateway 获取 |
| **Provider** | `internal/provider/` | 模型厂商协议适配 | TUI 通过 Gateway 获取 |
| **Tools** | `internal/tools/` | 工具契约、schema、执行、安全沙箱 | TUI 展示工具调用状态 |
| **Config** | `internal/config/` | 配置加载、校验 | TUI 通过 ConfigManager 读取 |

#### 1.2 TUI v1 代码量参考

| 文件 | 行数 | TUI v2 改进方向 |
|------|------|---------------|
| `update.go` | 7603 | 拆分为按消息类型组织的多个文件，每个 < 800 行 |
| `view.go` | 985 | 拆分为每个组件的独立 View 文件 |
| `app.go` | 458 | 精简为 App 装配 + 子组件组合，< 200 行 |
| `keymap.go` | 105 | v2 全新设计，包含 Input/Normal/Leader 三层键位 |

---

### 2. `internal/tui-v2/` 完整目录结构

```text
internal/tui-v2/
  tui.go                         # 包入口：NewProgram, 导出类型

  app/
    app.go                       # App Model 装配（< 200 行）
    update.go                    # 顶层消息路由
    view.go                      # 顶层 View() 拼接三层布局
    init.go                      # Init()：启动子组件 + Gateway 连接

  gateway/
    client.go                    # GatewayRPCClient 封装（复用 internal/tui/services/ 模式）
    stream.go                    # Gateway 事件流消费 → ViewModel 转换
    adapter.go                   # RemoteRuntimeAdapter（实现 Runtime 接口）
    contract.go                  # Runtime 接口定义（不依赖 internal/runtime）

  state/
    state.go                     # ViewState 顶层结构体
    stream.go                    # StreamEntry 类型定义
    runtime.go                   # RuntimeState、RuntimeStatus
    input.go                     # InputState、InputMode
    layout.go                    # LayoutState
    gateway.go                   # GatewayState
    theme.go                     # ThemeColors
    messages.go                  # Bubble Tea 消息类型
    constants.go                 # 常量定义

  theme/
    tokyo-night.go               # Tokyo Night 默认主题
    fallback.go                  # 16 色 / 256 色 fallback
    detect.go                    # 终端色彩能力检测

  keymap/
    input.go                     # Input Mode 键位
    normal.go                    # Normal Mode 键位
    leader.go                    # Leader Key 键位
    keys.go                      # 共享 key.Binding 定义

  mouse/
    mouse.go                     # 鼠标事件处理器
    zones.go                      # 鼠标区域检测（Stream/Prompt/Palette/Modal）

  components/
    ambient/
      status.go                  # Ambient Status 行组件
    stream/
      stream.go                  # Agent Stream 主体（tea.Model）
      entry.go                   # 单条 StreamEntry 渲染
      entry_you.go               #   you 消息渲染
      entry_neo.go               #   neo 消息渲染（含 Markdown）
      entry_tool.go              #   tool.* 调用渲染
      entry_file.go              #   file.* 事件渲染
      entry_event.go             #   语义事件渲染
      entry_error.go             #   error 条目渲染
      entry_thinking.go          #   thinking 折叠块渲染
      mission.go                 #   Mission Block（Todo 驱动的任务进度）
    inspector/
      inspector.go               # Soft Inspector 右侧弱信息列
    prompt/
      prompt.go                  # Command Prompt 组件
      prompt_normal.go           #   Normal Mode 指示行
      prompt_permission.go       #   权限等待模式
      prompt_question.go         #   ask_user 等待模式
      prompt_command.go          #   Slash 命令模式
    palette/
      palette.go                 # Telescope 风格命令面板
      session_picker.go          #   会话选择器
    modal/
      modal.go                   # 通用 Modal 容器
      help.go                    # 快捷键帮助（分组展示）
      confirm.go                 # 危险操作确认
      permission.go              # 权限确认弹窗

  services/                      # 可选：如需独立于 v1 的 Gateway 通信层
    # 或直接复用 internal/tui/services/ 中的 Runtime 接口和适配器

  infra/
    markdown.go                  # Markdown 渲染（Glamour）
    clipboard.go                 # 剪贴板
    image.go                     # 图片粘贴（可选）
```

---

### 3. 组件拆分原则

1. **每个组件是独立的 `tea.Model`**：有自己的 `Init()`、`Update()`、`View()`
2. **组件只消费 ViewModel**：不直接持有 `GatewayRPCClient` 引用
3. **App.Update() 负责路由**：将消息路由到对应子组件
4. **App.View() 负责拼接**：纵向排列 Ambient Status + Stream + Command Prompt，通过 `lipgloss.JoinVertical` 拼接
5. **Gateway DTO 与 ViewModel 分离**：`state/` 中的 ViewModel 类型不 import Gateway 包
6. **单文件大小控制**：每个文件 < 800 行（对比 v1 的 7603 行 update.go）
7. **鼠标事件在 `mouse/` 包统一处理**：各组件声明鼠标敏感区域，由 App 统一分发

---

### 4. App struct 与消息流

#### 4.1 App struct

```go
type App struct {
    state ViewState

    ambient    ambient.Model
    stream     stream.Model
    inspector  inspector.Model
    prompt     prompt.Model
    palette    palette.Model

    gateway    gateway.Client
    theme      theme.Colors
    keymap     keymap.Set

    width, height int
}
```

`Init()` 返回 `tea.Batch(stream.Init(), prompt.Init(), gateway.Init())`。

#### 4.2 Update() 消息路由

```go
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        a.width, a.height = msg.Width, msg.Height
    case tea.MouseMsg:
        return a.mouse.Dispatch(msg, a.currentZone())
    case tea.KeyMsg:
        return a.handleKey(msg)
    case RuntimeEvent:
        a.applyRuntimeEvent(msg)
        // 广播到子组件
    }
    // ...
}
```

#### 4.3 View() 拼接

```go
func (a *App) View() string {
    if a.state.Input.ShowPalette {
        return a.palette.View()
    }
    if a.state.Input.ShowHelp {
        return a.help.View()
    }
    return lipgloss.JoinVertical(lipgloss.Left,
        a.ambient.View(),
        a.stream.View(),
        a.prompt.View(),
    )
}
```

#### 4.4 消息流

```text
Gateway Event Stream
  → gateway/stream.go: 解码 JSON-RPC notification
  → state/state.go: 更新 ViewState
  → app/update.go: 路由到对应子组件
  → components/*/update.go: 各组件自行处理
  → components/*/view.go: 重新渲染
```

---

### 5. Gateway 通信层复用策略

TUI v2 的 Gateway 通信层有两个选择：

**策略 A（推荐）：复用 `internal/tui/services/` 中的 `Runtime` 接口和 `RemoteRuntimeAdapter`**

- 优点：零重复代码，v1/v2 共享同一套 Gateway 通信层
- 缺点：v2 依赖 v1 的 services 包
- 适用：如果 `Runtime` 接口已经足够通用

**策略 B：`internal/tui-v2/gateway/` 独立实现**

- 优点：v2 完全独立，不受 v1 演进影响
- 缺点：Gateway 通信逻辑重复
- 适用：如果 v2 的通信需求与 v1 差异大

初始实现建议使用策略 A，以减少重复代码。如果后续 v1 和 v2 的通信需求出现分歧，再独立实现。

---

### 6. 实施路线图

#### Phase 1：基础架构（P0）

1. 创建 `internal/tui-v2/` 目录结构
2. 实现 `app/app.go`、`app/update.go`、`app/view.go` — 三层布局框架
3. 实现 `state/` — ViewModel 类型定义
4. 实现 `theme/` — Tokyo Night 色彩系统 + fallback
5. 实现 `keymap/` — Input/Normal/Leader 三层键位
6. 实现 `gateway/` — Gateway 通信层（复用 `internal/tui/services/` 模式）
7. CLI `--tui=v2` flag 集成
8. 确认所有现有功能通过 Gateway 可用

**交付物**：可启动的三层布局框架（Ambient Status + 空 Stream + Command Prompt），能响应键盘切换 Input/Normal Mode。

#### Phase 2：视觉核心（P0–P1）

1. 实现 `components/ambient/` — 顶部状态行
2. 实现 `components/stream/` — Agent Stream + 所有 Entry 类型渲染
3. 实现 `components/prompt/` — Command Prompt + Normal Mode 指示行
4. 实现 `components/inspector/` — Soft Inspector
5. 启用 `runtime.snapshot.get` 和 `session.todos.list`

**交付物**：完整的 Ghost Console 交互——可发送消息、接收 Agent 回复、查看工具调用、看到状态变化。

#### Phase 3：交互增强（P1）

1. 实现 `components/palette/` — Telescope 风格命令面板
2. 实现 `components/modal/help.go` — 分组快捷键帮助
3. 实现 `components/modal/permission.go` — 权限确认弹窗
4. 实现 `components/modal/confirm.go` — 危险操作确认
5. 实现 `mouse/` — 鼠标事件分发系统

**交付物**：功能完整的 TUI v2——命令面板、帮助浮层、权限弹窗、鼠标操作全部可用。

#### Phase 4：打磨（P1–P2）

1. 新增 Gateway 接口对接（`gateway.health`、`gateway.listToolCalls` 等）
2. 动画与过渡效果
3. 鼠标操作完善（拖拽滚动、右键菜单）
4. 性能优化（大量 Stream Entry 虚拟滚动）
5. 主题预留（后续可扩展其他配色方案）

**交付物**：生产就绪的 TUI v2。

---

### 后续 Phase 进展（tui-v2-issues 规划）

早期 Phase 1–4 之外，TUI v2 按 `~/meno/tui-v2-issues/` 下的分阶段规划持续推进：

- **Phase 9（交互增强）**：Leader 键、模型选择器、确认弹窗、鼠标、命令路由（已交付，见 git 历史 `feat(tui-v2): Phase 9` 系列 commit）。
- **Phase 10（全新快捷键系统）**：三层键位（Input/Normal/Leader）补全 Input Mode 行编辑（Ctrl+A/E/K/W）、Normal Mode 整页翻页（Ctrl+F/B）与 stream 搜索（`/` + `n/N` + stale 提示）、`:` 命令行（q/debug/help/compact/mode）、Leader 动作调整（m=模型选择器、c=取消运行、r=重试、Space=切上一会话）、模式指示配色、Ctrl+D 上下文分发、Overlay typed 常量。详细键位与验收标准见规划文档 `phase-10-keybinding-system.md`。

> 注：Phase 10 不引入 `ModeChangedMsg`——模式切换通过同步修改 `ViewState.Mode` 完成（遵循 AGENTS.md「代码为准」原则，规划文档已同步）。

---

*本指南是 TUI v2 文档集的子文档。架构总览见 [架构导航](./tui-v2-architecture-hub.md)，视觉交互见 [UI/UX](./tui-v2-ui-ux-design.md)，数据契约见 [数据/契约](./tui-v2-data-and-gateway.md)。*
