package minimax

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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "minimax-m2"}},
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
	if len(models) != 1 || models[0].ID != "minimax-m2" {
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

	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "minimax-m2"}},
			})
		case "/chat/completions", "/":
			var err error
			requestBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			_, _ = w.Write([]byte(strings.Join([]string{
				`data: {"choices":[{"delta":{"reasoning_details":"plan","content":"answer"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`,
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
		DefaultModel:   "minimax-m2",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := providertypes.GenerateRequest{
		Model: "minimax-m2",
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
		}},
	}
	if _, err := p.EstimateInputTokens(context.Background(), req); err != nil {
		t.Fatalf("EstimateInputTokens() error = %v", err)
	}
	if _, err := p.DiscoverModels(context.Background()); err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	p, err = New(provider.RuntimeConfig{
		BaseURL:        server.URL + "/chat/completions",
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "minimax-m2",
		Driver:         DriverName,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	events := make(chan providertypes.StreamEvent, 8)
	if err := p.Generate(context.Background(), req, events); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	drained := drainMiniMaxProviderEvents(events)
	if len(drained) != 3 {
		t.Fatalf("expected 3 events, got %d", len(drained))
	}
	if !strings.Contains(string(requestBody), `"reasoning_split":true`) ||
		!strings.Contains(string(requestBody), `"enable_thinking":true`) {
		t.Fatalf("request body missing minimax params: %s", string(requestBody))
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "thinking unsupported"},
		})
	}))
	defer errorServer.Close()

	p, err = New(provider.RuntimeConfig{
		BaseURL:        errorServer.URL + "/chat/completions",
		APIKeyEnv:      "TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("secret"),
		DefaultModel:   "minimax-m2",
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
		Model: "minimax-m2",
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
	if err := p.generateOnce(context.Background(), chatcompletions.Request{}, make(chan providertypes.StreamEvent, 1)); err == nil {
		t.Fatal("expected api key resolve error")
	}
}

func drainMiniMaxProviderEvents(events <-chan providertypes.StreamEvent) []providertypes.StreamEvent {
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
