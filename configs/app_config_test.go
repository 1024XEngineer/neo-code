package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppConfigurationValidate(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "env-chat-key")
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestAppConfigurationValidateMissingEnvAPIKey(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "")
	cfg := validConfig()

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), DefaultAPIKeyEnvVar) {
		t.Fatalf("expected %s validation error, got: %v", DefaultAPIKeyEnvVar, err)
	}
}

func TestAppConfigurationValidateUsesCustomEnvVarName(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "")
	t.Setenv("CUSTOM_CHAT_KEY", "env-chat-key")
	cfg := validConfig()
	cfg.AI.APIKey = "CUSTOM_CHAT_KEY"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected custom env var to validate, got: %v", err)
	}
	if got := cfg.RuntimeAPIKey(); got != "env-chat-key" {
		t.Fatalf("expected runtime api key from custom env, got %q", got)
	}
}

func TestAppConfigurationValidateFallsBackToDefaultEnvVarName(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "fallback-key")
	cfg := validConfig()
	cfg.AI.APIKey = ""

	if got := cfg.APIKeyEnvVarName(); got != DefaultAPIKeyEnvVar {
		t.Fatalf("expected fallback env var name %q, got %q", DefaultAPIKeyEnvVar, got)
	}
	if got := cfg.RuntimeAPIKey(); got != "fallback-key" {
		t.Fatalf("expected fallback runtime api key, got %q", got)
	}
}

func TestAppConfigurationValidateBaseAllowsMissingAIKey(t *testing.T) {
	cfg := validConfig()
	cfg.AI.APIKey = ""

	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("expected base validation to allow missing api key, got: %v", err)
	}
}

func TestAppConfigurationValidateBaseAllowsProviderWithoutLegacyCatalog(t *testing.T) {
	cfg := validConfig()
	cfg.AI.Provider = "deepseek"
	cfg.AI.Model = "deepseek-chat"

	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("expected provider config without legacy catalog to validate, got: %v", err)
	}
}

func TestAppConfigurationValidateBaseRejectsUnsupportedProvider(t *testing.T) {
	cfg := validConfig()
	cfg.AI.Provider = "unknown"

	err := cfg.ValidateBase()
	if err == nil || !strings.Contains(err.Error(), "unsupported ai.provider") {
		t.Fatalf("expected unsupported provider error, got: %v", err)
	}
}

func TestAppConfigurationValidateRejectsUnsupportedMemoryExtractor(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "env-chat-key")
	cfg := validConfig()
	cfg.Memory.Extractor = "unknown"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "memory.extractor") {
		t.Fatalf("expected memory.extractor validation error, got: %v", err)
	}
}

func TestLoadAppConfig(t *testing.T) {
	t.Setenv("CUSTOM_CHAT_KEY", "env-chat-key")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`app:
  name: "NeoCode"
  version: "1.0.0"
ai:
  provider: "openll"
  api_key: "CUSTOM_CHAT_KEY"
  model: "gpt-5.4"
memory:
  top_k: 5
  min_match_score: 2.2
  max_prompt_chars: 1800
  max_items: 1000
  storage_path: "./data/memory_rules.json"
  project_files:
    - "AGENTS.md"
    - "CLAUDE.md"
  project_prompt_chars: 2400
  extractor: "auto"
  extractor_model: "gpt-5.4-mini"
  extractor_timeout_seconds: 15
history:
  short_term_turns: 6
  max_tool_context_messages: 3
  max_tool_context_output_size: 4000
  persist_session_state: true
  workspace_state_dir: "./data/workspaces"
  resume_last_session: true
persona:
  file_path: "./configs/persona.txt"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	GlobalAppConfig = nil
	if err := LoadAppConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}

	if GlobalAppConfig == nil || GlobalAppConfig.AI.Model != "gpt-5.4" {
		t.Fatalf("expected loaded config, got %+v", GlobalAppConfig)
	}
	if GlobalAppConfig.AI.APIKey != "CUSTOM_CHAT_KEY" {
		t.Fatalf("expected config api key env name to persist, got %q", GlobalAppConfig.AI.APIKey)
	}
	if got := GlobalAppConfig.RuntimeAPIKey(); got != "env-chat-key" {
		t.Fatalf("expected runtime api key from custom env, got %q", got)
	}
	if GlobalAppConfig.Memory.Extractor != "auto" {
		t.Fatalf("expected memory extractor auto, got %q", GlobalAppConfig.Memory.Extractor)
	}
	if GlobalAppConfig.Memory.ExtractorModel != "gpt-5.4-mini" {
		t.Fatalf("expected extractor model gpt-5.4-mini, got %q", GlobalAppConfig.Memory.ExtractorModel)
	}
}

func TestAppConfigurationValidateMissingMemoryStoragePath(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "env-chat-key")
	cfg := validConfig()
	cfg.Memory.StoragePath = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "memory.storage_path") {
		t.Fatalf("expected storage path validation error, got: %v", err)
	}
}

func TestEnsureConfigFileCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, created, err := EnsureConfigFile(path)
	if err != nil {
		t.Fatalf("ensure config: %v", err)
	}
	if !created {
		t.Fatal("expected config file to be created")
	}
	if cfg == nil || cfg.AI.Provider == "" {
		t.Fatalf("expected default config, got %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file on disk: %v", err)
	}
	if strings.TrimSpace(cfg.AI.APIKey) != DefaultAPIKeyEnvVar {
		t.Fatalf("expected default api key env name %q, got %q", DefaultAPIKeyEnvVar, cfg.AI.APIKey)
	}
}

func TestWriteAppConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	want := validConfig()

	if err := WriteAppConfig(path, want); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := LoadBootstrapConfig(path)
	if err != nil {
		t.Fatalf("load bootstrap config: %v", err)
	}
	if got.AI.APIKey != want.AI.APIKey {
		t.Fatalf("expected written config api key env name %q, got %q", want.AI.APIKey, got.AI.APIKey)
	}
	if got.AI.Model != want.AI.Model {
		t.Fatalf("expected model %q, got %q", want.AI.Model, got.AI.Model)
	}
	if got.Memory.StoragePath != want.Memory.StoragePath {
		t.Fatalf("expected storage path %q, got %q", want.Memory.StoragePath, got.Memory.StoragePath)
	}
	if got.Memory.Extractor != want.Memory.Extractor {
		t.Fatalf("expected extractor %q, got %q", want.Memory.Extractor, got.Memory.Extractor)
	}
}

func validConfig() *AppConfiguration {
	cfg := &AppConfiguration{}
	cfg.AI.Provider = "openll"
	cfg.AI.APIKey = DefaultAPIKeyEnvVar
	cfg.AI.Model = "gpt-5.4"
	cfg.Memory.TopK = 5
	cfg.Memory.MinMatchScore = 2.2
	cfg.Memory.MaxPromptChars = 1800
	cfg.Memory.MaxItems = 1000
	cfg.Memory.StoragePath = "./data/memory_rules.json"
	cfg.Memory.PersistTypes = []string{"user_preference", "project_rule", "code_fact", "fix_recipe"}
	cfg.Memory.ProjectFiles = []string{"AGENTS.md"}
	cfg.Memory.ProjectPromptChars = 2400
	cfg.Memory.Extractor = "rule"
	cfg.Memory.ExtractorModel = ""
	cfg.Memory.ExtractorTimeoutSecond = 20
	cfg.History.ShortTermTurns = 6
	cfg.History.MaxToolContextMessages = 3
	cfg.History.MaxToolContextOutputSize = 4000
	cfg.History.PersistSessionState = true
	cfg.History.WorkspaceStateDir = "./data/workspaces"
	cfg.History.ResumeLastSession = true
	cfg.Persona.FilePath = DefaultPersonaFilePath
	return cfg
}

func TestDefaultAppConfigUsesCheckedInPersonaPath(t *testing.T) {
	cfg := DefaultAppConfig()
	if cfg.Persona.FilePath != DefaultPersonaFilePath {
		t.Fatalf("expected default persona path %q, got %q", DefaultPersonaFilePath, cfg.Persona.FilePath)
	}
	if cfg.Memory.Extractor != "rule" {
		t.Fatalf("expected default memory extractor to be rule, got %q", cfg.Memory.Extractor)
	}
	if cfg.Memory.ProjectPromptChars != 2400 {
		t.Fatalf("expected default project prompt chars 2400, got %d", cfg.Memory.ProjectPromptChars)
	}
	if len(cfg.Memory.ProjectFiles) != 1 || cfg.Memory.ProjectFiles[0] != "AGENTS.md" {
		t.Fatalf("expected default project files to contain only AGENTS.md, got %+v", cfg.Memory.ProjectFiles)
	}
	if cfg.Memory.ExtractorTimeoutSecond != 20 {
		t.Fatalf("expected default extractor timeout 20, got %d", cfg.Memory.ExtractorTimeoutSecond)
	}
}

func TestAppConfigurationValidateRejectsInvalidProjectPromptChars(t *testing.T) {
	t.Setenv(DefaultAPIKeyEnvVar, "env-chat-key")
	cfg := validConfig()
	cfg.Memory.ProjectPromptChars = 0

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "memory.project_prompt_chars") {
		t.Fatalf("expected project prompt chars validation error, got: %v", err)
	}
}

func TestResolvePersonaFilePathFallsBackToCheckedInFile(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	configsDir := filepath.Join(tmpDir, "configs")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configsDir, "persona.txt"), []byte("persona"), 0o644); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	resolved := ResolvePersonaFilePath("./persona.txt")
	if resolved != DefaultPersonaFilePath {
		t.Fatalf("expected legacy persona path to resolve to %q, got %q", DefaultPersonaFilePath, resolved)
	}
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("expected resolved persona file to exist: %v", err)
	}
}
