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

func resolvedConfig(baseURL string, model string) config.ResolvedProviderConfig {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}

	return config.ResolvedProviderConfig{
		ProviderConfig: config.ProviderConfig{
			Name:      DriverName,
			Driver:    DriverName,
			BaseURL:   baseURL,
			Model:     model,
			APIKeyEnv: DefaultAPIKeyEnv,
		},
		APIKey: "test-key",
	}
}

func TestMergeToolCallDeltas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deltas     []toolCallDelta
		assert     func(t *testing.T, calls map[int]*domain.ToolCall)
		assertDisc func(t *testing.T, disc []domain.ToolCall)
	}{
		{
			name: "single tool call fragments are merged by index",
			deltas: []toolCallDelta{
				{Index: 0, ID: "call_1", Function: openAIFunctionCall{Name: "filesystem_edit", Arguments: `{"path":"a.go",`}},
				{Index: 0, Function: openAIFunctionCall{Arguments: `"search_string":"old",`}},
				{Index: 0, Function: openAIFunctionCall{Arguments: `"replace_string":"new"}`}},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				call := calls[0]
				if call == nil {
					t.Fatalf("expected call at index 0")
				}
				if call.Name != "filesystem_edit" {
					t.Fatalf("expected name filesystem_edit, got %q", call.Name)
				}
				if call.Arguments != `{"path":"a.go","search_string":"old","replace_string":"new"}` {
					t.Fatalf("unexpected arguments: %q", call.Arguments)
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 1 {
					t.Fatalf("expected 1 discovered tool call, got %d", len(disc))
				}
				if disc[0].ID != "call_1" || disc[0].Name != "filesystem_edit" {
					t.Fatalf("unexpected discovered call: %+v", disc[0])
				}
			},
		},
		{
			name: "multiple indices stay isolated",
			deltas: []toolCallDelta{
				{Index: 0, ID: "call_1", Function: openAIFunctionCall{Name: "filesystem_read_file", Arguments: `{"path":"a.go"}`}},
				{Index: 1, ID: "call_2", Function: openAIFunctionCall{Name: "filesystem_write_file", Arguments: `{"path":"b.go"`}},
				{Index: 1, Function: openAIFunctionCall{Arguments: `,"content":"ok"}`}},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				if len(calls) != 2 {
					t.Fatalf("expected 2 calls, got %d", len(calls))
				}
				if calls[1].Arguments != `{"path":"b.go","content":"ok"}` {
					t.Fatalf("unexpected second arguments: %q", calls[1].Arguments)
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 2 {
					t.Fatalf("expected 2 discovered tool calls, got %d", len(disc))
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := map[int]*domain.ToolCall{}
			discovered := mergeToolCallDeltas(calls, tt.deltas)
			tt.assert(t, calls)
			if tt.assertDisc != nil {
				tt.assertDisc(t, discovered)
			}
		})
	}
}

func TestBuiltinConfigIncludesProviderModels(t *testing.T) {
	t.Parallel()

	cfg := BuiltinConfig()
	if cfg.Name != DriverName {
		t.Fatalf("expected provider name %q, got %q", DriverName, cfg.Name)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", DefaultBaseURL, cfg.BaseURL)
	}
	if len(cfg.Models) < 3 {
		t.Fatalf("expected builtin models to be predeclared, got %+v", cfg.Models)
	}
	if !containsString(cfg.Models, "gpt-5.4") {
		t.Fatalf("expected builtin models to include gpt-5.4, got %+v", cfg.Models)
	}
}

func TestProviderChatConsumesSSEAndMergesToolCalls(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnv, "test-key")

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
	t.Setenv(DefaultAPIKeyEnv, "test-key")

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

			provider, err := New(resolvedConfig(server.URL, DefaultModel))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			provider.client = server.Client()

			_, err = provider.Chat(context.Background(), domain.ChatRequest{
				Model: DefaultModel,
			}, make(chan domain.StreamEvent, 1))
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestBuildRequestIncludesSystemPromptToolsAndToolMessages(t *testing.T) {
	t.Parallel()

	provider, err := New(resolvedConfig(DefaultBaseURL, DefaultModel))
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

	if payload.Model != DefaultModel {
		t.Fatalf("expected default model %q, got %q", DefaultModel, payload.Model)
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

	provider, err := New(resolvedConfig(DefaultBaseURL, DefaultModel))
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

	provider, err := New(resolvedConfig(DefaultBaseURL, DefaultModel))
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

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// --- emitToolCallStart / mergeToolCallDeltas 边界补充测试 ---

func TestEmitToolCallStartGuards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// nil events 守卫
	if err := emitToolCallStart(ctx, nil, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("expected nil events guard to return nil, got %v", err)
	}

	// 空 name 守卫
	events := make(chan domain.StreamEvent, 1)
	if err := emitToolCallStart(ctx, events, "call-1", ""); err != nil {
		t.Fatalf("expected empty name guard to return nil, got %v", err)
	}
	select {
	case <-events:
		t.Fatalf("expected no event for empty name")
	default:
	}

	// 正常发送
	if err := emitToolCallStart(ctx, events, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("emitToolCallStart() error = %v", err)
	}
	got := <-events
	if got.Type != domain.StreamEventToolCallStart || got.ToolName != "filesystem_edit" || got.ToolCallID != "call-1" {
		t.Fatalf("unexpected event: %+v", got)
	}

	// context 取消
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := emitToolCallStart(cancelledCtx, make(chan domain.StreamEvent), "call-1", "filesystem_edit"); err == nil {
		t.Fatalf("expected cancellation error")
	}
}

func TestMergeToolCallDeltasEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deltas     []toolCallDelta
		assert     func(t *testing.T, calls map[int]*domain.ToolCall)
		assertDisc func(t *testing.T, disc []domain.ToolCall)
	}{
		{
			name:   "empty deltas returns empty discovered",
			deltas: []toolCallDelta{},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				if len(calls) != 0 {
					t.Fatalf("expected empty calls, got %d", len(calls))
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 0 {
					t.Fatalf("expected empty discovered, got %d", len(disc))
				}
			},
		},
		{
			name: "only ID without Name is not discovered",
			deltas: []toolCallDelta{
				{Index: 0, ID: "call-1"},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				if calls[0].ID != "call-1" {
					t.Fatalf("expected ID to be set")
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 0 {
					t.Fatalf("expected 0 discovered when only ID, got %d", len(disc))
				}
			},
		},
		{
			name: "only Name without ID is discovered",
			deltas: []toolCallDelta{
				{Index: 0, Function: openAIFunctionCall{Name: "filesystem_edit"}},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				if calls[0].Name != "filesystem_edit" {
					t.Fatalf("expected Name to be set")
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 1 {
					t.Fatalf("expected 1 discovered, got %d", len(disc))
				}
				if disc[0].Name != "filesystem_edit" {
					t.Fatalf("expected discovered Name %q, got %q", "filesystem_edit", disc[0].Name)
				}
			},
		},
		{
			name: "only Arguments without Name is not discovered",
			deltas: []toolCallDelta{
				{Index: 0, Function: openAIFunctionCall{Arguments: `{"path":"x"}`}},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				if calls[0].Arguments != `{"path":"x"}` {
					t.Fatalf("expected Arguments to be set")
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 0 {
					t.Fatalf("expected 0 discovered when only Arguments, got %d", len(disc))
				}
			},
		},
		{
			name: "cumulative calls across multiple rounds",
			deltas: []toolCallDelta{
				{Index: 0, ID: "call-1", Function: openAIFunctionCall{Name: "tool_a"}},
			},
			assert: func(t *testing.T, calls map[int]*domain.ToolCall) {
				t.Helper()
				// 第二轮：添加新工具
				discovered := mergeToolCallDeltas(calls, []toolCallDelta{
					{Index: 1, ID: "call-2", Function: openAIFunctionCall{Name: "tool_b"}},
				})
				if len(discovered) != 1 || discovered[0].Name != "tool_b" {
					t.Fatalf("expected 1 new discovered in second round, got %+v", discovered)
				}
				if len(calls) != 2 {
					t.Fatalf("expected 2 calls after cumulative merge, got %d", len(calls))
				}
			},
			assertDisc: func(t *testing.T, disc []domain.ToolCall) {
				t.Helper()
				if len(disc) != 1 || disc[0].ID != "call-1" {
					t.Fatalf("expected first round discovered ID %q, got %+v", "call-1", disc)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := map[int]*domain.ToolCall{}
			discovered := mergeToolCallDeltas(calls, tt.deltas)
			tt.assert(t, calls)
			if tt.assertDisc != nil {
				tt.assertDisc(t, discovered)
			}
		})
	}
}

func TestProviderChatEmitsToolCallStartEvent(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnv, "test-key")

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

	provider, err := New(resolvedConfig(server.URL, DefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.client = server.Client()

	events := make(chan domain.StreamEvent, 8)
	_, err = provider.Chat(context.Background(), domain.ChatRequest{
		Model:    DefaultModel,
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
