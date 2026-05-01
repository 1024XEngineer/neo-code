package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

func writeLoaderConfig(t *testing.T, loader *Loader, raw string) {
	t.Helper()
	if err := os.MkdirAll(loader.BaseDir(), 0o755); err != nil {
		t.Fatalf("mkdir base dir: %v", err)
	}
	content := raw
	if strings.Contains(raw, "\n") {
		content = strings.TrimSpace(raw) + "\n"
	}
	if err := os.WriteFile(loader.ConfigPath(), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestDefaultBaseDirRejectsRelativeHomeEnv(t *testing.T) {
	t.Setenv("HOME", ".")

	got := defaultBaseDir()
	if !filepath.IsAbs(got) && got != dirName {
		t.Fatalf("defaultBaseDir() must resolve to absolute path or fallback dir name, got %q", got)
	}
}

func TestDefaultBaseDirUsesAbsoluteHomeEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := defaultBaseDir()
	want := filepath.Join(home, dirName)
	if got != want {
		t.Fatalf("defaultBaseDir() = %q, want %q", got, want)
	}
}

func saveCustomProviderWithModelsForTest(
	baseDir string,
	name string,
	driver string,
	baseURL string,
	apiKeyEnv string,
	discoveryEndpointPath string,
	chatEndpointPath string,
) error {
	if strings.TrimSpace(discoveryEndpointPath) == "" {
		discoveryEndpointPath = provider.DiscoveryEndpointPathModels
	}
	return SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                  name,
		Driver:                driver,
		BaseURL:               baseURL,
		ChatEndpointPath:      chatEndpointPath,
		APIKeyEnv:             apiKeyEnv,
		DiscoveryEndpointPath: discoveryEndpointPath,
		ModelSource:           ModelSourceDiscover,
	})
}

func TestLoaderLoadMissingConfigCreatesDefault(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	if _, err := os.Stat(loader.ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("expected config file to be missing before load, got %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config to be created")
	}
	if _, err := os.Stat(loader.ConfigPath()); err != nil {
		t.Fatalf("expected config file to be created, got %v", err)
	}
}

func TestLoaderLoadMalformedYAML(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	writeLoaderConfig(t, loader, "providers:\n  - name: [\n")

	_, err := loader.Load(context.Background())
	if err == nil || (!strings.Contains(err.Error(), "parse config file") && !strings.Contains(err.Error(), "normalize verification schema")) {
		t.Fatalf("expected malformed yaml parse error, got %v", err)
	}
}

func TestLoaderLoadRuntimeHooksConfig(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
runtime:
  hooks:
    enabled: true
    user_hooks_enabled: true
    default_timeout_sec: 3
    default_failure_policy: warn_only
    items:
      - id: warn-bash
        enabled: true
        point: before_tool_call
        scope: user
        kind: builtin
        mode: sync
        handler: warn_on_tool_call
        priority: 100
        timeout_sec: 2
        failure_policy: warn_only
        params:
          tool_name: bash
          message: "bash is called"
`
	writeLoaderConfig(t, loader, raw)
	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	if !cfg.Runtime.Hooks.IsEnabled() || !cfg.Runtime.Hooks.IsUserHooksEnabled() {
		t.Fatalf("unexpected hook switches: %+v", cfg.Runtime.Hooks)
	}
	if len(cfg.Runtime.Hooks.Items) != 1 {
		t.Fatalf("len(items)=%d, want 1", len(cfg.Runtime.Hooks.Items))
	}
	item := cfg.Runtime.Hooks.Items[0]
	if item.Handler != "warn_on_tool_call" {
		t.Fatalf("handler=%q, want warn_on_tool_call", item.Handler)
	}
}

func TestLoaderRejectsUnsupportedRuntimeHookHandler(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
runtime:
  hooks:
    items:
      - id: invalid-handler
        point: before_tool_call
        scope: user
        kind: builtin
        mode: sync
        handler: shell_exec
`
	writeLoaderConfig(t, loader, raw)
	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime.hooks.items[0]") {
		t.Fatalf("expected runtime hooks validation error, got %v", err)
	}
}

func TestLoaderRejectsLegacyWorkdirKey(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
workdir: .
shell: powershell
`
	writeLoaderConfig(t, loader, raw)

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field workdir not found") {
		t.Fatalf("expected legacy workdir rejection, got %v", err)
	}
}

func TestLoaderRejectsLegacyDefaultWorkdirKey(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
default_workdir: .
shell: powershell
`
	writeLoaderConfig(t, loader, raw)

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field default_workdir not found") {
		t.Fatalf("expected legacy default_workdir rejection, got %v", err)
	}
}

func TestLoaderDoesNotMigrateLegacyContextBudgetOnLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
context:
  auto_compact:
    input_token_threshold: 120000
    reserve_tokens: 13000
    fallback_input_token_threshold: 100000
`
	writeLoaderConfig(t, loader, raw)

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field auto_compact not found") {
		t.Fatalf("expected legacy auto_compact parse error, got %v", err)
	}
	if _, statErr := os.Stat(loader.ConfigPath() + ".bak"); !os.IsNotExist(statErr) {
		t.Fatalf("expected no backup file written by loader, got %v", statErr)
	}
	data, readErr := os.ReadFile(loader.ConfigPath())
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if string(data) != strings.TrimSpace(raw)+"\n" {
		t.Fatalf("loader should not rewrite config, got:\n%s", data)
	}
}

func TestLoaderRejectsLegacyAndCurrentContextBudgetMixWithoutPreflight(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
context:
  budget:
    prompt_budget: 110000
  auto_compact:
    input_token_threshold: 120000
`
	writeLoaderConfig(t, loader, raw)

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field auto_compact not found") {
		t.Fatalf("expected legacy auto_compact parse error, got %v", err)
	}
}

func TestLoaderLoadInvalidBaseDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	baseFile := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(baseFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}

	loader := NewLoader(baseFile, testDefaultConfig())
	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create config dir") {
		t.Fatalf("expected invalid base dir error, got %v", err)
	}
}

func TestLoaderRejectsLegacyProvidersFormatOnLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	legacy := `
selected_provider: openai
current_model: gpt-5.4
shell: powershell
providers:
  - name: openai
    type: openai
    base_url: https://example.com/v1
    model: gpt-5.4
    api_key_env: OPENAI_API_KEY
`
	writeLoaderConfig(t, loader, legacy)

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field providers not found") {
		t.Fatalf("expected legacy providers format rejection, got %v", err)
	}
}

func TestLoaderPreservesSelectionStateOnLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	raw := `
selected_provider: missing-provider
shell: powershell
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SelectedProvider != "missing-provider" {
		t.Fatalf("expected selected provider to remain unchanged, got %q", cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "" {
		t.Fatalf("expected current model to remain empty, got %q", cfg.CurrentModel)
	}

	persisted, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(persisted)
	if !strings.Contains(text, "selected_provider: missing-provider") {
		t.Fatalf("expected config file to remain unchanged, got:\n%s", text)
	}
}

func TestLoaderPreservesMissingCurrentModelOnLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	raw := `
selected_provider: openai
shell: powershell
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SelectedProvider != testProviderName {
		t.Fatalf("expected selected provider %q, got %q", testProviderName, cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "" {
		t.Fatalf("expected current model to remain empty, got %q", cfg.CurrentModel)
	}

	persisted, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(persisted)
	if strings.Contains(text, "current_model:") {
		t.Fatalf("expected config file to preserve missing current_model, got:\n%s", text)
	}
}

func TestLoaderAllowsSelectedCustomProviderWithEmptyCurrentModel(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	rawConfig := `
selected_provider: company-gateway
shell: powershell
`
	writeLoaderConfig(t, loader, rawConfig)

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SelectedProvider != "company-gateway" {
		t.Fatalf("expected selected provider %q, got %q", "company-gateway", cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "" {
		t.Fatalf("expected empty current model before discovery, got %q", cfg.CurrentModel)
	}
}

func TestLoaderLoadCustomProviderWithChatAPIMode(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "responses-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: responses-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: RESPONSES_GATEWAY_API_KEY
chat_api_mode: responses
chat_endpoint_path: /
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loadedProvider, err := cfg.ProviderByName("responses-gateway")
	if err != nil {
		t.Fatalf("ProviderByName() error = %v", err)
	}
	if loadedProvider.ChatAPIMode != provider.ChatAPIModeResponses {
		t.Fatalf("expected chat_api_mode responses, got %q", loadedProvider.ChatAPIMode)
	}
}

func TestLoaderRejectsCustomProviderWithInvalidChatAPIMode(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "invalid-chat-mode")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: invalid-chat-mode
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: INVALID_CHAT_MODE_API_KEY
chat_api_mode: unknown
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "chat_api_mode") {
		t.Fatalf("expected invalid chat_api_mode rejection, got %v", err)
	}
}

func TestLoaderLoadsCustomProvidersFromProvidersDirectory(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	if err := os.MkdirAll(filepath.Join(loader.BaseDir(), providersDirName, "company-gateway"), 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	rawConfig := `
selected_provider: company-gateway
current_model: deepseek-coder
shell: powershell
`
	writeLoaderConfig(t, loader, rawConfig)

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
discovery_endpoint_path: /gateway/models
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    name: DeepSeek Coder
`
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SelectedProvider != "company-gateway" {
		t.Fatalf("expected selected provider company-gateway, got %q", cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "deepseek-coder" {
		t.Fatalf("expected current model deepseek-coder, got %q", cfg.CurrentModel)
	}

	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if customProvider.Source != ProviderSourceCustom {
		t.Fatalf("expected custom provider source, got %+v", customProvider)
	}
	if customProvider.Driver != "openaicompat" {
		t.Fatalf("expected custom provider driver openaicompat, got %q", customProvider.Driver)
	}
	if customProvider.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("expected base url https://llm.example.com/v1, got %q", customProvider.BaseURL)
	}
	if customProvider.DiscoveryEndpointPath != "/gateway/models" {
		t.Fatalf("expected discovery endpoint /gateway/models, got %q", customProvider.DiscoveryEndpointPath)
	}
	if customProvider.Model != "" {
		t.Fatalf("expected custom provider default model to be empty, got %q", customProvider.Model)
	}
	if len(customProvider.Models) != 1 {
		t.Fatalf("expected custom provider models from provider.yaml, got %+v", customProvider.Models)
	}
	if customProvider.Models[0].ID != "deepseek-coder" || customProvider.Models[0].ContextWindow != 0 {
		t.Fatalf("expected parsed id/name only model, got %+v", customProvider.Models[0])
	}
}

func TestLoaderIgnoresDirectoriesWithoutProviderYAML(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	validDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	ignoredDir := filepath.Join(loader.BaseDir(), providersDirName, ".git")
	for _, dir := range []string{validDir, ignoredDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir custom provider dir: %v", err)
		}
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(validDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if customProvider.Source != ProviderSourceCustom {
		t.Fatalf("expected custom provider source, got %+v", customProvider)
	}
}

func TestLoaderRejectsMalformedCustomProviderYAML(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte("name: [\n"), 0o644); err != nil {
		t.Fatalf("write malformed provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected malformed custom provider parse error, got %v", err)
	}
}

func TestLoaderRejectsInvalidCustomProviderModelSource(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: manul
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unsupported model_source") {
		t.Fatalf("expected invalid model_source error, got %v", err)
	}
}

func TestLoaderLoadManualModelSourceIgnoresDiscoveryFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
model_source: manual
models:
  - id: manual-model
    name: Manual Model
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	loadedProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if loadedProvider.ModelSource != ModelSourceManual {
		t.Fatalf("expected model_source manual, got %q", loadedProvider.ModelSource)
	}
	if loadedProvider.DiscoveryEndpointPath != "" {
		t.Fatalf("expected manual mode to clear discovery endpoint path, got %q", loadedProvider.DiscoveryEndpointPath)
	}
}

func TestLoaderDefaultsModelSourceToDiscoverForLegacyCustomProvider(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: manual-model
    name: Manual Model
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "model_source discover requires discovery_endpoint_path") {
		t.Fatalf("expected legacy discover config without endpoint to fail, got %v", err)
	}
}

func TestLoaderAllowsTopLevelDiscoverySettingsForKnownDriver(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("expected top-level discovery settings to load, got %v", err)
	}
	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if customProvider.DiscoveryEndpointPath != "/models" {
		t.Fatalf("expected discovery endpoint /models, got %q", customProvider.DiscoveryEndpointPath)
	}
}

func TestLoaderIgnoresCustomProviderDefaultModelField(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
default_model: deepseek-coder
api_key_env: COMPANY_GATEWAY_API_KEY
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field default_model not found") {
		t.Fatalf("expected unknown default_model field rejection, got %v", err)
	}
}

func TestLoaderIgnoresCustomProviderModelsYAML(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	modelsYAML := `
models:
  - name: deepseek-coder
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "models.yaml"), []byte(strings.TrimSpace(modelsYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write models.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if len(customProvider.Models) != 0 {
		t.Fatalf("expected models.yaml to be ignored, got %+v", customProvider.Models)
	}
}

func TestLoaderRejectsCustomProviderModelWithoutID(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - name: DeepSeek Coder
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "models[0].id") {
		t.Fatalf("expected empty model id rejection, got %v", err)
	}
}

func TestLoaderRejectsCustomProviderModelWithUnsupportedContextWindow(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    name: DeepSeek Coder
    context_window: 131072
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field context_window not found") {
		t.Fatalf("expected unknown context_window rejection, got %v", err)
	}
}

func TestLoaderRejectsCustomProviderModelWithUnsupportedMaxOutputTokens(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    name: DeepSeek Coder
    max_output_tokens: 8192
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field max_output_tokens not found") {
		t.Fatalf("expected unknown max_output_tokens rejection, got %v", err)
	}
}

func TestLoaderRejectsCustomProviderDuplicateModelID(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    name: DeepSeek Coder
  - id: DeepSeek-Coder
    name: DeepSeek Coder Duplicate
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate model id rejection, got %v", err)
	}
}

func TestLoaderParsesBudgetFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-5.4
shell: powershell
context:
    budget:
      prompt_budget: 0
      reserve_tokens: 9000
      fallback_prompt_budget: 88000
      max_reactive_compacts: 4
  `
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Context.Budget.PromptBudget != 0 {
		t.Fatalf("expected implicit prompt budget 0, got %d", cfg.Context.Budget.PromptBudget)
	}
	if cfg.Context.Budget.ReserveTokens != 9000 {
		t.Fatalf("expected reserve_tokens=9000, got %d", cfg.Context.Budget.ReserveTokens)
	}
	if cfg.Context.Budget.FallbackPromptBudget != 88000 {
		t.Fatalf("expected fallback_prompt_budget=88000, got %d", cfg.Context.Budget.FallbackPromptBudget)
	}
	if cfg.Context.Budget.MaxReactiveCompacts != 4 {
		t.Fatalf("expected max_reactive_compacts=4, got %d", cfg.Context.Budget.MaxReactiveCompacts)
	}
}

func TestLoaderSavePersistsBudgetFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	cfg := testDefaultConfig().Clone()
	cfg.Context.Budget.PromptBudget = 0
	cfg.Context.Budget.ReserveTokens = 9000
	cfg.Context.Budget.FallbackPromptBudget = 88000
	cfg.Context.Budget.MaxReactiveCompacts = 4

	if err := loader.Save(context.Background(), &cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "prompt_budget: 100000") {
		t.Fatalf("expected implicit prompt budget to avoid default serialization, got:\n%s", text)
	}
	if !strings.Contains(text, "reserve_tokens: 9000") {
		t.Fatalf("expected reserve_tokens to persist, got:\n%s", text)
	}
	if !strings.Contains(text, "fallback_prompt_budget: 88000") {
		t.Fatalf("expected fallback_prompt_budget to persist, got:\n%s", text)
	}
	if !strings.Contains(text, "max_reactive_compacts: 4") {
		t.Fatalf("expected max_reactive_compacts to persist, got:\n%s", text)
	}
}

func TestLoaderRejectsCustomProviderNameConflictingWithBuiltin(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "openai")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: openai
driver: openaicompat
base_url: https://api.example.com/v1
api_key_env: OPENAI_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicate provider name") {
		t.Fatalf("expected duplicate provider name error, got %v", err)
	}
}

func TestLoaderRejectsDuplicateCustomProviderEndpointIdentity(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customA := filepath.Join(loader.BaseDir(), providersDirName, "gateway-a")
	customB := filepath.Join(loader.BaseDir(), providersDirName, "gateway-b")
	for _, dir := range []string{customA, customB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir custom provider dir: %v", err)
		}
	}

	providerA := `
name: gateway-a
driver: openaicompat
base_url: https://api.example.com/v1/
api_key_env: GATEWAY_A_API_KEY
discovery_endpoint_path: /models
`
	providerB := `
name: gateway-b
driver: openaicompat
base_url: https://API.EXAMPLE.COM/v1
api_key_env: GATEWAY_B_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customA, customProviderConfigName), []byte(strings.TrimSpace(providerA)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customB, customProviderConfigName), []byte(strings.TrimSpace(providerB)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider b: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicate provider endpoint") {
		t.Fatalf("expected duplicate provider endpoint error, got %v", err)
	}
}

func TestLoaderUsesOnlyDriverSpecificCustomProviderFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	rawConfig := `
selected_provider: company-gateway
current_model: server-model
shell: powershell
`
	writeLoaderConfig(t, loader, rawConfig)

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
gemini:
  base_url: https://gemini.example.com/v1beta
  deployment_mode: vertex
anthropic:
  base_url: https://anthropic.example.com/v1
  api_version: 2023-06-01
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field gemini not found") {
		t.Fatalf("expected strict field rejection for legacy driver blocks, got %v", err)
	}
}

func TestLoaderRejectsCrossDriverBaseURLFallback(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	rawConfig := `
selected_provider: company-gateway
current_model: server-model
shell: powershell
`
	writeLoaderConfig(t, loader, rawConfig)

	providerYAML := `
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected missing openai-compatible base_url error, got %v", err)
	}
}

func TestSaveCustomProviderPersistsDriverSpecificSettings(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	tests := []struct {
		name                  string
		driver                string
		baseURL               string
		discoveryEndpointPath string
		assert                func(t *testing.T, cfg ProviderConfig)
	}{
		{
			name:                  "openaicompat settings",
			driver:                provider.DriverOpenAICompat,
			baseURL:               "https://llm.example.com/v1",
			discoveryEndpointPath: "/gateway/models",
			assert: func(t *testing.T, cfg ProviderConfig) {
				t.Helper()
				if cfg.DiscoveryEndpointPath != "/gateway/models" {
					t.Fatalf("expected DiscoveryEndpointPath=/gateway/models, got %q", cfg.DiscoveryEndpointPath)
				}
			},
		},
		{
			name:                  "gemini settings",
			driver:                provider.DriverGemini,
			baseURL:               "https://generativelanguage.googleapis.com/v1beta/openai",
			discoveryEndpointPath: "/models",
			assert: func(t *testing.T, cfg ProviderConfig) {
				t.Helper()
				if cfg.DiscoveryEndpointPath != "/models" {
					t.Fatalf("expected DiscoveryEndpointPath=/models, got %q", cfg.DiscoveryEndpointPath)
				}
			},
		},
		{
			name:                  "anthropic settings",
			driver:                provider.DriverAnthropic,
			baseURL:               "https://api.anthropic.com/v1",
			discoveryEndpointPath: "/models",
			assert: func(t *testing.T, cfg ProviderConfig) {
				t.Helper()
				if cfg.DiscoveryEndpointPath != "/models" {
					t.Fatalf("expected DiscoveryEndpointPath=/models, got %q", cfg.DiscoveryEndpointPath)
				}
			},
		},
		{
			name:                  "unknown driver keeps top-level base url",
			driver:                "custom-driver",
			baseURL:               "https://custom.example.com/v1",
			discoveryEndpointPath: "/catalog/models",
			assert: func(t *testing.T, cfg ProviderConfig) {
				t.Helper()
				if cfg.BaseURL != "https://custom.example.com/v1" {
					t.Fatalf("expected BaseURL=https://custom.example.com/v1, got %q", cfg.BaseURL)
				}
				if cfg.DiscoveryEndpointPath != "/catalog/models" {
					t.Fatalf("expected DiscoveryEndpointPath=/catalog/models, got %q", cfg.DiscoveryEndpointPath)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			providerName := strings.ReplaceAll(tt.name, " ", "-")
			apiKeyEnv := strings.ToUpper(strings.ReplaceAll(providerName, "-", "_")) + "_API_KEY"
			if err := saveCustomProviderWithModelsForTest(
				baseDir,
				providerName,
				tt.driver,
				tt.baseURL,
				apiKeyEnv,
				tt.discoveryEndpointPath,
				"/chat/completions",
			); err != nil {
				t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
			}

			cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
			if err != nil {
				t.Fatalf("loadCustomProvider() error = %v", err)
			}
			if cfg.Driver != tt.driver {
				t.Fatalf("expected driver %q, got %q", tt.driver, cfg.Driver)
			}
			if cfg.BaseURL != tt.baseURL {
				t.Fatalf("expected baseURL %q, got %q", tt.baseURL, cfg.BaseURL)
			}
			if cfg.APIKeyEnv != apiKeyEnv {
				t.Fatalf("expected api_key_env %q, got %q", apiKeyEnv, cfg.APIKeyEnv)
			}
			tt.assert(t, cfg)
		})
	}
}

func TestSaveCustomProviderRejectsUnsafeProviderName(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	invalidNames := []string{
		"",
		" ",
		"../escape",
		"..",
		"team/gateway",
		`team\gateway`,
		"/tmp/abs",
		"中文",
	}
	for _, name := range invalidNames {
		err := saveCustomProviderWithModelsForTest(
			baseDir,
			name,
			provider.DriverOpenAICompat,
			"https://llm.example.com/v1",
			"CUSTOM_API_KEY",
			"",
			"/chat/completions",
		)
		if err == nil {
			t.Fatalf("expected SaveCustomProviderWithModels to reject %q", name)
		}
	}
}

func TestSaveCustomProviderRejectsManualSourceWithoutModels(t *testing.T) {
	t.Parallel()

	err := SaveCustomProviderWithModels(t.TempDir(), SaveCustomProviderInput{
		Name:        "manual-empty-models",
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "MANUAL_EMPTY_MODELS_API_KEY",
		ModelSource: ModelSourceManual,
		Models:      nil,
	})
	if err == nil || !strings.Contains(err.Error(), "manual model source requires non-empty models") {
		t.Fatalf("expected manual source empty models validation error, got %v", err)
	}
}

func TestSaveCustomProviderRejectsModelWithoutName(t *testing.T) {
	t.Parallel()

	err := SaveCustomProviderWithModels(t.TempDir(), SaveCustomProviderInput{
		Name:        "manual-missing-model-name",
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "MANUAL_MISSING_MODEL_NAME_API_KEY",
		ModelSource: ModelSourceManual,
		Models: []providertypes.ModelDescriptor{
			{ID: "manual-model-1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "models[0].name is empty") {
		t.Fatalf("expected model name required error, got %v", err)
	}
}

func TestSaveCustomProviderRejectsInvalidModelSource(t *testing.T) {
	t.Parallel()

	err := SaveCustomProviderWithModels(t.TempDir(), SaveCustomProviderInput{
		Name:        "invalid-model-source",
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "INVALID_MODEL_SOURCE_API_KEY",
		ModelSource: "manul",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported model_source") {
		t.Fatalf("expected invalid model_source error, got %v", err)
	}
}

func TestSaveCustomProviderAndLoadCustomProviderStayConsistent(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	tests := []struct {
		name                 string
		input                SaveCustomProviderInput
		wantModelSource      string
		wantChatAPIMode      string
		wantChatPath         string
		wantDiscoveryPath    string
		wantModelDescriptors int
	}{
		{
			name: "discover source",
			input: SaveCustomProviderInput{
				Name:                  "roundtrip-discover",
				Driver:                provider.DriverOpenAICompat,
				BaseURL:               "https://llm.example.com/v1",
				APIKeyEnv:             "ROUNDTRIP_DISCOVER_API_KEY",
				ModelSource:           ModelSourceDiscover,
				ChatAPIMode:           provider.ChatAPIModeResponses,
				DiscoveryEndpointPath: "/models",
			},
			wantModelSource:      ModelSourceDiscover,
			wantChatAPIMode:      provider.ChatAPIModeResponses,
			wantChatPath:         "",
			wantDiscoveryPath:    "/models",
			wantModelDescriptors: 0,
		},
		{
			name: "manual source",
			input: SaveCustomProviderInput{
				Name:                  "roundtrip-manual",
				Driver:                provider.DriverOpenAICompat,
				BaseURL:               "https://llm.example.com/v1",
				APIKeyEnv:             "ROUNDTRIP_MANUAL_API_KEY",
				ModelSource:           ModelSourceManual,
				DiscoveryEndpointPath: "/should-be-cleared",
				Models: []providertypes.ModelDescriptor{
					{
						ID:   "manual-model-1",
						Name: "Manual Model 1",
					},
				},
			},
			wantModelSource:      ModelSourceManual,
			wantChatAPIMode:      "",
			wantChatPath:         "",
			wantDiscoveryPath:    "",
			wantModelDescriptors: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := SaveCustomProviderWithModels(baseDir, tt.input); err != nil {
				t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
			}

			loaded, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, tt.input.Name))
			if err != nil {
				t.Fatalf("loadCustomProvider() error = %v", err)
			}

			if loaded.Name != tt.input.Name {
				t.Fatalf("expected name %q, got %q", tt.input.Name, loaded.Name)
			}
			if loaded.Driver != tt.input.Driver {
				t.Fatalf("expected driver %q, got %q", tt.input.Driver, loaded.Driver)
			}
			if loaded.BaseURL != tt.input.BaseURL {
				t.Fatalf("expected base url %q, got %q", tt.input.BaseURL, loaded.BaseURL)
			}
			if loaded.APIKeyEnv != tt.input.APIKeyEnv {
				t.Fatalf("expected api key env %q, got %q", tt.input.APIKeyEnv, loaded.APIKeyEnv)
			}
			if loaded.ModelSource != tt.wantModelSource {
				t.Fatalf("expected model_source %q, got %q", tt.wantModelSource, loaded.ModelSource)
			}
			if loaded.ChatAPIMode != tt.wantChatAPIMode {
				t.Fatalf("expected chat_api_mode %q, got %q", tt.wantChatAPIMode, loaded.ChatAPIMode)
			}
			if loaded.ChatEndpointPath != tt.wantChatPath {
				t.Fatalf("expected chat endpoint %q, got %q", tt.wantChatPath, loaded.ChatEndpointPath)
			}
			if loaded.DiscoveryEndpointPath != tt.wantDiscoveryPath {
				t.Fatalf("expected discovery endpoint %q, got %q", tt.wantDiscoveryPath, loaded.DiscoveryEndpointPath)
			}
			if len(loaded.Models) != tt.wantModelDescriptors {
				t.Fatalf("expected model descriptors %d, got %+v", tt.wantModelDescriptors, loaded.Models)
			}
		})
	}
}

func TestSaveCustomProviderManualModelsPersistIDAndNameOnly(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "manual-models-provider"
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:        providerName,
		Driver:      provider.DriverOpenAICompat,
		BaseURL:     "https://llm.example.com/v1",
		APIKeyEnv:   "MANUAL_MODELS_PROVIDER_API_KEY",
		ModelSource: ModelSourceManual,
		Models: []providertypes.ModelDescriptor{
			{
				ID:   "manual-model-1",
				Name: "Manual Model 1",
			},
			{
				ID:   "manual-model-2",
				Name: "Manual Model 2",
			},
		},
	})
	if err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
	if err != nil {
		t.Fatalf("loadCustomProvider() error = %v", err)
	}
	if cfg.ModelSource != ModelSourceManual {
		t.Fatalf("expected model source manual, got %q", cfg.ModelSource)
	}
	if cfg.DiscoveryEndpointPath != "" {
		t.Fatalf("expected discovery settings to be empty in manual mode, got %+v", cfg)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("expected model list with 2 entries, got %+v", cfg.Models)
	}
	if cfg.Models[0].ContextWindow != 0 || cfg.Models[0].MaxOutputTokens != 0 || cfg.Models[1].ContextWindow != 0 || cfg.Models[1].MaxOutputTokens != 0 {
		t.Fatalf("expected persisted manual models to omit metadata, got %+v", cfg.Models)
	}
}

func TestSaveAndLoadCustomProviderPersistsGenerateControls(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "retry-controls-provider"
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                   providerName,
		Driver:                 provider.DriverOpenAICompat,
		BaseURL:                "https://llm.example.com/v1",
		APIKeyEnv:              "RETRY_CONTROLS_PROVIDER_API_KEY",
		ModelSource:            ModelSourceDiscover,
		DiscoveryEndpointPath:  provider.DiscoveryEndpointPathModels,
		GenerateMaxRetries:     7,
		GenerateIdleTimeoutSec: 420,
	})
	if err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
	if err != nil {
		t.Fatalf("loadCustomProvider() error = %v", err)
	}
	if cfg.GenerateMaxRetries != 7 {
		t.Fatalf("expected GenerateMaxRetries=7, got %d", cfg.GenerateMaxRetries)
	}
	if cfg.GenerateIdleTimeoutSec != 420 {
		t.Fatalf("expected GenerateIdleTimeoutSec=420, got %d", cfg.GenerateIdleTimeoutSec)
	}
}

func TestSaveAndLoadCustomProviderPreservesExplicitZeroGenerateRetries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "zero-retry-provider"
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                   providerName,
		Driver:                 provider.DriverOpenAICompat,
		BaseURL:                "https://llm.example.com/v1",
		APIKeyEnv:              "ZERO_RETRY_PROVIDER_API_KEY",
		ModelSource:            ModelSourceDiscover,
		DiscoveryEndpointPath:  provider.DiscoveryEndpointPathModels,
		GenerateMaxRetries:     0,
		GenerateMaxRetriesSet:  true,
		GenerateIdleTimeoutSec: 420,
	})
	if err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(baseDir, providersDirName, providerName, customProviderConfigName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "generate_max_retries: 0") {
		t.Fatalf("expected generate_max_retries: 0 to be persisted, got %q", content)
	}

	cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
	if err != nil {
		t.Fatalf("loadCustomProvider() error = %v", err)
	}
	if !cfg.GenerateMaxRetriesSet {
		t.Fatal("expected explicit zero retry setting to remain marked as configured")
	}
	runtimeCfg, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	providerRuntimeCfg, err := runtimeCfg.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if providerRuntimeCfg.GenerateMaxRetries != 0 {
		t.Fatalf("expected explicit zero retry setting to disable retries, got %d", providerRuntimeCfg.GenerateMaxRetries)
	}
}

func TestSaveCustomProviderOmitsDefaultGenerateControlsWhenUnset(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "omit-default-generate-controls"
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "OMIT_DEFAULT_GENERATE_CONTROLS_API_KEY",
		ModelSource:           ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(baseDir, providersDirName, providerName, customProviderConfigName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if strings.Contains(content, "generate_max_retries") {
		t.Fatalf("expected generate_max_retries to be omitted, got %q", content)
	}
	if strings.Contains(content, "generate_idle_timeout_sec") {
		t.Fatalf("expected generate_idle_timeout_sec to be omitted, got %q", content)
	}
}

func TestLoadCustomProviderUsesDefaultGenerateRetriesWhenUnset(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	const providerName = "default-retry-provider"
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                  providerName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               "https://llm.example.com/v1",
		APIKeyEnv:             "DEFAULT_RETRY_PROVIDER_API_KEY",
		ModelSource:           ModelSourceDiscover,
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
	})
	if err != nil {
		t.Fatalf("SaveCustomProviderWithModels() error = %v", err)
	}

	cfg, err := loadCustomProvider(filepath.Join(baseDir, providersDirName, providerName))
	if err != nil {
		t.Fatalf("loadCustomProvider() error = %v", err)
	}
	if cfg.GenerateMaxRetriesSet {
		t.Fatal("expected omitted generate_max_retries to remain unset")
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	runtimeCfg, err := resolved.ToRuntimeConfig()
	if err != nil {
		t.Fatalf("ToRuntimeConfig() error = %v", err)
	}
	if runtimeCfg.GenerateMaxRetries != provider.DefaultGenerateMaxRetries {
		t.Fatalf("expected omitted generate_max_retries to use default %d, got %d", provider.DefaultGenerateMaxRetries, runtimeCfg.GenerateMaxRetries)
	}
}

func TestLoaderRejectsCustomProviderGenerateStartTimeoutField(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
generate_start_timeout_sec: 75
discovery_endpoint_path: /models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field generate_start_timeout_sec not found") {
		t.Fatalf("expected unknown field rejection for generate_start_timeout_sec, got %v", err)
	}
}

func TestSaveCustomProviderRejectsNegativeGenerateControls(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	err := SaveCustomProviderWithModels(baseDir, SaveCustomProviderInput{
		Name:                   "invalid-generate-controls",
		Driver:                 provider.DriverOpenAICompat,
		BaseURL:                "https://llm.example.com/v1",
		APIKeyEnv:              "INVALID_GENERATE_CONTROLS_API_KEY",
		ModelSource:            ModelSourceDiscover,
		DiscoveryEndpointPath:  provider.DiscoveryEndpointPathModels,
		GenerateIdleTimeoutSec: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "generate_idle_timeout_sec") {
		t.Fatalf("expected negative generate control to be rejected, got %v", err)
	}
}

func TestToCustomProviderModelFiles(t *testing.T) {
	t.Parallel()

	if got := toCustomProviderModelFiles(nil); got != nil {
		t.Fatalf("expected nil result for empty models, got %+v", got)
	}

	converted := toCustomProviderModelFiles([]providertypes.ModelDescriptor{
		{
			ID:   "model-a",
			Name: "Model A",
		},
		{
			ID:   "model-b",
			Name: "Model B",
		},
		{
			ID:   "Model-A",
			Name: "Merged duplicate key",
		},
	})
	if len(converted) != 2 {
		t.Fatalf("expected merged model count 2, got %+v", converted)
	}
	if converted[0].ID != "model-a" || converted[0].Name != "Model A" {
		t.Fatalf("expected normalized merge result for model-a, got %+v", converted[0])
	}
	if converted[1].ID != "model-b" || converted[1].Name != "Model B" {
		t.Fatalf("expected id/name only persistence for model-b, got %+v", converted[1])
	}
}

func TestValidateCustomProviderName(t *testing.T) {
	t.Parallel()

	if err := ValidateCustomProviderName("team.gateway_01"); err != nil {
		t.Fatalf("expected valid provider name, got %v", err)
	}
	if err := ValidateCustomProviderName("../escape"); err == nil {
		t.Fatal("expected invalid provider name rejection")
	}
}

func TestDeleteCustomProviderRejectsUnsafeProviderName(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	invalidNames := []string{
		"",
		"../escape",
		"team/gateway",
		`team\gateway`,
	}
	for _, name := range invalidNames {
		if err := DeleteCustomProvider(baseDir, name); err == nil {
			t.Fatalf("expected DeleteCustomProvider to reject %q", name)
		}
	}
}

func TestDeleteCustomProviderRemovesProviderDir(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	providerName := "team-gateway"
	providerDir := filepath.Join(baseDir, providersDirName, providerName)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := DeleteCustomProvider(baseDir, providerName); err != nil {
		t.Fatalf("DeleteCustomProvider() error = %v", err)
	}
	if _, err := os.Stat(providerDir); !os.IsNotExist(err) {
		t.Fatalf("expected provider dir to be removed, stat err = %v", err)
	}
}

func TestLoadCustomProvidersReadDirAndStatErrors(t *testing.T) {
	t.Run("providers dir read error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not support chmod 000 for directories")
		}

		baseDir := t.TempDir()
		providersPath := filepath.Join(baseDir, providersDirName)
		if err := os.MkdirAll(providersPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(providersPath, 0o000); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		defer func() { _ = os.Chmod(providersPath, 0o755) }()

		providers, err := loadCustomProviders(baseDir)
		if err == nil {
			t.Fatal("expected read providers dir error")
		}
		if providers != nil {
			t.Fatalf("expected nil providers on read error, got %d", len(providers))
		}
		if !strings.Contains(err.Error(), "read providers dir") {
			t.Fatalf("expected read providers dir error, got %v", err)
		}
	})

	t.Run("provider yaml stat error", func(t *testing.T) {
		baseDir := t.TempDir()
		providerDir := filepath.Join(baseDir, providersDirName, "blocked")
		if err := os.MkdirAll(providerDir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		// Windows 上 chmod(000) 不一定阻断访问，这里改为稳定的“provider.yaml 是目录”场景触发读取错误。
		if err := os.MkdirAll(filepath.Join(providerDir, customProviderConfigName), 0o755); err != nil {
			t.Fatalf("MkdirAll(provider.yaml dir) error = %v", err)
		}

		_, err := loadCustomProviders(baseDir)
		if err == nil {
			t.Fatal("expected custom provider read error")
		}
		if !strings.Contains(err.Error(), "read") {
			t.Fatalf("expected read error, got %v", err)
		}
	})
}

func TestLoadCustomProvidersReturnsEmptyWhenProvidersDirMissing(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	providersPath := filepath.Join(baseDir, providersDirName)
	if _, err := os.Stat(providersPath); !os.IsNotExist(err) {
		t.Fatalf("expected providers dir to be missing, got %v", err)
	}

	providers, err := loadCustomProviders(baseDir)
	if err != nil {
		t.Fatalf("loadCustomProviders() error = %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected no custom providers, got %d", len(providers))
	}
	if _, err := os.Stat(providersPath); !os.IsNotExist(err) {
		t.Fatalf("expected providers dir to remain missing, got %v", err)
	}
}

func TestLoadCustomProvidersRejectsProvidersPathFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	providersPath := filepath.Join(baseDir, providersDirName)
	if err := os.WriteFile(providersPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	providers, err := loadCustomProviders(baseDir)
	if err == nil {
		t.Fatal("expected providers dir read error")
	}
	if providers != nil {
		t.Fatalf("expected nil providers on read error, got %d", len(providers))
	}
	if !strings.Contains(err.Error(), "read providers dir") {
		t.Fatalf("expected read providers dir error, got %v", err)
	}
}

func TestLoadCustomProviderReadErrors(t *testing.T) {
	t.Run("missing provider yaml", func(t *testing.T) {
		providerDir := t.TempDir()
		if _, err := loadCustomProvider(providerDir); err == nil {
			t.Fatal("expected missing provider yaml error")
		}
	})

	t.Run("provider yaml read error", func(t *testing.T) {
		providerDir := t.TempDir()
		providerPath := filepath.Join(providerDir, customProviderConfigName)
		if err := os.MkdirAll(providerPath, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if _, err := loadCustomProvider(providerDir); err == nil {
			t.Fatal("expected provider yaml read error")
		}
	})
}

func TestSaveCustomProviderFileSystemErrors(t *testing.T) {
	t.Run("mkdir provider dir failed", func(t *testing.T) {
		root := t.TempDir()
		baseDir := filepath.Join(root, "base-file")
		if err := os.WriteFile(baseDir, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		err := saveCustomProviderWithModelsForTest(
			baseDir,
			"team-gateway",
			provider.DriverOpenAICompat,
			"https://llm.example.com/v1",
			"TEAM_GATEWAY_API_KEY",
			"",
			"/chat/completions",
		)
		if err == nil {
			t.Fatal("expected create provider dir error")
		}
	})

	t.Run("write provider yaml failed", func(t *testing.T) {
		baseDir := t.TempDir()
		providerDir := filepath.Join(baseDir, providersDirName, "team-gateway")
		if err := os.MkdirAll(filepath.Join(providerDir, customProviderConfigName), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}

		err := saveCustomProviderWithModelsForTest(
			baseDir,
			"team-gateway",
			provider.DriverOpenAICompat,
			"https://llm.example.com/v1",
			"TEAM_GATEWAY_API_KEY",
			"",
			"/chat/completions",
		)
		if err == nil {
			t.Fatal("expected write provider error")
		}
	})
}

func TestLoaderLoadsUnknownCustomProviderDriverUsingTopLevelBaseURL(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: custom-driver
base_url: https://custom.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
discovery_endpoint_path: /catalog/models
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("expected unknown custom driver with top-level base_url to load, got %v", err)
	}

	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if customProvider.Driver != "custom-driver" {
		t.Fatalf("expected custom driver to be preserved, got %q", customProvider.Driver)
	}
	if customProvider.BaseURL != "https://custom.example.com/v1" {
		t.Fatalf("expected top-level base_url to be used, got %q", customProvider.BaseURL)
	}
}

func TestLoaderIgnoresUnknownCustomProviderField(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
base_url: https://llm.example.com/v1
api_key_env: COMPANY_GATEWAY_API_KEY
profile: generic
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field profile not found") {
		t.Fatalf("expected unknown field rejection, got %v", err)
	}
}

func TestLoaderMemoConfigPreservesExplicitFalse(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
memo:
  enabled: false
  auto_extract: false
  max_entries: 123
  max_index_bytes: 4096
  extract_timeout_sec: 9
  extract_recent_messages: 4
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Memo.Enabled {
		t.Fatalf("expected memo.enabled to stay false")
	}
	if cfg.Memo.AutoExtract {
		t.Fatalf("expected memo.auto_extract to stay false")
	}
	if cfg.Memo.MaxEntries != 123 {
		t.Fatalf("expected memo.max_entries=123, got %d", cfg.Memo.MaxEntries)
	}
	if cfg.Memo.MaxIndexBytes != 4096 {
		t.Fatalf("expected memo.max_index_bytes=4096, got %d", cfg.Memo.MaxIndexBytes)
	}
	if cfg.Memo.ExtractTimeoutSec != 9 {
		t.Fatalf("expected memo.extract_timeout_sec=9, got %d", cfg.Memo.ExtractTimeoutSec)
	}
	if cfg.Memo.ExtractRecentMessages != 4 {
		t.Fatalf("expected memo.extract_recent_messages=4, got %d", cfg.Memo.ExtractRecentMessages)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "enabled: false") {
		t.Fatalf("expected persisted memo.enabled=false, got:\n%s", text)
	}
	if !strings.Contains(text, "auto_extract: false") {
		t.Fatalf("expected persisted memo.auto_extract=false, got:\n%s", text)
	}
}

func TestLoaderMemoConfigAppliesDefaultsWhenSectionMissing(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Memo.Enabled {
		t.Fatalf("expected memo.enabled default true when memo section missing")
	}
	if !cfg.Memo.AutoExtract {
		t.Fatalf("expected memo.auto_extract default true when memo section missing")
	}
	if cfg.Memo.MaxEntries <= 0 {
		t.Fatalf("expected memo.max_entries to be defaulted, got %d", cfg.Memo.MaxEntries)
	}
	if cfg.Memo.MaxIndexBytes <= 0 {
		t.Fatalf("expected memo.max_index_bytes to be defaulted, got %d", cfg.Memo.MaxIndexBytes)
	}
	if cfg.Memo.ExtractTimeoutSec <= 0 {
		t.Fatalf("expected memo.extract_timeout_sec to be defaulted, got %d", cfg.Memo.ExtractTimeoutSec)
	}
	if cfg.Memo.ExtractRecentMessages <= 0 {
		t.Fatalf("expected memo.extract_recent_messages to be defaulted, got %d", cfg.Memo.ExtractRecentMessages)
	}
}

func TestLoaderRejectsLegacyMemoMaxIndexLinesField(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
memo:
  max_index_lines: 123
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err == nil {
		t.Fatalf("expected legacy memo field to be rejected, cfg=%+v", cfg)
	}
	if !strings.Contains(err.Error(), "memo.max_index_lines has been removed") {
		t.Fatalf("expected migration hint for max_index_lines, got %v", err)
	}
}

func TestLoaderRejectsExplicitInvalidMemoNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fieldYAML  string
		errContain string
	}{
		{
			name:       "negative max_entries",
			fieldYAML:  "max_entries: -1",
			errContain: "config: memo: max_entries must be greater than 0",
		},
		{
			name:       "negative max_index_bytes",
			fieldYAML:  "max_index_bytes: -1",
			errContain: "config: memo: max_index_bytes must be greater than 0",
		},
		{
			name:       "negative extract_timeout_sec",
			fieldYAML:  "extract_timeout_sec: -1",
			errContain: "config: memo: extract_timeout_sec must be greater than 0",
		},
		{
			name:       "negative extract_recent_messages",
			fieldYAML:  "extract_recent_messages: -1",
			errContain: "config: memo: extract_recent_messages must be greater than 0",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loader := NewLoader(t.TempDir(), testDefaultConfig())
			raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
memo:
  ` + tt.fieldYAML + `
`
			writeLoaderConfig(t, loader, raw)

			_, err := loader.Load(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.errContain) {
				t.Fatalf("expected %q, got %v", tt.errContain, err)
			}
		})
	}
}

func TestLoaderLoadsCompactExtendedFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())

	raw := `
selected_provider: openai
current_model: gpt-4.1
shell: powershell
context:
  compact:
    manual_strategy: keep_recent
    manual_keep_recent_messages: 9
    max_summary_chars: 900
    micro_compact_retained_tool_spans: 4
    max_archived_prompt_chars: 4096
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Context.Compact.MicroCompactRetainedToolSpans != 4 {
		t.Fatalf("expected micro_compact_retained_tool_spans=4, got %d", cfg.Context.Compact.MicroCompactRetainedToolSpans)
	}
	if cfg.Context.Compact.MaxArchivedPromptChars != 4096 {
		t.Fatalf("expected max_archived_prompt_chars=4096, got %d", cfg.Context.Compact.MaxArchivedPromptChars)
	}
}

func TestLoaderSaveRoundTripsCompactExtendedFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	cfg := loader.DefaultConfig()
	cfg.Context.Compact.MicroCompactRetainedToolSpans = 5
	cfg.Context.Compact.MaxArchivedPromptChars = 3072

	if err := loader.Save(context.Background(), &cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "micro_compact_retained_tool_spans: 5") {
		t.Fatalf("expected persisted micro_compact_retained_tool_spans, got:\n%s", text)
	}
	if !strings.Contains(text, "max_archived_prompt_chars: 3072") {
		t.Fatalf("expected persisted max_archived_prompt_chars, got:\n%s", text)
	}

	loaded, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Context.Compact.MicroCompactRetainedToolSpans != 5 {
		t.Fatalf("expected round-trip micro_compact_retained_tool_spans=5, got %d", loaded.Context.Compact.MicroCompactRetainedToolSpans)
	}
	if loaded.Context.Compact.MaxArchivedPromptChars != 3072 {
		t.Fatalf("expected round-trip max_archived_prompt_chars=3072, got %d", loaded.Context.Compact.MaxArchivedPromptChars)
	}
}

func TestLoaderSaveAndLoadPersistsGenerateStartTimeoutSec(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	cfg := loader.DefaultConfig()
	cfg.GenerateStartTimeoutSec = 120

	if err := loader.Save(context.Background(), &cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "generate_start_timeout_sec: 120") {
		t.Fatalf("expected generate_start_timeout_sec to be persisted, got:\n%s", text)
	}

	loaded, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.GenerateStartTimeoutSec != 120 {
		t.Fatalf("expected GenerateStartTimeoutSec=120 after reload, got %d", loaded.GenerateStartTimeoutSec)
	}
}
