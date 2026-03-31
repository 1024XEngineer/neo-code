package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := os.MkdirAll(loader.BaseDir(), 0o755); err != nil {
		t.Fatalf("mkdir base dir: %v", err)
	}
	if err := os.WriteFile(loader.ConfigPath(), []byte("providers:\n  - name: [\n"), 0o644); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}

	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse config file") {
		t.Fatalf("expected malformed yaml parse error, got %v", err)
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

func TestLoaderRewritesLegacyProvidersFormatOnLoad(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	if err := os.MkdirAll(loader.BaseDir(), 0o755); err != nil {
		t.Fatalf("mkdir base dir: %v", err)
	}

	legacy := `
selected_provider: openai
current_model: gpt-5.4
workdir: .
shell: powershell
providers:
  - name: openai
    type: openai
    base_url: https://example.com/v1
    model: gpt-5.4
    models:
      - gpt-4.1
      - gpt-5.4
    api_key_env: OPENAI_API_KEY
`
	if err := os.WriteFile(loader.ConfigPath(), []byte(strings.TrimSpace(legacy)+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	provider, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if provider.BaseURL != testBaseURL {
		t.Fatalf("expected builtin provider base url %q, got %q", testBaseURL, provider.BaseURL)
	}
	if cfg.CurrentModel != "gpt-5.4" {
		t.Fatalf("expected current model to stay %q, got %q", "gpt-5.4", cfg.CurrentModel)
	}
	if len(provider.Models) != len(testDefaultProviderConfig().Models) {
		t.Fatalf("expected builtin provider models, got %+v", provider.Models)
	}

	rewritten, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read rewritten config: %v", err)
	}
	text := string(rewritten)
	if strings.Contains(text, "provider_overrides:") {
		t.Fatalf("expected rewritten config to drop provider overrides, got:\n%s", text)
	}
	if strings.Contains(text, "\nproviders:") || strings.HasPrefix(text, "providers:") {
		t.Fatalf("expected rewritten config to omit providers list, got:\n%s", text)
	}
	if strings.Contains(text, "models:") || strings.Contains(text, "base_url:") || strings.Contains(text, "api_key_env:") {
		t.Fatalf("expected rewritten config to omit provider metadata, got:\n%s", text)
	}
}
