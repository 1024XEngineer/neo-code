package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"neocode/internal/provider"
)

func TestProviderChatParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("expected one tool in request, got %#v", payload["tools"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"content": null,
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "fs_read_file",
							"arguments": "{\"path\":\"README.md\"}"
						}
					}]
				}
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 3,
				"total_tokens": 13
			}
		}`))
	}))
	defer server.Close()

	client := New("openai", server.URL, "test-key", 5*time.Second)
	response, err := client.Chat(context.Background(), provider.ChatRequest{
		Model: "gpt-4.1-mini",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "read README"},
		},
		Tools: []provider.ToolSpec{
			{
				Name:        "fs_read_file",
				Description: "Read a file",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	})
	if err != nil {
		t.Fatalf("chat returned error: %v", err)
	}

	if len(response.Message.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(response.Message.ToolCalls))
	}
	if response.Message.ToolCalls[0].Name != "fs_read_file" {
		t.Fatalf("unexpected tool call name: %s", response.Message.ToolCalls[0].Name)
	}
	if response.Usage.TotalTokens != 13 {
		t.Fatalf("unexpected total tokens: %d", response.Usage.TotalTokens)
	}
}

func TestProviderChatStreamParsesDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("expected flusher")
		}

		writer := bufio.NewWriter(w)
		_, _ = writer.WriteString("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"Hel\"},\"finish_reason\":\"\"}]}\n\n")
		_, _ = writer.WriteString("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = writer.WriteString("data: [DONE]\n\n")
		_ = writer.Flush()
		flusher.Flush()
	}))
	defer server.Close()

	client := New("openai", server.URL, "test-key", 5*time.Second)

	var streamed string
	response, err := client.ChatStream(context.Background(), provider.ChatRequest{
		Model: "gpt-4.1-mini",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Content: "say hello"},
		},
	}, func(delta string) error {
		streamed += delta
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}

	if streamed != "Hello" {
		t.Fatalf("expected streamed content Hello, got %q", streamed)
	}
	if response.Message.Content != "Hello" {
		t.Fatalf("expected final content Hello, got %q", response.Message.Content)
	}
	if response.FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %q", response.FinishReason)
	}
}
