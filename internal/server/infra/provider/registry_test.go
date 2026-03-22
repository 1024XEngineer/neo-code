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

func TestSupportedModelsForConfigNonCatalogProvider(t *testing.T) {
	cfg := configs.DefaultAppConfig()
	cfg.AI.Provider = "deepseek"
	cfg.AI.Model = "deepseek-chat"

	models := SupportedModelsForConfig(cfg)
	if len(models) != 0 {
		t.Fatalf("expected no built-in model list, got %v", models)
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
	cfg.AI.Model = "gpt-4o-mini"
	url, err = ResolveChatEndpoint(cfg, cfg.AI.Model)
	if err != nil {
		t.Fatalf("expected openai endpoint, got error: %v", err)
	}
	if want := "https://api.openai.com/v1/chat/completions"; url != want {
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
