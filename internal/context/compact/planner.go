package compact

import (
	"fmt"
	"strings"

	"neo-code/internal/config"
	"neo-code/internal/context/internalcompact"
	"neo-code/internal/provider"
)

// compactionPlan 描述一次 compact 在摘要生成前的归档与保留结果。
type compactionPlan struct {
	Archived             []provider.Message
	Retained             []provider.Message
	ArchivedMessageCount int
	Applied              bool
}

// compactionPlanner 负责根据策略规划 archived 和 retained 的消息边界。
type compactionPlanner struct{}

// Plan 根据 mode 与配置返回摘要前的裁剪规划结果。
func (compactionPlanner) Plan(mode Mode, messages []provider.Message, cfg config.CompactConfig) (compactionPlan, error) {
	if mode == ModeReactive {
		return planKeepRecent(messages, cfg.ManualKeepRecentMessages), nil
	}

	switch strings.ToLower(strings.TrimSpace(cfg.ManualStrategy)) {
	case config.CompactManualStrategyKeepRecent:
		return planKeepRecent(messages, cfg.ManualKeepRecentMessages), nil
	case config.CompactManualStrategyFullReplace:
		return planFullReplace(messages), nil
	default:
		return compactionPlan{}, fmt.Errorf("compact: manual strategy %q is not supported", cfg.ManualStrategy)
	}
}

// planKeepRecent 计算 keep_recent 策略下需要摘要与保留的消息集合。
func planKeepRecent(messages []provider.Message, keepMessages int) compactionPlan {
	spans := internalcompact.BuildMessageSpans(messages)
	retainedStart := internalcompact.RetainedStartForKeepRecentMessages(spans, keepMessages)
	if retainedStart <= 0 {
		return compactionPlan{
			Retained: cloneMessages(messages),
			Applied:  false,
		}
	}

	archived, retained := splitMessagesAt(messages, retainedStart)
	return compactionPlan{
		Archived:             archived,
		Retained:             retained,
		ArchivedMessageCount: len(archived),
		Applied:              len(archived) > 0,
	}
}

// planFullReplace 计算 full_replace 策略下需要摘要与保留的消息集合。
func planFullReplace(messages []provider.Message) compactionPlan {
	if len(messages) == 0 {
		return compactionPlan{}
	}

	spans := internalcompact.BuildMessageSpans(messages)
	retainedStart, hasProtectedTail := internalcompact.ProtectedTailStart(spans)
	if !hasProtectedTail {
		retainedStart = len(messages)
	}

	archived, retained := splitMessagesAt(messages, retainedStart)
	return compactionPlan{
		Archived:             archived,
		Retained:             retained,
		ArchivedMessageCount: len(archived),
		Applied:              len(archived) > 0,
	}
}

// splitMessagesAt 按 retained 起点切分 archived 与 retained，并返回深拷贝结果。
func splitMessagesAt(messages []provider.Message, retainedStart int) ([]provider.Message, []provider.Message) {
	if retainedStart <= 0 {
		return nil, cloneMessages(messages)
	}
	if retainedStart >= len(messages) {
		return cloneMessages(messages), nil
	}
	return cloneMessages(messages[:retainedStart]), cloneMessages(messages[retainedStart:])
}
