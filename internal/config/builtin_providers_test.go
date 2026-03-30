package config

import (
	"testing"
)

func TestDefaultProvidersReturnsAllBuiltins(t *testing.T) {
	t.Parallel()

	providers := DefaultProviders()
	if len(providers) != 3 {
		t.Fatalf("expected 3 builtin providers, got %d", len(providers))
	}

	expectedNames := []string{OpenAIName, GeminiName, OpenLLName}
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

func TestProviderConstants(t *testing.T) {
	t.Parallel()

	// Verify all constants are non-empty
	if OpenAIName == "" {
		t.Fatal("OpenAIName should not be empty")
	}
	if OpenAIDefaultBaseURL == "" {
		t.Fatal("OpenAIDefaultBaseURL should not be empty")
	}
	if OpenAIDefaultModel == "" {
		t.Fatal("OpenAIDefaultModel should not be empty")
	}
	if OpenAIDefaultAPIKeyEnv == "" {
		t.Fatal("OpenAIDefaultAPIKeyEnv should not be empty")
	}

	if GeminiName == "" {
		t.Fatal("GeminiName should not be empty")
	}
	if GeminiDefaultBaseURL == "" {
		t.Fatal("GeminiDefaultBaseURL should not be empty")
	}
	if GeminiDefaultModel == "" {
		t.Fatal("GeminiDefaultModel should not be empty")
	}
	if GeminiDefaultAPIKeyEnv == "" {
		t.Fatal("GeminiDefaultAPIKeyEnv should not be empty")
	}

	if OpenLLName == "" {
		t.Fatal("OpenLLName should not be empty")
	}
	if OpenLLDefaultBaseURL == "" {
		t.Fatal("OpenLLDefaultBaseURL should not be empty")
	}
	if OpenLLDefaultModel == "" {
		t.Fatal("OpenLLDefaultModel should not be empty")
	}
	if OpenLLDefaultAPIKeyEnv == "" {
		t.Fatal("OpenLLDefaultAPIKeyEnv should not be empty")
	}
}
