package minimax

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestInjectMiniMaxParams(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"test","messages":[],"stream":true}`)
	result, err := injectMiniMaxParams(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["reasoning_split"] != true {
		t.Fatalf("expected reasoning_split=true, got %v", raw["reasoning_split"])
	}
	if raw["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking=true, got %v", raw["enable_thinking"])
	}
}

func TestExtractThinkContent_WithTags(t *testing.T) {
	t.Parallel()

	content := "Some text <think>internal reasoning here</think> final answer"
	result := ExtractThinkContent(content)
	if result != "internal reasoning here" {
		t.Fatalf("expected 'internal reasoning here', got %q", result)
	}
}

func TestExtractThinkContent_MultipleTags(t *testing.T) {
	t.Parallel()

	content := "<think>first thought</think> action <think>second thought</think> done"
	result := ExtractThinkContent(content)
	if result != "first thought\nsecond thought" {
		t.Fatalf("expected 'first thought\\nsecond thought', got %q", result)
	}
}

func TestExtractThinkContent_NoTags(t *testing.T) {
	t.Parallel()

	result := ExtractThinkContent("plain text without tags")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestConsumeMiniMaxStream(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_details":"internal plan","content":"visible answer"}}],"usage":{"total_tokens":9}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 4)
	if err := ConsumeMiniMaxStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeMiniMaxStream() error = %v", err)
	}

	drained := drainMiniMaxEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d (%+v)", len(drained), drained)
	}
	thinking, err := drained[0].ThinkingDeltaValue()
	if err != nil || thinking.Text != "internal plan" {
		t.Fatalf("expected thinking delta, got err=%v event=%+v", err, drained[0])
	}
	text, err := drained[1].TextDeltaValue()
	if err != nil || text.Text != "visible answer" {
		t.Fatalf("expected text delta, got err=%v event=%+v", err, drained[1])
	}
	done, err := drained[2].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done, got err=%v", err)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 9 {
		t.Fatalf("unexpected usage payload: %+v", done.Usage)
	}
}

func TestConsumeMiniMaxStreamExtractsThinkTagsFromContent(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"<think>internal plan</think>visible answer"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 4)
	if err := ConsumeMiniMaxStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("ConsumeMiniMaxStream() error = %v", err)
	}

	drained := drainMiniMaxEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d (%+v)", len(drained), drained)
	}
	thinking, err := drained[0].ThinkingDeltaValue()
	if err != nil || thinking.Text != "internal plan" {
		t.Fatalf("expected extracted think tag, got err=%v event=%+v", err, drained[0])
	}
	text, err := drained[1].TextDeltaValue()
	if err != nil || text.Text != "visible answer" {
		t.Fatalf("expected think tags removed from text, got err=%v event=%+v", err, drained[1])
	}
}

func drainMiniMaxEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	drained := make([]providertypes.StreamEvent, 0, len(events))
	for {
		select {
		case event := <-events:
			drained = append(drained, event)
		default:
			return drained
		}
	}
}
