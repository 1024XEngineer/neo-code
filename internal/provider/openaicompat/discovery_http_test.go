package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type timeoutNetError struct {
	message string
}

func (e timeoutNetError) Error() string {
	return e.message
}

func (e timeoutNetError) Timeout() bool {
	return true
}

func (e timeoutNetError) Temporary() bool {
	return true
}

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
		Driver:       provider.DriverOpenAICompat,
		BaseURL:      server.URL,
		EndpointPath: "/models",
		APIKey:       "test-key",
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
		BaseURL:      server.URL,
		EndpointPath: "/models",
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

func TestDiscoverRawModelsReturnsHTTPClassifiedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		wantCode   provider.ProviderErrorCode
		wantPart   string
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantCode:   provider.ErrorCodeAuthFailed,
			wantPart:   "unauthorized (401)",
		},
		{
			name:       "forbidden",
			statusCode: http.StatusForbidden,
			wantCode:   provider.ErrorCodeForbidden,
			wantPart:   "forbidden (403)",
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantCode:   provider.ErrorCodeNotFound,
			wantPart:   "endpoint not found (404)",
		},
		{
			name:       "server error",
			statusCode: http.StatusBadGateway,
			wantCode:   provider.ErrorCodeServer,
			wantPart:   "upstream server error",
		},
		{
			name:       "generic client error",
			statusCode: http.StatusBadRequest,
			wantCode:   provider.ErrorCodeClient,
			wantPart:   "status=400",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, "error")
			}))
			defer server.Close()

			_, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
				BaseURL:      server.URL,
				EndpointPath: "/models",
			})
			if err == nil {
				t.Fatal("expected provider error")
			}
			var pErr *provider.ProviderError
			if !errors.As(err, &pErr) {
				t.Fatalf("expected provider error, got %T: %v", err, err)
			}
			if pErr.StatusCode != tt.statusCode {
				t.Fatalf("expected status %d, got %d", tt.statusCode, pErr.StatusCode)
			}
			if pErr.Code != tt.wantCode {
				t.Fatalf("expected code %q, got %q", tt.wantCode, pErr.Code)
			}
			if !strings.Contains(pErr.Message, tt.wantPart) {
				t.Fatalf("expected message to contain %q, got %q", tt.wantPart, pErr.Message)
			}
		})
	}
}

func TestDiscoverRawModelsIncludesSanitizedHTTPErrorBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "invalid\napi\tkey\x00")
	}))
	defer server.Close()

	_, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
		BaseURL:      server.URL,
		EndpointPath: "/models",
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected provider error, got %T: %v", err, err)
	}
	if pErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", pErr.StatusCode)
	}
	if !strings.Contains(pErr.Message, "upstream body: invalid api key") {
		t.Fatalf("expected sanitized upstream body summary, got %q", pErr.Message)
	}
}

func TestDiscoverRawModelsRedactsSensitiveHTTPErrorBody(t *testing.T) {
	t.Parallel()

	const (
		bearerSecret = "sk-secret-value-123456"
		apiKeySecret = "secret-api-key-value"
		authSecret   = "raw-auth-header-value"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(
			w,
			`authorization: Bearer `+bearerSecret+
				` x-api-key: `+apiKeySecret+
				` {"api_key":"`+apiKeySecret+`","authorization":"`+authSecret+`"}`,
		)
	}))
	defer server.Close()

	_, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
		BaseURL:      server.URL,
		EndpointPath: "/models",
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected provider error, got %T: %v", err, err)
	}

	for _, secret := range []string{bearerSecret, apiKeySecret, authSecret} {
		if strings.Contains(pErr.Message, secret) {
			t.Fatalf("expected secret %q to be redacted, got %q", secret, pErr.Message)
		}
	}
	if !strings.Contains(pErr.Message, "[REDACTED]") {
		t.Fatalf("expected redaction marker in message, got %q", pErr.Message)
	}
}

func TestDiscoverRawModelsTruncatesHTTPErrorBodySummary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", int(maxHTTPErrorSummaryBytes)+64))
	}))
	defer server.Close()

	_, err := DiscoverRawModels(context.Background(), server.Client(), RequestConfig{
		BaseURL:      server.URL,
		EndpointPath: "/models",
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected provider error, got %T: %v", err, err)
	}
	if !strings.Contains(pErr.Message, "...(truncated)") {
		t.Fatalf("expected truncated marker in message, got %q", pErr.Message)
	}
}

func TestDiscoverRawModelsReturnsTransportErrors(t *testing.T) {
	t.Parallel()

	t.Run("timeout via deadline exceeded", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			}),
		}
		_, err := DiscoverRawModels(context.Background(), client, RequestConfig{
			BaseURL:      "https://api.example.com",
			EndpointPath: "/models",
		})
		assertTransportProviderError(t, err, provider.ErrorCodeTimeout, "timeout")
	})

	t.Run("timeout via net error", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return nil, timeoutNetError{message: "i/o timeout"}
			}),
		}
		_, err := DiscoverRawModels(context.Background(), client, RequestConfig{
			BaseURL:      "https://api.example.com",
			EndpointPath: "/models",
		})
		assertTransportProviderError(t, err, provider.ErrorCodeTimeout, "i/o timeout")
	})

	t.Run("network error", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}
			}),
		}
		_, err := DiscoverRawModels(context.Background(), client, RequestConfig{
			BaseURL:      "https://api.example.com",
			EndpointPath: "/models",
		})
		assertTransportProviderError(t, err, provider.ErrorCodeNetwork, "send models request")
	})
}

func TestDiscoverRawModelsContextAndClientValidation(t *testing.T) {
	t.Parallel()

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := DiscoverRawModels(ctx, &http.Client{}, RequestConfig{
			BaseURL:      "https://api.example.com",
			EndpointPath: "/models",
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()

		_, err := DiscoverRawModels(context.Background(), nil, RequestConfig{BaseURL: "https://api.example.com"})
		if err == nil || err.Error() != "provider discovery: http client is nil" {
			t.Fatalf("expected nil client error, got %v", err)
		}
	})
}

func TestRequestConfigFromRuntime(t *testing.T) {
	t.Parallel()

	cfg, err := RequestConfigFromRuntime(provider.RuntimeConfig{
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://api.openai.com/v1",
		APIKey:                "test-key",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("RequestConfigFromRuntime() error = %v", err)
	}
	if cfg.EndpointPath != provider.DiscoveryEndpointPathModels {
		t.Fatalf("expected endpoint path %q, got %q", provider.DiscoveryEndpointPathModels, cfg.EndpointPath)
	}
	if cfg.APIKey != "test-key" {
		t.Fatalf("expected api key to be preserved, got %q", cfg.APIKey)
	}
	if cfg.Driver != provider.DriverOpenAICompat {
		t.Fatalf("expected driver %q, got %q", provider.DriverOpenAICompat, cfg.Driver)
	}
}

func TestRequestConfigFromRuntimeRejectsInvalidDiscoveryPath(t *testing.T) {
	t.Parallel()

	_, err := RequestConfigFromRuntime(provider.RuntimeConfig{
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://api.example.com/v1",
		DiscoveryEndpointPath: "https://api.example.com/models",
	})
	if err == nil {
		t.Fatal("expected discovery config error")
	}
	if !provider.IsDiscoveryConfigError(err) {
		t.Fatalf("expected discovery config error, got %v", err)
	}
}

func TestRequestConfigFromRuntimeDefaultsEmptyDiscoveryPath(t *testing.T) {
	t.Parallel()

	cfg, err := RequestConfigFromRuntime(provider.RuntimeConfig{
		Driver:  provider.DriverOpenAICompat,
		BaseURL: "https://api.example.com/v1",
	})
	if err != nil {
		t.Fatalf("RequestConfigFromRuntime() error = %v", err)
	}
	if cfg.EndpointPath != provider.DiscoveryEndpointPathModels {
		t.Fatalf("expected endpoint path %q, got %q", provider.DiscoveryEndpointPathModels, cfg.EndpointPath)
	}
}

func TestDiscoverModelDescriptors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4.1", "name": "GPT 4.1"},
				{"id": "gpt-4.1", "name": "GPT 4.1 Duplicate"},
				{"name": "missing-id"},
			},
		})
	}))
	defer server.Close()

	models, err := DiscoverModelDescriptors(context.Background(), server.Client(), RequestConfig{
		BaseURL:      server.URL,
		EndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("DiscoverModelDescriptors() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 merged descriptors, got %d (%+v)", len(models), models)
	}
	if models[0].ID != "gpt-4.1" {
		t.Fatalf("expected descriptor id gpt-4.1, got %q", models[0].ID)
	}
	if models[1].ID != "missing-id" {
		t.Fatalf("expected fallback descriptor id missing-id, got %q", models[1].ID)
	}
}

func assertTransportProviderError(t *testing.T, err error, wantCode provider.ProviderErrorCode, wantPart string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected provider error")
	}
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected provider error, got %T: %v", err, err)
	}
	if pErr.Code != wantCode {
		t.Fatalf("expected code %q, got %q", wantCode, pErr.Code)
	}
	if !strings.Contains(pErr.Message, wantPart) {
		t.Fatalf("expected message to contain %q, got %q", wantPart, pErr.Message)
	}
	if pErr.StatusCode != 0 {
		t.Fatalf("expected status 0 for transport error, got %d", pErr.StatusCode)
	}
}
