package state

import (
	"strings"
	"testing"

	configpkg "neo-code/internal/config"
	providerpkg "neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestCatalogInputFromProviderBuiltinIncludesDefaultsAndLazyDiscovery(t *testing.T) {
	t.Setenv("CATALOG_PROVIDER_API_KEY", "secret-key")

	cfg := configpkg.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://API.EXAMPLE.COM/v1/",
		Model:     "server-default",
		APIKeyEnv: "CATALOG_PROVIDER_API_KEY",
		APIStyle:  " Responses ",
		Models: []providertypes.ModelDescriptor{
			{ID: " model-a ", Name: " Model A "},
		},
		Source: configpkg.ProviderSourceBuiltin,
	}

	input, err := catalogInputFromProvider(cfg)
	if err != nil {
		t.Fatalf("catalogInputFromProvider() error = %v", err)
	}

	if input.Identity.Driver != "openaicompat" {
		t.Fatalf("expected normalized driver, got %+v", input.Identity)
	}
	if input.Identity.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected normalized base URL, got %+v", input.Identity)
	}
	if input.Identity.APIStyle != "responses" {
		t.Fatalf("expected normalized api_style, got %+v", input.Identity)
	}
	if len(input.DefaultModels) != 1 || input.DefaultModels[0].ID != "server-default" {
		t.Fatalf("expected builtin default model, got %+v", input.DefaultModels)
	}
	if len(input.ConfiguredModels) != 1 || input.ConfiguredModels[0].ID != "model-a" {
		t.Fatalf("expected configured models to be normalized, got %+v", input.ConfiguredModels)
	}

	cfg.Models[0].ID = "mutated"
	if input.ConfiguredModels[0].ID != "model-a" {
		t.Fatalf("expected configured models to be cloned, got %+v", input.ConfiguredModels)
	}

	runtimeConfig, err := input.ResolveDiscoveryConfig()
	if err != nil {
		t.Fatalf("ResolveDiscoveryConfig() error = %v", err)
	}
	if runtimeConfig.DefaultModel != "server-default" || runtimeConfig.APIKey != "secret-key" {
		t.Fatalf("expected runtime config to resolve model and api key, got %+v", runtimeConfig)
	}
	if runtimeConfig.APIStyle != " Responses " {
		t.Fatalf("expected runtime config to preserve configured api_style, got %+v", runtimeConfig)
	}
}

func TestCatalogInputFromProviderDefaultsOpenAICompatibleIdentityAPIStyle(t *testing.T) {
	t.Setenv("CATALOG_PROVIDER_API_KEY", "secret-key")

	input, err := catalogInputFromProvider(configpkg.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://API.EXAMPLE.COM/v1/",
		Model:     "server-default",
		APIKeyEnv: "CATALOG_PROVIDER_API_KEY",
		Source:    configpkg.ProviderSourceBuiltin,
	})
	if err != nil {
		t.Fatalf("catalogInputFromProvider() error = %v", err)
	}

	if input.Identity.APIStyle != providerpkg.OpenAICompatibleAPIStyleChatCompletions {
		t.Fatalf(
			"expected default api_style %q, got %+v",
			providerpkg.OpenAICompatibleAPIStyleChatCompletions,
			input.Identity,
		)
	}

	runtimeConfig, err := input.ResolveDiscoveryConfig()
	if err != nil {
		t.Fatalf("ResolveDiscoveryConfig() error = %v", err)
	}
	if runtimeConfig.APIStyle != "" {
		t.Fatalf("expected runtime config to preserve original empty api_style, got %+v", runtimeConfig)
	}
}

func TestCatalogInputFromProviderCustomOmitsDefaultModels(t *testing.T) {
	t.Setenv("CATALOG_PROVIDER_API_KEY", "secret-key")

	input, err := catalogInputFromProvider(configpkg.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://llm.example.com/v1",
		APIKeyEnv: "CATALOG_PROVIDER_API_KEY",
		Source:    configpkg.ProviderSourceCustom,
	})
	if err != nil {
		t.Fatalf("catalogInputFromProvider() error = %v", err)
	}
	if input.DefaultModels != nil {
		t.Fatalf("expected custom provider to omit default models, got %+v", input.DefaultModels)
	}
}

func TestCatalogInputFromProviderPropagatesIdentityErrors(t *testing.T) {
	t.Parallel()

	_, err := catalogInputFromProvider(configpkg.ProviderConfig{
		Name:      "broken-provider",
		Driver:    "openaicompat",
		BaseURL:   "   ",
		APIKeyEnv: "CATALOG_PROVIDER_API_KEY",
		Source:    configpkg.ProviderSourceBuiltin,
	})
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected base_url validation error, got %v", err)
	}
}
