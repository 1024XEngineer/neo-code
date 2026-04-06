package state

import "time"

// ToolLifecycleStatus 描述工具执行生命周期状态。
type ToolLifecycleStatus string

const (
	ToolLifecyclePlanned   ToolLifecycleStatus = "planned"
	ToolLifecycleRunning   ToolLifecycleStatus = "running"
	ToolLifecycleSucceeded ToolLifecycleStatus = "succeeded"
	ToolLifecycleFailed    ToolLifecycleStatus = "failed"
)

// ToolState 记录单个工具调用在 UI 中展示的状态。
type ToolState struct {
	ToolCallID string
	ToolName   string
	Status     ToolLifecycleStatus
	Message    string
	DurationMS int64
	UpdatedAt  time.Time
}

// ContextWindowState 描述 runtime 透出的上下文窗口信息。
type ContextWindowState struct {
	RunID     string
	SessionID string
	Provider  string
	Model     string
	Workdir   string
	Mode      string
}

// TokenUsageState 描述 token 统计在 UI 的展示结构。
type TokenUsageState struct {
	RunInputTokens      int
	RunOutputTokens     int
	RunTotalTokens      int
	SessionInputTokens  int
	SessionOutputTokens int
	SessionTotalTokens  int
}
