package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyProviderYAML() string {
	return strings.TrimSpace(`
name: company-gateway
driver: openaicompat
api_key_env: COMPANY_API_KEY
openai_compatible:
  base_url: https://llm.example.com/v1
  chat_endpoint_path: /gateway/chat
  discovery_endpoint_path: /gateway/models
`) + "\n"
}

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

func TestRunMigrationWithTargetFileDryRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, providerConfigFileName)
	if err := os.WriteFile(target, []byte(legacyProviderYAML()), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	results, err := runMigration(dir, target, true)
	if err != nil {
		t.Fatalf("runMigration() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Changed {
		t.Fatal("expected dry-run to detect migration change")
	}
	if results[0].Backup != "" {
		t.Fatalf("expected no backup in dry-run, got %q", results[0].Backup)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if !strings.Contains(string(content), "openai_compatible:") {
		t.Fatalf("expected dry-run not to mutate file, got:\n%s", string(content))
	}
}

func TestRunMigrationReturnsErrorWhenTargetIsMissing(t *testing.T) {
	t.Parallel()

	_, err := runMigration(t.TempDir(), filepath.Join(t.TempDir(), providerConfigFileName), true)
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "stat target") {
		t.Fatalf("expected stat target error, got %v", err)
	}
}

func TestRunMigrationReturnsErrorWhenProviderFileIsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, providerConfigFileName)
	if err := os.WriteFile(target, []byte("name: [invalid"), 0o644); err != nil {
		t.Fatalf("write invalid target file: %v", err)
	}

	_, err := runMigration(dir, target, false)
	if err == nil {
		t.Fatal("expected migration error for invalid yaml")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("expected migrate error wrapper, got %v", err)
	}
}

func TestCollectProviderFilesRejectsNonProviderFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "not-provider.yaml")
	if err := os.WriteFile(target, []byte("name: x\n"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	_, err := collectProviderFiles(dir, target)
	if err == nil {
		t.Fatal("expected error for non-provider target file")
	}
	if !strings.Contains(err.Error(), "target must be provider.yaml or a providers dir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollectProviderFilesFromProvidersDirNoProviderYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := filepath.Join(dir, "providers")
	if err := os.MkdirAll(filepath.Join(providersDir, "only-dir"), 0o755); err != nil {
		t.Fatalf("mkdir providers dir: %v", err)
	}

	_, err := collectProviderFilesFromProvidersDir(providersDir)
	if err == nil {
		t.Fatal("expected error when no provider.yaml exists")
	}
	if !strings.Contains(err.Error(), "no provider.yaml found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigrateProviderFileDryRunDoesNotWriteBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, providerConfigFileName)
	if err := os.WriteFile(target, []byte(legacyProviderYAML()), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	result, err := migrateProviderFile(target, true)
	if err != nil {
		t.Fatalf("migrateProviderFile() error = %v", err)
	}
	if !result.Changed {
		t.Fatal("expected dry-run result changed")
	}
	if result.Backup != "" {
		t.Fatalf("expected no backup in dry-run, got %q", result.Backup)
	}
	if _, err := os.Stat(target + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected no backup file created, stat err=%v", err)
	}
}

func TestMigrateProviderFileReturnsReadError(t *testing.T) {
	t.Parallel()

	_, err := migrateProviderFile(filepath.Join(t.TempDir(), providerConfigFileName), false)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("expected read wrapper, got %v", err)
	}
}

func TestSelectLegacyBlock(t *testing.T) {
	t.Parallel()

	t.Run("prefers driver matched block", func(t *testing.T) {
		t.Parallel()

		cfg := migrationFile{
			Driver: "gemini",
			OpenAICompatible: legacyProviderProtocol{
				BaseURL: "https://openai.example.com",
			},
			Gemini: legacyProviderProtocol{
				BaseURL: "https://gemini.example.com",
			},
		}

		got := selectLegacyBlock(cfg)
		if got.BaseURL != "https://gemini.example.com" {
			t.Fatalf("expected gemini block, got %#v", got)
		}
	})

	t.Run("falls back to first non-empty block", func(t *testing.T) {
		t.Parallel()

		cfg := migrationFile{
			Driver: "unknown-driver",
			OpenAICompatible: legacyProviderProtocol{
				BaseURL: "https://fallback.example.com",
			},
		}

		got := selectLegacyBlock(cfg)
		if got.BaseURL != "https://fallback.example.com" {
			t.Fatalf("expected fallback block, got %#v", got)
		}
	})
}

func TestDefaultBaseDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		got := defaultBaseDir()
		if got != filepath.Join(home, defaultNeoCodeDirName) {
			t.Fatalf("expected %q, got %q", filepath.Join(home, defaultNeoCodeDirName), got)
		}
		return
	}

	t.Setenv("HOME", "")
	if got := defaultBaseDir(); got != filepath.Join("~", defaultNeoCodeDirName) {
		t.Fatalf("expected fallback path %q, got %q", filepath.Join("~", defaultNeoCodeDirName), got)
	}
}
