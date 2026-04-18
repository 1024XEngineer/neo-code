package shared

import (
	"net/http"
	"testing"

	"neo-code/internal/provider"
)

func TestValidateRuntimeConfig(t *testing.T) {
	t.Parallel()

	t.Run("empty base url", func(t *testing.T) {
		t.Parallel()

		err := ValidateRuntimeConfig(provider.RuntimeConfig{
			BaseURL: "",
			APIKey:  "test-key",
		})
		if err == nil || err.Error() != ErrorPrefix+"base url is empty" {
			t.Fatalf("expected base url error, got %v", err)
		}
	})

	t.Run("empty api key", func(t *testing.T) {
		t.Parallel()

		err := ValidateRuntimeConfig(provider.RuntimeConfig{
			BaseURL: "https://api.example.com/v1",
			APIKey:  "   ",
		})
		if err == nil || err.Error() != ErrorPrefix+"api key is empty" {
			t.Fatalf("expected api key error, got %v", err)
		}
	})

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()

		err := ValidateRuntimeConfig(provider.RuntimeConfig{
			BaseURL: " https://api.example.com/v1 ",
			APIKey:  " test-key ",
		})
		if err != nil {
			t.Fatalf("expected valid config, got %v", err)
		}
	})
}

func TestEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{
			name:    "trims whitespace and trailing slash",
			baseURL: " https://api.example.com/v1/ ",
			path:    "/models",
			want:    "https://api.example.com/v1/models",
		},
		{
			name:    "adds leading slash for path",
			baseURL: "https://api.example.com/v1",
			path:    "chat/completions",
			want:    "https://api.example.com/v1/chat/completions",
		},
		{
			name:    "empty path returns normalized base",
			baseURL: "https://api.example.com/v1///",
			path:    "",
			want:    "https://api.example.com/v1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Endpoint(tt.baseURL, tt.path); got != tt.want {
				t.Fatalf("Endpoint(%q, %q) = %q, want %q", tt.baseURL, tt.path, got, tt.want)
			}
		})
	}
}

func TestSetBearerAuthorization(t *testing.T) {
	t.Parallel()

	t.Run("nil header is ignored", func(t *testing.T) {
		t.Parallel()

		SetBearerAuthorization(nil, "test-key")
	})

	t.Run("empty api key is ignored", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		SetBearerAuthorization(header, "   ")
		if got := header.Get("Authorization"); got != "" {
			t.Fatalf("expected no authorization header, got %q", got)
		}
	})

	t.Run("sets bearer authorization", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		SetBearerAuthorization(header, " test-key ")
		if got := header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("expected bearer authorization, got %q", got)
		}
	})

	t.Run("overwrites existing authorization", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		header.Set("Authorization", "Basic abc")
		SetBearerAuthorization(header, "next-key")
		if got := header.Get("Authorization"); got != "Bearer next-key" {
			t.Fatalf("expected authorization overwrite, got %q", got)
		}
	})
}

func TestApplyAuthHeaders(t *testing.T) {
	t.Parallel()

	t.Run("x_api_key", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		ApplyAuthHeaders(header, provider.RuntimeConfig{
			AuthStrategy: provider.AuthStrategyXAPIKey,
			APIKey:       "x-key",
		})
		if got := header.Get("X-API-Key"); got != "x-key" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
	})

	t.Run("anthropic default version", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		ApplyAuthHeaders(header, provider.RuntimeConfig{
			AuthStrategy: provider.AuthStrategyAnthropic,
			APIKey:       "anthropic-key",
		})
		if got := header.Get("x-api-key"); got != "anthropic-key" {
			t.Fatalf("expected anthropic x-api-key, got %q", got)
		}
		if got := header.Get("anthropic-version"); got == "" {
			t.Fatal("expected default anthropic version")
		}
	})

	t.Run("anthropic explicit version", func(t *testing.T) {
		t.Parallel()

		header := http.Header{}
		ApplyAuthHeaders(header, provider.RuntimeConfig{
			AuthStrategy: provider.AuthStrategyAnthropic,
			APIKey:       "anthropic-key",
			APIVersion:   "2024-01-01",
		})
		if got := header.Get("anthropic-version"); got != "2024-01-01" {
			t.Fatalf("expected explicit anthropic version, got %q", got)
		}
	})
}
