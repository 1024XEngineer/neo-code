package responses

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestEmitFromStreamSupportsMultilineSSEData(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta",`,
		`data: "delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 6)
	if err := EmitFromStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("EmitFromStream() error = %v", err)
	}

	drained := drainResponseEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected 2 events, got %d (%+v)", len(drained), drained)
	}
	text, err := drained[0].TextDeltaValue()
	if err != nil || text.Text != "hello" {
		t.Fatalf("expected text delta hello, got err=%v event=%+v", err, drained[0])
	}
	done, err := drained[1].MessageDoneValue()
	if err != nil {
		t.Fatalf("expected message done event, got err=%v", err)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected usage in done event: %+v", done.Usage)
	}
}

func TestEmitFromStreamSupportsLongDataLine(t *testing.T) {
	t.Parallel()

	largeDelta := strings.Repeat("a", 70*1024)
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.output_text.delta","delta":"%s"}`,
			largeDelta,
		),
		"",
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	events := make(chan providertypes.StreamEvent, 6)
	if err := EmitFromStream(context.Background(), strings.NewReader(body), events); err != nil {
		t.Fatalf("EmitFromStream() long line error = %v", err)
	}

	drained := drainResponseEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected 2 events, got %d", len(drained))
	}
	text, err := drained[0].TextDeltaValue()
	if err != nil {
		t.Fatalf("expected text delta event, got err=%v", err)
	}
	if len(text.Text) != len(largeDelta) {
		t.Fatalf("expected delta length %d, got %d", len(largeDelta), len(text.Text))
	}
}

func TestEmitFromStreamReturnsInterruptedWithoutCompletionMarker(t *testing.T) {
	t.Parallel()

	body := `data: {"type":"response.output_text.delta","delta":"hello"}`
	events := make(chan providertypes.StreamEvent, 4)
	err := EmitFromStream(context.Background(), strings.NewReader(body), events)
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
}

func TestEmitFromStreamReturnsAfterCompletedEventWithoutDoneMarker(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	events := make(chan providertypes.StreamEvent, 4)
	result := make(chan error, 1)

	go func() {
		result <- EmitFromStream(context.Background(), reader, events)
	}()

	_, writeErr := io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	if writeErr != nil {
		t.Fatalf("write stream payload failed: %v", writeErr)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("EmitFromStream() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EmitFromStream should return after response.completed")
	}

	_ = writer.Close()
}

func TestEmitFromStreamReturnsAfterCompletedEventFollowedByKeepaliveLine(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	events := make(chan providertypes.StreamEvent, 4)
	result := make(chan error, 1)

	go func() {
		result <- EmitFromStream(context.Background(), reader, events)
	}()

	_, writeErr := io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n: keep-alive\n")
	if writeErr != nil {
		t.Fatalf("write stream payload failed: %v", writeErr)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("EmitFromStream() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EmitFromStream should return after response.completed")
	}

	_ = writer.Close()
}

func drainResponseEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
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

func TestProcessEventAndToolCallMerge(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 8)
	usage := providertypes.Usage{}
	finishReason := ""
	done := false
	toolCalls := make(map[int]*providertypes.ToolCall)
	itemToIndex := make(map[string]int)
	slot := 0

	err := processEvent(context.Background(), events, &streamEvent{
		Type: "response.output_item.done",
		Item: &streamEventItem{
			Type:      "function_call",
			ID:        "item_1",
			CallID:    "call_1",
			Name:      "read_file",
			Arguments: `{"path":"README.md"}`,
		},
	}, &usage, &finishReason, &done, toolCalls, itemToIndex, &slot)
	if err != nil {
		t.Fatalf("processEvent(function_call) error = %v", err)
	}

	err = processEvent(context.Background(), events, &streamEvent{
		Type:     "response.completed",
		Response: &streamResponse{Status: "completed", Usage: &streamUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}},
	}, &usage, &finishReason, &done, toolCalls, itemToIndex, &slot)
	if err != nil {
		t.Fatalf("processEvent(completed) error = %v", err)
	}

	drained := drainResponseEvents(events)
	if len(drained) != 2 {
		t.Fatalf("expected 2 events, got %d (%+v)", len(drained), drained)
	}
	start, err := drained[0].ToolCallStartValue()
	if err != nil || start.Name != "read_file" {
		t.Fatalf("expected tool call start read_file, got err=%v event=%+v", err, drained[0])
	}
	delta, err := drained[1].ToolCallDeltaValue()
	if err != nil || !strings.Contains(delta.ArgumentsDelta, "README.md") {
		t.Fatalf("expected tool call delta, got err=%v event=%+v", err, drained[1])
	}
	if usage.TotalTokens != 3 {
		t.Fatalf("expected usage total tokens 3, got %d", usage.TotalTokens)
	}
	if finishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %q", finishReason)
	}
	if !done {
		t.Fatal("expected done flag to be true after response.completed")
	}
}

func TestProcessEventErrorBranches(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 2)
	usage := providertypes.Usage{}
	finishReason := ""
	toolCalls := make(map[int]*providertypes.ToolCall)
	itemToIndex := make(map[string]int)
	slot := 0
	done := false

	tests := []struct {
		name    string
		event   *streamEvent
		wantErr string
	}{
		{
			name: "failed response with message",
			event: &streamEvent{
				Type: "response.failed",
				Response: &streamResponse{
					Error: &streamError{Message: "upstream failed"},
				},
			},
			wantErr: "upstream failed",
		},
		{
			name: "failed response fallback message",
			event: &streamEvent{
				Type:     "response.failed",
				Response: &streamResponse{},
			},
			wantErr: "response failed",
		},
		{
			name: "error event fallback message",
			event: &streamEvent{
				Type:  "error",
				Error: &streamError{},
			},
			wantErr: "response stream error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := processEvent(context.Background(), events, tt.event, &usage, &finishReason, &done, toolCalls, itemToIndex, &slot)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestBoundedLineReaderLimits(t *testing.T) {
	t.Parallel()

	lineTooLong := newBoundedLineReader(strings.NewReader("123456\n"), 3, 1024)
	if _, err := lineTooLong.ReadLine(); !errors.Is(err, provider.ErrLineTooLong) {
		t.Fatalf("expected ErrLineTooLong, got %v", err)
	}

	streamTooLarge := newBoundedLineReader(bytes.NewBufferString("abc\n"), 10, 3)
	if _, err := streamTooLarge.ReadLine(); !errors.Is(err, provider.ErrStreamTooLarge) {
		t.Fatalf("expected ErrStreamTooLarge, got %v", err)
	}

	readerErr := newBoundedLineReader(errorReader{}, 10, 1024)
	if _, err := readerErr.ReadLine(); err == nil || strings.TrimSpace(err.Error()) == "" {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestResolveToolCallIndexAndFinishReason(t *testing.T) {
	t.Parallel()

	idx := 2
	next := 0
	byItem := map[string]int{}
	if got := resolveToolCallIndex(&idx, "item", byItem, &next); got != 2 {
		t.Fatalf("expected output index 2, got %d", got)
	}
	if got := resolveToolCallIndex(nil, "item", byItem, &next); got != 2 {
		t.Fatalf("expected cached item index 2, got %d", got)
	}
	if got := resolveToolCallIndex(nil, "", byItem, &next); got != 0 {
		t.Fatalf("expected incremental index 0, got %d", got)
	}

	if got := resolveFinishReason("incomplete", &streamResponse{
		Status:            "incomplete",
		IncompleteDetails: &streamIncompleteDetails{Reason: "content_filter"},
	}); got != "content_filter" {
		t.Fatalf("expected content_filter, got %q", got)
	}
	if got := resolveFinishReason("unknown", &streamResponse{}); got != "" {
		t.Fatalf("expected empty finish reason, got %q", got)
	}
}

type errorReader struct{}

func (errorReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
