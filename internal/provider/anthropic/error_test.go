package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestMapAnthropicSDKErrorWithAPIError(t *testing.T) {
	// 构造一个带 HTTP 429 的 API 错误场景
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Rate exceeded"}}`)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:               provider.DriverAnthropic,
		BaseURL:              server.URL,
		DefaultModel:         "claude-3-7-sonnet",
		APIKeyEnv:            "ANTHROPIC_TEST_KEY",
		APIKeyResolver:       provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries:   0,
		GenerateStartTimeout: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if providerErr.Code != provider.ErrorCodeRateLimit {
		t.Fatalf("expected rate_limit code, got %q", providerErr.Code)
	}
}

func TestMapAnthropicSDKErrorWithAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"Invalid key"}}`)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:               provider.DriverAnthropic,
		BaseURL:              server.URL,
		DefaultModel:         "claude-3-7-sonnet",
		APIKeyEnv:            "ANTHROPIC_TEST_KEY",
		APIKeyResolver:       provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries:   0,
		GenerateStartTimeout: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if providerErr.Code != provider.ErrorCodeAuthFailed {
		t.Fatalf("expected auth_failed code, got %q", providerErr.Code)
	}
}

func TestMapAnthropicSDKErrorServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"server_error","message":"Internal"}}`)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:               provider.DriverAnthropic,
		BaseURL:              server.URL,
		DefaultModel:         "claude-3-7-sonnet",
		APIKeyEnv:            "ANTHROPIC_TEST_KEY",
		APIKeyResolver:       provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries:   0,
		GenerateStartTimeout: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if providerErr.Code != provider.ErrorCodeServer {
		t.Fatalf("expected server code, got %q", providerErr.Code)
	}
	if !providerErr.Retryable {
		t.Fatal("expected 500 to be retryable")
	}
}

func TestMapAnthropicSDKErrorContextTooLong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"max context length exceeded"}}`)
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:               provider.DriverAnthropic,
		BaseURL:              server.URL,
		DefaultModel:         "claude-3-7-sonnet",
		APIKeyEnv:            "ANTHROPIC_TEST_KEY",
		APIKeyResolver:       provider.StaticAPIKeyResolver("test-key"),
		GenerateMaxRetries:   0,
		GenerateStartTimeout: 0,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	var providerErr *provider.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if providerErr.Code != provider.ErrorCodeContextTooLong {
		t.Fatalf("expected context_too_long code, got %q", providerErr.Code)
	}
}

func TestToAnthropicToolSchemaValid(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "file path",
			},
		},
		"required": []any{"path"},
	}
	param, err := toAnthropicToolSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// marshal back to verify structure
	raw, _ := json.Marshal(param)
	if !strings.Contains(string(raw), "path") {
		t.Fatalf("expected path in marshalled schema, got %s", string(raw))
	}
}

func TestToAnthropicToolSchemaEmptySchema(t *testing.T) {
	param, err := toAnthropicToolSchema(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(param)
	if string(raw) == "" {
		t.Fatal("expected non-empty marshalled schema")
	}
}

func TestToAnthropicToolSchemaNonObjectType(t *testing.T) {
	schema := map[string]any{
		"type": "string",
	}
	param, err := toAnthropicToolSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, _ := json.Marshal(param)
	if !strings.Contains(string(raw), "object") {
		t.Fatalf("expected schema to be normalized to object type, got %s", string(raw))
	}
}

func TestGenerateEmptyStreamHandled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	p, err := New(provider.RuntimeConfig{
		Driver:         provider.DriverAnthropic,
		BaseURL:        server.URL,
		DefaultModel:   "claude-3-7-sonnet",
		APIKeyEnv:      "ANTHROPIC_TEST_KEY",
		APIKeyResolver: provider.StaticAPIKeyResolver("test-key"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	events := make(chan providertypes.StreamEvent, 8)
	err = p.Generate(context.Background(), providertypes.GenerateRequest{
		Messages: []providertypes.Message{{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")},
		}},
	}, events)
	if err == nil {
		t.Fatal("expected error for empty stream")
	}
	if !errors.Is(err, provider.ErrStreamInterrupted) {
		t.Fatalf("expected ErrStreamInterrupted, got %v", err)
	}
}
