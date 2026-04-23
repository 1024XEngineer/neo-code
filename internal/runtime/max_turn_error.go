package runtime

import "fmt"

// maxTurnLimitError 表示 Run 达到 runtime.max_turns 上限后触发的受控停止错误。
type maxTurnLimitError struct {
	limit int
}

// Error 返回可读错误信息，供日志与上层错误链路复用。
func (e maxTurnLimitError) Error() string {
	return fmt.Sprintf("runtime: max turn limit reached (%d)", e.limit)
}

// Limit 暴露触发停止时的最大轮数阈值。
func (e maxTurnLimitError) Limit() int {
	return e.limit
}

// newMaxTurnLimitError 构造统一的 max_turns 停止错误。
func newMaxTurnLimitError(limit int) error {
	return maxTurnLimitError{limit: limit}
}
