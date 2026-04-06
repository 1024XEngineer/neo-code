# runtime-interface-spec

> 状态：V2 Draft（语义收敛版）  
> 版本：v2.0.0-draft.2  
> 更新日期：2026-04-06

## 1. 标签约定

- `[CURRENT]`：当前仓库实现基线，可直接联调。
- `[PROPOSED]`：V2 目标态设计，尚未全部落地。
- `[FUTURE]`：后续阶段能力，本期不承诺实现。
- `[NOT IMPLEMENTED YET]`：明确未落地，禁止按现状依赖。

本文档按“Current Baseline + V2 Proposed”双分区编排，避免目标态与现状混写。

## 2. Current Baseline（当前实现基线）

### 2.1 入口模型

- `[CURRENT]` 当前稳定入口：`TUI/CLI -> Runtime`。
- `[CURRENT]` 仓库内尚无稳定对外 `Gateway(REST/WS)` 实现。

### 2.2 Runtime 对外接口（当前命名）

```go
package runtime

import "context"

// Runtime 是当前编排层对 TUI/CLI 暴露的统一入口。
type Runtime interface {
	// Run 执行一次完整回合。
	// 输入语义：input 传入 run_id、session_id、用户文本与可选 workdir 覆盖。
	// 并发约束：同一 Runtime 实例串行化 Run 与 Compact。
	// 生命周期：单次调用从 user_message 开始，到终态事件结束。
	// 错误语义：返回本次运行终态错误；取消场景返回 context.Canceled。
	Run(ctx context.Context, input UserInput) error
	// Compact 手动触发会话压缩。
	// 输入语义：input.SessionID 必填。
	// 并发约束：与 Run 互斥，避免并发写会话。
	// 生命周期：一次 compact_start 到 compact_done 或 compact_error。
	// 错误语义：返回 compact 执行失败或会话保存失败。
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	// CancelActiveRun 取消当前活跃运行。
	// 输入语义：无。
	// 并发约束：线程安全且幂等。
	// 生命周期：仅影响当前活跃 run。
	// 错误语义：返回值表示是否命中可取消运行。
	CancelActiveRun() bool
	// Events 返回运行时事件流通道。
	// 输入语义：无。
	// 并发约束：默认单消费者语义，多消费者需上层自行扇出。
	// 生命周期：Runtime 生命周期内持续有效。
	// 错误语义：业务错误通过 EventError 事件透出。
	Events() <-chan RuntimeEvent
	// ListSessions 读取会话摘要。
	// 输入语义：ctx 控制读取超时。
	// 并发约束：可与只读操作并发。
	// 生命周期：随时可调用。
	// 错误语义：返回存储读取失败。
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	// LoadSession 加载会话详情。
	// 输入语义：id 为会话标识。
	// 并发约束：可与只读操作并发。
	// 生命周期：用于会话恢复与切换。
	// 错误语义：返回会话不存在或反序列化错误。
	LoadSession(ctx context.Context, id string) (Session, error)
	// SetSessionWorkdir 更新会话工作目录映射。
	// 输入语义：sessionID 必填，workdir 可传相对路径。
	// 并发约束：需保证映射读写线程安全。
	// 生命周期：会话级偏好设置。
	// 错误语义：返回路径解析失败、会话缺失或路径非法错误。
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (Session, error)
}
```

### 2.3 事件与载荷（当前行为）

- `[CURRENT]` 当前事件常量：
  - `user_message`
  - `agent_chunk`
  - `tool_call_thinking`
  - `tool_start`
  - `tool_chunk`
  - `tool_result`
  - `provider_retry`
  - `permission_request`
  - `permission_resolved`
  - `compact_start`
  - `compact_done`
  - `compact_error`
  - `run_canceled`
  - `error`
  - `agent_done`

- `[CURRENT]` compact 事件 payload 兼容矩阵：

| 事件 | 当前 payload 类型 | 说明 |
|---|---|---|
| `compact_start` | `string` | 当前发送 `"manual"`（裸字符串）。 |
| `compact_done` | `CompactDonePayload` | 结构化载荷，含 `trigger_mode` 等字段。 |
| `compact_error` | `CompactErrorPayload` | 结构化载荷，含 `trigger_mode` 与错误信息。 |

### 2.4 审批流（当前行为）

- `[CURRENT]` 当工具权限命中 `ask` 时，runtime 会顺序发出：
  1. `permission_request`
  2. `permission_resolved`
- `[CURRENT]` 该流程当前不是“交互式等待用户确认”，而是 runtime 内部顺序事件。

### 2.5 Context 契约（当前行为）

```go
package context

import (
	"context"
	"neo-code/internal/provider"
)

// Builder 是当前上下文构建入口。
type Builder interface {
	// Build 组装 provider 调用所需上下文。
	// 输入语义：input 包含历史消息与基础元数据。
	// 并发约束：实现应支持并发调用。
	// 生命周期：每轮 provider 调用前由 runtime 调用。
	// 错误语义：返回规则加载或渲染失败。
	Build(ctx context.Context, input BuildInput) (BuildResult, error)
}

// BuildInput 是当前已落地输入结构。
type BuildInput struct {
	// Messages 是会话消息快照。
	Messages []provider.Message
	// Metadata 是基础运行元数据。
	Metadata Metadata
}
```

### 2.6 Reactive Compact（当前行为）

- `[CURRENT]` `internal/context/compact` 的 runner 已支持 `manual/reactive`。
- `[CURRENT]` runtime 主链尚未在 provider 上下文过长错误时自动触发 reactive compact。
- `[CURRENT]` 当前未落地“每个 run 仅一次 reactive 自动重试”的门禁实现。

## 3. V2 Proposed（目标态规范）

### 3.1 入口模型与 Gateway

- `[PROPOSED][NOT IMPLEMENTED YET]` 统一入口模型：`TUI/CLI/Web -> Gateway(REST/WS) -> Runtime`。
- `[PROPOSED][NOT IMPLEMENTED YET]` Gateway 是协议适配层，Runtime 仍保留 Go 接口供内部调用。

```go
package gateway

import "context"

// MessageFrame 是网关与客户端通信帧。
type MessageFrame struct {
	// Type 是消息类型，例如 run_start、runtime_event、permission_resolve。
	Type string `json:"type"`
	// RunID 是运行标识。
	RunID string `json:"run_id,omitempty"`
	// SessionID 是会话标识。
	SessionID string `json:"session_id,omitempty"`
	// Payload 是业务负载。
	Payload any `json:"payload,omitempty"`
}

// ChatGateway 定义网关最小生命周期契约。
type ChatGateway interface {
	// Start 启动网关服务。
	// 输入语义：ctx 控制服务生命周期。
	// 并发约束：需支持多连接并发。
	// 生命周期：应用启动时调用。
	// 错误语义：返回监听或启动失败。
	Start(ctx context.Context) error
	// Close 优雅关闭网关。
	// 输入语义：ctx 控制关闭超时。
	// 并发约束：需幂等。
	// 生命周期：应用退出时调用。
	// 错误语义：返回剩余连接未完成关闭错误。
	Close(ctx context.Context) error
}
```

### 3.2 Context 扩展输入

- `[PROPOSED][NOT IMPLEMENTED YET]` 在 `BuildInput` 增加可选字段：`LoopState`、`TokenBudget`、`WorkspaceMap`、`TaskScope`。
- `[PROPOSED]` 保持单入口 `Builder.Build(ctx, BuildInput)`，不拆多入口。

### 3.3 Provider 错误归一化

- `[PROPOSED][NOT IMPLEMENTED YET]` 新增 `ErrorClassifier.IsContextOverflow(err)`。
- `[PROPOSED][NOT IMPLEMENTED YET]` 新增统一错误码：`context_overflow`。
- `[PROPOSED]` 识别顺序：typed error > 结构化字段 > 文本兜底。

### 3.4 审批闭环（交互式）

- `[PROPOSED][NOT IMPLEMENTED YET]` 改为真实人机确认：
  1. 发送 `permission_request`
  2. 等待客户端确认/拒绝
  3. 再发送 `permission_resolved`

### 3.5 Compact 事件格式升级

- `[PROPOSED][NOT IMPLEMENTED YET]` `compact_start` 由字符串 payload 升级为结构化 payload。
- `[PROPOSED]` 保留 `compact_done` / `compact_error` 结构化格式。

```go
package runtime

// CompactStartPayload 是未来 compact_start 的结构化载荷。
type CompactStartPayload struct {
	// TriggerMode 是触发模式，例如 manual、reactive。
	TriggerMode string `json:"trigger_mode"`
}
```

### 3.6 Reactive Compact 主链（本期目标态）

- `[PROPOSED][NOT IMPLEMENTED YET]` provider 命中上下文过长错误时，runtime 自动触发一次 `compact.Run(mode=reactive)`。
- `[PROPOSED][NOT IMPLEMENTED YET]` 每个 `run_id` 仅允许一次 reactive 自动重试。
- `[PROPOSED][NOT IMPLEMENTED YET]` 复用 `compact_start/compact_done/compact_error`，并要求 `trigger_mode=reactive`。

```go
package runtime

// ReactiveRetryGate 约束每个 run 的 reactive 重试次数。
type ReactiveRetryGate interface {
	// Acquire 尝试获取当前 run 的唯一重试资格。
	// 输入语义：runID 为运行标识。
	// 并发约束：必须线程安全。
	// 生命周期：同一 runID 最多成功一次。
	// 错误语义：false 表示资格已使用。
	Acquire(runID string) bool
	// Release 清理 run 相关状态。
	// 输入语义：runID 为运行标识。
	// 并发约束：幂等。
	// 生命周期：run 终态后调用。
	// 错误语义：无错误返回。
	Release(runID string)
}
```

## 4. 测试与验收

### 4.1 Baseline 验收（当前可测）

- `compact_start` 事件 payload 解码为字符串不报错。
- `permission_request` 与 `permission_resolved` 顺序行为与当前 runtime 一致。
- `BuildInput` 仅包含 `Messages/Metadata` 的调用链可稳定运行。

### 4.2 V2 Proposed 验收（目标态）

- provider 上下文过长错误可触发 reactive compact。
- 每个 run 仅一次 reactive 自动重试，无无限循环。
- `compact_start` 升级结构化 payload 后，兼容层可平滑解码。
- 审批流具备真实“请求-等待-决议”闭环。

## 5. 交叉一致性约束

- 本文档、`context-compact.md`、`interface-migration-map.md`、`README.md` 对以下状态描述必须一致：
  - Gateway 实现状态。
  - proactive compact 状态。
  - reactive 自动恢复状态。
  - `compact_start` payload 类型。
