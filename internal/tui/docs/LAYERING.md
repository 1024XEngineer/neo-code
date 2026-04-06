# TUI 分层约束（Iteration 0）

本文档用于约束 `internal/tui` 的分层职责与依赖方向，确保后续迭代按层收敛，不跨层扩散。

## 改造范围

- 本轮只处理 `internal/tui`。
- 入口层 `cmd/neocode` 暂不处理。

## 分层定义

### L1 - Entry（暂缓）

- 位置：`cmd/neocode/`
- 职责：参数解析、终端初始化、启动 Program。
- 本轮状态：暂不纳入改造。

### L2 - Bootstrap

- 位置：`internal/tui/bootstrap/`
- 职责：依赖注入（DI）与初始化编排。
- 负责：工作区/配置初始化、服务装配、Offline/Mock 注入切换。

### L3 - App/Core

- 位置：`internal/tui/core/`
- 职责：Bubble Tea 状态机中枢（ELM 单向数据流）。
- 负责：消息路由、状态变更、布局调度。

### L4 - State

- 位置：`internal/tui/state/`
- 职责：纯数据容器。
- 约束：只放结构体和常量，不放方法与副作用。

### L5 - Component Adapter

- 位置：`internal/tui/components/`
- 职责：原子渲染组件。
- 输入：基础数据或 state。
- 输出：渲染字符串。

### L6 - Services

- 位置：`internal/tui/services/`
- 职责：对接 runtime/provider/本地系统能力。
- 约束：统一返回 `tea.Cmd` 或异步产出 `tea.Msg`。

### L7 - Infrastructure

- 位置：`internal/tui/infra/`
- 职责：底层 I/O 与系统能力。
- 范围：shell 执行、文件扫描、终端 I/O、渲染器、剪贴板等。

## 依赖方向（允许）

- `core` -> `state`
- `core` -> `components`
- `core` -> `services`
- `services` -> `infra`

## 禁止项

- 禁止 `components` 直接访问 runtime/provider 或执行外部 I/O。
- 禁止 `core` 直接调用底层系统能力（应经 `services`）。
- 禁止 `state` 承载业务逻辑、网络调用或文件操作。
- 禁止新增跨层直连（例如 `core` 直接依赖 `infra`）。
- 禁止在本轮引入行为变更；Iteration 0 只做骨架与规则。

## Iteration 0 验收

- 目录骨架已创建：`bootstrap/core/state/components/services/infra`
- 分层约束文档已建立
- `go test ./internal/tui/...` 通过

## Iteration 6 补充（Bootstrap 落地）

- `internal/tui/bootstrap` 已提供 `Build` 装配入口，统一完成 `ConfigManager + Runtime + ProviderService` 注入。
- 支持 `Mode`（`live/offline/mock`）与 `ServiceFactory` 扩展点，可在不修改 `core` 的情况下替换注入实现。
- `internal/tui.New(...)` 保持兼容签名，对外作为薄封装；实际装配路径为 `New -> bootstrap.Build -> newApp`。

## Iteration 7 补充（Runtime Source 收敛）

- Runtime 事件新增并接入 UI 桥接：
  - `EventToolStatus`
  - `EventRunContext`
  - `EventUsage`
- Runtime 查询接口已落地：
  - `GetRunSnapshot(runID)`
  - `GetSessionContext(sessionID)`
  - `GetSessionUsage(sessionID)`
  - `GetRunUsage(runID)`
- `internal/tui/core/runtime_bridge.go` 统一处理 payload -> VM 映射与 Tool 状态去重合并（覆盖重复/乱序事件场景）。
- TUI 在会话刷新时优先通过 runtime 查询回填 context/token 快照，避免由 UI 本地推导。
