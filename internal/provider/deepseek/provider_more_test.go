package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	"neo-code/internal/provider/openaicompat/chatcompletions"
	providertypes "neo-code/internal/provider/types"
)

func TestDriverBuildAndDiscover(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "deepseek-chat"}},
		})
	}))
	defer server.Close()

	cfg := provider.RuntimeConfig{
		BaseURL:        server.URL,
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		Driver:         DriverName,
	}
	driver := Driver()
	if _, err := driver.Build(context.Background(), cfg); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	models, err := driver.Discover(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "deepseek-chat" {
		t.Fatalf("unexpected models: %+v", models)
	}
	if err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{}); err != nil {
		t.Fatalf("ValidateCatalogIdentity() error = %v", err)
	}
	if _, err := driver.Discover(context.Background(), provider.RuntimeConfig{}); err == nil {
		t.Fatal("expected invalid config discover error")
	}
}

func TestProviderEstimateGenerateAndThinkingErrors(t *testing.T) {
	t.Parallel()

	var authHeader string
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "deepseek-chat"}},
			})
		case "/v1/chat/completions":
			var err error
			requestBody, err = ioReadAll(r)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"choices":[{"delta":{"reasoning_content":"plan","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
				`data: [DONE]`,
				"",
			}, "\n")))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		BaseURL:        server.URL,
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "deepseek-chat",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	req := providertypes.GenerateRequest{
		Model: "deepseek-chat",
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		}},
		ThinkingConfig: &providertypes.ThinkingConfig{Enabled: true, Effort: "high"},
	}
	estimate, err := p.EstimateInputTokens(context.Background(), req)
	if err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}
	if estimate.EstimatedInputTokens <= 0 {
		t.Fatalf("expected positive token estimate, got %+v", estimate)
	}

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "deepseek-chat" {
		t.Fatalf("unexpected models: %+v", models)
	}

	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), req, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	drained := drainDeepSeekEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d", len(drained))
	}
	thinking, err := drained[0].ThinkingDeltaValue()
	if err != nil || thinking.Text != "plan" {
		t.Fatalf("unexpected thinking event: err=%v event=%+v", err, drained[0])
	}
	if authHeader != "Bearer secret" {
		t.Fatalf("authorization header = %q, want bearer token", authHeader)
	}
	if !strings.Contains(string(requestBody), `"reasoning_effort":"high"`) {
		t.Fatalf("request body missing reasoning_effort: %s", string(requestBody))
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "thinking is not supported"},
		})
	}))
	defer errorServer.Close()

	p, err = New(provider.RuntimeConfig{
		BaseURL:        errorServer.URL,
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "deepseek-chat",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = p.Generate(context.Background(), req, make(chan providertypes.StreamEvent, 1))
	if !provider.IsThinkingNotSupportedError(err) {
		t.Fatalf("expected thinking-not-supported error, got %v", err)
	}

	p, err = New(provider.RuntimeConfig{
		BaseURL:        server.URL,
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "deepseek-chat",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.generateClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	err = p.Generate(context.Background(), req, make(chan providertypes.StreamEvent, 1))
	if err == nil || !strings.Contains(err.Error(), "send request") {
		t.Fatalf("expected send request error, got %v", err)
	}
	invalidReq := providertypes.GenerateRequest{
		Model: "deepseek-chat",
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{{Kind: "invalid"}},
		}},
	}
	if _, err := p.EstimateInputTokens(context.Background(), invalidReq); err == nil {
		t.Fatal("expected invalid estimate request error")
	}
	if err := p.Generate(context.Background(), invalidReq, make(chan providertypes.StreamEvent, 1)); err == nil {
		t.Fatal("expected invalid generate request error")
	}
	p.cfg.APIKeyResolver = provider.StaticAPIKeyResolver("")
	if _, err := p.DiscoverModels(context.Background()); err == nil {
		t.Fatal("expected discovery api key error")
	}
	if err := p.generateOnce(context.Background(), chatcompletions.Request{}, nil, make(chan providertypes.StreamEvent, 1)); err == nil {
		t.Fatal("expected api key resolve error")
	}
}

func drainDeepSeekEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
	var drained []providertypes.StreamEvent
	for {
		select {
		case event := <-events:
			drained = append(drained, event)
		default:
			return drained
		}
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
