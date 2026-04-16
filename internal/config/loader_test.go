package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err == nil || !strings.Contains(err.Error(), "parse config file") {
		t.Fatalf("expected malformed yaml parse error, got %v", err)
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
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
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
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    name: DeepSeek Coder
    context_window: 131072
    max_output_tokens: 8192
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: chat_completions
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
	if customProvider.APIStyle != "chat_completions" {
		t.Fatalf("expected api_style chat_completions, got %q", customProvider.APIStyle)
	}
	if customProvider.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("expected base url https://llm.example.com/v1, got %q", customProvider.BaseURL)
	}
	if customProvider.Model != "" {
		t.Fatalf("expected custom provider default model to be empty, got %q", customProvider.Model)
	}
	if len(customProvider.Models) != 1 {
		t.Fatalf("expected custom provider model metadata from provider.yaml, got %+v", customProvider.Models)
	}
	if customProvider.Models[0].ID != "deepseek-coder" || customProvider.Models[0].ContextWindow != 131072 {
		t.Fatalf("expected parsed model metadata, got %+v", customProvider.Models[0])
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
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: chat_completions
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

func TestLoaderRejectsCustomProviderDefaultModel(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
default_model: deepseek-coder
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: chat_completions
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "field default_model not found") {
		t.Fatalf("expected unknown field rejection for default_model, got %v", err)
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
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: chat_completions
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
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - name: DeepSeek Coder
openai_compatible:
  base_url: https://llm.example.com/v1
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "models[0].id") {
		t.Fatalf("expected empty model id rejection, got %v", err)
	}
}

func TestLoaderRejectsCustomProviderModelWithInvalidContextWindow(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    context_window: 0
openai_compatible:
  base_url: https://llm.example.com/v1
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "context_window") {
		t.Fatalf("expected invalid context_window rejection, got %v", err)
	}
}

func TestLoaderRejectsCustomProviderModelWithInvalidMaxOutputTokens(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
    max_output_tokens: 0
openai_compatible:
  base_url: https://llm.example.com/v1
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "max_output_tokens") {
		t.Fatalf("expected invalid max_output_tokens rejection, got %v", err)
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
api_key_env: COMPANY_GATEWAY_API_KEY
models:
  - id: deepseek-coder
  - id: DeepSeek-Coder
openai_compatible:
  base_url: https://llm.example.com/v1
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate model id rejection, got %v", err)
	}
}

func TestLoaderParsesAutoCompactDerivedFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	raw := `
selected_provider: openai
current_model: gpt-5.4
shell: powershell
context:
  auto_compact:
    enabled: true
    input_token_threshold: 0
    reserve_tokens: 9000
    fallback_input_token_threshold: 88000
`
	writeLoaderConfig(t, loader, raw)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Context.AutoCompact.InputTokenThreshold != 0 {
		t.Fatalf("expected implicit threshold 0, got %d", cfg.Context.AutoCompact.InputTokenThreshold)
	}
	if cfg.Context.AutoCompact.ReserveTokens != 9000 {
		t.Fatalf("expected reserve_tokens=9000, got %d", cfg.Context.AutoCompact.ReserveTokens)
	}
	if cfg.Context.AutoCompact.FallbackInputTokenThreshold != 88000 {
		t.Fatalf("expected fallback_input_token_threshold=88000, got %d", cfg.Context.AutoCompact.FallbackInputTokenThreshold)
	}
}

func TestLoaderSavePersistsAutoCompactDerivedFields(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	cfg := testDefaultConfig().Clone()
	cfg.Context.AutoCompact.Enabled = true
	cfg.Context.AutoCompact.InputTokenThreshold = 0
	cfg.Context.AutoCompact.ReserveTokens = 9000
	cfg.Context.AutoCompact.FallbackInputTokenThreshold = 88000

	if err := loader.Save(context.Background(), &cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "input_token_threshold: 100000") {
		t.Fatalf("expected implicit threshold to avoid legacy default, got:\n%s", text)
	}
	if !strings.Contains(text, "reserve_tokens: 9000") {
		t.Fatalf("expected reserve_tokens to persist, got:\n%s", text)
	}
	if !strings.Contains(text, "fallback_input_token_threshold: 88000") {
		t.Fatalf("expected fallback_input_token_threshold to persist, got:\n%s", text)
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
api_key_env: OPENAI_GATEWAY_API_KEY
openai_compatible:
  base_url: https://api.example.com/v1
  api_style: chat_completions
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
api_key_env: GATEWAY_A_API_KEY
openai_compatible:
  base_url: https://api.example.com/v1/
  api_style: responses
`
	providerB := `
name: gateway-b
driver: openaicompat
api_key_env: GATEWAY_B_API_KEY
openai_compatible:
  base_url: https://API.EXAMPLE.COM/v1
  api_style: Responses
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
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  api_style: responses
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

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	customProvider, err := cfg.ProviderByName("company-gateway")
	if err != nil {
		t.Fatalf("ProviderByName(company-gateway) error = %v", err)
	}
	if customProvider.BaseURL != "https://llm.example.com/v1" {
		t.Fatalf("expected openai-compatible base url, got %q", customProvider.BaseURL)
	}
	if customProvider.APIStyle != "responses" {
		t.Fatalf("expected openai-compatible api_style, got %q", customProvider.APIStyle)
	}
	if customProvider.DeploymentMode != "" {
		t.Fatalf("expected gemini deployment_mode to be ignored, got %q", customProvider.DeploymentMode)
	}
	if customProvider.APIVersion != "" {
		t.Fatalf("expected anthropic api_version to be ignored, got %q", customProvider.APIVersion)
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
gemini:
  base_url: https://gemini.example.com/v1beta
  deployment_mode: vertex
`
	if err := os.WriteFile(filepath.Join(customDir, customProviderConfigName), []byte(strings.TrimSpace(providerYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write provider.yaml: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "base_url is empty") {
		t.Fatalf("expected missing openai-compatible base_url error, got %v", err)
	}
}

func TestResolveCustomProviderSettingsByDriver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file customProviderFile
		want customProviderSettings
	}{
		{
			name: "openaicompat prefers protocol block fields",
			file: customProviderFile{
				Driver: "openaicompat",
				OpenAICompatible: customOpenAICompatibleFile{
					BaseURL:  " https://llm.example.com/v1 ",
					APIStyle: " responses ",
				},
				Gemini: customGeminiProviderFile{
					BaseURL:        "https://gemini.example.com",
					DeploymentMode: "vertex",
				},
			},
			want: customProviderSettings{
				BaseURL:  "https://llm.example.com/v1",
				APIStyle: "responses",
			},
		},
		{
			name: "gemini uses base_url only from gemini block",
			file: customProviderFile{
				Driver:  "gemini",
				BaseURL: " https://gateway.example.com ",
				Gemini: customGeminiProviderFile{
					BaseURL:        "https://gemini.example.com",
					DeploymentMode: " vertex ",
				},
				Anthropic: customAnthropicProviderFile{
					APIVersion: "2023-06-01",
				},
			},
			want: customProviderSettings{
				BaseURL:        "https://gemini.example.com",
				DeploymentMode: "vertex",
			},
		},
		{
			name: "anthropic uses api version only from anthropic block",
			file: customProviderFile{
				Driver: "anthropic",
				Anthropic: customAnthropicProviderFile{
					BaseURL:    " https://anthropic.example.com/v1 ",
					APIVersion: " 2023-06-01 ",
				},
			},
			want: customProviderSettings{
				BaseURL:    "https://anthropic.example.com/v1",
				APIVersion: "2023-06-01",
			},
		},
		{
			name: "unknown driver ignores protocol blocks",
			file: customProviderFile{
				Driver:  "custom-driver",
				BaseURL: " https://custom.example.com/v1 ",
				Gemini: customGeminiProviderFile{
					BaseURL: " https://gemini.example.com/v1beta ",
				},
				Anthropic: customAnthropicProviderFile{
					BaseURL: "https://anthropic.example.com/v1",
				},
			},
			want: customProviderSettings{
				BaseURL: "https://custom.example.com/v1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveCustomProviderSettings(tt.file)
			if got != tt.want {
				t.Fatalf("resolveCustomProviderSettings() = %+v, want %+v", got, tt.want)
			}
		})
	}
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
gemini:
  base_url: https://gemini.example.com/v1beta
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
	if customProvider.APIStyle != "" || customProvider.DeploymentMode != "" || customProvider.APIVersion != "" {
		t.Fatalf("expected protocol-specific fields to stay empty for unknown driver, got %+v", customProvider)
	}
}

func TestLoaderRejectsUnknownCustomProviderField(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	customDir := filepath.Join(loader.BaseDir(), providersDirName, "company-gateway")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("mkdir custom provider dir: %v", err)
	}

	providerYAML := `
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_GATEWAY_API_KEY
openai_compatible:
  profile: generic
  base_url: https://llm.example.com/v1
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
  max_index_lines: 123
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
	if cfg.Memo.MaxIndexLines != 123 {
		t.Fatalf("expected memo.max_index_lines=123, got %d", cfg.Memo.MaxIndexLines)
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
	if cfg.Memo.MaxIndexLines <= 0 {
		t.Fatalf("expected memo.max_index_lines to be defaulted, got %d", cfg.Memo.MaxIndexLines)
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
