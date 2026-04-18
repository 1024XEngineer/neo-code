package httpdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestDiscoverRawModels(t *testing.T) {
	t.Parallel()

	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if r.URL.Path != "/models" {
			t.Fatalf("expected /models path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1"},
			},
		})
	}))
	defer server.Close()

	models, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
		BaseURL:           server.URL,
		DiscoveryProtocol: provider.DiscoveryProtocolOpenAIModels,
		AuthStrategy:      provider.AuthStrategyBearer,
		APIKey:            "test-key",
	})
	if err != nil {
		t.Fatalf("DiscoverRawModels() error = %v", err)
	}
	if authHeader != "Bearer test-key" {
		t.Fatalf("expected bearer auth header, got %q", authHeader)
	}
	if len(models) != 1 || models[0]["id"] != "gpt-4.1" {
		t.Fatalf("unexpected models result: %+v", models)
	}
}

func TestDiscoverRawModelsRejectsInvalidEndpointPath(t *testing.T) {
	t.Parallel()

	_, err := DiscoverRawModels(context.Background(), &http.Client{}, RequestConfig{
		BaseURL:      "https://api.example.com/v1",
		EndpointPath: "https://api.example.com/models",
	})
	if err == nil || !provider.IsDiscoveryConfigError(err) {
		t.Fatalf("expected discovery config error, got %v", err)
	}
}

func TestDiscoverRawModelsRejectsTooLargeResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}],"padding":"` + strings.Repeat("x", int(maxDiscoveryResponseBodyBytes)) + `"}`))
	}))
	defer server.Close()

	_, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
		BaseURL:           server.URL,
		DiscoveryProtocol: provider.DiscoveryProtocolOpenAIModels,
	})
	if err == nil {
		t.Fatal("expected oversized body error")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected provider error, got %T: %v", err, err)
	}
	if pErr.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d", pErr.StatusCode)
	}
}
