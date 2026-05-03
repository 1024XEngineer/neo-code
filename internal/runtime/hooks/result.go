package hooks

import "time"

// HookResultStatus 表示单个 hook 的执行结果状态。
type HookResultStatus string

const (
	// HookResultPass 表示 hook 执行通过。
	HookResultPass HookResultStatus = "pass"
	// HookResultBlock 表示 hook 主动阻断后续流程。
	HookResultBlock HookResultStatus = "block"
	// HookResultFailed 表示 hook 执行失败（如 panic/timeout/错误）。
	HookResultFailed HookResultStatus = "failed"
)

// HookResult 描述单个 hook 的结构化执行结果。
type HookResult struct {
	HookID     string
	Point      HookPoint
	Scope      HookScope
	Source     HookSource
	Status     HookResultStatus
	Message    string
	Error      string
	Metadata   HookResultMetadata
	StartedAt  time.Time
	DurationMS int64
}

// HookResultMetadata 描述 hook 执行结果的结构化附加信息。
type HookResultMetadata struct {
	Rewake        bool
	RewakeReason  string
	RewakeSummary string
}

// RunOutput 是一次点位执行的聚合结果。
type RunOutput struct {
	Results       []HookResult
	Blocked       bool
	BlockedBy     string
	BlockedSource HookSource
}
