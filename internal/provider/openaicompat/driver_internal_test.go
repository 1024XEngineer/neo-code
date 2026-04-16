package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestDriverClosuresAndAPIStyle(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gpt-4.1", "name": "GPT-4.1"}},
		})
	}))
	defer server.Close()

	cfg := provider.RuntimeConfig{
		Name:         DriverName,
		Driver:       DriverName,
		BaseURL:      server.URL,
		DefaultModel: "gpt-4.1",
		APIKey:       "test-key",
	}
	driver := Driver()

	built, err := driver.Build(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	typed, ok := built.(*Provider)
	if !ok || typed.client == nil || typed.client.Transport == nil {
		t.Fatalf("unexpected built provider: %T %+v", built, typed)
	}

	models, err := driver.Discover(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4.1" {
		t.Fatalf("unexpected models: %+v", models)
	}

	if got := normalizedAPIStyle(""); got != provider.OpenAICompatibleAPIStyleChatCompletions {
		t.Fatalf("expected default api style, got %q", got)
	}
	if got := normalizedAPIStyle(" Responses "); got != "responses" {
		t.Fatalf("expected normalized responses style, got %q", got)
	}
	if got, err := supportedAPIStyle(""); err != nil || got != provider.OpenAICompatibleAPIStyleChatCompletions {
		t.Fatalf("expected supported default api style, got style=%q err=%v", got, err)
	}
	if _, err := supportedAPIStyle(" Responses "); err == nil || !strings.Contains(err.Error(), `api_style "responses" is not supported yet`) {
		t.Fatalf("expected unsupported responses api_style, got %v", err)
	}
	if _, err := supportedAPIStyle("custom_style"); err == nil || !strings.Contains(err.Error(), `unsupported api_style "custom_style"`) {
		t.Fatalf("expected unsupported custom api_style, got %v", err)
	}
}

func TestFetchModelsAndGenerateExtraBranches(t *testing.T) {
	t.Parallel()

	p := &Provider{
		cfg: provider.RuntimeConfig{
			Name:    DriverName,
			Driver:  DriverName,
			BaseURL: "://bad",
			APIKey:  "test-key",
		},
		client: &http.Client{},
	}
	if _, err := p.fetchModels(context.Background()); err == nil || !strings.Contains(err.Error(), "build models request") {
		t.Fatalf("expected build models request error, got %v", err)
	}

	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	p = &Provider{
		cfg: provider.RuntimeConfig{
			Name:    DriverName,
			Driver:  DriverName,
			BaseURL: server.URL,
			APIKey:  "   ",
		},
		client: server.Client(),
	}
	if _, err := p.fetchModels(context.Background()); err != nil {
		t.Fatalf("fetchModels() error = %v", err)
	}
	if auth != "" {
		t.Fatalf("expected no authorization header, got %q", auth)
	}

	p, err := New(provider.RuntimeConfig{
		Name:         DriverName,
		Driver:       DriverName,
		BaseURL:      "https://api.example.com/v1",
		DefaultModel: "gpt-4.1",
		APIKey:       "test-key",
		APIStyle:     "custom_style",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `unsupported api_style "custom_style"`) {
		t.Fatalf("expected unsupported api_style error, got %v", err)
	}

	p, err = New(provider.RuntimeConfig{
		Name:         DriverName,
		Driver:       DriverName,
		BaseURL:      "https://api.example.com/v1",
		DefaultModel: "gpt-4.1",
		APIKey:       "test-key",
		APIStyle:     "responses",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := p.DiscoverModels(context.Background()); err == nil || !strings.Contains(err.Error(), `api_style "responses" is not supported yet`) {
		t.Fatalf("expected discovery to reject responses api_style, got %v", err)
	}

	p, err = New(provider.RuntimeConfig{
		Name:         DriverName,
		Driver:       DriverName,
		BaseURL:      "https://api.example.com/v1",
		DefaultModel: "gpt-4.1",
		APIKey:       "test-key",
		APIStyle:     "custom_style",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := p.DiscoverModels(context.Background()); err == nil || !strings.Contains(err.Error(), `unsupported api_style "custom_style"`) {
		t.Fatalf("expected discovery to reject custom api_style, got %v", err)
	}
}
