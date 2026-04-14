package context

import (
	providertypes "neo-code/internal/provider/types"
)

// messageTrimPolicy 约束消息裁剪策略的最小接口，避免 Builder 直接持有裁剪细节。
type messageTrimPolicy interface {
	Trim(messages []providertypes.Message, options CompactOptions) []providertypes.Message
}

// spanMessageTrimPolicy 以消息 span 为单位裁剪历史，确保 tool block 不被拆散。
type spanMessageTrimPolicy struct{}

// Trim 返回保留关键 tool block 原子性的裁剪后消息副本。
func (spanMessageTrimPolicy) Trim(messages []providertypes.Message, options CompactOptions) []providertypes.Message {
	return trimMessages(messages, options.ReadTimeMaxMessageSpans)
}
