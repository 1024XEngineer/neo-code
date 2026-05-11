package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

var errGenerateTextMissingMessageDone = errors.New("provider stream ended without message_done event")

// GenerateText 聚合并消费流式事件，直接返回完整字符串。消灭上层流处理样板代码。
func GenerateText(ctx context.Context, p Provider, req providertypes.GenerateRequest) (string, error) {
	events := make(chan providertypes.StreamEvent, 32)
	done := make(chan error, 1)
	var builder strings.Builder

	go func() {
		var streamErr error
		messageDone := false
		for event := range events {
			switch event.Type {
			case providertypes.StreamEventTextDelta:
				payload, err := event.TextDeltaValue()
				if err != nil {
					if streamErr == nil {
						streamErr = err
					}
					continue
				}
				builder.WriteString(payload.Text)
			case providertypes.StreamEventMessageDone:
				if _, err := event.MessageDoneValue(); err != nil {
					if streamErr == nil {
						streamErr = err
					}
					continue
				}
				messageDone = true
			case providertypes.StreamEventThinkingDelta:
				if _, err := event.ThinkingDeltaValue(); err != nil {
					if streamErr == nil {
						streamErr = err
					}
					continue
				}
			default:
				if streamErr == nil {
					streamErr = fmt.Errorf("unexpected provider stream event %q", event.Type)
				}
			}
		}
		if streamErr == nil && !messageDone {
			streamErr = errGenerateTextMissingMessageDone
		}
		done <- streamErr
	}()

	err := p.Generate(ctx, req, events)
	close(events)

	streamErr := <-done
	if err != nil {
		if streamErr == nil || errors.Is(streamErr, errGenerateTextMissingMessageDone) {
			return "", err
		}
		return "", fmt.Errorf("generate failed: %v: %w", streamErr, err)
	}
	if streamErr != nil {
		return "", streamErr
	}
	return builder.String(), nil
}
