# interface-migration-map

> 状态：V2 Draft（语义收敛版）  
> 更新日期：2026-04-06  
> 迁移方向：`docs/interfaces` -> `docs/architecture`（逐步替代）

## 1. 状态定义

- `Current`：当前仓库已有稳定行为。
- `Proposed`：目标态设计，未全部落地。
- `Deprecated`：旧命名或旧语义，迁移期保留。

## 2. Runtime 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `RunLoop(userInput)` | `Runtime.Run(ctx, UserInput)` | `Runtime.Run(ctx, UserInput)` | Current | 保持当前主入口。 |
| `CancelRun` | `Runtime.CancelActiveRun()` | `Runtime.CancelActiveRun()` | Current | 语义明确，取消当前活跃运行。 |
| `GetEvents` | `Runtime.Events()` | `Runtime.Events()` | Current | 当前事件通道模式。 |
| `CompactNow` | `Runtime.Compact(ctx, CompactInput)` | `Runtime.Compact(ctx, CompactInput)` | Current | 手动 compact 入口。 |
| `SetWorkdir` | `Runtime.SetSessionWorkdir(...)` | `Runtime.SetSessionWorkdir(...)` | Current | 会话级工作目录映射。 |
| `TerminalState` | 无独立接口 | `TerminalEventGate` | Proposed | 终态唯一性门禁是目标态设计。 |

## 3. Session 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `SessionManager.GetHistory(sessionID)` | `Store.Load(ctx, id)` | `Store.Load(ctx, id)` | Current | 当前按会话 ID 加载完整快照。 |
| `SessionManager.AppendMessage(...)` | runtime 内部拼装后 `Store.Save(...)` | `Runtime + Store.Save` | Deprecated | 追加语义已收敛为整会话保存。 |
| `SessionManager.List()` | `Store.ListSummaries(ctx)` | `Store.ListSummaries(ctx)` | Current | 当前按更新时间倒序返回摘要。 |
| `SessionRuntimeStateStore` | 无独立接口 | `SessionRuntimeStateStore` | Proposed | 运行态拆分能力尚未落地。 |
| `ArchiveStore` | 无独立接口 | `ArchiveStore` | Proposed | 归档与 compact 联动属于目标态。 |

## 4. Context 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `ContextEngine.Compose(...)` | `Builder.Build(ctx, BuildInput)` | `Builder.Build(ctx, BuildInput)` | Deprecated | 统一为单入口 Build。 |
| `ContextEngine.CheckAndCompact(usage)` | 分散在 runtime/compact runner | `AutoCompactPolicy + ReactiveRetryGate` | Proposed | 当前无自动链路，目标态再收敛。 |
| `BuildInput(messages, metadata)` | `BuildInput{Messages, Metadata}` | `BuildInput + LoopState/TokenBudget/WorkspaceMap/TaskScope` | Proposed | 扩展字段尚未落地。 |
| `ContextMessage` | `provider.Message` | `provider.Message` | Current | 继续复用 provider 消息结构。 |

## 5. Provider 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `LLMProvider.Chat(messages, tools)` | `Provider.Chat(ctx, ChatRequest, events)` | `Provider.Chat(ctx, ChatRequest, events)` | Current | 当前流式事件调用已存在。 |
| `ProviderClient.Chat(req)` | `provider.Provider` | `provider.Provider` | Deprecated | 名称归并。 |
| `IsContextTooLong(err)` | 无统一公开接口 | `ErrorClassifier.IsContextOverflow(err)` | Proposed | 目标态统一错误归一化。 |
| `ProviderError(kind)` | `ProviderError{Code, Retryable}` | `ProviderError + context_overflow` | Proposed | context_overflow 错误码尚未稳定落地。 |

## 6. Tools 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `ToolExecutor.Execute(...)` | `Manager.Execute(ctx, ToolCallInput)` | `Manager.Execute(ctx, ToolCallInput)` | Current | 当前统一执行边界。 |
| `ToolExecutor.List()` | `Manager.ListAvailableSpecs(...)` | `Manager.ListAvailableSpecs(...)` | Current | 当前 schema 暴露边界。 |
| `ToolExecutor.SpawnSubAgent(...)` | 无公开接口 | `SubAgentOrchestrator.Spawn/Wait/Cancel` | Proposed | 子 Agent 扩展未落地。 |
| `NeedApproval` 布尔返回 | `PermissionDecisionError + permission 事件` | 交互式审批闭环 | Proposed | 当前为 runtime 内部顺序事件，不是交互式确认。 |

## 7. Config 侧映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `ConfigRegistry.GetProviderConfig(name)` | `Manager.SelectedProvider()/ResolvedSelectedProvider()` | `Registry.Snapshot()+ProviderByName` | Proposed | Snapshot/Registry 为目标态抽象。 |
| `ConfigRegistry.GetAutoCompactLimit()` | `Config.Context.Compact` | `Config.Context.Compact + AutoCompactPolicy` | Proposed | 自动阈值策略尚未落地。 |
| `ConfigRegistry.OnConfigChange(callback)` | 无稳定监听接口 | `Registry.Watch(fn)` | Proposed | 热更新监听未落地。 |
| `ConfigRegistry.UpdateConfig` | `Manager.Update(ctx, mutate)` | `Registry.Update(ctx, mutate)` | Current | 当前已有事务式更新。 |

## 8. Gateway 与协议映射

| 旧协商名 | 当前项目名 | V2 定名 | 状态 | 说明 |
|---|---|---|---|---|
| `runtime direct call from tui` | `tui -> runtime interface` | `TUI/CLI/Web -> Gateway -> Runtime` | Proposed | Gateway 为目标态，当前未稳定实现。 |
| `/ws`（泛称） | 暂无稳定公共网关实现 | `/ws/chat` | Proposed | 路径规范属于目标态。 |
| `POST /compact`（草案） | TUI 调 `Runtime.Compact` | `POST /api/compact -> Runtime.Compact` | Proposed | HTTP 入口未落地。 |

## 9. 冲突语义收敛

- `compact_start`：
  - Current：字符串 payload。
  - Proposed：结构化 payload（`CompactStartPayload`）。
- 审批闭环：
  - Current：ask 命中后 runtime 顺序发 `permission_request` 与 `permission_resolved`。
  - Proposed：网关交互式确认后再发 `permission_resolved`。
- proactive：
  - Current：不存在自动触发链路。
  - Proposed：预算驱动自动触发。

## 10. 文档迁移计划

1. 先把各模块契约沉淀到 `docs/architecture/<module>/README.md + interface.go`。  
2. 再将 `docs/interfaces` 逐步降级为迁移索引与兼容说明。  
3. `docs/session-persistence-design.md` 的核心内容优先迁移到 `docs/architecture/session/*`。  
