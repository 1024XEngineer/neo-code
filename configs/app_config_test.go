package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppConfigurationValidate(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestAppConfigurationValidateMissingAIKey(t *testing.T) {
	cfg := validConfig()
	cfg.AI.APIKey = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ai.api_key") {
		t.Fatalf("expected ai.api_key validation error, got: %v", err)
	}
}

func TestAppConfigurationValidateBaseAllowsMissingAIKey(t *testing.T) {
	cfg := validConfig()
	cfg.AI.APIKey = ""

	if err := cfg.ValidateBase(); err != nil {
		t.Fatalf("expected base validation to allow missing api key, got: %v", err)
	}
}

func TestLoadAppConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`app:
  name: "NeoCode"
  version: "1.0.0"
ai:
  provider: "modelscope"
  api_key: "chat-key"
  model: "chat-model"
memory:
  top_k: 5
  min_match_score: 2.2
  max_prompt_chars: 1800
  max_items: 1000
  storage_path: "./data/memory_rules.json"
history:
  short_term_turns: 6
persona:
  file_path: "./persona.txt"
models:
  chat:
    default_model: "chat-model"
    models:
      - name: "chat-model"
        url: "https://chat.example"
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	GlobalAppConfig = nil
	if err := LoadAppConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}

	if GlobalAppConfig == nil || GlobalAppConfig.AI.Model != "chat-model" {
		t.Fatalf("expected loaded config, got %+v", GlobalAppConfig)
	}
}

func TestAppConfigurationValidateMissingMemoryStoragePath(t *testing.T) {
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
	if strings.TrimSpace(cfg.AI.APIKey) != "" {
		t.Fatalf("expected default api key to be empty, got %q", cfg.AI.APIKey)
	}
}

func TestGetChatModelURLFromConfig(t *testing.T) {
	cfg := validConfig()
	url, ok := GetChatModelURLFromConfig(cfg, "chat-model")
	if !ok {
		t.Fatal("expected model lookup to succeed")
	}
	if url != "https://chat.example" {
		t.Fatalf("expected chat model url, got %q", url)
	}

	if _, ok := GetChatModelURLFromConfig(cfg, "missing-model"); ok {
		t.Fatal("expected missing model lookup to fail")
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
	if got.AI.Model != want.AI.Model {
		t.Fatalf("expected model %q, got %q", want.AI.Model, got.AI.Model)
	}
	if got.Memory.StoragePath != want.Memory.StoragePath {
		t.Fatalf("expected storage path %q, got %q", want.Memory.StoragePath, got.Memory.StoragePath)
	}
}

func validConfig() *AppConfiguration {
	cfg := &AppConfiguration{}
	cfg.AI.Provider = "modelscope"
	cfg.AI.APIKey = "chat-key"
	cfg.AI.Model = "chat-model"
	cfg.Memory.TopK = 5
	cfg.Memory.MinMatchScore = 2.2
	cfg.Memory.MaxPromptChars = 1800
	cfg.Memory.MaxItems = 1000
	cfg.Memory.StoragePath = "./data/memory_rules.json"
	cfg.Memory.PersistTypes = []string{"user_preference", "project_rule", "code_fact", "fix_recipe"}
	cfg.History.ShortTermTurns = 6
	cfg.Persona.FilePath = "./persona.txt"
	cfg.Models.Chat.DefaultModel = "chat-model"
	cfg.Models.Chat.Models = []ModelDetail{{Name: "chat-model", URL: "https://chat.example"}}
	return cfg
}
