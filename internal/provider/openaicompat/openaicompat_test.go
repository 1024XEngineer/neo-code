package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
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

	p, err := New(cfg, withTransport(customTransport))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if p.client.Transport != customTransport {
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
		cfg := provider.RuntimeConfig{
			Name:         DriverName,
			Driver:       DriverName,
			BaseURL:      "",
			DefaultModel: config.OpenAIDefaultModel,
			APIKey:       "test-key",
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty base url")
		}
		if !strings.Contains(err.Error(), "base url is empty") {
			t.Fatalf("expected base url error, got: %v", err)
		}
	})
}

func TestNewDefaultTransportWhenNoOption(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig("", "")
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if p.client.Transport == nil {
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

	p, err := New(resolvedConfig(server.URL, ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	models, err := p.DiscoverModels(context.Background())
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

// --- toOpenAIMessage 转换测试 ---

func TestToOpenAIMessage_BasicMessage(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role:    "user",
		Content: "hello world",
	}
	result := chatcompletions.ToOpenAIMessage(msg)

	if result.Role != "user" || result.Content != "hello world" {
		t.Fatalf("unexpected basic message: role=%q content=%q", result.Role, result.Content)
	}
	if result.ToolCallID != "" || len(result.ToolCalls) > 0 {
		t.Fatal("basic message should not have tool call fields")
	}
}

func TestToOpenAIMessage_ToolRoleMessage(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role:       "tool",
		Content:    "result data",
		ToolCallID: "call_123",
	}
	result := chatcompletions.ToOpenAIMessage(msg)

	if result.Role != "tool" || result.ToolCallID != "call_123" {
		t.Fatalf("unexpected tool message: role=%q toolCallID=%q", result.Role, result.ToolCallID)
	}
}

func TestToOpenAIMessage_AssistantWithToolCalls(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{
		Role: "assistant",
		ToolCalls: []providertypes.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`},
			{ID: "call_2", Name: "write_file", Arguments: `{"path":"test.go","content":"..."}`},
		},
	}
	result := chatcompletions.ToOpenAIMessage(msg)

	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	tc1 := result.ToolCalls[0]
	if tc1.ID != "call_1" || tc1.Type != "function" {
		t.Fatalf("unexpected first tool call: id=%q type=%q", tc1.ID, tc1.Type)
	}
	if tc1.Function.Name != "read_file" || tc1.Function.Arguments != `{"path":"main.go"}` {
		t.Fatalf("unexpected first function: name=%q args=%q", tc1.Function.Name, tc1.Function.Arguments)
	}
	tc2 := result.ToolCalls[1]
	if tc2.Function.Name != "write_file" {
		t.Fatalf("unexpected second function name: %q", tc2.Function.Name)
	}
}

func TestToOpenAIMessage_EmptyToolCalls(t *testing.T) {
	t.Parallel()

	msg := providertypes.Message{Role: "user", Content: "test"}
	result := chatcompletions.ToOpenAIMessage(msg)
	if len(result.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls for user message, got %d", len(result.ToolCalls))
	}
}

// --- extractStreamUsage 测试 ---

func TestExtractStreamUsage_NilInput(t *testing.T) {
	t.Parallel()

	var usage providertypes.Usage
	chatcompletions.ExtractStreamUsage(&usage, nil)
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		t.Fatalf("expected zero values for nil input, got %+v", usage)
	}
}

func TestExtractStreamUsage_NormalValues(t *testing.T) {
	t.Parallel()

	var usage providertypes.Usage
	raw := &chatcompletions.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	chatcompletions.ExtractStreamUsage(&usage, raw)
	if usage.InputTokens != 100 || usage.OutputTokens != 50 || usage.TotalTokens != 150 {
		t.Fatalf("unexpected usage values: %+v", usage)
	}
}

func TestExtractStreamUsage_ZeroValues(t *testing.T) {
	t.Parallel()

	var usage providertypes.Usage
	usage.InputTokens = 999
	raw := &chatcompletions.Usage{}
	chatcompletions.ExtractStreamUsage(&usage, raw)
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0 {
		t.Fatalf("expected zero values to overwrite previous, got %+v", usage)
	}
}

func TestExtractStreamUsage_MultipleOverwrites(t *testing.T) {
	t.Parallel()

	var usage providertypes.Usage
	chatcompletions.ExtractStreamUsage(&usage, &chatcompletions.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	chatcompletions.ExtractStreamUsage(&usage, &chatcompletions.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30})
	if usage.TotalTokens != 30 {
		t.Fatalf("expected last write to win (total=30), got %d", usage.TotalTokens)
	}
}

// --- buildRequest 边界测试 ---

func TestBuildRequest_EmptyModelReturnsError(t *testing.T) {
	t.Parallel()

	// 直接构造 Provider 跳过 New() 的 Validate 校验，
	// 以便测试 buildRequest 对空 model 的独立校验。
	p := &Provider{
		cfg: provider.RuntimeConfig{
			Name:         DriverName,
			Driver:       DriverName,
			BaseURL:      config.OpenAIDefaultBaseURL,
			DefaultModel: "",
			APIKey:       "test-key",
		},
		client: &http.Client{},
	}

	_, buildErr := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{})
	if buildErr == nil {
		t.Fatal("expected error for empty model")
	}
	if !strings.Contains(buildErr.Error(), "model is empty") {
		t.Fatalf("unexpected error message: %v", buildErr)
	}
}

func TestBuildRequest_FallsBackToConfigModel(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.Model != config.OpenAIDefaultModel {
		t.Fatalf("expected model %q, got %q", config.OpenAIDefaultModel, payload.Model)
	}
}

func TestBuildRequest_RequestModelTakesPrecedence(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{Model: "gpt-4-custom", Messages: []providertypes.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.Model != "gpt-4-custom" {
		t.Fatalf("expected model %q, got %q", "gpt-4-custom", payload.Model)
	}
}

func TestBuildRequest_NoSystemPrompt(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{SystemPrompt: "", Messages: []providertypes.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	for _, msg := range payload.Messages {
		if msg.Role == "system" {
			t.Fatal("expected no system message when SystemPrompt is empty")
		}
	}
}

func TestBuildRequest_NoTools(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Content: "hi"}}, Tools: nil})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.ToolChoice != "" || len(payload.Tools) != 0 {
		t.Fatalf("expected no tools, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
}

func TestBuildRequest_EmptyToolsSlice(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{Messages: []providertypes.Message{{Role: "user", Content: "hi"}}, Tools: []providertypes.ToolSpec{}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if payload.ToolChoice != "" || len(payload.Tools) != 0 {
		t.Fatalf("expected empty tools for empty slice, got choice=%q tools=%d", payload.ToolChoice, len(payload.Tools))
	}
}

func TestBuildRequest_MultipleTools(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{
		Messages: []providertypes.Message{{Role: "user", Content: "use tools"}},
		Tools: []providertypes.ToolSpec{
			{Name: "tool_a", Description: "Tool A", Schema: map[string]any{"type": "object"}},
			{Name: "tool_b", Description: "Tool B", Schema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(payload.Tools))
	}
	if payload.ToolChoice != "auto" {
		t.Fatalf("expected tool_choice=auto, got %q", payload.ToolChoice)
	}
}

func TestBuildRequest_WhitespaceSystemPromptSkipped(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{SystemPrompt: "   ", Messages: []providertypes.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	for _, msg := range payload.Messages {
		if msg.Role == "system" {
			t.Fatal("expected no system message for whitespace-only system prompt")
		}
	}
}

// --- consumeStream SSE 场景测试 ---

func TestConsumeStream_SSECommentIgnored(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `: heartbeat
data: {"id":"a","choices":[{"delta":{"content":"ok"},"finish_reason":""}]}
data: [DONE]

`
	events := make(chan providertypes.StreamEvent, 4)
	err = p.ConsumeStream(context.Background(), strings.NewReader(sseData), events)
	if err != nil {
		t.Fatalf("consumeStream() error = %v", err)
	}
	drained := drainStreamEvents(events)
	var foundText bool
	for _, evt := range drained {
		if evt.Type == providertypes.StreamEventTextDelta && requireTextDeltaPayload(t, evt).Text == "ok" {
			foundText = true
		}
	}
	if !foundText {
		t.Fatal("expected text_delta event after SSE comment")
	}
}

func TestConsumeStream_ChunkErrorInPayload(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `data: {"error":{"message":"rate limit exceeded"}}
`
	events := make(chan providertypes.StreamEvent, 1)
	err = p.ConsumeStream(context.Background(), strings.NewReader(sseData), events)
	if err == nil {
		t.Fatal("expected error for chunk with error field")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("expected rate limit error, got: %v", err)
	}
}

func TestConsumeStream_MultiLineDataPayload(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `data: {"id":"a","choices":[{"delta":{"content":"part1"},"finish_reason":""}]}
data: {"id":"b","choices":[{"delta":{"content":"part2"},"finish_reason":"stop"}]}
data: [DONE]

`
	events := make(chan providertypes.StreamEvent, 8)
	err = p.ConsumeStream(context.Background(), strings.NewReader(sseData), events)
	if err != nil {
		t.Fatalf("consumeStream() error = %v", err)
	}
	drained := drainStreamEvents(events)
	if len(drained) == 0 {
		t.Fatal("expected events from multi-line data payload")
	}
}

func TestConsumeStream_EOFWithoutDoneReturnsInterrupted(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `data: {"id":"a","choices":[{"delta":{"content":"partial"},"finish_reason":""}]}
`
	events := make(chan providertypes.StreamEvent, 4)
	err = p.ConsumeStream(context.Background(), strings.NewReader(sseData), events)
	if err == nil {
		t.Fatal("expected interrupted error for EOF without [DONE]")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
	drained := drainStreamEvents(events)
	var foundText bool
	for _, evt := range drained {
		if evt.Type == providertypes.StreamEventTextDelta {
			foundText = true
		}
	}
	if !foundText {
		t.Fatal("expected text_delta event before EOF")
	}
}

func TestConsumeStream_ContextCancellation(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sseData := `data: {"id":"a","choices":[{"delta":{"content":"should not emit"}}]}

`
	events := make(chan providertypes.StreamEvent, 1)
	err = p.ConsumeStream(ctx, strings.NewReader(sseData), events)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected cancellation to win over stream interruption, got: %v", err)
	}
}

func TestConsumeStream_ContextCancellationOnReadErrorReturnsCanceled(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelThenErrorReader{cancel: cancel, err: io.ErrClosedPipe}

	err = p.ConsumeStream(ctx, body, make(chan providertypes.StreamEvent, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected cancellation to win over stream interruption, got %v", err)
	}
}

func TestConsumeStream_ContextCancellationAtEOFWithoutDoneReturnsCanceled(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sseData := `data: {"id":"a","choices":[{"delta":{"content":"hello"}}]}
`
	body := &cancelOnEOFReader{
		reader: strings.NewReader(sseData),
		cancel: cancel,
	}
	events := make(chan providertypes.StreamEvent, 8)

	err = p.ConsumeStream(ctx, body, events)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected cancellation to win over stream interruption, got %v", err)
	}
}

func TestConsumeStream_DoneThenCancellationStillFinishes(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelAfterDoneReader{
		payload: []byte("data: [DONE]\n"),
		cancel:  cancel,
		err:     io.ErrClosedPipe,
	}
	events := make(chan providertypes.StreamEvent, 4)

	err = p.ConsumeStream(ctx, body, events)
	if err != nil {
		t.Fatalf("expected completed stream after [DONE], got %v", err)
	}

	var foundDone bool
	for _, evt := range drainStreamEvents(events) {
		if evt.Type == providertypes.StreamEventMessageDone {
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatal("expected message_done event after cancellation race post-[DONE]")
	}
}

func TestConsumeStream_FinishReasonAccumulation(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `data: {"id":"a","choices":[{"delta":{"content":"text"},"finish_reason":""}]}
data: {"id":"b","choices":[{"index":0,"finish_reason":"stop"}]}
data: [DONE]

`
	events := make(chan providertypes.StreamEvent, 8)
	err = p.ConsumeStream(context.Background(), strings.NewReader(sseData), events)
	if err != nil {
		t.Fatalf("consumeStream() error = %v", err)
	}

	drained := drainStreamEvents(events)
	var donePayload *providertypes.MessageDonePayload
	for _, evt := range drained {
		if evt.Type == providertypes.StreamEventMessageDone {
			p := requireMessageDonePayload(t, evt)
			donePayload = &p
		}
	}
	if donePayload == nil {
		t.Fatal("expected message_done event")
	}
	if donePayload.FinishReason != "stop" {
		t.Fatalf("expected finish_reason=stop, got %q", donePayload.FinishReason)
	}
}

// --- emit 函数守卫和边界测试 ---

func TestEmitTextDelta_NilEventsGuard(t *testing.T) {
	t.Parallel()
	if err := chatcompletions.EmitTextDelta(context.Background(), nil, "some text"); err != nil {
		t.Fatalf("expected nil events guard to return nil, got %v", err)
	}
}

func TestEmitTextDelta_EmptyTextGuard(t *testing.T) {
	t.Parallel()
	events := make(chan providertypes.StreamEvent, 1)
	if err := chatcompletions.EmitTextDelta(context.Background(), events, ""); err != nil {
		t.Fatalf("expected empty text guard to return nil, got %v", err)
	}
	select {
	case <-events:
		t.Fatal("expected no event for empty text")
	default:
	}
}

// --- flushDataLines 测试 ---

func TestFlushDataLines_EmptyLines(t *testing.T) {
	t.Parallel()
	called := false
	err := chatcompletions.FlushDataLines([]string{}, func(string) error { called = true; return nil })
	if err != nil {
		t.Fatalf("flushDataLines() error = %v", err)
	}
	if called {
		t.Fatal("processChunk should not be called for empty lines")
	}
}

func TestFlushDataLines_SingleLine(t *testing.T) {
	t.Parallel()
	var received string
	err := chatcompletions.FlushDataLines([]string{"line1"}, func(p string) error { received = p; return nil })
	if err != nil {
		t.Fatalf("flushDataLines() error = %v", err)
	}
	if received != "line1" {
		t.Fatalf("expected %q, got %q", "line1", received)
	}
}

func TestFlushDataLines_MultipleLinesProcessedIndividually(t *testing.T) {
	t.Parallel()
	var received []string
	err := chatcompletions.FlushDataLines([]string{"a", "b", "c"}, func(p string) error { received = append(received, p); return nil })
	if err != nil {
		t.Fatalf("flushDataLines() error = %v", err)
	}
	if len(received) != 3 || received[0] != "a" || received[1] != "b" || received[2] != "c" {
		t.Fatalf("expected each line processed individually, got %v", received)
	}
}

func TestFlushDataLines_ProcessChunkError(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("process error")
	err := chatcompletions.FlushDataLines([]string{"data"}, func(string) error { return expectedErr })
	if err != expectedErr {
		t.Fatalf("expected processChunk error, got %v", err)
	}
}

// --- DiscoverModels 错误场景测试 ---

func TestDiscoverModels_HTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
	if models != nil {
		t.Fatalf("expected nil models on error, got %d models", len(models))
	}
}

func TestDiscoverModels_NetworkError(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig("http://127.0.0.1:1", ""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = &http.Client{Timeout: time.Millisecond * 10}

	_, err = p.DiscoverModels(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestFetchModelsSetsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "header-key"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	if _, err := p.fetchModels(context.Background()); err != nil {
		t.Fatalf("fetchModels() error = %v", err)
	}
	if authorization != "Bearer test-key" {
		t.Fatalf("expected bearer authorization header, got %q", authorization)
	}
}

func TestFetchModelsDecodeError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid-json"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "decode-key"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	_, err = p.fetchModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode models response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestDiscoverModelsSkipsInvalidEntriesAndDedupes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1", "name": "GPT-4.1"},
				{"foo": "bar"},
				{"id": "gpt-4.1", "name": "GPT-4.1 Duplicate"},
			},
		})
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "discover-key"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected invalid and duplicate models to be filtered, got %+v", models)
	}
	if models[0].ID != "gpt-4.1" {
		t.Fatalf("expected remaining model to be gpt-4.1, got %+v", models[0])
	}
}

// --- mergeToolCallDelta 边界测试 ---

func TestMergeToolCallDelta_MultipleIndices(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 8)
	toolCalls := make(map[int]*providertypes.ToolCall)

	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{
		Index: 0, ID: "call_0",
		Function: chatcompletions.FunctionCall{Name: "tool_a", Arguments: `{"arg":"a"`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}
	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{
		Index: 1, ID: "call_1",
		Function: chatcompletions.FunctionCall{Name: "tool_b", Arguments: `{"arg":"b"}`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}
	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{
		Index:    0,
		Function: chatcompletions.FunctionCall{Arguments: `,"more":"data"}`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	call0 := toolCalls[0]
	if call0.Name != "tool_a" || call0.ID != "call_0" {
		t.Fatalf("unexpected tool call 0: %+v", call0)
	}
	expectedArgs0 := `{"arg":"a","more":"data"}`
	if call0.Arguments != expectedArgs0 {
		t.Fatalf("expected arguments %q for call 0, got %q", expectedArgs0, call0.Arguments)
	}
	call1 := toolCalls[1]
	if call1.Name != "tool_b" || call1.ID != "call_1" {
		t.Fatalf("unexpected tool call 1: %+v", call1)
	}
}

func TestMergeToolCallDelta_IDUpdateOnly(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 4)
	toolCalls := make(map[int]*providertypes.ToolCall)

	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{Index: 0, ID: "call_only_id"}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}

	call := toolCalls[0]
	if call == nil {
		t.Fatal("expected tool call entry to be created")
	}
	if call.ID != "call_only_id" {
		t.Fatalf("expected ID %q, got %q", "call_only_id", call.ID)
	}
	select {
	case <-events:
		t.Fatal("expected no event for ID-only delta")
	default:
	}
}

// --- Generate 集成测试 ---

func TestGenerate_BaseURLTrailingSlashHandled(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}
data: [DONE]

`))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL+"/", config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan providertypes.StreamEvent, 4)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []providertypes.Message{{Role: "user", Content: "hi"}},
	}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
}

// --- parseError 边界测试 ---

func TestParseError_ReadBodyFailure(t *testing.T) {
	t.Parallel()

	readErr := errors.New("simulated read failure")
	resp := &http.Response{Status: "400 Bad Request", StatusCode: 400, Body: &failingReadCloser{err: readErr}}

	err := chatcompletions.ParseError(resp)
	if err == nil {
		t.Fatal("expected error when body read fails")
	}
	if !strings.Contains(err.Error(), "read error response") {
		t.Fatalf("expected read error in message, got: %v", err)
	}
}

func TestParseError_InvalidJSONBody(t *testing.T) {
	t.Parallel()

	resp := &http.Response{Status: "400 Bad Request", StatusCode: 400, Body: ioNopCloser("this is not json at all")}
	err := chatcompletions.ParseError(resp)
	if err == nil {
		t.Fatal("expected error for non-JSON body")
	}
	if !strings.Contains(err.Error(), "this is not json at all") {
		t.Fatalf("expected plain text fallback, got: %v", err)
	}
}

func TestParseError_ClassifiesContextTooLong(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		Status:     "400 Bad Request",
		StatusCode: 400,
		Body:       ioNopCloser(`{"error":{"message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 140000 tokens."}}`),
	}
	err := chatcompletions.ParseError(resp)
	if err == nil {
		t.Fatal("expected context too long error")
	}
	if !provider.IsContextTooLong(err) {
		t.Fatalf("expected parsed error to be classified as context too long, got %v", err)
	}
}

// --- 原有保留的集成测试（保持兼容） ---

func TestProviderGenerateConsumesSSEAndMergesToolCalls(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}

		var payload chatcompletions.Request
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "gpt-5.4" {
			t.Fatalf("expected model gpt-5.4, got %q", payload.Model)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "Hello "}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": 0, "id": "call_1", "type": "function", "function": map[string]any{"name": "filesystem_edit", "arguments": `{"path":"main.go","search_string":"old"`}}}}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "world"}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": 0, "function": map[string]any{"arguments": `,"replace_string":"new"}`}}}}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, "gpt-5.4"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Model: "gpt-5.4",
		Messages: []providertypes.Message{
			{Role: "user", Content: "please edit the file"},
			{Role: "assistant", ToolCalls: []providertypes.ToolCall{{ID: "call_1", Name: "filesystem_edit", Arguments: `{"path":"main.go","search_string":"old","replace_string":"new"}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: "tool finished"},
		},
		Tools: []providertypes.ToolSpec{{Name: "filesystem_edit", Description: "Edit one matching block in a file", Schema: map[string]any{"type": "object"}}},
	}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	streamEvents := drainStreamEvents(events)
	if len(streamEvents) == 0 {
		t.Fatal("expected streamed events")
	}

	var chunks []string
	var toolCallStartSeen bool
	var toolCallArgs strings.Builder
	var messageDone *providertypes.MessageDonePayload

	for _, event := range streamEvents {
		switch event.Type {
		case providertypes.StreamEventTextDelta:
			chunks = append(chunks, requireTextDeltaPayload(t, event).Text)
		case providertypes.StreamEventToolCallStart:
			payload := requireToolCallStartPayload(t, event)
			toolCallStartSeen = true
			if payload.Index != 0 || payload.ID != "call_1" || payload.Name != "filesystem_edit" {
				t.Fatalf("unexpected tool_call_start payload: %+v", payload)
			}
		case providertypes.StreamEventToolCallDelta:
			payload := requireToolCallDeltaPayload(t, event)
			if payload.Index != 0 || payload.ID != "call_1" {
				t.Fatalf("unexpected tool_call_delta payload: %+v", payload)
			}
			toolCallArgs.WriteString(payload.ArgumentsDelta)
		case providertypes.StreamEventMessageDone:
			p := requireMessageDonePayload(t, event)
			messageDone = &p
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

func TestProviderGenerateHTTPErrorResponses(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	tests := []struct {
		name      string
		status    int
		body      string
		expectErr string
	}{
		{name: "http 401 json error", status: http.StatusUnauthorized, body: `{"error":{"message":"invalid api key"}}`, expectErr: "invalid api key"},
		{name: "http 500 empty body falls back to status", status: http.StatusInternalServerError, body: ``, expectErr: "500 Internal Server Error"},
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

			p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			p.client = server.Client()

			err = p.Generate(context.Background(), providertypes.GenerateRequest{Model: config.OpenAIDefaultModel}, make(chan providertypes.StreamEvent, 1))
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestProviderGenerateRejectsUnsupportedAPIStyle(t *testing.T) {
	t.Parallel()

	cfg := resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel)
	cfg.APIStyle = "responses"

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Model:    config.OpenAIDefaultModel,
		Messages: []providertypes.Message{{Role: "user", Content: "hi"}},
	}, make(chan providertypes.StreamEvent, 1))
	if err == nil {
		t.Fatal("expected unsupported api_style error")
	}
	if !strings.Contains(err.Error(), `api_style "responses" is not supported yet`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRequestIncludesSystemPromptToolsAndToolMessages(t *testing.T) {
	t.Parallel()

	p, err := New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload, err := chatcompletions.BuildRequest(p.cfg, providertypes.GenerateRequest{
		SystemPrompt: "system prompt",
		Messages: []providertypes.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCalls: []providertypes.ToolCall{{ID: "call_1", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: "tool finished"},
		},
		Tools: []providertypes.ToolSpec{{Name: "filesystem_edit", Description: "Edit file content", Schema: map[string]any{"type": "object"}}},
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

	tests := []struct {
		name      string
		status    string
		body      string
		expectErr string
	}{
		{"json error payload", "400 Bad Request", `{"error":{"message":"invalid request"}}`, "invalid request"},
		{"plain text fallback", "502 Bad Gateway", `gateway timeout`, "gateway timeout"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{Status: tt.status, Body: ioNopCloser(tt.body)}
			err := chatcompletions.ParseError(resp)
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}

	eventCh := make(chan providertypes.StreamEvent, 1)
	if err := chatcompletions.EmitTextDelta(context.Background(), eventCh, "chunk"); err != nil {
		t.Fatalf("emitTextDelta() error = %v", err)
	}
	if got := <-eventCh; got.Type != providertypes.StreamEventTextDelta || requireTextDeltaPayload(t, got).Text != "chunk" {
		t.Fatalf("unexpected stream event: %+v", got)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chatcompletions.EmitTextDelta(cancelledCtx, make(chan providertypes.StreamEvent), "chunk"); err == nil {
		t.Fatalf("expected cancellation error")
	}
}

func TestProviderConsumeStreamRejectsDirtyJSON(t *testing.T) {
	t.Parallel()

	p, err := chatcompletions.New(resolvedConfig(config.OpenAIDefaultBaseURL, config.OpenAIDefaultModel), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	err = p.ConsumeStream(context.Background(), strings.NewReader("data: {not-json}\n\n"), make(chan providertypes.StreamEvent, 1))
	if err == nil || !strings.Contains(err.Error(), "decode stream chunk") {
		t.Fatalf("expected dirty JSON decode error, got %v", err)
	}
}

// --- 辅助函数 ---

func resolvedConfig(baseURL string, model string) provider.RuntimeConfig {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = config.OpenAIDefaultBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = config.OpenAIDefaultModel
	}
	return provider.RuntimeConfig{
		Name:         DriverName,
		Driver:       DriverName,
		BaseURL:      baseURL,
		DefaultModel: model,
		APIKey:       "test-key",
	}
}

func drainStreamEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	drained := make([]providertypes.StreamEvent, 0)
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

func requireTextDeltaPayload(t *testing.T, event providertypes.StreamEvent) providertypes.TextDeltaPayload {
	t.Helper()
	payload, err := event.TextDeltaValue()
	if err != nil {
		t.Fatalf("TextDeltaValue() error = %v", err)
	}
	return payload
}

func requireToolCallStartPayload(t *testing.T, event providertypes.StreamEvent) providertypes.ToolCallStartPayload {
	t.Helper()
	payload, err := event.ToolCallStartValue()
	if err != nil {
		t.Fatalf("ToolCallStartValue() error = %v", err)
	}
	return payload
}

func requireToolCallDeltaPayload(t *testing.T, event providertypes.StreamEvent) providertypes.ToolCallDeltaPayload {
	t.Helper()
	payload, err := event.ToolCallDeltaValue()
	if err != nil {
		t.Fatalf("ToolCallDeltaValue() error = %v", err)
	}
	return payload
}

func requireMessageDonePayload(t *testing.T, event providertypes.StreamEvent) providertypes.MessageDonePayload {
	t.Helper()
	payload, err := event.MessageDoneValue()
	if err != nil {
		t.Fatalf("MessageDoneValue() error = %v", err)
	}
	return payload
}

func containsToolRoleMessage(messages []chatcompletions.Message, toolCallID string, content string) bool {
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID == toolCallID && m.Content == content {
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
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func ioNopCloser(body string) *readCloser { return &readCloser{Reader: strings.NewReader(body)} }

type readCloser struct{ *strings.Reader }

func (r *readCloser) Close() error { return nil }

// --- 错误包装测试 ---

func TestConsumeStream_WrapsNonEOFAsInterrupted(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	errReader := &errReader{err: io.ErrClosedPipe}
	err = p.ConsumeStream(context.Background(), errReader, make(chan providertypes.StreamEvent, 1))
	if err == nil {
		t.Fatal("expected error for broken reader")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted wrapping, got: %v", err)
	}
}

func TestConsumeStream_FlushesPendingDataOnNonEOFError(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	p, err := chatcompletions.New(resolvedConfig("", ""), &http.Client{})
	if err != nil {
		t.Fatalf("chatcompletions.New() error = %v", err)
	}

	sseData := `data: {"id":"a","choices":[{"delta":{"content":"hello"},"finish_reason":""}]}
`
	body := io.MultiReader(strings.NewReader(sseData), &errReader{err: io.ErrClosedPipe})
	events := make(chan providertypes.StreamEvent, 10)

	err = p.ConsumeStream(context.Background(), body, events)
	if err == nil {
		t.Fatal("expected error for broken reader")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got: %v", err)
	}

	drained := drainStreamEvents(events)
	var foundText bool
	for _, evt := range drained {
		if evt.Type == providertypes.StreamEventTextDelta {
			foundText = true
		}
	}
	if !foundText {
		t.Fatal("expected text_delta event from flushed pending data")
	}
}

type errReader struct{ err error }

func (e *errReader) Read(_ []byte) (int, error) { return 0, e.err }

type cancelThenErrorReader struct {
	cancel func()
	err    error
}

func (r *cancelThenErrorReader) Read(_ []byte) (int, error) {
	r.cancel()
	return 0, r.err
}

type cancelOnEOFReader struct {
	reader io.Reader
	cancel func()
}

func (r *cancelOnEOFReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.cancel()
	}
	return n, err
}

type cancelAfterDoneReader struct {
	payload []byte
	cancel  func()
	err     error
	read    bool
}

func (r *cancelAfterDoneReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		n := copy(p, r.payload)
		return n, nil
	}
	r.cancel()
	return 0, r.err
}

type failingReadCloser struct{ err error }

func (f *failingReadCloser) Read(_ []byte) (int, error) { return 0, f.err }
func (f *failingReadCloser) Close() error               { return f.err }

// --- emitToolCallStart 和 mergeToolCallDelta 保留测试 ---

func TestEmitToolCallStartGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if err := chatcompletions.EmitToolCallStart(ctx, nil, 0, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("expected nil events guard to return nil, got %v", err)
	}

	events := make(chan providertypes.StreamEvent, 1)
	if err := chatcompletions.EmitToolCallStart(ctx, events, 0, "call-1", ""); err != nil {
		t.Fatalf("expected empty name guard to return nil, got %v", err)
	}
	select {
	case <-events:
		t.Fatalf("expected no event for empty name")
	default:
	}

	if err := chatcompletions.EmitToolCallStart(ctx, events, 2, "call-1", "filesystem_edit"); err != nil {
		t.Fatalf("emitToolCallStart() error = %v", err)
	}
	got := <-events
	payload := requireToolCallStartPayload(t, got)
	if got.Type != providertypes.StreamEventToolCallStart || payload.Name != "filesystem_edit" || payload.ID != "call-1" || payload.Index != 2 {
		t.Fatalf("unexpected event: %+v", got)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chatcompletions.EmitToolCallStart(cancelledCtx, make(chan providertypes.StreamEvent), 0, "call-1", "filesystem_edit"); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestMergeToolCallDeltaEmitsStartWhenNameArrivesLater(t *testing.T) {
	t.Parallel()

	events := make(chan providertypes.StreamEvent, 4)
	toolCalls := make(map[int]*providertypes.ToolCall)

	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{Index: 0, ID: "call_late_name"}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}
	select {
	case evt := <-events:
		t.Fatalf("expected no event before tool name arrives, got %+v", evt)
	default:
	}

	if err := chatcompletions.MergeToolCallDelta(context.Background(), events, toolCalls, chatcompletions.ToolCallDelta{
		Index: 0, Function: chatcompletions.FunctionCall{Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
	}); err != nil {
		t.Fatalf("MergeToolCallDelta() error = %v", err)
	}

	start := <-events
	if start.Type != providertypes.StreamEventToolCallStart {
		t.Fatalf("expected tool_call_start event, got %+v", start)
	}
	startPayload := requireToolCallStartPayload(t, start)
	if startPayload.ID != "call_late_name" || startPayload.Name != "filesystem_edit" {
		t.Fatalf("unexpected tool_call_start payload: %+v", startPayload)
	}

	delta := <-events
	if delta.Type != providertypes.StreamEventToolCallDelta {
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

func TestProviderGenerateEmitsToolCallStartEvent(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": 0, "id": "call_tool", "type": "function", "function": map[string]any{"name": "filesystem_edit", "arguments": `{}`}}}}}}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Model: config.OpenAIDefaultModel, Messages: []providertypes.Message{{Role: "user", Content: "edit"}},
		Tools: []providertypes.ToolSpec{{Name: "filesystem_edit", Description: "edit", Schema: map[string]any{"type": "object"}}},
	}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var foundToolCallStart bool
	for _, evt := range drainStreamEvents(events) {
		if evt.Type == providertypes.StreamEventToolCallStart {
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

func TestProviderGenerateEmitsFullEventStream(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "Hello"}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{"index": 0, "id": "call_tool_1", "type": "function", "function": map[string]any{"name": "filesystem_edit", "arguments": `{"path":"a.go"}`}}}}}}})
		writeSSEChunk(t, w, map[string]any{"choices": []map[string]any{{"index": 0, "finish_reason": "tool_calls"}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p, err := New(resolvedConfig(server.URL, config.OpenAIDefaultModel))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.client = server.Client()

	events := make(chan providertypes.StreamEvent, 16)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{Model: config.OpenAIDefaultModel, Messages: []providertypes.Message{{Role: "user", Content: "test"}}}, events)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var foundTextDelta, foundToolCallStart, foundToolCallDelta, foundMessageDone bool
	var toolCallDeltaContent string
	var messageDonePayload *providertypes.MessageDonePayload

	for _, evt := range drainStreamEvents(events) {
		switch evt.Type {
		case providertypes.StreamEventTextDelta:
			foundTextDelta = true
		case providertypes.StreamEventToolCallStart:
			foundToolCallStart = true
			p := requireToolCallStartPayload(t, evt)
			if p.Name != "filesystem_edit" {
				t.Fatalf("expected ToolName %q, got %q", "filesystem_edit", p.Name)
			}
		case providertypes.StreamEventToolCallDelta:
			foundToolCallDelta = true
			toolCallDeltaContent += requireToolCallDeltaPayload(t, evt).ArgumentsDelta
		case providertypes.StreamEventMessageDone:
			foundMessageDone = true
			p := requireMessageDonePayload(t, evt)
			messageDonePayload = &p
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
	if toolCallDeltaContent != `{"path":"a.go"}` {
		t.Fatalf("expected tool call delta content %q, got %q", `{"path":"a.go"}`, toolCallDeltaContent)
	}
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
