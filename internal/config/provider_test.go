package config

import (
	"os"
	"strings"
	"testing"

	providerpkg "neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func TestContainsProviderName(t *testing.T) {
	t.Parallel()

	providers := []ProviderConfig{
		{Name: "OpenAI"},
		{Name: "Gemini"},
	}
	if !containsProviderName(providers, "openai") {
		t.Fatal("expected openai to be found (case insensitive)")
	}
	if !containsProviderName(providers, "OPENAI") {
		t.Fatal("expected OPENAI to be found")
	}
	if containsProviderName(providers, "Anthropic") {
		t.Fatal("expected Anthropic to not be found")
	}
	if containsProviderName(nil, "openai") {
		t.Fatal("expected nil providers to return false")
	}
	if containsProviderName(providers, "") {
		t.Fatal("expected empty name to return false")
	}
	if containsProviderName(providers, "  ") {
		t.Fatal("expected whitespace name to return false")
	}
}

func TestResolveSelectedProviderEmptyString(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "selected provider is empty") {
		t.Fatalf("expected empty selected provider error, got %v", err)
	}
}

func TestResolveSelectedProviderNotFound(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "nonexistent-provider",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestProviderConfigIdentity(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:      "test-openai",
		Driver:    "openaicompat",
		BaseURL:   "https://api.openai.com/v1",
		Model:     "gpt-4o",
		APIKeyEnv: "TEST_KEY",
		APIStyle:  "chat_completions",
	}

	identity, err := cfg.Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if identity.Driver != "openaicompat" {
		t.Fatalf("expected driver openaicompat, got %q", identity.Driver)
	}
	if identity.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected base URL, got %q", identity.BaseURL)
	}
	if identity.APIStyle != "chat_completions" {
		t.Fatalf("expected api_style chat_completions, got %q", identity.APIStyle)
	}
}

func TestProviderConfigResolveAPIKeyEmptyEnvName(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{Name: "test", APIKeyEnv: ""}
	_, err := cfg.ResolveAPIKey()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error, got %v", err)
	}
}

func TestProviderIdentityFromConfigDefaultsAPIStyleForOpenAICompat(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:      "test-openai",
		Driver:    "openaicompat",
		BaseURL:   "https://api.openai.com/v1",
		APIKeyEnv: "TEST_KEY",
	}

	identity, err := providerIdentityFromConfig(cfg)
	if err != nil {
		t.Fatalf("providerIdentityFromConfig() error = %v", err)
	}
	if identity.APIStyle != providerpkg.OpenAICompatibleAPIStyleChatCompletions {
		t.Fatalf("expected default api_style %q, got %q", providerpkg.OpenAICompatibleAPIStyleChatCompletions, identity.APIStyle)
	}
}

func TestQiniuProviderConfig(t *testing.T) {
	t.Parallel()

	provider := QiniuProvider()
	if provider.Name != QiniuName {
		t.Fatalf("expected name %q, got %q", QiniuName, provider.Name)
	}
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver openaicompat, got %q", provider.Driver)
	}
	if provider.BaseURL != QiniuDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", QiniuDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != QiniuDefaultModel {
		t.Fatalf("expected default model %q, got %q", QiniuDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != QiniuDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", QiniuDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if provider.Source != ProviderSourceBuiltin {
		t.Fatalf("expected builtin source, got %q", provider.Source)
	}
}

func TestNormalizeConfigKey(t *testing.T) {
	t.Parallel()

	if normalizeConfigKey(" OpenAI ") != "openai" {
		t.Fatal("expected normalized lowercase trimmed key")
	}
	if normalizeConfigKey("") != "" {
		t.Fatal("expected empty key to remain empty")
	}
}

func TestNormalizeProviderDriver(t *testing.T) {
	t.Parallel()

	if normalizeProviderDriver(" OPENAICOMPAT ") != "openaicompat" {
		t.Fatal("expected normalized driver")
	}
	if normalizeProviderDriver("") != "" {
		t.Fatal("expected empty driver to remain empty")
	}
}

func TestProviderConfigResolveWrapsAPIKeyError(t *testing.T) {
	restoreEnv(t, "UNRESOLVABLE_API_KEY_FOR_TEST")
	_ = os.Unsetenv("UNRESOLVABLE_API_KEY_FOR_TEST")

	cfg := ProviderConfig{
		Name:      "test",
		Driver:    "custom",
		BaseURL:   "https://example.com",
		APIKeyEnv: "UNRESOLVABLE_API_KEY_FOR_TEST",
	}
	_, err := cfg.Resolve()
	if err == nil || !strings.Contains(err.Error(), "UNRESOLVABLE_API_KEY_FOR_TEST") {
		t.Fatalf("expected unresolved API key error, got %v", err)
	}
}

func TestCloneProviderConfigModelDescriptorsIndependence(t *testing.T) {
	t.Parallel()

	original := ProviderConfig{
		Name:   "test",
		Models: []providertypes.ModelDescriptor{{ID: "model-a"}, {ID: "model-b"}},
	}
	cloned := cloneProviderConfig(original)
	cloned.Models[0].ID = "mutated"
	if original.Models[0].ID == cloned.Models[0].ID {
		t.Fatal("expected Models descriptor clone to be independent")
	}
}

func TestCustomProviderModelsParsesSupportedMetadata(t *testing.T) {
	t.Parallel()

	contextWindow := 131072
	maxOutputTokens := 8192
	models, err := customProviderModels([]customProviderModelFile{
		{
			ID:              "deepseek-coder",
			Name:            "DeepSeek Coder",
			ContextWindow:   &contextWindow,
			MaxOutputTokens: &maxOutputTokens,
		},
	})
	if err != nil {
		t.Fatalf("customProviderModels() error = %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected one parsed model, got %+v", models)
	}
	if models[0].ID != "deepseek-coder" || models[0].ContextWindow != 131072 || models[0].MaxOutputTokens != 8192 {
		t.Fatalf("unexpected parsed model descriptor: %+v", models[0])
	}
}

func TestCustomProviderModelsRejectsEmptyID(t *testing.T) {
	t.Parallel()

	_, err := customProviderModels([]customProviderModelFile{{Name: "Missing ID"}})
	if err == nil || !strings.Contains(err.Error(), "models[0].id") {
		t.Fatalf("expected empty id validation error, got %v", err)
	}
}

func TestCustomProviderModelsRejectsNonPositiveContextWindow(t *testing.T) {
	t.Parallel()

	contextWindow := 0
	_, err := customProviderModels([]customProviderModelFile{{
		ID:            "deepseek-coder",
		ContextWindow: &contextWindow,
	}})
	if err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("expected context_window validation error, got %v", err)
	}
}

func TestCustomProviderModelsRejectsNonPositiveMaxOutputTokens(t *testing.T) {
	t.Parallel()

	maxOutputTokens := 0
	_, err := customProviderModels([]customProviderModelFile{{
		ID:              "deepseek-coder",
		MaxOutputTokens: &maxOutputTokens,
	}})
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("expected max_output_tokens validation error, got %v", err)
	}
}

func TestCustomProviderModelsRejectsDuplicateID(t *testing.T) {
	t.Parallel()

	_, err := customProviderModels([]customProviderModelFile{
		{ID: "deepseek-coder"},
		{ID: " DeepSeek-Coder "},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate id validation error, got %v", err)
	}
}

func TestProviderByNameCaseInsensitive(t *testing.T) {
	t.Parallel()

	cfg := &Config{Providers: []ProviderConfig{testDefaultProviderConfig()}}
	found, err := cfg.ProviderByName("OPENAI")
	if err != nil {
		t.Fatalf("ProviderByName(OPENAI) error = %v", err)
	}
	if found.Name != testProviderName {
		t.Fatalf("expected provider %q, got %q", testProviderName, found.Name)
	}
}

func TestResolveSelectedProviderWrapsNotFoundError(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "nonexistent",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected wrapped not found error, got %v", err)
	}
}

func TestCloneProvidersEmptySlice(t *testing.T) {
	t.Parallel()

	result := cloneProviders([]ProviderConfig{})
	if result != nil {
		t.Fatalf("expected nil for empty input, got %+v", result)
	}
}

func TestProviderByNameEmptyNameOnNonEmptyList(t *testing.T) {
	t.Parallel()

	cfg := &Config{Providers: []ProviderConfig{testDefaultProviderConfig()}}
	_, err := cfg.ProviderByName("")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found for empty name, got %v", err)
	}
}

func TestResolveAPIKeyEmptyEnvName(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{Name: "test"}
	_, err := cfg.ResolveAPIKey()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error, got %v", err)
	}
}

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
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver %q, got %q", "openaicompat", provider.Driver)
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
}

func TestGeminiProviderConfig(t *testing.T) {
	t.Parallel()

	provider := GeminiProvider()

	if provider.Name != GeminiName {
		t.Fatalf("expected name %q, got %q", GeminiName, provider.Name)
	}
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver %q, got %q", "openaicompat", provider.Driver)
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
}

func TestOpenLLProviderConfig(t *testing.T) {
	t.Parallel()

	provider := OpenLLProvider()

	if provider.Name != OpenLLName {
		t.Fatalf("expected name %q, got %q", OpenLLName, provider.Name)
	}
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver %q, got %q", "openaicompat", provider.Driver)
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
}

func TestProviderDefaultsAreIndependent(t *testing.T) {
	t.Parallel()

	provider1 := OpenAIProvider()
	provider1.Model = "modified-model"

	provider2 := OpenAIProvider()
	if provider2.Model == "modified-model" {
		t.Fatal("expected builtin provider defaults to be independent between calls")
	}
	if provider2.Model != OpenAIDefaultModel {
		t.Fatalf("expected default model %q, got %q", OpenAIDefaultModel, provider2.Model)
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

func TestResolvedProviderConfigToRuntimeConfig(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:           "company-gateway",
			Driver:         "openaicompat",
			BaseURL:        "https://llm.example.com/v1",
			Model:          "server-default",
			APIStyle:       "responses",
			DeploymentMode: "ignored",
			APIVersion:     "ignored",
		},
		APIKey: "secret-key",
	}

	got := resolved.ToRuntimeConfig()
	want := providerpkg.RuntimeConfig{
		Name:           "company-gateway",
		Driver:         "openaicompat",
		BaseURL:        "https://llm.example.com/v1",
		DefaultModel:   "server-default",
		APIKey:         "secret-key",
		APIStyle:       "responses",
		DeploymentMode: "ignored",
		APIVersion:     "ignored",
	}

	if got != want {
		t.Fatalf("ToRuntimeConfig() = %+v, want %+v", got, want)
	}
}
