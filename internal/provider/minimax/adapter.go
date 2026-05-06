package minimax

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const (
	maxSSELineSize        = 256 * 1024
	maxSSEStreamTotalSize = 10 << 20
)

// minimaxChunk 匹配 MiniMax chat completion 响应格式。
type minimaxChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content,omitempty"`
			ReasoningDetails string `json:"reasoning_details,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// ConsumeMiniMaxStream 解析 MiniMax SSE 流，优先从 reasoning_details 提取 thinking，
// 兜底从 content 中剥离 <think> 标签。
func ConsumeMiniMaxStream(ctx context.Context, body io.Reader, events chan<- providertypes.StreamEvent) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, maxSSELineSize), maxSSELineSize)

	var (
		finishReason string
		usage        providertypes.Usage
	)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimSpace(data)

		if data == "[DONE]" {
			return provider.EmitMessageDone(ctx, events, finishReason, doneUsagePtr(usage))
		}

		var chunk minimaxChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
			usage = providertypes.Usage{
				InputTokens:    0,
				OutputTokens:   chunk.Usage.TotalTokens,
				TotalTokens:    chunk.Usage.TotalTokens,
				OutputObserved: true,
			}
		}

		for _, choice := range chunk.Choices {
			if strings.TrimSpace(choice.FinishReason) != "" {
				finishReason = strings.TrimSpace(choice.FinishReason)
			}

			// 优先使用 reasoning_details 作为 thinking 内容
			useContent := choice.Delta.Content
			if reasoning := strings.TrimSpace(choice.Delta.ReasoningDetails); reasoning != "" {
				if err := provider.EmitThinkingDelta(ctx, events, reasoning); err != nil {
					return err
				}
			} else if thinkText := ExtractThinkContent(choice.Delta.Content); thinkText != "" {
				// 兜底：从 content 中剥离 <think> 标签，避免泄漏到正文
				if err := provider.EmitThinkingDelta(ctx, events, thinkText); err != nil {
					return err
				}
				useContent = thinkTagRe.ReplaceAllString(choice.Delta.Content, "")
			}

			if err := provider.EmitTextDelta(ctx, events, useContent); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%ssse scanner: %w", errorPrefix, err)
	}

	return provider.EmitMessageDone(ctx, events, finishReason, doneUsagePtr(usage))
}

func doneUsagePtr(usage providertypes.Usage) *providertypes.Usage {
	if !usage.OutputObserved {
		return nil
	}
	cp := usage
	return &cp
}
