package chatcompletions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	openai "github.com/openai/openai-go/v3"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// EmitFromSDKStream 消费 OpenAI SDK 的 typed stream 并发出统一流式事件。
func EmitFromSDKStream(
	ctx context.Context,
	stream any,
	events chan<- providertypes.StreamEvent,
) error {
	var (
		finishReason string
		usage        providertypes.Usage
		toolCalls    = make(map[int]*providertypes.ToolCall)
	)

	// Since we cannot easily reference the generic Stream type here without importing internal pkgs,
	// we use a more dynamic approach or just accept the type from the caller.
	// In Go, we can't easily iterate over a generic type passed as any without reflection or a specific interface.
	// But the SDK's stream has Next(), Current(), and Err() methods.

	type StreamScanner interface {
		Next() bool
		Current() openai.ChatCompletionChunk
		Err() error
	}

	typedStream, ok := stream.(StreamScanner)
	if !ok {
		return fmt.Errorf("invalid stream type: %T", stream)
	}

	for typedStream.Next() {
		chunk := typedStream.Current()

		// In v3 SDK, Usage is a struct, we check if it's non-zero.
		if chunk.Usage.TotalTokens > 0 {
			extractStreamUsage(&usage, chunk.Usage)
		}

		for _, choice := range chunk.Choices {
			if string(choice.FinishReason) != "" {
				finishReason = string(choice.FinishReason)
			}
			if choice.Delta.Content != "" {
				if err := provider.EmitTextDelta(ctx, events, choice.Delta.Content); err != nil {
					return err
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				if err := mergeToolCallDeltaFromSDK(ctx, events, toolCalls, delta); err != nil {
					return err
				}
			}
		}
	}

	if err := typedStream.Err(); err != nil {
		return fmt.Errorf("SDK stream error: %w", err)
	}

	return provider.EmitMessageDone(ctx, events, finishReason, &usage)
}

const (
	maxSSELineSize        = 256 * 1024
	maxSSEStreamTotalSize = 10 << 20
)

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                                          `json:"content,omitempty"`
			ToolCalls []openai.ChatCompletionChunkChoiceDeltaToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *streamUsage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ConsumeStream 消费 chat/completions SSE 流并发出统一流式事件，兼容弱 SSE 事件分隔格式。
func ConsumeStream(
	ctx context.Context,
	body io.Reader,
	events chan<- providertypes.StreamEvent,
) error {
	reader := newBoundedLineReader(body, maxSSELineSize, maxSSEStreamTotalSize)

	var (
		finishReason string
		usage        providertypes.Usage
		done         bool
		toolCalls    = make(map[int]*providertypes.ToolCall)
		dataLines    []string
	)

	processPayload := func(payload string) error {
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return fmt.Errorf("openaicompat provider: decode stream chunk: %w", err)
		}

		if chunk.Error != nil && strings.TrimSpace(chunk.Error.Message) != "" {
			return errors.New(strings.TrimSpace(chunk.Error.Message))
		}
		extractLegacyStreamUsage(&usage, chunk.Usage)

		for _, choice := range chunk.Choices {
			if strings.TrimSpace(choice.FinishReason) != "" {
				finishReason = strings.TrimSpace(choice.FinishReason)
			}
			if err := provider.EmitTextDelta(ctx, events, choice.Delta.Content); err != nil {
				return err
			}
			for _, delta := range choice.Delta.ToolCalls {
				if err := mergeToolCallDeltaFromSDK(ctx, events, toolCalls, delta); err != nil {
					return err
				}
			}
		}
		return nil
	}

	flushDataLines := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		lines := append([]string(nil), dataLines...)
		defer func() {
			dataLines = dataLines[:0]
		}()
		joined := strings.Join(lines, "\n")
		if err := processPayload(joined); err != nil {
			if len(lines) <= 1 {
				return err
			}
			for _, line := range lines {
				if itemErr := processPayload(line); itemErr != nil {
					return err
				}
			}
		}
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadLine()
		if err != nil && !errors.Is(err, io.EOF) {
			if done {
				if flushErr := flushDataLines(); flushErr != nil {
					return flushErr
				}
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			}
			if flushErr := flushDataLines(); flushErr != nil {
				return flushErr
			}
			if strings.TrimSpace(finishReason) != "" {
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			}
			return fmt.Errorf("%w: %w", provider.ErrStreamInterrupted, err)
		}

		switch {
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:]
			}
			data = strings.TrimRight(data, "\r")
			if strings.TrimSpace(data) == "[DONE]" {
				if flushErr := flushDataLines(); flushErr != nil {
					return flushErr
				}
				done = true
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			} else {
				dataLines = append(dataLines, data)
			}
		case line == "":
			if flushErr := flushDataLines(); flushErr != nil {
				return flushErr
			}
			if done {
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			}
		default:
			if len(dataLines) == 0 {
				break
			}
			if flushErr := flushDataLines(); flushErr != nil {
				return flushErr
			}
			if done {
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			}
		}

		if errors.Is(err, io.EOF) {
			if flushErr := flushDataLines(); flushErr != nil {
				return flushErr
			}
			if done || strings.TrimSpace(finishReason) != "" {
				return provider.EmitMessageDone(ctx, events, finishReason, &usage)
			}
			return fmt.Errorf("%w: missing [DONE] marker before EOF", provider.ErrStreamInterrupted)
		}
	}
}

// extractLegacyStreamUsage 将弱 SSE 解析路径中的 usage 覆盖到统一 token 统计。
func extractLegacyStreamUsage(usage *providertypes.Usage, raw *streamUsage) {
	if raw == nil {
		return
	}
	*usage = providertypes.Usage{
		InputTokens:  raw.PromptTokens,
		OutputTokens: raw.CompletionTokens,
		TotalTokens:  raw.TotalTokens,
	}
}

// extractStreamUsage 将 OpenAI usage 覆盖到统一 token 统计。
func extractStreamUsage(usage *providertypes.Usage, raw openai.CompletionUsage) {
	*usage = providertypes.Usage{
		InputTokens:  int(raw.PromptTokens),
		OutputTokens: int(raw.CompletionTokens),
		TotalTokens:  int(raw.TotalTokens),
	}
}

// mergeToolCallDeltaFromSDK 将单个 SDK tool call 增量合并到累积状态，并在必要时发出起始/增量事件。
func mergeToolCallDeltaFromSDK(
	ctx context.Context,
	events chan<- providertypes.StreamEvent,
	toolCalls map[int]*providertypes.ToolCall,
	delta openai.ChatCompletionChunkChoiceDeltaToolCall,
) error {
	index := int(delta.Index)
	call, exists := toolCalls[index]
	if !exists {
		call = &providertypes.ToolCall{}
		toolCalls[index] = call
	}

	hadName := strings.TrimSpace(call.Name) != ""
	if id := strings.TrimSpace(delta.ID); id != "" {
		call.ID = id
	}
	if name := strings.TrimSpace(delta.Function.Name); name != "" {
		call.Name = name
	}

	if !hadName && strings.TrimSpace(call.Name) != "" {
		if err := provider.EmitToolCallStart(ctx, events, index, call.ID, call.Name); err != nil {
			return err
		}
	}

	if args := delta.Function.Arguments; args != "" {
		call.Arguments += args
		if err := provider.EmitToolCallDelta(ctx, events, index, call.ID, args); err != nil {
			return err
		}
	}
	return nil
}

type boundedLineReader struct {
	reader             *bufio.Reader
	totalRead          int64
	maxLineSize        int
	maxStreamTotalSize int64
}

// newBoundedLineReader 创建带有单行/总量限制的 SSE 读取器。
func newBoundedLineReader(r io.Reader, maxLineSize int, maxStreamTotalSize int64) *boundedLineReader {
	if maxLineSize <= 0 {
		maxLineSize = maxSSELineSize
	}
	if maxStreamTotalSize <= 0 {
		maxStreamTotalSize = maxSSEStreamTotalSize
	}
	return &boundedLineReader{
		reader:             bufio.NewReaderSize(r, maxLineSize+1),
		maxLineSize:        maxLineSize,
		maxStreamTotalSize: maxStreamTotalSize,
	}
}

// ReadLine 读取单行并执行长度限制，返回值不包含行尾换行符。
func (r *boundedLineReader) ReadLine() (string, error) {
	line, err := r.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return "", provider.ErrLineTooLong
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	rawLen := len(line)
	if rawLen > 0 && line[rawLen-1] == '\n' {
		rawLen--
	}
	if rawLen > r.maxLineSize {
		return "", provider.ErrLineTooLong
	}

	r.totalRead += int64(len(line))
	if r.totalRead > r.maxStreamTotalSize {
		return "", provider.ErrStreamTooLarge
	}
	return trimLineEnding(string(line)), err
}

// trimLineEnding 去除行尾连续的 CR/LF 字符。
func trimLineEnding(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
