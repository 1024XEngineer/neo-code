package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNormalizesDefaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	err := os.WriteFile(configPath, []byte(`
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    model: gpt-4.1-mini
    api_key_env: OPENAI_API_KEY
workdir: .
`), 0o644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.SelectedProvider != "openai" {
		t.Fatalf("expected selected provider to default to openai, got %q", cfg.SelectedProvider)
	}
	if cfg.CurrentModel != "gpt-4.1-mini" {
		t.Fatalf("expected current model default, got %q", cfg.CurrentModel)
	}
	if cfg.Workdir != tempDir {
		t.Fatalf("expected workdir %q, got %q", tempDir, cfg.Workdir)
	}
	if cfg.Shell == "" {
		t.Fatalf("expected default shell to be populated")
	}
	if cfg.SessionsPath == "" {
		t.Fatalf("expected sessions path default to be populated")
	}
}

func TestLoadAutoCreatesConfigWhenMissing(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.SelectedProvider != "openai" {
		t.Fatalf("expected selected provider openai, got %q", cfg.SelectedProvider)
	}

	payload, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if len(payload) == 0 {
		t.Fatalf("expected generated config file to be non-empty")
	}
}

func TestLoadNormalizesAPIKeyEnvName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")

	err := os.WriteFile(configPath, []byte(`
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    model: gpt-4.1-mini
    api_key_env: ${OPENAI_API_KEY}
selected_provider: openai
current_model: gpt-4.1-mini
workdir: .
shell: powershell
sessions_path: ./sessions.json
`), 0o644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})
	if err := os.Chdir(filepath.Dir(configPath)); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Providers[0].APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("expected normalized env name OPENAI_API_KEY, got %q", cfg.Providers[0].APIKeyEnv)
	}
}

func TestValidateRejectsUnknownSelectedProvider(t *testing.T) {
	err := Validate(Config{
		Providers: []ProviderConfig{
			{
				Name:      "openai",
				Type:      "openai",
				BaseURL:   "https://api.openai.com/v1",
				Model:     "gpt-4.1-mini",
				APIKeyEnv: "OPENAI_API_KEY",
			},
		},
		SelectedProvider: "missing",
		CurrentModel:     "gpt-4.1-mini",
		Workdir:          t.TempDir(),
		Shell:            "powershell",
	})
	if err == nil {
		t.Fatalf("expected validation error for missing selected provider")
	}
}
