# NeoCode 架构评审记录（Issue #217）

本文档用于沉淀 `Issue #217` 中对当前仓库架构的复核结论，重点关注主链路、职责边界、无效抽象与可维护性风险。

## 评审范围

- `internal/app`
- `internal/gateway`
- `internal/runtime`
- `internal/tools`
- `docs`
- 根目录 `AGENTS.md`

## 总体结论

当前项目主链路 `TUI -> Runtime -> Provider / Tool Manager` 基本清晰，运行时编排、工具调度、配置管理与 TUI 展示的边界总体可读，适合作为 Agent CLI 的 MVP 基础。

但仓库中仍存在几类会直接拉低可维护性和演进效率的问题：

- 存在未落地的抽象层，增加理解成本且没有产生真实隔离收益。
- 个别运行时状态通过包级全局变量维护，削弱封装并引入并发与测试隔离风险。
- 部分安全兜底行为过于宽松，容易在调用方漏配时悄悄退化为不安全路径。
- 少量关键事件投递失败后没有被显式处理，可能造成 UI 视图与真实运行状态不一致。

## 主要问题

### 1. `internal/gateway` 当前属于未落地抽象

相关位置：

- `internal/gateway/contracts.go`

现状：

- `Gateway` 与 `RuntimePort` 只定义了契约，没有看到对应实现或被主链路消费。
- `PermissionResolutionDecision`、`CompactResult`、`RunInput` 等类型与 `internal/runtime` 中的运行时结构存在明显语义重叠。
- 当前 TUI 直接消费 runtime 事件与能力，没有经过 gateway 这一层。

影响：

- 形成“为了将来可能需要”而保留的预留层。
- 新人阅读时会误判系统存在第二套接入边界。
- 同类结构重复定义，后续演进时容易出现字段漂移。

建议：

- 如果当前版本没有 HTTP / RPC 网关落地计划，先删除 `internal/gateway`，统一以 runtime 契约作为唯一编排入口。
- 如果必须保留，则应尽快补齐真实实现，并让 gateway 直接复用 runtime 领域类型，避免重复建模。

### 2. 权限待决状态使用包级全局变量，封装性不足

相关位置：

- `internal/runtime/permission.go`

现状：

- `runtimePendingPermissions` 通过包级全局变量保存 `*Service -> pendingPermissionRequest` 的映射。
- 状态生命周期没有收敛在 `Service` 实例内部。

影响：

- 多实例测试或未来并发运行时更容易产生隐式共享状态。
- 状态释放路径不够直观，后续改动时容易引入泄漏或串扰。
- 当前结构默认每个 `Service` 同时只有一个待审批请求，不利于并行工具调用扩展。

建议：

- 将待审批状态下沉到 `Service` 字段中，由实例自己持有 `pending request` 与互斥锁。
- 若后续要支持并行工具调用，建议改成按 `requestID` 建索引，而不是单值槽位。

### 3. `NoopWorkspaceSandbox` 的兜底语义过宽

相关位置：

- `internal/tools/manager.go`
- `internal/app/bootstrap.go`

现状：

- `buildToolManager` 正常路径会注入 `security.NewWorkspaceSandbox()`，这条链路是合理的。
- 但 `tools.NewManager` 在 `sandbox == nil` 时会回退到 `NoopWorkspaceSandbox`。
- 该实现的 `Check` 直接返回 `nil, ctx.Err()`；在上下文正常时等价于“不做任何工作区限制”。

影响：

- 调用方一旦漏传 sandbox，不会快速失败，而是静默退化为无隔离执行。
- 类型名称中的 `Noop` 容易让人忽略它实际上是在跳过关键安全检查。

建议：

- 优先改为构造期强校验，要求调用方显式传入 sandbox。
- 如果保留兜底实现，也应使用更明确的命名，并在运行时输出清晰错误，避免静默放行。

### 4. 关键事件发射失败后未被统一处理

相关位置：

- `internal/runtime/runtime.go`

现状：

- `s.emit(...)` 在运行主循环中多次调用，但大多数返回值没有被处理。
- 一旦事件通道阻塞或上下文取消，事件丢失不会总是反映到上层控制流。

影响：

- UI 可能漏掉 `EventToolStart`、`EventToolResult`、`EventAgentDone` 等关键状态。
- 当展示层状态与真实运行进度不一致时，问题排查会比较困难。

建议：

- 对关键事件建立统一的错误处理策略。
- 至少对会影响交互一致性的事件进行检查，并决定是中止当前运行、写入诊断日志，还是降级告警。

## 优先级建议

建议按以下顺序治理：

1. 先处理 `runtimePendingPermissions` 的实例内收敛，降低运行时共享状态风险。
2. 再收紧 `NoopWorkspaceSandbox` 的默认行为，避免安全能力因误用而失效。
3. 随后清理 `internal/gateway`，减少空抽象和重复类型。
4. 最后补强事件投递失败时的处理策略，提高 TUI 与 runtime 的一致性。

## 收益预期

完成上述收敛后，项目会得到几项直接收益：

- 主链路更单纯，仓库学习成本更低。
- 运行时状态边界更清晰，测试隔离性更好。
- 安全能力从“依赖正确接线”变成“默认不允许漏接”。
- 事件驱动链路更可观测，后续排查 UI 异常更直接。
