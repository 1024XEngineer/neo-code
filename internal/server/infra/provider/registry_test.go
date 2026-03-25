package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
)

func TestNormalizeProviderName(t *testing.T) {
	tests := map[string]string{
		"modelscope":  "modelscope",
		"DEEPSEEK":    "deepseek",
		"OPENLL":      "openll",
		"siliconflow": "siliconflow",
		"豆包大模型":       "豆包大模型",
		"openai":      "openai",
	}

	for input, want := range tests {
		got, ok := NormalizeProviderName(input)
		if !ok {
			t.Fatalf("expected provider %q to normalize", input)
		}
		if got != want {
			t.Fatalf("expected normalized provider %q, got %q", want, got)
		}
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	tests := map[string]string{
		"modelscope":  "Qwen/Qwen3-Coder-480B-A35B-Instruct",
		"deepseek":    "deepseek-chat",
		"openll":      "gpt-5.4",
		"siliconflow": "zai-org/GLM-4.6",
		"豆包大模型":       "doubao-pro-v1",
		"openai":      "gpt-5.4",
	}

	for providerName, want := range tests {
		if got := DefaultModelForProvider(providerName); got != want {
			t.Fatalf("expected default model %q for provider %q, got %q", want, providerName, got)
		}
	}
}

func TestResolveChatEndpoint(t *testing.T) {
	cfg := configs.DefaultAppConfig()

	url, err := ResolveChatEndpoint(cfg, cfg.AI.Model)
	if err != nil {
		t.Fatalf("expected modelscope endpoint, got error: %v", err)
	}
	if url == "" {
		t.Fatal("expected modelscope endpoint url")
	}

	cfg.AI.Provider = "openai"
	cfg.AI.Model = "gpt-5.4"
	url, err = ResolveChatEndpoint(cfg, cfg.AI.Model)
	if err != nil {
		t.Fatalf("expected openai endpoint, got error: %v", err)
	}
	if want := "https://api.openai.com/v1/chat/completions"; url != want {
		t.Fatalf("expected endpoint %q, got %q", want, url)
	}

	cfg.AI.Provider = "openll"
	cfg.AI.Model = "gpt-5.4"
	url, err = ResolveChatEndpoint(cfg, cfg.AI.Model)
	if err != nil {
		t.Fatalf("expected openll endpoint, got error: %v", err)
	}
	if want := "https://www.openll.top/v1/chat/completions"; url != want {
		t.Fatalf("expected endpoint %q, got %q", want, url)
	}
}

func TestChatCompletionProviderChatReturnsErrorOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	p := &ChatCompletionProvider{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "missing-model",
	}

	stream, err := p.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		if stream != nil {
			for range stream {
			}
		}
		t.Fatal("expected chat to fail for bad status")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("expected model error in message, got: %v", err)
	}
}

func TestChatCompletionProviderChatStreamsFallbackMessageOnMalformedChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {invalid json}\n"))
	}))
	defer server.Close()

	p := &ChatCompletionProvider{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	}

	stream, err := p.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected stream request to succeed, got error: %v", err)
	}

	var output strings.Builder
	for chunk := range stream {
		output.WriteString(chunk.Content)
	}

	got := output.String()
	if !strings.Contains(got, "[STREAM_ERROR]") {
		t.Fatalf("expected fallback stream error marker, got: %q", got)
	}
	if !strings.Contains(got, "chat stream decode failed") {
		t.Fatalf("expected decode failure details, got: %q", got)
	}
}

func TestChatCompletionProviderChatStreamsFallbackMessageOnUnexpectedEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &ChatCompletionProvider{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
	}

	stream, err := p.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected stream request to succeed, got error: %v", err)
	}

	var output strings.Builder
	for chunk := range stream {
		output.WriteString(chunk.Content)
	}

	got := output.String()
	if !strings.Contains(got, "[STREAM_ERROR]") {
		t.Fatalf("expected fallback stream error marker, got: %q", got)
	}
	if !strings.Contains(got, "unexpectedly before completion") {
		t.Fatalf("expected EOF fallback details, got: %q", got)
	}
}

func TestEmitStreamErrorMessageIgnoresNilError(t *testing.T) {
	out := make(chan domain.ChatEvent, 1)
	emitStreamErrorMessage(context.Background(), out, nil)

	select {
	case got := <-out:
		t.Fatalf("expected no message for nil error, got %q", got.Content)
	default:
	}
}

func TestEmitStreamErrorMessageReturnsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan domain.ChatEvent)
	done := make(chan struct{})

	go func() {
		emitStreamErrorMessage(ctx, out, errors.New("stream failed"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("emitStreamErrorMessage should return quickly when context is canceled")
	}
}

func TestStreamReadErrorForNilAndGenericError(t *testing.T) {
	if got := streamReadError(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}

	got := streamReadError(io.ErrUnexpectedEOF)
	if got == nil {
		t.Fatal("expected wrapped stream read error")
	}
	if !strings.Contains(got.Error(), "chat stream read failed") {
		t.Fatalf("expected generic stream read failure, got %v", got)
	}
}
