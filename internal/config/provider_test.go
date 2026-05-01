package config

import (
	"os"
	"strings"
	"testing"
	"time"

	providerpkg "neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
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

func TestResolveSelectedProviderIncludesRuntimeAssetPolicyAndBudget(t *testing.T) {
	const (
		maxSingle = int64(128)
		maxTotal  = int64(512)
	)
	envName := "TEST_PROVIDER_KEY"
	t.Setenv(envName, "secret")

	cfg := Config{
		SelectedProvider: "custom",
		Providers: []ProviderConfig{
			{
				Name:      "custom",
				Driver:    "openaicompat",
				BaseURL:   "https://llm.example.com/v1",
				APIKeyEnv: envName,
				Source:    ProviderSourceCustom,
			},
		},
		Runtime: RuntimeConfig{
			Assets: RuntimeAssetsConfig{
				MaxSessionAssetBytes:       maxSingle,
				MaxSessionAssetsTotalBytes: maxTotal,
			},
		},
	}

	resolved, err := ResolveSelectedProvider(cfg)
	if err != nil {
		t.Fatalf("ResolveSelectedProvider() error = %v", err)
	}

	if resolved.SessionAssetPolicy.MaxSessionAssetBytes != maxSingle {
		t.Fatalf("expected MaxSessionAssetBytes=%d, got %d", maxSingle, resolved.SessionAssetPolicy.MaxSessionAssetBytes)
	}
	if resolved.RequestAssetBudget.MaxSessionAssetsTotalBytes != maxTotal {
		t.Fatalf(
			"expected MaxSessionAssetsTotalBytes=%d, got %d",
			maxTotal,
			resolved.RequestAssetBudget.MaxSessionAssetsTotalBytes,
		)
	}
}

func TestResolveSelectedProviderPrefersRootGenerateStartTimeout(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig().Clone()
	cfg.GenerateStartTimeoutSec = 90

	resolved, err := ResolveSelectedProvider(cfg)
	if err != nil {
		t.Fatalf("ResolveSelectedProvider() error = %v", err)
	}
	if resolved.GenerateStartTimeoutSec != 90 {
		t.Fatalf("expected resolved GenerateStartTimeoutSec=90, got %d", resolved.GenerateStartTimeoutSec)
	}

	runtimeCfg, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if runtimeCfg.GenerateStartTimeout != 90*time.Second {
		t.Fatalf("expected runtime GenerateStartTimeout=90s, got %s", runtimeCfg.GenerateStartTimeout)
	}
}

func TestProviderConfigIdentity(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:                  "test-openai",
		Driver:                "openaicompat",
		BaseURL:               "https://api.openai.com/v1",
		Model:                 "gpt-4o",
		APIKeyEnv:             "TEST_KEY",
		DiscoveryEndpointPath: "models",
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
	if identity.ChatEndpointPath != "" {
		t.Fatalf("expected default chat endpoint path to be omitted, got %q", identity.ChatEndpointPath)
	}
	if identity.DiscoveryEndpointPath != "/models" {
		t.Fatalf("expected discovery endpoint /models, got %q", identity.DiscoveryEndpointPath)
	}
}

func TestProviderConfigValidateAllowsEmptyBaseURLForSDKDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		driver string
	}{
		{name: "gemini", driver: providerpkg.DriverGemini},
		{name: "anthropic", driver: providerpkg.DriverAnthropic},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := ProviderConfig{
				Name:                  tt.name,
				Driver:                tt.driver,
				BaseURL:               "",
				APIKeyEnv:             "TEST_KEY",
				DiscoveryEndpointPath: providerpkg.DiscoveryEndpointPathModels,
				Source:                ProviderSourceCustom,
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("expected empty base_url to be accepted for %s, got %v", tt.driver, err)
			}
		})
	}
}

func TestProviderIdentityFromConfigUsesDefaultBaseURLWhenEmptyForSDKDrivers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		driver      string
		expectedURL string
	}{
		{name: "gemini", driver: providerpkg.DriverGemini, expectedURL: GeminiDefaultBaseURL},
		{name: "anthropic", driver: providerpkg.DriverAnthropic, expectedURL: AnthropicDefaultBaseURL},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := ProviderConfig{
				Name:    tt.name,
				Driver:  tt.driver,
				BaseURL: "",
			}
			identity, err := providerIdentityFromConfig(cfg)
			if err != nil {
				t.Fatalf("providerIdentityFromConfig() error = %v", err)
			}
			if identity.BaseURL != tt.expectedURL {
				t.Fatalf("expected identity base URL %q, got %q", tt.expectedURL, identity.BaseURL)
			}
			if identity.ChatEndpointPath != "" {
				t.Fatalf("expected sdk identity to omit protocol matrix fields, got %+v", identity)
			}
		})
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

func TestProviderIdentityFromConfigDefaultsPathsForOpenAICompat(t *testing.T) {
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
	if identity.ChatEndpointPath != "" {
		t.Fatalf("expected default chat endpoint path to be omitted, got %q", identity.ChatEndpointPath)
	}
	if identity.DiscoveryEndpointPath != providerpkg.DiscoveryEndpointPathModels {
		t.Fatalf("expected default discovery endpoint %q, got %q", providerpkg.DiscoveryEndpointPathModels, identity.DiscoveryEndpointPath)
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
	if provider.DiscoveryEndpointPath != "" {
		t.Fatalf("expected builtin discovery endpoint to stay empty, got %q", provider.DiscoveryEndpointPath)
	}
	if !containsModelDescriptorID(provider.Models, provider.Model) {
		t.Fatalf("expected builtin static models to include default model, got %+v", provider.Models)
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
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	runtimeConfig, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	_, err = runtimeConfig.ResolveAPIKeyValue()
	if err == nil || !strings.Contains(err.Error(), "UNRESOLVABLE_API_KEY_FOR_TEST") {
		t.Fatalf("expected unresolved API key error, got %v", err)
	}
}

func TestProviderConfigValidateRejectsInvalidDiscoverySettings(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:                  "test-openai",
		Driver:                providerpkg.DriverOpenAICompat,
		BaseURL:               "https://api.openai.com/v1",
		Model:                 "gpt-4.1",
		APIKeyEnv:             "TEST_KEY",
		DiscoveryEndpointPath: "https://api.openai.com/models",
		Source:                ProviderSourceBuiltin,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "discovery endpoint path") {
		t.Fatalf("expected invalid discovery endpoint path error, got %v", err)
	}

	cfg.DiscoveryEndpointPath = "/models"
	cfg.ChatEndpointPath = "https://api.openai.com/chat/completions"
	cfg.Model = "gpt-4.1"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must be a relative path") {
		t.Fatalf("expected invalid chat endpoint path error, got %v", err)
	}
}

func TestProviderConfigValidateRejectsChatAPIModeForNonOpenAICompatDriver(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:        "gemini",
		Driver:      providerpkg.DriverGemini,
		BaseURL:     GeminiDefaultBaseURL,
		Model:       GeminiDefaultModel,
		APIKeyEnv:   "TEST_KEY",
		ChatAPIMode: providerpkg.ChatAPIModeResponses,
		Source:      ProviderSourceBuiltin,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "chat_api_mode") {
		t.Fatalf("expected chat_api_mode validation error, got %v", err)
	}
}

func TestProviderConfigValidateRejectsBaseURLWithUserinfo(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:      "test-openai",
		Driver:    providerpkg.DriverOpenAICompat,
		BaseURL:   "https://token@api.openai.com/v1",
		Model:     "gpt-4.1",
		APIKeyEnv: "TEST_KEY",
		Source:    ProviderSourceBuiltin,
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "must not include userinfo") {
		t.Fatalf("expected base_url userinfo validation error, got %v", err)
	}
}

func TestProviderConfigValidateRejectsNegativeGenerateControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*ProviderConfig)
		errContain string
	}{
		{
			name: "negative retries",
			mutate: func(cfg *ProviderConfig) {
				cfg.GenerateMaxRetries = -1
			},
			errContain: "generate_max_retries",
		},
		{
			name: "retries exceed upper bound",
			mutate: func(cfg *ProviderConfig) {
				cfg.GenerateMaxRetries = providerpkg.MaxGenerateMaxRetries + 1
			},
			errContain: "generate_max_retries",
		},
		{
			name: "negative idle timeout",
			mutate: func(cfg *ProviderConfig) {
				cfg.GenerateIdleTimeoutSec = -1
			},
			errContain: "generate_idle_timeout_sec",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := ProviderConfig{
				Name:                  "company-gateway",
				Driver:                providerpkg.DriverOpenAICompat,
				BaseURL:               "https://llm.example.com/v1",
				APIKeyEnv:             "TEST_KEY",
				ModelSource:           ModelSourceDiscover,
				DiscoveryEndpointPath: providerpkg.DiscoveryEndpointPathModels,
				Source:                ProviderSourceCustom,
			}
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Fatalf("expected %q validation error, got %v", tt.errContain, err)
			}
		})
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

func TestCustomProviderModelsParsesIDAndNameOnly(t *testing.T) {
	t.Parallel()

	models, err := customProviderModels([]customProviderModelFile{
		{
			ID:   "deepseek-coder",
			Name: "DeepSeek Coder",
		},
	})
	if err != nil {
		t.Fatalf("customProviderModels() error = %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected one parsed model, got %+v", models)
	}
	if models[0].ID != "deepseek-coder" || models[0].ContextWindow != 0 || models[0].MaxOutputTokens != 0 {
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

func TestCustomProviderModelsRejectsEmptyName(t *testing.T) {
	t.Parallel()

	_, err := customProviderModels([]customProviderModelFile{{ID: "deepseek-coder"}})
	if err == nil || !strings.Contains(err.Error(), "models[0].name") {
		t.Fatalf("expected empty name validation error, got %v", err)
	}
}

func TestNormalizeCustomProviderModelsRejectsContextWindow(t *testing.T) {
	t.Parallel()

	_, err := NormalizeCustomProviderModels([]providertypes.ModelDescriptor{{
		ID:            "deepseek-coder",
		Name:          "DeepSeek Coder",
		ContextWindow: 131072,
	}})
	if err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("expected context_window validation error, got %v", err)
	}
}

func TestNormalizeCustomProviderModelsRejectsMaxOutputTokens(t *testing.T) {
	t.Parallel()

	_, err := NormalizeCustomProviderModels([]providertypes.ModelDescriptor{{
		ID:              "deepseek-coder",
		Name:            "DeepSeek Coder",
		MaxOutputTokens: 8192,
	}})
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("expected max_output_tokens validation error, got %v", err)
	}
}

func TestNormalizeCustomProviderModelsRejectsMetadataFields(t *testing.T) {
	t.Parallel()

	_, err := NormalizeCustomProviderModels([]providertypes.ModelDescriptor{
		{
			ID:          "deepseek-coder",
			Name:        "DeepSeek Coder",
			Description: "desc",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("expected description validation error, got %v", err)
	}

	_, err = NormalizeCustomProviderModels([]providertypes.ModelDescriptor{
		{
			ID:   "deepseek-coder",
			Name: "DeepSeek Coder",
			CapabilityHints: providertypes.ModelCapabilityHints{
				ToolCalling: providertypes.ModelCapabilityStateSupported,
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "capability_hints") {
		t.Fatalf("expected capability_hints validation error, got %v", err)
	}
}

func TestCustomProviderModelsRejectsDuplicateID(t *testing.T) {
	t.Parallel()

	_, err := customProviderModels([]customProviderModelFile{
		{ID: "deepseek-coder", Name: "DeepSeek Coder"},
		{ID: " DeepSeek-Coder ", Name: "DeepSeek Coder Duplicate"},
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

	expectedNames := []string{OpenAIName, GeminiName, QiniuName, ModelScopeName}
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
	if !containsModelDescriptorID(provider.Models, provider.Model) {
		t.Fatalf("expected builtin static models to include default model, got %+v", provider.Models)
	}
}

func TestGeminiProviderConfig(t *testing.T) {
	t.Parallel()

	provider := GeminiProvider()

	if provider.Name != GeminiName {
		t.Fatalf("expected name %q, got %q", GeminiName, provider.Name)
	}
	if provider.Driver != "gemini" {
		t.Fatalf("expected driver %q, got %q", "gemini", provider.Driver)
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
	if !containsModelDescriptorID(provider.Models, provider.Model) {
		t.Fatalf("expected builtin static models to include default model, got %+v", provider.Models)
	}
}

func TestModelScopeProviderConfig(t *testing.T) {
	t.Parallel()

	provider := ModelScopeProvider()

	if provider.Name != ModelScopeName {
		t.Fatalf("expected name %q, got %q", ModelScopeName, provider.Name)
	}
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver %q, got %q", "openaicompat", provider.Driver)
	}
	if provider.BaseURL != ModelScopeDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", ModelScopeDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != ModelScopeDefaultModel {
		t.Fatalf("expected default model %q, got %q", ModelScopeDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != ModelScopeDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", ModelScopeDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if provider.Source != ProviderSourceBuiltin {
		t.Fatalf("expected builtin source, got %q", provider.Source)
	}
	if provider.DiscoveryEndpointPath != "" {
		t.Fatalf("expected builtin discovery endpoint to stay empty, got %q", provider.DiscoveryEndpointPath)
	}
	if !containsModelDescriptorID(provider.Models, provider.Model) {
		t.Fatalf("expected builtin static models to include default model, got %+v", provider.Models)
	}
}

func TestBuiltinProvidersExposeStaticModelMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		provider  ProviderConfig
		wantTool  providertypes.ModelCapabilityState
		wantImage providertypes.ModelCapabilityState
	}{
		{
			name:      "openai",
			provider:  OpenAIProvider(),
			wantTool:  providertypes.ModelCapabilityStateSupported,
			wantImage: providertypes.ModelCapabilityStateSupported,
		},
		{
			name:      "gemini",
			provider:  GeminiProvider(),
			wantTool:  providertypes.ModelCapabilityStateSupported,
			wantImage: providertypes.ModelCapabilityStateSupported,
		},
		{
			name:      "qiniu",
			provider:  QiniuProvider(),
			wantTool:  providertypes.ModelCapabilityStateSupported,
			wantImage: providertypes.ModelCapabilityStateUnsupported,
		},
		{
			name:      "modelscope",
			provider:  ModelScopeProvider(),
			wantTool:  providertypes.ModelCapabilityStateSupported,
			wantImage: providertypes.ModelCapabilityStateUnsupported,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var target providertypes.ModelDescriptor
			var found bool
			for _, model := range tt.provider.Models {
				if model.ID == tt.provider.Model {
					target = model
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected default model %q in provider models: %+v", tt.provider.Model, tt.provider.Models)
			}
			if target.Description == "" {
				t.Fatalf("expected default model %q to include description", target.ID)
			}
			if target.ContextWindow <= 0 {
				t.Fatalf("expected default model %q to include context window, got %d", target.ID, target.ContextWindow)
			}
			if target.MaxOutputTokens <= 0 {
				t.Fatalf("expected default model %q to include max output tokens, got %d", target.ID, target.MaxOutputTokens)
			}
			if target.CapabilityHints.ToolCalling != tt.wantTool {
				t.Fatalf("expected default model %q tool calling=%q, got %+v", target.ID, tt.wantTool, target.CapabilityHints)
			}
			if target.CapabilityHints.ImageInput != tt.wantImage {
				t.Fatalf("expected default model %q image input=%q, got %+v", target.ID, tt.wantImage, target.CapabilityHints)
			}
		})
	}
}

func TestModelScopeProviderIncludesMiniMaxFallbackModel(t *testing.T) {
	t.Parallel()

	provider := ModelScopeProvider()
	if !containsModelDescriptorID(provider.Models, "MiniMax/MiniMax-M2.5") {
		t.Fatalf("expected modelscope static models to include MiniMax/MiniMax-M2.5, got %+v", provider.Models)
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
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://llm.example.com/v1",
			Model:     "server-default",
			APIKeyEnv: "COMPANY_GATEWAY_KEY",
		},
		SessionAssetPolicy: session.AssetPolicy{
			MaxSessionAssetBytes: 1024,
		},
		RequestAssetBudget: providerpkg.RequestAssetBudget{
			MaxSessionAssetsTotalBytes: 2048,
		},
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	want := providerpkg.RuntimeConfig{
		Name:         "company-gateway",
		Driver:       "openaicompat",
		BaseURL:      "https://llm.example.com/v1",
		DefaultModel: "server-default",
		APIKeyEnv:    "COMPANY_GATEWAY_KEY",
		SessionAssetPolicy: session.AssetPolicy{
			MaxSessionAssetBytes: 1024,
		},
		RequestAssetBudget: providerpkg.RequestAssetBudget{
			MaxSessionAssetsTotalBytes: 2048,
		},
		ChatAPIMode:           "",
		ChatEndpointPath:      "",
		DiscoveryEndpointPath: providerpkg.DiscoveryEndpointPathModels,
		GenerateMaxRetries:    providerpkg.DefaultGenerateMaxRetries,
		GenerateStartTimeout:  providerpkg.DefaultGenerateStartTimeout,
		GenerateIdleTimeout:   providerpkg.DefaultGenerateIdleTimeout,
	}

	if got.APIKeyResolver == nil {
		t.Fatal("expected APIKeyResolver to be set")
	}
	got.APIKeyResolver = nil
	if got.Name != want.Name ||
		got.Driver != want.Driver ||
		got.BaseURL != want.BaseURL ||
		got.DefaultModel != want.DefaultModel ||
		got.APIKeyEnv != want.APIKeyEnv ||
		got.SessionAssetPolicy != want.SessionAssetPolicy ||
		got.RequestAssetBudget != want.RequestAssetBudget ||
		got.ChatAPIMode != want.ChatAPIMode ||
		got.ChatEndpointPath != want.ChatEndpointPath ||
		got.DiscoveryEndpointPath != want.DiscoveryEndpointPath ||
		got.GenerateMaxRetries != want.GenerateMaxRetries ||
		got.GenerateStartTimeout != want.GenerateStartTimeout ||
		got.GenerateIdleTimeout != want.GenerateIdleTimeout {
		t.Fatalf("ToRuntimeConfig() = %+v, want %+v", got, want)
	}
}

func TestResolvedProviderConfigToRuntimeConfigPreservesExplicitZeroGenerateRetries(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:                  "company-gateway",
			Driver:                "openaicompat",
			BaseURL:               "https://llm.example.com/v1",
			Model:                 "server-default",
			APIKeyEnv:             "COMPANY_GATEWAY_KEY",
			GenerateMaxRetries:    0,
			GenerateMaxRetriesSet: true,
		},
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if got.GenerateMaxRetries != 0 {
		t.Fatalf("expected explicit GenerateMaxRetries=0 to disable retries, got %d", got.GenerateMaxRetries)
	}
}

func TestResolvedProviderConfigToRuntimeConfigMapsGenerateControls(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:                   "company-gateway",
			Driver:                 "openaicompat",
			BaseURL:                "https://llm.example.com/v1",
			Model:                  "server-default",
			APIKeyEnv:              "COMPANY_GATEWAY_KEY",
			GenerateMaxRetries:     7,
			GenerateIdleTimeoutSec: 420,
		},
		GenerateStartTimeoutSec: 75,
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if got.GenerateMaxRetries != 7 {
		t.Fatalf("expected GenerateMaxRetries=7, got %d", got.GenerateMaxRetries)
	}
	if got.GenerateStartTimeout != 75*time.Second {
		t.Fatalf("expected GenerateStartTimeout=75s, got %s", got.GenerateStartTimeout)
	}
	if got.GenerateIdleTimeout != 420*time.Second {
		t.Fatalf("expected GenerateIdleTimeout=420s, got %s", got.GenerateIdleTimeout)
	}
}

func TestResolvedProviderConfigToRuntimeConfigStripsBaseURLUserinfo(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://token@llm.example.com/v1",
			Model:     "server-default",
			APIKeyEnv: "COMPANY_GATEWAY_KEY",
		},
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if strings.Contains(got.BaseURL, "token@") {
		t.Fatalf("expected runtime base URL to strip userinfo, got %q", got.BaseURL)
	}
	if got.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("expected sanitized runtime base URL, got %q", got.BaseURL)
	}
}

func TestResolvedProviderConfigToRuntimeConfigReturnsProtocolNormalizationError(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:             "company-gateway",
			Driver:           "openaicompat",
			BaseURL:          "https://llm.example.com/v1",
			APIKeyEnv:        "TEST_KEY",
			ChatEndpointPath: "https://llm.example.com/chat/completions",
		},
	}

	_, err := resolved.ToRuntimeConfig()
	if err == nil {
		t.Fatal("expected ToRuntimeConfig() to return normalization error for invalid chat endpoint path")
	}
	if !strings.Contains(err.Error(), "must be a relative path") {
		t.Fatalf("expected relative path error, got %v", err)
	}
}

func TestResolvedProviderConfigToRuntimeConfigUsesNormalizedOpenAICompatPaths(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:             "responses-gateway",
			Driver:           "openaicompat",
			BaseURL:          "https://llm.example.com/v1",
			Model:            "gpt-5.4",
			APIKeyEnv:        "RESPONSES_GATEWAY_KEY",
			ChatAPIMode:      providerpkg.ChatAPIModeResponses,
			ChatEndpointPath: "/responses",
		},
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if got.ChatEndpointPath != "/responses" {
		t.Fatalf("expected normalized chat endpoint path %q, got %q", "/responses", got.ChatEndpointPath)
	}
	if got.ChatAPIMode != providerpkg.ChatAPIModeResponses {
		t.Fatalf("expected chat api mode %q, got %q", providerpkg.ChatAPIModeResponses, got.ChatAPIMode)
	}
}

func TestResolvedProviderConfigToRuntimeConfigStripsSDKChatEndpointPath(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:                  "gemini",
			Driver:                providerpkg.DriverGemini,
			BaseURL:               GeminiDefaultBaseURL,
			Model:                 GeminiDefaultModel,
			APIKeyEnv:             "GEMINI_KEY",
			ChatEndpointPath:      "/models",
			DiscoveryEndpointPath: providerpkg.DiscoveryEndpointPathModels,
		},
	}

	got, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if got.ChatEndpointPath != "" {
		t.Fatalf("expected sdk runtime config to omit chat endpoint path, got %q", got.ChatEndpointPath)
	}
	if got.DiscoveryEndpointPath != providerpkg.DiscoveryEndpointPathModels {
		t.Fatalf("expected discovery endpoint %q, got %q", providerpkg.DiscoveryEndpointPathModels, got.DiscoveryEndpointPath)
	}
}
