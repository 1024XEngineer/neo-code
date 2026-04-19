package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateProviderContentFlattensLegacyOpenAICompatibleBlock(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  chat_endpoint_path: /gateway/chat
  discovery_endpoint_path: /gateway/models
`) + "\n")

	out, changed, err := migrateProviderContent(input)
	if err != nil {
		t.Fatalf("migrateProviderContent() error = %v", err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	text := string(out)
	if strings.Contains(text, "openai_compatible:") {
		t.Fatalf("expected legacy block removed, got:\n%s", text)
	}
	if !strings.Contains(text, "base_url: https://llm.example.com/v1") {
		t.Fatalf("expected base_url migrated, got:\n%s", text)
	}
	if !strings.Contains(text, "chat_endpoint_path: /gateway/chat") {
		t.Fatalf("expected chat_endpoint_path migrated, got:\n%s", text)
	}
	if !strings.Contains(text, "discovery_endpoint_path: /gateway/models") {
		t.Fatalf("expected discovery_endpoint_path migrated, got:\n%s", text)
	}
}

func TestMigrateProviderContentKeepsExistingFlatFields(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_API_KEY
base_url: https://new.example.com/v1
chat_endpoint_path: /new/chat
openai_compatible:
  base_url: https://old.example.com/v1
  chat_endpoint_path: /old/chat
`) + "\n")

	out, changed, err := migrateProviderContent(input)
	if err != nil {
		t.Fatalf("migrateProviderContent() error = %v", err)
	}
	if !changed {
		t.Fatal("expected migration change")
	}
	text := string(out)
	if !strings.Contains(text, "base_url: https://new.example.com/v1") {
		t.Fatalf("expected existing flat base_url kept, got:\n%s", text)
	}
	if !strings.Contains(text, "chat_endpoint_path: /new/chat") {
		t.Fatalf("expected existing flat chat_endpoint_path kept, got:\n%s", text)
	}
	if strings.Contains(text, "old.example.com") {
		t.Fatalf("expected old legacy value not override flat value, got:\n%s", text)
	}
}

func TestMigrateProviderContentNoLegacyReturnsUnchanged(t *testing.T) {
	t.Parallel()

	input := []byte(strings.TrimSpace(`
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_API_KEY
base_url: https://new.example.com/v1
`) + "\n")

	out, changed, err := migrateProviderContent(input)
	if err != nil {
		t.Fatalf("migrateProviderContent() error = %v", err)
	}
	if changed {
		t.Fatal("expected no migration change")
	}
	if string(out) != string(input) {
		t.Fatalf("expected output unchanged, got:\n%s", string(out))
	}
}

func TestCollectProviderFilesFromProvidersDir(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	providersDir := filepath.Join(baseDir, "providers")
	if err := os.MkdirAll(filepath.Join(providersDir, "a"), 0o755); err != nil {
		t.Fatalf("mkdir provider a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(providersDir, "b"), 0o755); err != nil {
		t.Fatalf("mkdir provider b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "a", providerConfigFileName), []byte("name: a\n"), 0o644); err != nil {
		t.Fatalf("write provider a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "b", providerConfigFileName), []byte("name: b\n"), 0o644); err != nil {
		t.Fatalf("write provider b: %v", err)
	}

	files, err := collectProviderFiles(baseDir, "")
	if err != nil {
		t.Fatalf("collectProviderFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestMigrateProviderFileCreatesBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, providerConfigFileName)
	original := strings.TrimSpace(`
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
`) + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write provider file: %v", err)
	}

	result, err := migrateProviderFile(path, false)
	if err != nil {
		t.Fatalf("migrateProviderFile() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("expected file changed")
	}
	if strings.TrimSpace(result.Backup) == "" {
		t.Fatal("expected backup path")
	}

	backupContent, err := os.ReadFile(result.Backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContent) != original {
		t.Fatalf("expected backup keep original content, got:\n%s", string(backupContent))
	}

	newContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if strings.Contains(string(newContent), "openai_compatible:") {
		t.Fatalf("expected migrated file remove legacy block, got:\n%s", string(newContent))
	}
}
