package responses

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestConsumeStreamCompletedWithToolCalls(t *testing.T) {
	t.Parallel()

	sseBody := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"Hello "}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"item_1","delta":"\"README.md\"}"}`,
		"",
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 16)
	err := ConsumeStream(context.Background(), strings.NewReader(sseBody), events)
	if err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	collected := drainStreamEvents(events)
	if len(collected) != 5 {
		t.Fatalf("expected 5 events, got %d", len(collected))
	}

	text, err := collected[0].TextDeltaValue()
	if err != nil || text.Text != "Hello " {
		t.Fatalf("expected text delta event, got err=%v event=%+v", err, collected[0])
	}
	start, err := collected[1].ToolCallStartValue()
	if err != nil || start.Index != 0 || start.ID != "call_1" || start.Name != "read_file" {
		t.Fatalf("expected tool start event, got err=%v event=%+v", err, collected[1])
	}
	firstDelta, err := collected[2].ToolCallDeltaValue()
	if err != nil || firstDelta.ArgumentsDelta != "{\"path\":" {
		t.Fatalf("expected first tool delta, got err=%v event=%+v", err, collected[2])
	}
	secondDelta, err := collected[3].ToolCallDeltaValue()
	if err != nil || secondDelta.ArgumentsDelta != "\"README.md\"}" {
		t.Fatalf("expected second tool delta, got err=%v event=%+v", err, collected[3])
	}
	done, err := collected[4].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done event, got err=%v event=%+v", err, collected[4])
	}
	if done.FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %q", done.FinishReason)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 18 {
		t.Fatalf("expected usage from completed event, got %+v", done.Usage)
	}
}

func TestConsumeStreamToolCallArgumentsAddedDeltaDoneNoDuplicate(t *testing.T) {
	t.Parallel()

	sseBody := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"README.md\"}"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"item_1","delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"README.md\"}"}}`,
		"",
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 16)
	err := ConsumeStream(context.Background(), strings.NewReader(sseBody), events)
	if err != nil {
		t.Fatalf("ConsumeStream() error = %v", err)
	}

	collected := drainStreamEvents(events)
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d", len(collected))
	}

	start, err := collected[0].ToolCallStartValue()
	if err != nil || start.Name != "read_file" {
		t.Fatalf("expected tool start event, got err=%v event=%+v", err, collected[0])
	}
	delta, err := collected[1].ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("expected tool delta event, got err=%v event=%+v", err, collected[1])
	}
	if delta.ArgumentsDelta != `{"path":"README.md"}` {
		t.Fatalf("expected single non-duplicated arguments delta, got %q", delta.ArgumentsDelta)
	}
	if _, err := collected[2].MessageDoneValue(); err != nil {
		t.Fatalf("expected message done event, got err=%v event=%+v", err, collected[2])
	}
}

func TestConsumeStreamIncompleteAndFailures(t *testing.T) {
	t.Parallel()

	t.Run("incomplete maps to length", func(t *testing.T) {
		t.Parallel()

		sseBody := strings.Join([]string{
			`data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")

		events := make(chan providertypes.StreamEvent, 8)
		err := ConsumeStream(context.Background(), strings.NewReader(sseBody), events)
		if err != nil {
			t.Fatalf("ConsumeStream() error = %v", err)
		}
		collected := drainStreamEvents(events)
		if len(collected) != 1 {
			t.Fatalf("expected 1 event, got %d", len(collected))
		}
		done, err := collected[0].MessageDoneValue()
		if err != nil {
			t.Fatalf("expected message done event, got err=%v", err)
		}
		if done.FinishReason != "length" {
			t.Fatalf("expected finish reason length, got %q", done.FinishReason)
		}
	})

	t.Run("failed event returns response error", func(t *testing.T) {
		t.Parallel()

		sseBody := strings.Join([]string{
			`data: {"type":"response.failed","response":{"error":{"message":"upstream failed"}}}`,
			"",
		}, "\n")

		err := ConsumeStream(context.Background(), strings.NewReader(sseBody), make(chan providertypes.StreamEvent, 1))
		if err == nil || !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("expected failed error, got %v", err)
		}
	})
}

func TestConsumeStreamEOFWithoutDoneReturnsInterrupted(t *testing.T) {
	t.Parallel()

	err := ConsumeStream(context.Background(), strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n"), make(chan providertypes.StreamEvent, 2))
	if err == nil {
		t.Fatal("expected interrupted error")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
}

func TestResolveToolCallIndexAndFinishReasonHelpers(t *testing.T) {
	t.Parallel()

	next := 0
	byItem := map[string]int{}
	indexFromItem := resolveToolCallIndex(nil, "item_a", byItem, &next)
	if indexFromItem != 0 {
		t.Fatalf("expected first inferred index 0, got %d", indexFromItem)
	}

	out := 3
	indexFromOutput := resolveToolCallIndex(&out, "item_a", byItem, &next)
	if indexFromOutput != 3 {
		t.Fatalf("expected output index 3, got %d", indexFromOutput)
	}
	if byItem["item_a"] != 3 {
		t.Fatalf("expected item mapping updated to 3, got %d", byItem["item_a"])
	}

	if got := resolveFinishReason("incomplete", &streamResponse{Status: "incomplete", IncompleteDetails: &streamIncompleteDetails{Reason: "content_filter"}}); got != "content_filter" {
		t.Fatalf("expected content_filter, got %q", got)
	}
	if got := resolveFinishReason("completed", &streamResponse{Status: "completed"}); got != "stop" {
		t.Fatalf("expected stop, got %q", got)
	}
}

func drainStreamEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
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
