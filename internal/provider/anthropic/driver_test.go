package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"neo-code/internal/provider"
)

func TestDriverBuild(t *testing.T) {
	t.Parallel()

	driver := Driver()
	p, err := driver.Build(context.Background(), provider.RuntimeConfig{
		Driver:  DriverName,
		BaseURL: "https://api.anthropic.com/v1",
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestDriverDiscover(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			t.Fatalf("expected /models or /v1/models path, got %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("expected anthropic x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("expected anthropic-version header")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"id":           "claude-3-7-sonnet",
				"display_name": "Claude 3.7 Sonnet",
			}},
			"has_more": false,
		})
	}))
	defer server.Close()

	driver := Driver()
	models, err := driver.Discover(context.Background(), provider.RuntimeConfig{
		Driver:                DriverName,
		BaseURL:               server.URL,
		APIKey:                "test-key",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "claude-3-7-sonnet" {
		t.Fatalf("unexpected models result: %+v", models)
	}
}

func TestDriverValidateCatalogIdentity(t *testing.T) {
	t.Parallel()

	driver := Driver()

	t.Run("accepts default identity", func(t *testing.T) {
		t.Parallel()

		err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:                DriverName,
			DiscoveryEndpointPath: "/models",
		})
		if err != nil {
			t.Fatalf("expected valid identity, got %v", err)
		}
	})

	t.Run("accepts custom endpoints in sdk mode", func(t *testing.T) {
		t.Parallel()

		err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:                DriverName,
			ChatEndpointPath:      "/gateway/messages",
			DiscoveryEndpointPath: "/custom/models",
		})
		if err != nil {
			t.Fatalf("expected custom endpoints to be accepted, got %v", err)
		}
	})

	t.Run("accepts non-relative endpoints in catalog identity", func(t *testing.T) {
		t.Parallel()

		err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:                DriverName,
			DiscoveryEndpointPath: "https://api.example.com/models",
			ChatEndpointPath:      "https://api.example.com/messages",
		})
		if err != nil {
			t.Fatalf("expected non-relative endpoints to be accepted, got %v", err)
		}
	})
}
