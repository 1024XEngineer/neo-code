package chatcompletions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestConsumeStreamEmitsTextToolAndDone(t *testing.T) {
	t.Parallel()

	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"Hi "}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"README.md\"}"}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 16)
	err := ConsumeStream(context.Background(), strings.NewReader(sseBody), events)
	if err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	collected := drainChatEvents(events)
	if len(collected) != 5 {
		t.Fatalf("expected 5 events, got %d", len(collected))
	}
	text, err := collected[0].TextDeltaValue()
	if err != nil || text.Text != "Hi " {
		t.Fatalf("expected text delta event, got err=%v event=%+v", err, collected[0])
	}
	start, err := collected[1].ToolCallStartValue()
	if err != nil || start.Index != 0 || start.ID != "call_1" || start.Name != "read_file" {
		t.Fatalf("expected tool start event, got err=%v event=%+v", err, collected[1])
	}
	delta1, err := collected[2].ToolCallDeltaValue()
	if err != nil || delta1.ArgumentsDelta != "{\"path\":" {
		t.Fatalf("expected first tool delta, got err=%v event=%+v", err, collected[2])
	}
	delta2, err := collected[3].ToolCallDeltaValue()
	if err != nil || delta2.ArgumentsDelta != "\"README.md\"}" {
		t.Fatalf("expected second tool delta, got err=%v event=%+v", err, collected[3])
	}
	done, err := collected[4].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done event, got err=%v", err)
	}
	if done.FinishReason != "stop" {
		t.Fatalf("expected stop finish reason, got %q", done.FinishReason)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 20 {
		t.Fatalf("expected usage tokens in done event, got %+v", done.Usage)
	}
}

func TestConsumeStreamErrorAndEOFBranches(t *testing.T) {
	t.Parallel()

	t.Run("error payload", func(t *testing.T) {
		t.Parallel()

		err := ConsumeStream(context.Background(), strings.NewReader("data: {\"error\":{\"message\":\"bad key\"}}\n\n"), make(chan providertypes.StreamEvent, 2))
		if err == nil || !strings.Contains(err.Error(), "bad key") {
			t.Fatalf("expected error payload propagation, got %v", err)
		}
	})

	t.Run("EOF with finish_reason still emits done", func(t *testing.T) {
		t.Parallel()

		sseBody := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
		events := make(chan providertypes.StreamEvent, 2)
		err := ConsumeStream(context.Background(), strings.NewReader(sseBody), events)
		if err != nil {
			t.Fatalf("expected graceful finish on EOF with finish_reason, got %v", err)
		}
		collected := drainChatEvents(events)
		if len(collected) != 1 {
			t.Fatalf("expected one done event, got %d", len(collected))
		}
		done, err := collected[0].MessageDoneValue()
		if err != nil || done.FinishReason != "stop" {
			t.Fatalf("expected stop done event, got err=%v event=%+v", err, collected[0])
		}
	})

	t.Run("EOF without done marker and finish reason", func(t *testing.T) {
		t.Parallel()

		err := ConsumeStream(context.Background(), strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"), make(chan providertypes.StreamEvent, 2))
		if err == nil {
			t.Fatal("expected interrupted error")
		}
		if !errors.Is(err, provider.ErrStreamInterrupted) {
			t.Fatalf("expected ErrStreamInterrupted, got %v", err)
		}
	})
}

func TestExtractAndMergeHelpers(t *testing.T) {
	t.Parallel()

	usage := providertypes.Usage{InputTokens: 1}
	extractStreamUsage(&usage, &StreamUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7})
	if usage.TotalTokens != 7 || usage.InputTokens != 3 || usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage mapping: %+v", usage)
	}

	events := make(chan providertypes.StreamEvent, 4)
	toolCalls := map[int]*providertypes.ToolCall{}
	if err := mergeToolCallDelta(context.Background(), events, toolCalls, StreamToolCallDelta{
		Index: 0,
		ID:    "call_1",
		Function: FunctionCall{
			Name:      "edit",
			Arguments: "{",
		},
	}); err != nil {
		t.Fatalf("first mergeToolCallDelta() error = %v", err)
	}
	if err := mergeToolCallDelta(context.Background(), events, toolCalls, StreamToolCallDelta{
		Index: 0,
		Function: FunctionCall{
			Arguments: "}",
		},
	}); err != nil {
		t.Fatalf("second mergeToolCallDelta() error = %v", err)
	}
	if toolCalls[0].Arguments != "{}" {
		t.Fatalf("expected accumulated arguments, got %q", toolCalls[0].Arguments)
	}

	collected := drainChatEvents(events)
	if len(collected) != 3 {
		t.Fatalf("expected start+2deltas events, got %d", len(collected))
	}
	if _, err := collected[0].ToolCallStartValue(); err != nil {
		t.Fatalf("expected first event to be tool start, got err=%v", err)
	}
}

func TestExportedStreamHelperWrappers(t *testing.T) {
	t.Parallel()

	usage := providertypes.Usage{}
	ExtractStreamUsage(&usage, &Usage{PromptTokens: 2, CompletionTokens: 5, TotalTokens: 7})
	if usage.InputTokens != 2 || usage.OutputTokens != 5 || usage.TotalTokens != 7 {
		t.Fatalf("unexpected usage after ExtractStreamUsage: %+v", usage)
	}

	events := make(chan providertypes.StreamEvent, 4)
	toolCalls := map[int]*providertypes.ToolCall{}
	err := MergeToolCallDelta(context.Background(), events, toolCalls, ToolCallDelta{
		Index: 1,
		ID:    "call_2",
		Function: FunctionCall{
			Name:      "run",
			Arguments: "{\"cmd\":\"pwd\"}",
		},
	})
	if err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}
	if toolCalls[1] == nil || toolCalls[1].Name != "run" {
		t.Fatalf("expected tool call state to be updated, got %+v", toolCalls[1])
	}

	collected := drainChatEvents(events)
	if len(collected) != 2 {
		t.Fatalf("expected tool start+delta events, got %d", len(collected))
	}
	if _, err := collected[0].ToolCallStartValue(); err != nil {
		t.Fatalf("expected first wrapper event tool start, got %v", err)
	}
}

func drainChatEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	out := make([]providertypes.StreamEvent, 0, len(events))
	for {
		select {
		case ev := <-events:
			out = append(out, ev)
		default:
			return out
		}
	}
}
