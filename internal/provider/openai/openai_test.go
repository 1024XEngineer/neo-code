package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/config"
	domain "neo-code/internal/provider"
)

func TestDriver(t *testing.T) {
	t.Parallel()

	driver := Driver()
	if driver.Name != DriverName {
		t.Fatalf("expected driver name %q, got %q", DriverName, driver.Name)
	}
	if driver.Build == nil {
		t.Fatal("expected Build function to be non-nil")
	}
	if driver.Discover == nil {
		t.Fatal("expected Discover function to be non-nil")
	}
}

func TestWithTransport(t *testing.T) {
	t.Parallel()

	customTransport := &http.Transport{}
	cfg := resolvedConfig("", "")

	provider, err := New(cfg, WithTransport(customTransport))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if provider.client.Transport != customTransport {
		t.Fatal("expected custom transport to be set")
	}
}

func TestDefaultRetryTransport(t *testing.T) {
	t.Parallel()

	transport := defaultRetryTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestDiscoverModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4", "name": "GPT-4"},
				{"id": "gpt-3.5-turbo"},
			},
		})
	}))
	defer server.Close()

	provider, err := New(resolvedConfig(server.URL, ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.client = server.Client()

	models, err := provider.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4" || models[0].Name != "GPT-4" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
}

func TestEmitToolCallDelta(t *testing.T) {
	t.Parallel()

	t.Run("nil events guard", func(t *testing.T) {
		t.Parallel()
		if err := emitToolCallDelta(context.Background(), nil, 0, "args"); err != nil {
			t.Fatalf("expected nil events guard to return nil, got %v", err)
		}
	})

	t.Run("empty arguments guard", func(t *testing.T) {
		t.Parallel()
		events := make(chan domain.StreamEvent, 1)
		if err := emitToolCallDelta(context.Background(), events, 0, ""); err != nil {
			t.Fatalf("expected empty arguments guard to return nil, got %v", err)
		}
		select {
		case <-events:
			t.Fatal("expected no event for empty arguments")
		default:
		}
	})

	t.Run("normal send", func(t *testing.T) {
		t.Parallel()
		events := make(chan domain.StreamEvent, 1)
		if err := emitToolCallDelta(context.Background(), events, 3, `{"path":"main.go"}`); err != nil {
			t.Fatalf("emitToolCallDelta() error = %v", err)
		}
		got := <-events
		if got.Type != domain.StreamEventToolCallDelta || got.ToolCallIndex != 3 || got.ToolArgumentsDelta != `{"path":"main.go"}` {
			t.Fatalf("unexpected event: %+v", got)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := emitToolCallDelta(cancelledCtx, make(chan domain.StreamEvent), 0, "args"); err == nil {
			t.Fatal("expected cancellation error")
		}
	})
}

func TestEmitMessageDone(t *testing.T) {
	t.Parallel()

	t.Run("nil events guard", func(t *testing.T) {
		t.Parallel()
		if err := emitMessageDone(context.Background(), nil, "stop", nil); err != nil {
			t.Fatalf("expected nil events guard to return nil, got %v", err)
		}
	})

	t.Run("normal send", func(t *testing.T) {
		t.Parallel()
		events := make(chan domain.StreamEvent, 1)
		usage := &domain.Usage{TotalTokens: 100}
		if err := emitMessageDone(context.Background(), events, "stop", usage); err != nil {
			t.Fatalf("emitMessageDone() error = %v", err)
		}
		got := <-events
		if got.Type != domain.StreamEventMessageDone || got.FinishReason != "stop" || got.Usage.TotalTokens != 100 {
			t.Fatalf("unexpected event: %+v", got)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := emitMessageDone(cancelledCtx, make(chan domain.StreamEvent), "stop", nil); err == nil {
			t.Fatal("expected cancellation error")
		}
	})
}

func resolvedConfig(baseURL string, model string) config.ResolvedProviderConfig {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = config.OpenAIDefaultBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = config.OpenAIDefaultModel
	}

	return config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{
			Name:      DriverName,
			Driver:    DriverName,
			BaseURL:   baseURL,
			Model:     model,
			APIKeyEnv: config.OpenAIDefaultAPIKeyEnv,
		},
		APIKey: "test-key",
	}
}

func TestProviderChatConsumesSSEAndMergesToolCalls(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}

		var payload chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "gpt-5.4" {
			t.Fatalf("expected model gpt-5.4, got %q", payload.Model)
		}
		if !containsToolRoleMessage(payload.Messages, "call_1", "tool finished") {
			t.Fatalf("expected tool role message with tool_call_id in payload: %+v", payload.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": "Hello ",
					},
				},
			},
		})
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      "filesystem_edit",
									"arguments": `{"path":"main.go",`,
								},
							},
						},
					},
				},
			},
		})
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": "world",
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"function": map[string]any{
									"arguments": `"search_string":"old",`,
								},
							},
						},
					},
				},
			},
		})
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "tool_calls",
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"function": map[string]any{
									"arguments": `"replace_string":"new"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := New(resolvedConfig(server.URL, "gpt-5.4"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.client = server.Client()

	events := make(chan domain.StreamEvent, 8)
	response, err := provider.Chat(context.Background(), domain.ChatRequest{
		Model: "gpt-5.4",
		Messages: []domain.Message{
			{Role: "user", Content: "please edit the file"},
			{
				Role: "assistant",
				ToolCalls: []domain.ToolCall{
					{
						ID:        "call_1",
						Name:      "filesystem_edit",
						Arguments: `{"path":"main.go","search_string":"old","replace_string":"new"}`,
					},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "tool finished"},
		},
		Tools: []domain.ToolSpec{
			{
				Name:        "filesystem_edit",
				Description: "Edit one matching block in a file",
				Schema: map[string]any{
					"type": "object",
				},
			},
		},
	}, events)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if response.Message.Content != "Hello world" {
		t.Fatalf("expected content %q, got %q", "Hello world", response.Message.Content)
	}
	if response.FinishReason != "tool_calls" {
		t.Fatalf("expected finish reason tool_calls, got %q", response.FinishReason)
	}
	if response.Usage.TotalTokens != 15 {
		t.Fatalf("expected total tokens 15, got %d", response.Usage.TotalTokens)
	}
	if len(response.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(response.Message.ToolCalls))
	}

	call := response.Message.ToolCalls[0]
	if call.ID != "call_1" || call.Name != "filesystem_edit" {
		t.Fatalf("unexpected tool call: %+v", call)
	}
	if call.Arguments != `{"path":"main.go","search_string":"old","replace_string":"new"}` {
		t.Fatalf("unexpected merged arguments: %q", call.Arguments)
	}

	var chunks []string
	for {
		select {
		case event := <-events:
			chunks = append(chunks, event.Text)
		default:
			if strings.Join(chunks, "") != "Hello world" {
				t.Fatalf("expected streamed chunks to form %q, got %q", "Hello world", strings.Join(chunks, ""))
			}
			return
		}
	}
}

func TestProviderChatHTTPErrorResponses(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	tests := []struct {
		name      string
		status    int
		body      string
		expectErr string
	}{
		{
			name:      "http 401 json error",
			status:    http.StatusUnauthorized,
			body:      `{"error":{"message":"invalid api key"}}`,
			expectErr: "invalid api key",
		},
		{
			name:      "http 500 empty body falls back to status",
			status:    http.StatusInternalServerError,
			body:      ``,
			expectErr: "500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				if tt.body != "" {
					_, _ = w.Write([]byte(tt.body))
				}
			}))
			defer server.Close()

			provider, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			provider.client = server.Client()

			_, err = provider.Chat(context.Background(), domain.ChatRequest{
				Model: config.OpenAIDefaultModel,
			}, make(chan domain.StreamEvent, 1))
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestBuildRequestIncludesSystemPromptToolsAndToolMessages(t *testing.T) {
	t.Parallel()

	provider, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := provider.buildRequest(domain.ChatRequest{
		SystemPrompt: "system prompt",
		Messages: []domain.Message{
			{Role: "user", Content: "hello"},
			{
				Role: "assistant",
				ToolCalls: []domain.ToolCall{
					{
						ID:        "call_1",
						Name:      "filesystem_edit",
						Arguments: `{"path":"main.go"}`,
					},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "tool finished"},
		},
		Tools: []domain.ToolSpec{
			{
				Name:        "filesystem_edit",
				Description: "Edit file content",
				Schema: map[string]any{
					"type": "object",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}

	if payload.Model != config.OpenAIDefaultModel {
		t.Fatalf("expected default model %q, got %q", config.OpenAIDefaultModel, payload.Model)
	}
	if !payload.Stream {
		t.Fatalf("expected stream=true")
	}
	if payload.ToolChoice != "auto" {
		t.Fatalf("expected tool choice auto, got %q", payload.ToolChoice)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "filesystem_edit" {
		t.Fatalf("unexpected tool payload: %+v", payload.Tools)
	}
	if payload.Messages[0].Role != "system" || payload.Messages[0].Content != "system prompt" {
		t.Fatalf("unexpected system message: %+v", payload.Messages[0])
	}
	if !containsToolRoleMessage(payload.Messages, "call_1", "tool finished") {
		t.Fatalf("expected tool role message, got %+v", payload.Messages)
	}
	if len(payload.Messages[2].ToolCalls) != 1 || payload.Messages[2].ToolCalls[0].Function.Name != "filesystem_edit" {
		t.Fatalf("expected assistant tool call payload, got %+v", payload.Messages[2])
	}
}

func TestParseErrorAndEmitTextDelta(t *testing.T) {
	t.Parallel()

	provider, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name      string
		status    string
		body      string
		expectErr string
	}{
		{
			name:      "json error payload",
			status:    "400 Bad Request",
			body:      `{"error":{"message":"invalid request"}}`,
			expectErr: "invalid request",
		},
		{
			name:      "plain text fallback",
			status:    "502 Bad Gateway",
			body:      `gateway timeout`,
			expectErr: "gateway timeout",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{
				Status: tt.status,
				Body:   ioNopCloser(tt.body),
			}
			err := provider.parseError(resp)
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}

	eventCh := make(chan domain.StreamEvent, 1)
	if err := emitTextDelta(context.Background(), eventCh, "chunk"); err != nil {
		t.Fatalf("emitTextDelta() error = %v", err)
	}
	if got := <-eventCh; got.Text != "chunk" || got.Type != domain.StreamEventTextDelta {
		t.Fatalf("unexpected stream event: %+v", got)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := emitTextDelta(cancelledCtx, make(chan domain.StreamEvent), "chunk"); err == nil {
		t.Fatalf("expected cancellation error")
	}
}

func TestProviderConsumeStreamRejectsDirtyJSON(t *testing.T) {
	t.Parallel()

	provider, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = provider.consumeStream(context.Background(), strings.NewReader("data: {not-json}\n\n"), make(chan domain.StreamEvent, 1))
	if err == nil || !strings.Contains(err.Error(), "decode stream chunk") {
		t.Fatalf("expected dirty JSON decode error, got %v", err)
	}
}

func containsToolRoleMessage(messages []openAIMessage, toolCallID string, content string) bool {
	for _, message := range messages {
		if message.Role == "tool" && message.ToolCallID == toolCallID && message.Content == content {
			return true
		}
	}
	return false
}

func writeSSEChunk(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal SSE payload: %v", err)
	}
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		t.Fatalf("write SSE payload: %v", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func ioNopCloser(body string) *readCloser {
	return &readCloser{Reader: strings.NewReader(body)}
}

type readCloser struct {
	*strings.Reader
}

func (r *readCloser) Close() error {
	return nil
}

// --- emitToolCallStart 边界测试 ---

func TestEmitToolCallStartGuards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// nil events 守卫
	if err := emitToolCallStart(ctx, nil, 0, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("expected nil events guard to return nil, got %v", err)
	}

	// 空 name 守卫
	events := make(chan domain.StreamEvent, 1)
	if err := emitToolCallStart(ctx, events, 0, "call-1", ""); err != nil {
		t.Fatalf("expected empty name guard to return nil, got %v", err)
	}
	select {
	case <-events:
		t.Fatalf("expected no event for empty name")
	default:
	}

	// 正常发送
	if err := emitToolCallStart(ctx, events, 2, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("emitToolCallStart() error = %v", err)
	}
	got := <-events
	if got.Type != domain.StreamEventToolCallStart || got.ToolName != "filesystem_edit" || got.ToolCallID != "call-1" || got.ToolCallIndex != 2 {
		t.Fatalf("unexpected event: %+v", got)
	}

	// context 取消
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := emitToolCallStart(cancelledCtx, make(chan domain.StreamEvent), 0, "call-1", "filesystem_edit"); err == nil {
		t.Fatalf("expected cancellation error")
	}
}

func TestMergeToolCallDeltaEmitsStartWhenNameArrivesLater(t *testing.T) {
	t.Parallel()

	events := make(chan domain.StreamEvent, 4)
	toolCalls := make(map[int]*domain.ToolCall)

	if err := mergeToolCallDelta(context.Background(), events, toolCalls, toolCallDelta{
		Index: 0,
		ID:    "call_late_name",
	}); err != nil {
		t.Fatalf("mergeToolCallDelta() first delta error = %v", err)
	}

	select {
	case evt := <-events:
		t.Fatalf("expected no event before tool name arrives, got %+v", evt)
	default:
	}

	if err := mergeToolCallDelta(context.Background(), events, toolCalls, toolCallDelta{
		Index: 0,
		Function: openAIFunctionCall{
			Name:      "filesystem_edit",
			Arguments: `{"path":"main.go"}`,
		},
	}); err != nil {
		t.Fatalf("mergeToolCallDelta() late-name delta error = %v", err)
	}

	start := <-events
	if start.Type != domain.StreamEventToolCallStart {
		t.Fatalf("expected tool_call_start event, got %+v", start)
	}
	if start.ToolCallID != "call_late_name" || start.ToolName != "filesystem_edit" {
		t.Fatalf("unexpected tool_call_start payload: %+v", start)
	}

	delta := <-events
	if delta.Type != domain.StreamEventToolCallDelta {
		t.Fatalf("expected tool_call_delta event, got %+v", delta)
	}
	if delta.ToolArgumentsDelta != `{"path":"main.go"}` {
		t.Fatalf("unexpected tool arguments delta: %+v", delta)
	}

	call := toolCalls[0]
	if call == nil {
		t.Fatal("expected tool call to be accumulated")
	}
	if call.ID != "call_late_name" || call.Name != "filesystem_edit" || call.Arguments != `{"path":"main.go"}` {
		t.Fatalf("unexpected accumulated tool call: %+v", call)
	}
}

func TestProviderChatEmitsToolCallStartEvent(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_tool",
								"type":  "function",
								"function": map[string]any{
									"name":      "filesystem_edit",
									"arguments": `{}`,
								},
							},
						},
					},
				},
			},
		})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.client = server.Client()

	events := make(chan domain.StreamEvent, 8)
	_, err = provider.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "edit"}},
		Tools: []domain.ToolSpec{
			{Name: "filesystem_edit", Description: "edit", Schema: map[string]any{"type": "object"}},
		},
	}, events)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	close(events)

	var foundToolCallStart bool
	for evt := range events {
		if evt.Type == domain.StreamEventToolCallStart {
			foundToolCallStart = true
			if evt.ToolName != "filesystem_edit" {
				t.Fatalf("expected ToolName %q, got %q", "filesystem_edit", evt.ToolName)
			}
			if evt.ToolCallID != "call_tool" {
				t.Fatalf("expected ToolCallID %q, got %q", "call_tool", evt.ToolCallID)
			}
		}
	}
	if !foundToolCallStart {
		t.Fatalf("expected StreamEventToolCallStart event in stream")
	}
}

// TestProviderChatEmitsFullEventStream 测试完整的事件流（包括 tool_call_delta 和 message_done）。
func TestProviderChatEmitsFullEventStream(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// 发送文本 delta
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": "Hello",
					},
				},
			},
		})

		// 发送 tool call start 和 delta
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"id":    "call_tool_1",
								"type":  "function",
								"function": map[string]any{
									"name":      "filesystem_edit",
									"arguments": `{"path":"a.`,
								},
							},
						},
					},
				},
			},
		})

		// 发送 tool call delta（参数增量）
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							{
								"index": 0,
								"function": map[string]any{
									"arguments": `go"}`,
								},
							},
						},
					},
				},
			},
		})

		// 发送 usage 和 finish_reason
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 50,
				"total_tokens":      150,
			},
		})

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.client = server.Client()

	events := make(chan domain.StreamEvent, 16)
	_, err = provider.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "test"}},
	}, events)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	close(events)

	var (
		foundTextDelta       bool
		foundToolCallStart   bool
		foundToolCallDelta   bool
		foundMessageDone     bool
		toolCallDeltaContent string
		messageDoneEvt       *domain.StreamEvent
	)

	for evt := range events {
		switch evt.Type {
		case domain.StreamEventTextDelta:
			foundTextDelta = true
		case domain.StreamEventToolCallStart:
			foundToolCallStart = true
			if evt.ToolName != "filesystem_edit" {
				t.Fatalf("expected ToolName %q, got %q", "filesystem_edit", evt.ToolName)
			}
			if evt.ToolCallIndex != 0 {
				t.Fatalf("expected ToolCallIndex %d for tool_call_start, got %d", 0, evt.ToolCallIndex)
			}
		case domain.StreamEventToolCallDelta:
			foundToolCallDelta = true
			toolCallDeltaContent += evt.ToolArgumentsDelta
		case domain.StreamEventMessageDone:
			foundMessageDone = true
			messageDoneEvt = &evt
		}
	}

	if !foundTextDelta {
		t.Fatal("expected StreamEventTextDelta event")
	}
	if !foundToolCallStart {
		t.Fatal("expected StreamEventToolCallStart event")
	}
	if !foundToolCallDelta {
		t.Fatal("expected StreamEventToolCallDelta event")
	}
	if !foundMessageDone {
		t.Fatal("expected StreamEventMessageDone event")
	}

	// 验证 tool_call_delta 内容被正确累加
	expectedDelta := `{"path":"a.go"}`
	if toolCallDeltaContent != expectedDelta {
		t.Fatalf("expected tool call delta content %q, got %q", expectedDelta, toolCallDeltaContent)
	}

	// 验证 message_done 事件包含正确的字段
	if messageDoneEvt == nil {
		t.Fatal("message_done event is nil")
	}
	if messageDoneEvt.FinishReason != "tool_calls" {
		t.Fatalf("expected FinishReason %q, got %q", "tool_calls", messageDoneEvt.FinishReason)
	}
	if messageDoneEvt.Usage == nil {
		t.Fatal("expected Usage in message_done event")
	}
	if messageDoneEvt.Usage.TotalTokens != 150 {
		t.Fatalf("expected TotalTokens %d, got %d", 150, messageDoneEvt.Usage.TotalTokens)
	}
}
