package state

import (
	"strings"
	"testing"

	configpkg "neo-code/internal/config"
	providerpkg "neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestCatalogInputFromProviderBuiltinUsesStaticModelsAndDisablesDiscovery(t *testing.T) {
	t.Setenv("CATALOG_PROVIDER_API_KEY", "secret-key")

	cfg := configpkg.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://API.EXAMPLE.COM/v1/",
		Model:     "server-default",
		APIKeyEnv: "CATALOG_PROVIDER_API_KEY",
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
	if input.Identity.ChatEndpointPath != "" {
		t.Fatalf("expected default chat endpoint path to be omitted, got %+v", input.Identity)
	}
	if input.Identity.DiscoveryEndpointPath != providerpkg.DiscoveryEndpointPathModels {
		t.Fatalf("expected default discovery endpoint, got %+v", input.Identity)
	}
	if len(input.DefaultModels) != 1 || input.DefaultModels[0].ID != "model-a" {
		t.Fatalf("expected builtin static models as defaults, got %+v", input.DefaultModels)
	}
	if len(input.ConfiguredModels) != 1 || input.ConfiguredModels[0].ID != "model-a" {
		t.Fatalf("expected configured models to be normalized, got %+v", input.ConfiguredModels)
	}
	if !input.DisableDiscovery {
		t.Fatal("expected builtin provider to disable discovery")
	}

	cfg.Models[0].ID = "mutated"
	if input.ConfiguredModels[0].ID != "model-a" {
		t.Fatalf("expected configured models to be cloned, got %+v", input.ConfiguredModels)
	}

	runtimeConfig, err := input.ResolveDiscoveryConfig()
	if err != nil {
		t.Fatalf("ResolveDiscoveryConfig() error = %v", err)
	}
	if runtimeConfig.DefaultModel != "server-default" {
		t.Fatalf("expected runtime config to resolve model and api key, got %+v", runtimeConfig)
	}
	apiKey, err := runtimeConfig.ResolveAPIKeyValue()
	if err != nil {
		t.Fatalf("ResolveAPIKeyValue() error = %v", err)
	}
	if apiKey != "secret-key" {
		t.Fatalf("expected resolved api key secret-key, got %q", apiKey)
	}
}

func TestCatalogInputFromProviderDefaultsOpenAICompatibleIdentityPaths(t *testing.T) {
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

	if input.Identity.ChatEndpointPath != "" {
		t.Fatalf("expected default chat endpoint path to be omitted, got %+v", input.Identity)
	}
	if input.Identity.DiscoveryEndpointPath != providerpkg.DiscoveryEndpointPathModels {
		t.Fatalf(
			"expected default discovery endpoint %q, got %+v",
			providerpkg.DiscoveryEndpointPathModels,
			input.Identity,
		)
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

func TestCatalogInputFromProviderResolveDiscoveryConfigPropagatesResolveError(t *testing.T) {
	t.Parallel()

	input, err := catalogInputFromProvider(configpkg.ProviderConfig{
		Name:      "company-gateway",
		Driver:    "openaicompat",
		BaseURL:   "https://api.example.com/v1",
		Model:     "server-default",
		APIKeyEnv: "MISSING_PROVIDER_API_KEY",
		Source:    configpkg.ProviderSourceBuiltin,
	})
	if err != nil {
		t.Fatalf("catalogInputFromProvider() error = %v", err)
	}

	runtimeConfig, err := input.ResolveDiscoveryConfig()
	if err != nil {
		t.Fatalf("ResolveDiscoveryConfig() error = %v", err)
	}
	_, err = runtimeConfig.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "environment variable MISSING_PROVIDER_API_KEY is empty") {
		t.Fatalf("expected resolve api key error, got %v", err)
	}
}

func TestSameProviderIdentityPropagatesIdentityErrors(t *testing.T) {
	t.Parallel()

	_, err := sameProviderIdentity(configpkg.ProviderConfig{
		Name:      "broken-left",
		Driver:    "openaicompat",
		BaseURL:   " ",
		APIKeyEnv: "LEFT_KEY",
	}, configpkg.ProviderConfig{
		Name:      "right",
		Driver:    "openaicompat",
		BaseURL:   "https://api.example.com/v1",
		APIKeyEnv: "RIGHT_KEY",
	})
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected left identity error, got %v", err)
	}

	_, err = sameProviderIdentity(configpkg.ProviderConfig{
		Name:      "left",
		Driver:    "openaicompat",
		BaseURL:   "https://api.example.com/v1",
		APIKeyEnv: "LEFT_KEY",
	}, configpkg.ProviderConfig{
		Name:      "broken-right",
		Driver:    "openaicompat",
		BaseURL:   " ",
		APIKeyEnv: "RIGHT_KEY",
	})
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected right identity error, got %v", err)
	}
}

func TestSameSelectionSnapshotReturnsFalseWhenLatestSelectionMissing(t *testing.T) {
	t.Parallel()

	same, err := sameSelectionSnapshot(
		configpkg.Config{
			SelectedProvider: "",
			Providers: []configpkg.ProviderConfig{
				configpkg.OpenAIProvider(),
			},
		},
		configpkg.Config{CurrentModel: configpkg.OpenAIDefaultModel},
		configpkg.OpenAIProvider(),
	)
	if err != nil {
		t.Fatalf("sameSelectionSnapshot() unexpected error = %v", err)
	}
	if same {
		t.Fatalf("expected latest snapshot without selection to be treated as different")
	}
}

func TestSameSelectionSnapshotPropagatesIdentityErrors(t *testing.T) {
	t.Parallel()

	latest := configpkg.Config{
		SelectedProvider: "openai",
		Providers: []configpkg.ProviderConfig{
			{
				Name:      "openai",
				Driver:    "openaicompat",
				BaseURL:   " ",
				Model:     configpkg.OpenAIDefaultModel,
				APIKeyEnv: "OPENAI_API_KEY",
				Source:    configpkg.ProviderSourceBuiltin,
			},
		},
	}

	_, err := sameSelectionSnapshot(
		latest,
		configpkg.Config{
			SelectedProvider: "openai",
			CurrentModel:     configpkg.OpenAIDefaultModel,
			Providers:        []configpkg.ProviderConfig{configpkg.OpenAIProvider()},
		},
		configpkg.OpenAIProvider(),
	)
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected identity error from latest selected provider, got %v", err)
	}
}
