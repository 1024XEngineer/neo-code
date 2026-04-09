package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// consumeStream 消费 SSE 响应流，使用有界读取器防止缓冲区溢出。
func (p *Provider) consumeStream(
	ctx context.Context,
	body io.Reader,
	events chan<- providertypes.StreamEvent,
) error {
	reader := newBoundedSSEReader(body)

	var (
		finishReason string
		usage        providertypes.Usage
		done         bool
		toolCalls    = make(map[int]*providertypes.ToolCall)
	)

	dataLines := make([]string, 0, 4)

	// processChunk 解析单个 SSE data payload，发送事件。
	processChunk := func(payload string) error {
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("openai provider: decode stream chunk: %w", err)
		}

		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return errors.New(chunk.Error.Message)
		}

		extractStreamUsage(&usage, chunk.Usage)

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				if err := emitTextDelta(ctx, events, choice.Delta.Content); err != nil {
					return err
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				if err := mergeToolCallDelta(ctx, events, toolCalls, delta); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// finishStream 统一的流结束处理：发送 message_done 事件。
	finishStream := func() error {
		log.Printf("[DEBUG-STREAM] finishStream called: finishReason=%q, done=%v", finishReason, done)
		return emitMessageDone(ctx, events, finishReason, &usage)
	}

	flushPendingData := func() error {
		defer func() { dataLines = dataLines[:0] }()
		return flushDataLines(dataLines, processChunk)
	}

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		line, err := reader.ReadLine()

		if err != nil && !errors.Is(err, io.EOF) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			// 非 EOF 的读取错误：先刷新缓冲的 data 行，再包装为流中断，
			// 避免中断前最后一段数据丢失。
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			return fmt.Errorf("%w: %w", provider.ErrStreamInterrupted, err)
		}

		trimmed := line

		switch {
		case strings.HasPrefix(trimmed, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			// data: [DONE] 需要立即处理：先刷新已缓冲的 data 行，再标记结束，
			// 避免与前面的合法 JSON 拼接后导致 json.Unmarshal 失败。
			if data == "[DONE]" {
				if flushErr := flushPendingData(); flushErr != nil {
					return flushErr
				}
				done = true
			} else {
				dataLines = append(dataLines, data)
			}
		case trimmed == "":
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			if done {
				return finishStream()
			}
		case strings.HasPrefix(trimmed, ":"):
			// SSE comment/heartbeat; ignore.
		}

		if errors.Is(err, io.EOF) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			// [DEBUG] 流 EOF 时打印关键状态，用于诊断截断原因
			log.Printf("[DEBUG-STREAM] EOF reached: done=%v, finishReason=%q, totalRead=%d, toolCallCount=%d",
				done, finishReason, reader.totalRead, len(toolCalls))
			if !done {
				log.Printf("[DEBUG-STREAM] WARNING: stream ended WITHOUT [DONE] marker — treating as interruption")
				if flushErr := flushPendingData(); flushErr != nil {
					return flushErr
				}
				return fmt.Errorf("%w: missing [DONE] marker before EOF", provider.ErrStreamInterrupted)
			}
			if flushErr := flushPendingData(); flushErr != nil {
				return flushErr
			}
			return finishStream()
		}
	}
}

// extractStreamUsage 从 OpenAI usage 响应提取并覆盖累积的 token 统计。
func extractStreamUsage(usage *providertypes.Usage, raw *openAIUsage) {
	if raw == nil {
		return
	}
	*usage = providertypes.Usage{
		InputTokens:  raw.PromptTokens,
		OutputTokens: raw.CompletionTokens,
		TotalTokens:  raw.TotalTokens,
	}
}
