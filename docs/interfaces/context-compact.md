# context-compact

> 状态：V2 Draft（语义收敛版）  
> 版本：v2.0.0-draft.2  
> 更新日期：2026-04-06

## 1. 标签约定

- `[CURRENT]`：当前仓库已实现且可联调。
- `[PROPOSED]`：目标态设计。
- `[NOT IMPLEMENTED YET]`：明确尚未落地。

## 2. Current Baseline（当前实现基线）

### 2.1 触发模式现状

- `[CURRENT]` Runtime 当前只提供手动入口：`Runtime.Compact(...)`。
- `[CURRENT]` `internal/context/compact.Runner` 已支持两种 mode：`manual`、`reactive`。
- `[CURRENT]` runtime 主链尚未自动触发 `reactive`，仅可由上层显式调用 runner。

```go
package compact

// Mode 是当前 runner 已实现的触发模式。
type Mode string

const (
	// ModeManual 表示用户手动触发。
	ModeManual Mode = "manual"
	// ModeReactive 表示错误恢复触发（runner 支持，runtime 主链未自动接入）。
	ModeReactive Mode = "reactive"
)
```

### 2.2 当前可执行契约

```go
package compact

import (
	"context"
	"neo-code/internal/config"
	"neo-code/internal/provider"
)

// Input 是当前 compact runner 的输入。
type Input struct {
	// Mode 是触发模式（manual/reactive）。
	Mode Mode
	// SessionID 是会话标识。
	SessionID string
	// Workdir 是工作目录。
	Workdir string
	// Messages 是压缩前消息快照。
	Messages []provider.Message
	// Config 是 compact 配置快照。
	Config config.CompactConfig
}

// Result 是当前 compact runner 的执行结果。
type Result struct {
	// Messages 是压缩后消息。
	Messages []provider.Message
	// Metrics 是压缩前后指标。
	Metrics Metrics
	// TranscriptID 是压缩前 transcript 标识。
	TranscriptID string
	// TranscriptPath 是压缩前 transcript 路径。
	TranscriptPath string
	// Applied 表示是否发生实际压缩。
	Applied bool
}

// Runner 是当前 compact 执行接口。
type Runner interface {
	// Run 执行一次 compact。
	// 输入语义：input 提供模式、消息与配置。
	// 并发约束：同一会话应由 runtime 串行调用。
	// 生命周期：一次调用对应一次压缩判定。
	// 错误语义：返回摘要生成、校验或落盘失败。
	Run(ctx context.Context, input Input) (Result, error)
}
```

### 2.3 事件与解码（当前行为）

| 事件 | 当前 payload | 当前解码建议 |
|---|---|---|
| `compact_start` | `string` | 按字符串解码（当前常见值 `manual`）。 |
| `compact_done` | `CompactDonePayload` | 按结构体解码。 |
| `compact_error` | `CompactErrorPayload` | 按结构体解码。 |

- `[CURRENT]` 如果消费者把 `compact_start` 当结构体解码，会与现实现不兼容。

### 2.4 摘要协议（当前已生效）

compact summary 当前遵循固定结构：

```text
[compact_summary]

done:
- ...

in_progress:
- ...

decisions:
- ...

code_changes:
- ...

constraints:
- ...
```

### 2.5 当前状态结论

- `[CURRENT]` 手动 compact 是已落地主链。
- `[CURRENT]` reactive 仅在 runner 具备能力，runtime 自动恢复未接入。
- `[CURRENT]` proactive 自动触发链路尚不存在。

## 3. V2 Proposed（目标态扩展）

### 3.1 reactive 自动恢复

- `[PROPOSED][NOT IMPLEMENTED YET]` provider 命中上下文过长错误后，runtime 自动执行 `compact.Run(mode=reactive)`。
- `[PROPOSED][NOT IMPLEMENTED YET]` 每个 `run_id` 只允许一次 reactive 自动重试。

```go
package runtime

// ReactiveRetryGate 管理每个 run 的单次 reactive 重试门禁。
type ReactiveRetryGate interface {
	// Acquire 尝试获取单次重试资格。
	// 输入语义：runID 为运行标识。
	// 并发约束：线程安全。
	// 生命周期：同一 runID 最多成功一次。
	// 错误语义：false 表示资格已耗尽。
	Acquire(runID string) bool
	// Release 释放 run 相关门禁状态。
	// 输入语义：runID 为运行标识。
	// 并发约束：幂等。
	// 生命周期：run 终态后调用。
	// 错误语义：无错误返回。
	Release(runID string)
}
```

### 3.2 proactive 自动压缩

- `[PROPOSED][NOT IMPLEMENTED YET]` 新增 `proactive` 模式，在预算逼近阈值时提前压缩。
- `[PROPOSED][NOT IMPLEMENTED YET]` 本能力落地前置条件：
  - `context.BuildInput` 具备预算字段（例如 `TokenBudget`）。
  - runtime 在 pre-turn / mid-turn 有稳定触发点。
  - 门禁策略明确每轮自动触发上限，避免重复压缩。

### 3.3 事件格式升级

- `[PROPOSED][NOT IMPLEMENTED YET]` `compact_start` 升级为结构化 payload。
- `[PROPOSED]` 升级后需保留兼容层，避免当前消费者解码中断。

```go
package runtime

// CompactStartPayload 是未来 compact_start 的结构化负载。
type CompactStartPayload struct {
	// TriggerMode 是触发模式：manual/reactive/proactive。
	TriggerMode string `json:"trigger_mode"`
}
```

## 4. 测试与验收

### 4.1 Baseline 验收

- `compact_start` 当前按字符串解码成功。
- 手动 `Runtime.Compact` 继续输出 `compact_start -> compact_done/compact_error`。
- 摘要协议结构校验与 transcript 落盘行为不回归。

### 4.2 V2 Proposed 验收

- provider 上下文过长错误可自动触发 reactive compact。
- 每个 run 的 reactive 自动重试最多一次。
- proactive 触发链路可控且不会无限触发。
- `compact_start` 结构化后客户端仍可平滑兼容。

## 5. 与主规范对齐约束

- 本文档与 `runtime-interface-spec.md` 在以下语义必须一致：
  - Gateway 状态。
  - reactive 自动恢复是否落地。
  - proactive 状态（仅 proposed）。
  - `compact_start` 当前 payload 类型（字符串）。
