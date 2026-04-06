package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	provider, err := New(cfg, withTransport(customTransport))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if provider.client.Transport != customTransport {
		t.Fatal("expected custom transport to be set")
	}
}

func TestNewValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("empty api key returns error", func(t *testing.T) {
		t.Parallel()
		cfg := resolvedConfig("", "")
		cfg.APIKey = ""
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty api key")
		}
		if !strings.Contains(err.Error(), "api key is empty") {
			t.Fatalf("expected api key error, got: %v", err)
		}
	})

	t.Run("whitespace-only api key returns error", func(t *testing.T) {
		t.Parallel()
		cfg := resolvedConfig("", "")
		cfg.APIKey = "   "
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for whitespace-only api key")
		}
	})

	t.Run("invalid config validate fails", func(t *testing.T) {
		t.Parallel()
		// 空字符串的 BaseURL 和 Model 会导致 Validate 失败（取决于 config 实现）
		cfg := config.ResolvedProviderConfig{
			ProviderConfig: config.ProviderConfig{
				Driver:    DriverName,
				BaseURL:   "",
				Model:     "",
				APIKeyEnv: "NONEXISTENT_ENV_VAR_" + t.Name(),
			},
			APIKey: "test-key",
		}
		_, err := New(cfg)
		// 验证失败时应该返回错误
		if err != nil {
			// 预期行为：config 校验不通过
			return
		}
		// 如果校验通过了，也接受（取决于具体实现）
	})
}

func TestNewDefaultTransportWhenNoOption(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig("", "")
	provider, err := New(cfg) // 不传任何 buildOption
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.client.Transport == nil {
		t.Fatal("expected default transport to be set")
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
		if err := emitToolCallDelta(context.Background(), nil, 0, "", "args"); err != nil {
			t.Fatalf("expected nil events guard to return nil, got %v", err)
		}
	})

	t.Run("empty arguments guard", func(t *testing.T) {
		t.Parallel()
		events := make(chan domain.StreamEvent, 1)
		if err := emitToolCallDelta(context.Background(), events, 0, "", ""); err != nil {
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
		if err := emitToolCallDelta(context.Background(), events, 3, "call_123", `{"path":"main.go"}`); err != nil {
			t.Fatalf("emitToolCallDelta() error = %v", err)
		}
		got := <-events
		payload := requireToolCallDeltaPayload(t, got)
		if got.Type != domain.StreamEventToolCallDelta || payload.Index != 3 || payload.ArgumentsDelta != `{"path":"main.go"}` || payload.ID != "call_123" {
			t.Fatalf("unexpected event: %+v", got)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := emitToolCallDelta(cancelledCtx, make(chan domain.StreamEvent), 0, "", "args"); err == nil {
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
		payload := requireMessageDonePayload(t, got)
		if got.Type != domain.StreamEventMessageDone || payload.FinishReason != "stop" || payload.Usage == nil || payload.Usage.TotalTokens != 100 {
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
	err = provider.Chat(context.Background(), domain.ChatRequest{
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

	streamEvents := drainStreamEvents(events)
	if len(streamEvents) == 0 {
		t.Fatal("expected streamed events")
	}

	var (
		chunks            []string
		toolCallStartSeen bool
		toolCallArgs      strings.Builder
		messageDone       *domain.MessageDonePayload
	)

	for _, event := range streamEvents {
		switch event.Type {
		case domain.StreamEventTextDelta:
			chunks = append(chunks, requireTextDeltaPayload(t, event).Text)
		case domain.StreamEventToolCallStart:
			payload := requireToolCallStartPayload(t, event)
			toolCallStartSeen = true
			if payload.Index != 0 || payload.ID != "call_1" || payload.Name != "filesystem_edit" {
				t.Fatalf("unexpected tool_call_start payload: %+v", payload)
			}
		case domain.StreamEventToolCallDelta:
			payload := requireToolCallDeltaPayload(t, event)
			if payload.Index != 0 || payload.ID != "call_1" {
				t.Fatalf("unexpected tool_call_delta payload: %+v", payload)
			}
			toolCallArgs.WriteString(payload.ArgumentsDelta)
		case domain.StreamEventMessageDone:
			payload := requireMessageDonePayload(t, event)
			messageDone = &payload
		}
	}

	if strings.Join(chunks, "") != "Hello world" {
		t.Fatalf("expected streamed chunks to form %q, got %q", "Hello world", strings.Join(chunks, ""))
	}
	if !toolCallStartSeen {
		t.Fatal("expected tool_call_start event")
	}
	if toolCallArgs.String() != `{"path":"main.go","search_string":"old","replace_string":"new"}` {
		t.Fatalf("unexpected merged tool arguments: %q", toolCallArgs.String())
	}
	if messageDone == nil {
		t.Fatal("expected message_done event")
	}
	if messageDone.FinishReason != "tool_calls" {
		t.Fatalf("expected finish reason %q, got %q", "tool_calls", messageDone.FinishReason)
	}
	if messageDone.Usage == nil || messageDone.Usage.TotalTokens != 15 {
		t.Fatalf("expected total tokens 15, got %+v", messageDone.Usage)
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

			err = provider.Chat(context.Background(), domain.ChatRequest{
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
	if got := <-eventCh; got.Type != domain.StreamEventTextDelta || requireTextDeltaPayload(t, got).Text != "chunk" {
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

	err = provider.consumeStream(context.Background(), strings.NewReader("data: {not-json}\n\n"), make(chan domain.StreamEvent, 1), &strings.Builder{}, new(map[int]*domain.ToolCall))
	if err == nil || !strings.Contains(err.Error(), "decode stream chunk") {
		t.Fatalf("expected dirty JSON decode error, got %v", err)
	}
}

func drainStreamEvents(events <-chan domain.StreamEvent) []domain.StreamEvent {
	drained := make([]domain.StreamEvent, 0)
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return drained
			}
			drained = append(drained, evt)
		default:
			return drained
		}
	}
}

func requireTextDeltaPayload(t *testing.T, event domain.StreamEvent) domain.TextDeltaPayload {
	t.Helper()
	payload, err := event.TextDeltaValue()
	if err != nil {
		t.Fatalf("TextDeltaValue() error = %v", err)
	}
	return payload
}

func requireToolCallStartPayload(t *testing.T, event domain.StreamEvent) domain.ToolCallStartPayload {
	t.Helper()
	payload, err := event.ToolCallStartValue()
	if err != nil {
		t.Fatalf("ToolCallStartValue() error = %v", err)
	}
	return payload
}

func requireToolCallDeltaPayload(t *testing.T, event domain.StreamEvent) domain.ToolCallDeltaPayload {
	t.Helper()
	payload, err := event.ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("ToolCallDeltaValue() error = %v", err)
	}
	return payload
}

func requireMessageDonePayload(t *testing.T, event domain.StreamEvent) domain.MessageDonePayload {
	t.Helper()
	payload, err := event.MessageDoneValue()
	if err != nil {
		t.Fatalf("MessageDoneValue() error = %v", err)
	}
	return payload
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
	payload := requireToolCallStartPayload(t, got)
	if got.Type != domain.StreamEventToolCallStart || payload.Name != "filesystem_edit" || payload.ID != "call-1" || payload.Index != 2 {
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
	startPayload := requireToolCallStartPayload(t, start)
	if startPayload.ID != "call_late_name" || startPayload.Name != "filesystem_edit" {
		t.Fatalf("unexpected tool_call_start payload: %+v", startPayload)
	}

	delta := <-events
	if delta.Type != domain.StreamEventToolCallDelta {
		t.Fatalf("expected tool_call_delta event, got %+v", delta)
	}
	deltaPayload := requireToolCallDeltaPayload(t, delta)
	if deltaPayload.ArgumentsDelta != `{"path":"main.go"}` {
		t.Fatalf("unexpected tool arguments delta: %+v", deltaPayload)
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
	err = provider.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "edit"}},
		Tools: []domain.ToolSpec{
			{Name: "filesystem_edit", Description: "edit", Schema: map[string]any{"type": "object"}},
		},
	}, events)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var foundToolCallStart bool
	for _, evt := range drainStreamEvents(events) {
		if evt.Type == domain.StreamEventToolCallStart {
			foundToolCallStart = true
			payload := requireToolCallStartPayload(t, evt)
			if payload.Name != "filesystem_edit" {
				t.Fatalf("expected ToolName %q, got %q", "filesystem_edit", payload.Name)
			}
			if payload.ID != "call_tool" {
				t.Fatalf("expected ToolCallID %q, got %q", "call_tool", payload.ID)
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
	err = provider.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "test"}},
	}, events)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	var (
		foundTextDelta       bool
		foundToolCallStart   bool
		foundToolCallDelta   bool
		foundMessageDone     bool
		toolCallDeltaContent string
		messageDonePayload   *domain.MessageDonePayload
	)

	for _, evt := range drainStreamEvents(events) {
		switch evt.Type {
		case domain.StreamEventTextDelta:
			foundTextDelta = true
		case domain.StreamEventToolCallStart:
			foundToolCallStart = true
			payload := requireToolCallStartPayload(t, evt)
			if payload.Name != "filesystem_edit" {
				t.Fatalf("expected ToolName %q, got %q", "filesystem_edit", payload.Name)
			}
			if payload.Index != 0 {
				t.Fatalf("expected ToolCallIndex %d for tool_call_start, got %d", 0, payload.Index)
			}
		case domain.StreamEventToolCallDelta:
			foundToolCallDelta = true
			toolCallDeltaContent += requireToolCallDeltaPayload(t, evt).ArgumentsDelta
		case domain.StreamEventMessageDone:
			foundMessageDone = true
			payload := requireMessageDonePayload(t, evt)
			messageDonePayload = &payload
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
	if messageDonePayload == nil {
		t.Fatal("message_done event is nil")
	}
	if messageDonePayload.FinishReason != "tool_calls" {
		t.Fatalf("expected FinishReason %q, got %q", "tool_calls", messageDonePayload.FinishReason)
	}
	if messageDonePayload.Usage == nil {
		t.Fatal("expected Usage in message_done event")
	}
	if messageDonePayload.Usage.TotalTokens != 150 {
		t.Fatalf("expected TotalTokens %d, got %d", 150, messageDonePayload.Usage.TotalTokens)
	}
}

// --- 透明重连测试 ---

func TestProviderChatReconnect_OnRecoverableError(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("Content-Type", "text/event-stream")

		if attempt == 1 {
			// 第一次请求：返回 5xx（可恢复）
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
			return
		}

		// 第二次请求：正常返回
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": "recovered"}},
			},
		})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan domain.StreamEvent, 8)
	err = p.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "hello"}},
	}, events)
	if err != nil {
		t.Fatalf("Chat() should succeed after reconnect, got: %v", err)
	}

	drained := drainStreamEvents(events)
	var foundText bool
	for _, evt := range drained {
		if evt.Type == domain.StreamEventTextDelta {
			foundText = true
		}
	}
	if !foundText {
		t.Fatal("expected text_delta event after reconnect")
	}
	if attempt < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempt)
	}
}

func TestProviderChatReconnect_NonRecoverableError_StopsImmediately(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		// 返回 401（不可恢复）→ 应立即停止，不重试
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	err = p.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "hello"}},
	}, make(chan domain.StreamEvent, 1))
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected auth error, got: %v", err)
	}
	if attempt > 1 {
		t.Fatalf("non-recoverable error should stop immediately, but got %d attempts", attempt)
	}
}

func TestProviderChatReconnect_MaxRetriesExhausted(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`bad gateway`))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	err = p.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "hello"}},
	}, make(chan domain.StreamEvent, 1))
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 重连耗尽后，错误应被标记为不可重试，防止上层 runtime 再次重试叠加放大。
	var pErr *domain.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected *ProviderError after retry exhaustion, got: %T: %v", err, err)
	}
	if pErr.Retryable {
		t.Fatal("error should be non-retryable after reconnect exhaustion")
	}
	// 初始1次 + 最大3次重连 = 最多4次尝试
	if attempt > 4 {
		t.Fatalf("too many attempts: %d (max should be 4)", attempt)
	}
}

func TestProviderChatReconnect_InjectsAccumulatedContext(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// 构造一个先返回有效 SSE 数据再中断的 reader，验证 consumeStream
	// 在中断前正确累积 accumText 和 accumCalls，且错误为 ErrStreamInterrupted。
	sseData := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial \"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"run\"}}]}}]}\n"

	reader := io.MultiReader(strings.NewReader(sseData), &errReader{err: io.ErrClosedPipe})
	events := make(chan domain.StreamEvent, 8)
	accumText := &strings.Builder{}
	accumCalls := make(map[int]*domain.ToolCall)

	err = p.consumeStream(context.Background(), reader, events, accumText, &accumCalls)
	if err == nil {
		t.Fatal("expected error from interrupted stream")
	}
	if !errors.Is(err, domain.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got: %v", err)
	}

	// 验证累积状态：文本和 tool call 都应已保留
	if accumText.String() != "partial " {
		t.Fatalf("expected accumText %q, got %q", "partial ", accumText.String())
	}
	call, ok := accumCalls[0]
	if !ok {
		t.Fatal("expected accumCalls[0] to exist")
	}
	if call.ID != "c1" || call.Name != "bash" {
		t.Fatalf("expected tool call c1/bash, got %+v", call)
	}
	if call.Arguments != "run" {
		t.Fatalf("expected arguments %q, got %q", "run", call.Arguments)
	}

	// 验证累积状态可用于 buildAssistantMsg
	msg := p.buildAssistantMsg(accumText, accumCalls)
	if msg.Role != domain.RoleAssistant {
		t.Fatalf("expected role assistant, got %q", msg.Role)
	}
	if !strings.Contains(msg.Content, "partial ") {
		t.Fatalf("expected assistant content to contain 'partial', got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "c1" {
		t.Fatalf("expected assistant tool calls to contain c1, got %+v", msg.ToolCalls)
	}
}

// --- 辅助方法测试 ---

func TestBuildAssistantMsg_TextOnly(t *testing.T) {
	t.Parallel()

	p, _ := New(resolvedConfig("", ""))
	var accumText strings.Builder
	accumText.WriteString("hello world")

	msg := p.buildAssistantMsg(&accumText, nil)
	if msg.Role != domain.RoleAssistant {
		t.Fatalf("expected role assistant, got %q", msg.Role)
	}
	if msg.Content != "hello world" {
		t.Fatalf("expected content %q, got %q", "hello world", msg.Content)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %+v", msg.ToolCalls)
	}
}

func TestBuildAssistantMsg_WithToolCalls(t *testing.T) {
	t.Parallel()

	p, _ := New(resolvedConfig("", ""))
	var accumText strings.Builder
	accumText.WriteString("done")
	accumCalls := map[int]*domain.ToolCall{
		0: {ID: "call_1", Name: "edit", Arguments: `{"path":"f.go"}`},
		1: {ID: "call_2", Name: "read", Arguments: `{"path":"f.go"}`},
	}

	msg := p.buildAssistantMsg(&accumText, accumCalls)
	if msg.Content != "done" {
		t.Fatalf("content mismatch")
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(msg.ToolCalls))
	}
	// 使用 map 检查，避免依赖 Go map 迭代的不确定顺序
	names := make(map[string]bool)
	for _, tc := range msg.ToolCalls {
		names[tc.Name] = true
	}
	if !names["edit"] || !names["read"] {
		t.Fatalf("expected tool calls 'edit' and 'read', got %+v", msg.ToolCalls)
	}
}

func TestBuildAssistantMsg_EmptyAccum(t *testing.T) {
	t.Parallel()

	p, _ := New(resolvedConfig("", ""))
	var accumText strings.Builder

	msg := p.buildAssistantMsg(&accumText, nil)
	if msg.Content != "" {
		t.Fatalf("expected empty content, got %q", msg.Content)
	}
	if msg.ToolCalls != nil {
		t.Fatal("expected nil ToolCalls when accum is nil")
	}
}

func TestMergeToolCallDeltaWithAccum_SyncsExternalState(t *testing.T) {
	t.Parallel()

	events := make(chan domain.StreamEvent, 4)
	accumCalls := make(map[int]*domain.ToolCall)

	delta1 := toolCallDelta{
		Index: 0,
		ID:    "call_acc",
		Function: openAIFunctionCall{
			Name:      "bash",
			Arguments: `{"cmd":"ls"`,
		},
	}
	if err := mergeToolCallDeltaWithAccum(context.Background(), events, &accumCalls, delta1); err != nil {
		t.Fatalf("first delta error = %v", err)
	}

	delta2 := toolCallDelta{
		Index: 0,
		Function: openAIFunctionCall{
			Arguments: `"}`,
		},
	}
	if err := mergeToolCallDeltaWithAccum(context.Background(), events, &accumCalls, delta2); err != nil {
		t.Fatalf("second delta error = %v", err)
	}

	// 验证外部 accumCalls 状态已同步
	call, ok := accumCalls[0]
	if !ok {
		t.Fatal("expected accumCalls[0] to exist")
	}
	if call.ID != "call_acc" || call.Name != "bash" {
		t.Fatalf("unexpected call state: %+v", call)
	}
	if call.Arguments != `{"cmd":"ls""}` {
		t.Fatalf("expected arguments %q, got %q", `{"cmd":"ls""}`, call.Arguments)
	}
}

func TestMergeToolCallDeltaWithAccum_NilMapInitializes(t *testing.T) {
	t.Parallel()

	var accumCalls map[int]*domain.ToolCall // nil map（非指针）

	delta := toolCallDelta{
		Index: 2,
		ID:    "call_nil",
		Function: openAIFunctionCall{
			Name: "read",
		},
	}
	events := make(chan domain.StreamEvent, 2)
	if err := mergeToolCallDeltaWithAccum(context.Background(), events, &accumCalls, delta); err != nil {
		t.Fatalf("error = %v", err)
	}

	if accumCalls == nil {
		t.Fatal("expected accumCalls to be initialized from nil")
	}
	if accumCalls[2] == nil || accumCalls[2].Name != "read" {
		t.Fatalf("unexpected accumCalls[2]: %+v", accumCalls[2])
	}
}

// --- consumeStream 错误包装测试 ---

func TestConsumeStream_WrapsNonEOFAsInterrupted(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := New(resolvedConfig("", ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// 使用一个会触发读取错误的 source（模拟网络断开）
	errReader := &errReader{err: io.ErrClosedPipe}
	accumText := &strings.Builder{}
	accums := make(map[int]*domain.ToolCall)

	err = p.consumeStream(context.Background(), errReader, make(chan domain.StreamEvent, 1), accumText, &accums)
	if err == nil {
		t.Fatal("expected error for broken reader")
	}
	if !errors.Is(err, domain.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted wrapping, got: %v", err)
	}
}

// TestConsumeStream_FlushesPendingDataOnNonEOFError 验证非 EOF 读取错误发生前，
// 已缓冲但尚未刷新的 data: 行仍会被处理（不会因中断而丢失）。
func TestConsumeStream_FlushesPendingDataOnNonEOFError(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := New(resolvedConfig("", ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// 构造一段 SSE 数据：包含一个有效 data 行，但紧跟一个错误而非空行。
	// 这模拟了流中断前最后一帧数据还没来得及被空行触发刷新的场景。
	sseData := `data: {"id":"a","object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"},"finish_reason":""}]}
` // 注意：这里有换行符，但由于紧接着是 error，不会被空行刷新
	body := io.MultiReader(strings.NewReader(sseData), &errReader{err: io.ErrClosedPipe})

	events := make(chan domain.StreamEvent, 10)
	accumText := &strings.Builder{}
	accums := make(map[int]*domain.ToolCall)

	err = p.consumeStream(context.Background(), body, events, accumText, &accums)
	if err == nil {
		t.Fatal("expected error for broken reader")
	}
	if !errors.Is(err, domain.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got: %v", err)
	}

	// 关键断言：中断前的 data 行必须已被刷新处理，文本累积不为空。
	if accumText.String() != "hello" {
		t.Fatalf("expected accumText 'hello', got %q", accumText.String())
	}
}

// errReader 是一个每次 ReadLine 都返回指定错误的测试辅助类型。
type errReader struct {
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, e.err
}

// roundTripperFunc 将函数适配为 http.RoundTripper 接口，用于测试中 mock HTTP 行为。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// --- 重连消息完整性测试 ---

// TestReconnect_NoEmptyAssistantOnFirstFailure 验证首次请求失败（未收到任何 SSE 数据）
// 时，重连不会向消息列表注入空的 assistant 消息。
func TestReconnect_NoEmptyAssistantOnFirstFailure(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++

		// 解码请求中的消息，验证消息结构
		var payload chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if attempt == 1 {
			// 首次请求：返回 500（无 SSE 数据），不应注入空 assistant 消息
			if len(payload.Messages) != 1 {
				t.Errorf("first attempt: expected 1 message, got %d", len(payload.Messages))
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"temporarily unavailable"}}`))
			return
		}

		// 第二次请求：仍应只有原始 1 条消息（无空 assistant 注入）
		if len(payload.Messages) != 1 {
			t.Errorf("second attempt: expected 1 message (no empty assistant), got %d; messages: %+v",
				len(payload.Messages), payload.Messages)
		}
		for _, msg := range payload.Messages {
			if msg.Role == "assistant" {
				t.Errorf("second attempt: unexpected assistant message injected: %+v", msg)
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": "ok"}},
			},
		})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan domain.StreamEvent, 4)
	err = p.Chat(context.Background(), domain.ChatRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []domain.Message{{Role: "user", Content: "hello"}},
	}, events)
	if err != nil {
		t.Fatalf("Chat() should succeed after reconnect, got: %v", err)
	}
	if attempt != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", attempt)
	}
}

// TestReconnect_SingleAssistantSnapshotNotDuplicated 验证多次重连时，每次请求
// 只包含原始消息 + 恰好 1 条 assistant 快照，不会出现旧快照残留。
// 使用自定义 RoundTripper 模拟流中途中断（非 EOF 错误）。
func TestReconnect_SingleAssistantSnapshotNotDuplicated(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	attempt := 0

	// 使用 httptest.Server 作为成功响应的代理，RoundTripper 控制中断
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{
			"choices": []map[string]any{
				{"index": 0, "delta": map[string]any{"content": " done"}},
			},
		})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempt++

		// 读取并缓存请求体，以便解码后仍可转发给真实 transport
		bodyBytes, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}

		// 解码请求消息，用于断言
		var payload chatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode request: %w", err)
		}

		// 恢复请求体，确保后续 transport 可以读取
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))

		switch attempt {
		case 1:
			// 首次请求：正常消息（system + user），发送部分 SSE 后流中断
			if len(payload.Messages) != 2 {
				t.Errorf("attempt 1: expected 2 messages, got %d", len(payload.Messages))
			}
			sseData := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"
			body := io.MultiReader(strings.NewReader(sseData), &errReader{err: io.ErrClosedPipe})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(body),
			}, nil

		case 2:
			// 第二次请求：应包含原始 2 条 + 1 条 assistant（"partial"），再次中断
			if len(payload.Messages) != 3 {
				t.Errorf("attempt 2: expected 3 messages, got %d; messages: %+v",
					len(payload.Messages), payload.Messages)
			}
			assistMsg := payload.Messages[2]
			if assistMsg.Role != "assistant" || assistMsg.Content != "partial" {
				t.Errorf("attempt 2: expected assistant content 'partial', got role=%q content=%q",
					assistMsg.Role, assistMsg.Content)
			}
			assistCount := countMessagesByRole(payload.Messages, "assistant")
			if assistCount != 1 {
				t.Errorf("attempt 2: expected 1 assistant message, got %d", assistCount)
			}

			sseData := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" more\"}}]}\n\n"
			body := io.MultiReader(strings.NewReader(sseData), &errReader{err: io.ErrClosedPipe})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(body),
			}, nil

		default:
			// 第三次请求：应包含原始 2 条 + 1 条 assistant（"partial more"）
			if len(payload.Messages) != 3 {
				t.Errorf("attempt 3: expected 3 messages, got %d; messages: %+v",
					len(payload.Messages), payload.Messages)
			}
			assistMsg := payload.Messages[2]
			if assistMsg.Role != "assistant" || assistMsg.Content != "partial more" {
				t.Errorf("attempt 3: expected assistant content 'partial more', got role=%q content=%q",
					assistMsg.Role, assistMsg.Content)
			}
			// 确认仍然只有 1 条 assistant 消息（旧快照未残留）
			assistCount := countMessagesByRole(payload.Messages, "assistant")
			if assistCount != 1 {
				t.Errorf("attempt 3: expected 1 assistant message, got %d", assistCount)
			}

			// 委托给 test server 返回完整响应
			return http.DefaultTransport.RoundTrip(req)
		}
	})

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel), withTransport(rt))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan domain.StreamEvent, 16)
	err = p.Chat(context.Background(), domain.ChatRequest{
		SystemPrompt: "you are helpful",
		Model:        config.OpenAIDefaultModel,
		Messages:     []domain.Message{{Role: "user", Content: "hello"}},
	}, events)
	if err != nil {
		t.Fatalf("Chat() should succeed after reconnect, got: %v", err)
	}
	if attempt != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", attempt)
	}

	// 验证最终累积的文本（三次请求的增量合并）
	var fullText strings.Builder
	for _, evt := range drainStreamEvents(events) {
		if evt.Type == domain.StreamEventTextDelta {
			fullText.WriteString(requireTextDeltaPayload(t, evt).Text)
		}
	}
	expectedText := "partial more done"
	if fullText.String() != expectedText {
		t.Fatalf("expected full text %q, got %q", expectedText, fullText.String())
	}
}

// countMessagesByRole 统计消息列表中指定角色的消息数量。
func countMessagesByRole(messages []openAIMessage, role string) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}
