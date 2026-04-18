package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"neo-code/internal/provider"
)

func TestDriverDiscover(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "gemini-2.5-flash"},
			},
		})
	}))
	defer server.Close()

	driver := Driver()
	models, err := driver.Discover(context.Background(), provider.RuntimeConfig{
		Driver:                DriverName,
		BaseURL:               server.URL,
		APIKey:                "test-key",
		DiscoveryProtocol:     provider.DiscoveryProtocolGeminiModels,
		ResponseProfile:       provider.DiscoveryResponseProfileGemini,
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected one model, got %+v", models)
	}
}

func TestDriverBuild(t *testing.T) {
	t.Parallel()

	driver := Driver()
	p, err := driver.Build(context.Background(), provider.RuntimeConfig{
		Driver:       DriverName,
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
		APIKey:       "test-key",
		ChatProtocol: provider.ChatProtocolOpenAIChatCompletions,
		AuthStrategy: provider.AuthStrategyBearer,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestDriverValidateCatalogIdentity(t *testing.T) {
	t.Parallel()

	driver := Driver()

	t.Run("valid identity", func(t *testing.T) {
		t.Parallel()

		err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:                DriverName,
			ChatProtocol:          provider.ChatProtocolOpenAIChatCompletions,
			DiscoveryProtocol:     provider.DiscoveryProtocolGeminiModels,
			DiscoveryEndpointPath: "/models",
			AuthStrategy:          provider.AuthStrategyBearer,
			ResponseProfile:       provider.DiscoveryResponseProfileGemini,
		})
		if err != nil {
			t.Fatalf("expected valid identity, got %v", err)
		}
	})

	t.Run("invalid discovery protocol", func(t *testing.T) {
		t.Parallel()

		err := driver.ValidateCatalogIdentity(provider.ProviderIdentity{
			Driver:                DriverName,
			ChatProtocol:          provider.ChatProtocolOpenAIChatCompletions,
			DiscoveryProtocol:     "unknown-discovery",
			DiscoveryEndpointPath: "/models",
			AuthStrategy:          provider.AuthStrategyBearer,
			ResponseProfile:       provider.DiscoveryResponseProfileGemini,
		})
		if err == nil {
			t.Fatal("expected discovery config error")
		}
		if !provider.IsDiscoveryConfigError(err) {
			t.Fatalf("expected discovery config error, got %v", err)
		}
		if !strings.Contains(err.Error(), "discovery protocol") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}
