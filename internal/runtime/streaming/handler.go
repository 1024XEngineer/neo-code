package streaming

import (
	"fmt"

	providertypes "neo-code/internal/provider/types"
)

// Hooks 描述 runtime 在消费 provider 流时可选的回调挂点。
type Hooks struct {
	OnTextDelta     func(string)
	OnThinkingDelta func(string)
	OnToolCallStart func(providertypes.ToolCallStartPayload)
	OnMessageDone   func(providertypes.MessageDonePayload)
}

// HandleEvent 解析并应用单个 provider 流式事件。
func HandleEvent(event providertypes.StreamEvent, acc *Accumulator, hooks Hooks) error {
	switch event.Type {
	case providertypes.StreamEventTextDelta:
		payload, err := event.TextDeltaValue()
		if err != nil {
			return err
		}
		if hooks.OnTextDelta != nil {
			hooks.OnTextDelta(payload.Text)
		}
		if acc != nil {
			acc.AccumulateText(payload.Text)
		}
	case providertypes.StreamEventToolCallStart:
		payload, err := event.ToolCallStartValue()
		if err != nil {
			return err
		}
		if hooks.OnToolCallStart != nil {
			hooks.OnToolCallStart(payload)
		}
		if acc != nil {
			acc.AccumulateToolCallStart(payload.Index, payload.ID, payload.Name)
		}
	case providertypes.StreamEventToolCallDelta:
		payload, err := event.ToolCallDeltaValue()
		if err != nil {
			return err
		}
		if acc != nil {
			acc.AccumulateToolCallDelta(payload.Index, payload.ID, payload.ArgumentsDelta)
		}
	case providertypes.StreamEventThinkingDelta:
		payload, err := event.ThinkingDeltaValue()
		if err != nil {
			return err
		}
		if hooks.OnThinkingDelta != nil {
			hooks.OnThinkingDelta(payload.Text)
		}
		// thinking 独立累积（不混入 assistant 正文），后续写入 ThinkingMetadata
		if acc != nil {
			acc.AccumulateThinking(payload.Text)
		}
	case providertypes.StreamEventMessageDone:
		payload, err := event.MessageDoneValue()
		if err != nil {
			return err
		}
		if acc != nil {
			acc.MarkMessageDone()
		}
		if hooks.OnMessageDone != nil {
			hooks.OnMessageDone(payload)
		}
	default:
		return fmt.Errorf("runtime: unsupported provider stream event type %q", event.Type)
	}
	return nil
}
