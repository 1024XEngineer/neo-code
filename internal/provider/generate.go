package provider

import (
	"context"
	"fmt"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

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
			default:
				if streamErr == nil {
					streamErr = fmt.Errorf("unexpected provider stream event %q", event.Type)
				}
			}
		}
		if streamErr == nil && !messageDone {
			streamErr = fmt.Errorf("provider stream ended without message_done event")
		}
		done <- streamErr
	}()

	err := p.Generate(ctx, req, events)
	close(events)

	if streamErr := <-done; streamErr != nil {
		if err != nil {
			return "", fmt.Errorf("generate failed: %v: %w", streamErr, err)
		}
		return "", streamErr
	}
	if err != nil {
		return "", err
	}
	return builder.String(), nil
}
