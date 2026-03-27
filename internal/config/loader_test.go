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

	loader := NewLoader(t.TempDir())
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

	loader := NewLoader(t.TempDir())
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

func TestLoaderLoadEnvironmentSilentlyIgnoresDotEnvFailures(t *testing.T) {
	tempDir := t.TempDir()
	restoreEnv(t, DefaultOpenAIAPIKeyEnv)
	_ = os.Unsetenv(DefaultOpenAIAPIKeyEnv)

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousWD)
	})

	if err := os.MkdirAll(filepath.Join(tempDir, ".env"), 0o755); err != nil {
		t.Fatalf("mkdir cwd .env dir: %v", err)
	}

	loader := NewLoader(filepath.Join(tempDir, ".neocode"))
	if err := os.MkdirAll(loader.EnvPath(), 0o755); err != nil {
		t.Fatalf("mkdir managed .env dir: %v", err)
	}

	loader.LoadEnvironment()

	if got := os.Getenv(DefaultOpenAIAPIKeyEnv); got != "" {
		t.Fatalf("expected env to stay empty when dotenv loading fails, got %q", got)
	}
}

func TestLoaderLoadInvalidBaseDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	baseFile := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(baseFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}

	loader := NewLoader(baseFile)
	_, err := loader.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create config dir") {
		t.Fatalf("expected invalid base dir error, got %v", err)
	}
}
