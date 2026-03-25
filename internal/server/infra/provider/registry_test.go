package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
)

func TestNormalizeProviderName(t *testing.T) {
	tests := map[string]string{
		"modelscope":  "modelscope",
		"DEEPSEEK":    "deepseek",
		"OPENLL":      "openll",
		"siliconflow": "siliconflow",
		"doubao":      "doubao",
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
		"doubao":      "doubao-pro-v1",
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

	stream, err := p.Chat(context.Background(), []domain.Message{{Role: "user", Content: "hi"}})
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
