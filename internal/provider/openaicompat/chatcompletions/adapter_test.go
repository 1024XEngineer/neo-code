package chatcompletions

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestConsumeStreamSupportsWeakSSEFormat(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 4)
	if err := ConsumeStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	drained := drainEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected 2 events, got %d", len(drained))
	}
	text, err := drained[0].TextDeltaValue()
	if err != nil || text.Text != "ok" {
		t.Fatalf("expected text delta 'ok', got err=%v event=%+v", err, drained[0])
	}
	done, err := drained[1].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done, got err=%v", err)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected usage: %+v", done.Usage)
	}
	if !done.Usage.InputObserved || !done.Usage.OutputObserved {
		t.Fatalf("expected usage observed flags to be true, got %+v", done.Usage)
	}
}

func TestConsumeStreamEmitsNilUsageWhenProviderDidNotReturnUsage(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 4)
	if err := ConsumeStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	drained := drainEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected 2 events, got %d", len(drained))
	}
	done, err := drained[1].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done, got err=%v", err)
	}
	if done.Usage != nil {
		t.Fatalf("expected nil usage when stream carries no usage, got %+v", done.Usage)
	}
}

func TestConsumeStreamParsesMultilineDataEvent(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file"`,
		`data: ,"arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"stop"}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 8)
	if err := ConsumeStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	drained := drainEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d (%+v)", len(drained), drained)
	}
	if _, err := drained[0].ToolCallStartValue(); err != nil {
		t.Fatalf("expected tool call start, got err=%v", err)
	}
	delta, err := drained[1].ToolCallDeltaValue()
	if err != nil || !strings.Contains(delta.ArgumentsDelta, "README.md") {
		t.Fatalf("expected tool call delta, got err=%v event=%+v", err, drained[1])
	}
}

func TestConsumeStreamEmitsThinkingDeltaFromReasoningFields(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"plan","content":"answer"}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"fallback reasoning"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 8)
	if err := ConsumeStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	drained := drainEvents(events)
	if len(drained) != 4 {
		t.Fatalf("expected 4 events, got %d (%+v)", len(drained), drained)
	}
	first, err := drained[0].ThinkingDeltaValue()
	if err != nil || first.Text != "plan" {
		t.Fatalf("expected reasoning_content thinking delta, got err=%v event=%+v", err, drained[0])
	}
	text, err := drained[1].TextDeltaValue()
	if err != nil || text.Text != "answer" {
		t.Fatalf("expected text delta, got err=%v event=%+v", err, drained[1])
	}
	second, err := drained[2].ThinkingDeltaValue()
	if err != nil || second.Text != "fallback reasoning" {
		t.Fatalf("expected reasoning fallback thinking delta, got err=%v event=%+v", err, drained[2])
	}
}

func TestConsumeStreamEOFWithoutDoneAndWithoutFinishReason(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 4)
	err := ConsumeStream(context.Background(), strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n"), events)
	if err == nil {
		t.Fatal("expected stream interruption error")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
}

func TestConsumeStreamReturnsImmediatelyAfterDoneWithoutEventSeparator(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	events := make(chan providertypes.StreamEvent, 4)
	result := make(chan error, 1)

	go func() {
		result <- ConsumeStream(context.Background(), reader, events)
	}()

	_, writeErr := io.WriteString(
		writer,
		"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n"+
			"data: [DONE]\n",
	)
	if writeErr != nil {
		t.Fatalf("write stream payload failed: %v", writeErr)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("ConsumeStream() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ConsumeStream should return immediately after [DONE]")
	}

	_ = writer.Close()
}

func drainEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	out := make([]providertypes.StreamEvent, 0, len(events))
	for {
		select {
		case evt := <-events:
			out = append(out, evt)
		default:
			return out
		}
	}
}

func TestEmitFromSDKStream(t *testing.T) {
	t.Parallel()

	stream := &fakeSDKStream{
		chunks: []openai.ChatCompletionChunk{
			{
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Delta: openai.ChatCompletionChunkChoiceDelta{
							Content: "hello",
							ToolCalls: []openai.ChatCompletionChunkChoiceDeltaToolCall{
								{
									Index: 0,
									ID:    "call_1",
									Function: openai.ChatCompletionChunkChoiceDeltaToolCallFunction{
										Name:      "read_file",
										Arguments: `{"path":"README.md"}`,
									},
								},
							},
						},
						FinishReason: "",
					},
				},
			},
			{
				Usage: openai.CompletionUsage{
					PromptTokens:     1,
					CompletionTokens: 2,
					TotalTokens:      3,
				},
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Delta:        openai.ChatCompletionChunkChoiceDelta{},
						FinishReason: "stop",
					},
				},
			},
		},
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := EmitFromSDKStream(context.Background(), stream, events); err != nil {
		t.Fatalf("EmitFromSDKStream() error = %v", err)
	}

	drained := drainEvents(events)
	if len(drained) != 4 {
		t.Fatalf("expected 4 events, got %d (%+v)", len(drained), drained)
	}
	if _, err := drained[0].TextDeltaValue(); err != nil {
		t.Fatalf("expected text delta event, got err=%v", err)
	}
	if _, err := drained[1].ToolCallStartValue(); err != nil {
		t.Fatalf("expected tool call start event, got err=%v", err)
	}
	if _, err := drained[2].ToolCallDeltaValue(); err != nil {
		t.Fatalf("expected tool call delta event, got err=%v", err)
	}
	done, err := drained[3].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done event, got err=%v", err)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 3 {
		t.Fatalf("expected usage total tokens 3, got %+v", done.Usage)
	}
	if !done.Usage.InputObserved || !done.Usage.OutputObserved {
		t.Fatalf("expected usage observed flags to be true, got %+v", done.Usage)
	}
}

func TestEmitFromSDKStreamErrors(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 4)
	if err := EmitFromSDKStream(context.Background(), struct{}{}, events); err == nil {
		t.Fatal("expected invalid stream type error")
	}

	errStream := &fakeSDKStream{err: errors.New("decode failed")}
	if err := EmitFromSDKStream(context.Background(), errStream, events); err == nil || !strings.Contains(err.Error(), "SDK stream error") {
		t.Fatalf("expected SDK stream error, got %v", err)
	}
}

type fakeSDKStream struct {
	chunks []openai.ChatCompletionChunk
	index  int
	err    error
}

func (s *fakeSDKStream) Next() bool {
	if s.index >= len(s.chunks) {
		return false
	}
	s.index++
	return true
}

func (s *fakeSDKStream) Current() openai.ChatCompletionChunk {
	if s.index == 0 {
		return openai.ChatCompletionChunk{}
	}
	return s.chunks[s.index-1]
}

func (s *fakeSDKStream) Err() error {
	return s.err
}
