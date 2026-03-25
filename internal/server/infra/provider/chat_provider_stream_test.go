package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-llm-demo/internal/server/domain"
)

func TestChatProviderToolCallsAggregated(t *testing.T) {
	t.Parallel()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, buildStreamLine("ok", []toolCallDelta{
			{
				Index: 0,
				ID:    "call_1",
				Type:  "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "read",
					Arguments: `{"path":"README`,
				},
			},
		}, ""))
		_, _ = io.WriteString(w, buildStreamLine("", []toolCallDelta{
			{
				Index: 0,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Arguments: `.md"}`,
				},
			},
		}, "tool_calls"))
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)

	tools := []domain.ToolSchema{
		{
			Type: "function",
			Function: domain.ToolFunctionSchema{
				Name: "read",
				Parameters: domain.ToolParametersSchema{
					Type: "object",
					Properties: map[string]domain.ToolParamSchema{
						"path": {Type: "string"},
					},
					Required: []string{"path"},
				},
			},
		},
	}

	provider := &ChatCompletionProvider{
		APIKey:  "test",
		BaseURL: server.URL,
		Model:   "test-model",
	}

	ch, err := provider.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}}, tools)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	events := collectChatEvents(ch)
	if got := requestBody["tool_choice"]; got != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", got)
	}
	if _, ok := requestBody["tools"]; !ok {
		t.Fatal("expected tools in request body")
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != domain.ChatEventDelta || events[0].Content != "ok" {
		t.Fatalf("expected delta event, got %+v", events[0])
	}
	if events[1].Type != domain.ChatEventToolCall || events[1].ToolCall == nil {
		t.Fatalf("expected tool call event, got %+v", events[1])
	}
	if events[1].ToolCall.ID != "call_1" {
		t.Fatalf("expected tool call id call_1, got %q", events[1].ToolCall.ID)
	}
	if events[1].ToolCall.Function.Name != "read" {
		t.Fatalf("expected function read, got %q", events[1].ToolCall.Function.Name)
	}
	if events[1].ToolCall.Function.Arguments != `{"path":"README.md"}` {
		t.Fatalf("expected merged arguments, got %q", events[1].ToolCall.Function.Arguments)
	}
}

func TestChatProviderToolCallsOrdered(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, buildStreamLine("", []toolCallDelta{
			{
				Index: 1,
				ID:    "call_2",
				Type:  "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "write",
					Arguments: `{"path":"b.txt"}`,
				},
			},
		}, ""))
		_, _ = io.WriteString(w, buildStreamLine("", []toolCallDelta{
			{
				Index: 0,
				ID:    "call_1",
				Type:  "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "read",
					Arguments: `{"path":"a.txt"}`,
				},
			},
		}, "tool_calls"))
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)

	provider := &ChatCompletionProvider{
		APIKey:  "test",
		BaseURL: server.URL,
		Model:   "test-model",
	}

	ch, err := provider.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	events := collectChatEvents(ch)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].ToolCall == nil || events[1].ToolCall == nil {
		t.Fatalf("expected tool call events, got %+v", events)
	}
	if events[0].ToolCall.Function.Name != "read" || events[0].ToolCall.ID != "call_1" {
		t.Fatalf("expected first call to be read call_1, got %+v", events[0].ToolCall)
	}
	if events[1].ToolCall.Function.Name != "write" || events[1].ToolCall.ID != "call_2" {
		t.Fatalf("expected second call to be write call_2, got %+v", events[1].ToolCall)
	}
}

func buildStreamLine(content string, deltas []toolCallDelta, finishReason string) string {
	payload := map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"content":    content,
					"tool_calls": deltas,
				},
				"finish_reason": finishReason,
			},
		},
	}
	data, _ := json.Marshal(payload)
	return "data: " + string(data) + "\n"
}

func collectChatEvents(ch <-chan domain.ChatEvent) []domain.ChatEvent {
	events := []domain.ChatEvent{}
	for event := range ch {
		events = append(events, event)
	}
	return events
}
