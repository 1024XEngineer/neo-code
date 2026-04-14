package controlplane

// StopReason 表示一次 Run 的最终停止原因，互斥且由决议器唯一确定。
type StopReason string

const (
	// StopReasonSuccess 表示助手正常结束（无待执行工具调用）。
	StopReasonSuccess StopReason = "success"
	// StopReasonMaxLoops 表示达到配置的最大推理轮数。
	StopReasonMaxLoops StopReason = "max_loops"
	// StopReasonError 表示不可恢复的运行时或 provider 错误。
	StopReasonError StopReason = "error"
	// StopReasonCanceled 表示运行上下文被取消（含用户中断）。
	StopReasonCanceled StopReason = "canceled"
)
