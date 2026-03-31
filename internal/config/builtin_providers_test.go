package config

import (
	"testing"
)

func TestDefaultProvidersReturnsAllBuiltins(t *testing.T) {
	t.Parallel()

	providers := DefaultProviders()
	if len(providers) != 4 {
		t.Fatalf("expected 4 builtin providers, got %d", len(providers))
	}

	expectedNames := []string{OpenAIName, GeminiName, OpenLLName, QiniuName}
	for i, provider := range providers {
		if provider.Name != expectedNames[i] {
			t.Fatalf("expected provider[%d] name %q, got %q", i, expectedNames[i], provider.Name)
		}
	}
}

func TestOpenAIProviderConfig(t *testing.T) {
	t.Parallel()

	provider := OpenAIProvider()

	if provider.Name != OpenAIName {
		t.Fatalf("expected name %q, got %q", OpenAIName, provider.Name)
	}
	if provider.Driver != "openai" {
		t.Fatalf("expected driver %q, got %q", "openai", provider.Driver)
	}
	if provider.BaseURL != OpenAIDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", OpenAIDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != OpenAIDefaultModel {
		t.Fatalf("expected default model %q, got %q", OpenAIDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != OpenAIDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", OpenAIDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if len(provider.Models) == 0 {
		t.Fatal("expected non-empty models list")
	}
	if !ContainsModelID(provider.Models, OpenAIDefaultModel) {
		t.Fatalf("expected models to contain default model %q", OpenAIDefaultModel)
	}
}

func TestGeminiProviderConfig(t *testing.T) {
	t.Parallel()

	provider := GeminiProvider()

	if provider.Name != GeminiName {
		t.Fatalf("expected name %q, got %q", GeminiName, provider.Name)
	}
	if provider.Driver != "openai" {
		t.Fatalf("expected driver %q, got %q", "openai", provider.Driver)
	}
	if provider.BaseURL != GeminiDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", GeminiDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != GeminiDefaultModel {
		t.Fatalf("expected default model %q, got %q", GeminiDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != GeminiDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", GeminiDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if len(provider.Models) == 0 {
		t.Fatal("expected non-empty models list")
	}
	if !ContainsModelID(provider.Models, GeminiDefaultModel) {
		t.Fatalf("expected models to contain default model %q", GeminiDefaultModel)
	}
}

func TestOpenLLProviderConfig(t *testing.T) {
	t.Parallel()

	provider := OpenLLProvider()

	if provider.Name != OpenLLName {
		t.Fatalf("expected name %q, got %q", OpenLLName, provider.Name)
	}
	if provider.Driver != "openai" {
		t.Fatalf("expected driver %q, got %q", "openai", provider.Driver)
	}
	if provider.BaseURL != OpenLLDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", OpenLLDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != OpenLLDefaultModel {
		t.Fatalf("expected default model %q, got %q", OpenLLDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != OpenLLDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", OpenLLDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if len(provider.Models) == 0 {
		t.Fatal("expected non-empty models list")
	}
	if !ContainsModelID(provider.Models, OpenLLDefaultModel) {
		t.Fatalf("expected models to contain default model %q", OpenLLDefaultModel)
	}
}

func TestProviderModelsAreImmutable(t *testing.T) {
	t.Parallel()

	// Verify that modifying returned models slice doesn't affect future calls
	provider1 := OpenAIProvider()
	provider1.Models[0] = "modified-model"

	provider2 := OpenAIProvider()
	if provider2.Models[0] == "modified-model" {
		t.Fatal("expected models slice to be independent between calls")
	}
	if provider2.Models[0] != OpenAIDefaultModel {
		t.Fatalf("expected first model %q, got %q", OpenAIDefaultModel, provider2.Models[0])
	}
}

func TestDefaultProvidersReturnsIndependentSlices(t *testing.T) {
	t.Parallel()

	providers1 := DefaultProviders()
	providers1[0].Name = "modified"

	providers2 := DefaultProviders()
	if providers2[0].Name == "modified" {
		t.Fatal("expected DefaultProviders to return independent slices")
	}
	if providers2[0].Name != OpenAIName {
		t.Fatalf("expected first provider name %q, got %q", OpenAIName, providers2[0].Name)
	}
}
