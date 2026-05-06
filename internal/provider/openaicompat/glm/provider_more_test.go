package glm

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

	cfg := provider.RuntimeConfig{
		BaseURL:        "https://example.com",
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		Driver:         DriverName,
	}
	driver := Driver()
	if _, err := driver.Build(context.Background(), cfg); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if driver.Discover != nil {
		t.Fatal("expected Discover to be nil for built-in driver")
	}
	if err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{}); err != nil {
		t.Fatalf("ValidateCatalogIdentity() error = %v", err)
	}
}

func TestProviderEstimateGenerateAndThinkingErrors(t *testing.T) {
	t.Parallel()

	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			var err error
			requestBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
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
		DefaultModel:   "glm-5.1",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := providertypes.GenerateRequest{
		Model: "glm-5.1",
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		}},
		ThinkingConfig: &providertypes.ThinkingConfig{Enabled: false},
	}
	if _, err := p.EstimateInputTokens(context.Background(), req); err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}
	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), req, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	drained := drainGLMEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d", len(drained))
	}
	if !strings.Contains(string(requestBody), `"enable_thinking":false`) ||
		!strings.Contains(string(requestBody), `"clear_thinking":true`) {
		t.Fatalf("request body missing glm params: %s", string(requestBody))
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "reasoning unsupported"},
		})
	}))
	defer errorServer.Close()

	p, err = New(provider.RuntimeConfig{
		BaseURL:        errorServer.URL,
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "glm-5.1",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = p.Generate(context.Background(), req, make(chan providertypes.StreamEvent, 1))
	if !provider.IsThinkingNotSupportedError(err) {
		t.Fatalf("expected thinking-not-supported error, got %v", err)
	}

	if _, err := New(provider.RuntimeConfig{APIKeyEnv: "KEY"}); err == nil {
		t.Fatal("expected base url validation error")
	}
	if _, err := New(provider.RuntimeConfig{BaseURL: "https://example.com"}); err == nil {
		t.Fatal("expected api key env validation error")
	}
	p.generateClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	if err := p.Generate(context.Background(), req, make(chan providertypes.StreamEvent, 1)); err == nil || !strings.Contains(err.Error(), "send request") {
		t.Fatalf("expected send request error, got %v", err)
	}
	invalidReq := providertypes.GenerateRequest{
		Model: "glm-5.1",
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
	if err := p.generateOnce(context.Background(), chatcompletions.Request{}, nil, make(chan providertypes.StreamEvent, 1)); err == nil {
		t.Fatal("expected api key resolve error")
	}
}

func drainGLMEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
