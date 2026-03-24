package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsMissingVarsOnly(t *testing.T) {
	t.Setenv("EXISTING_KEY", "keep-me")
	t.Setenv("NEW_KEY", "")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "EXISTING_KEY=override\nNEW_KEY= new-value \n# comment\nINVALID_LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := os.Getenv("EXISTING_KEY"); got != "keep-me" {
		t.Fatalf("expected existing env var to be preserved, got %q", got)
	}
	if got := os.Getenv("NEW_KEY"); got != "new-value" {
		t.Fatalf("expected new env var to load, got %q", got)
	}
}

func TestLoadDotEnvIgnoresMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	if err := loadDotEnv(missing); err != nil {
		t.Fatalf("expected missing file to be ignored, got %v", err)
	}
}

func TestLoadPersonaPromptReturnsTrimmedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persona.txt")
	if err := os.WriteFile(path, []byte("\n hello persona \n"), 0o644); err != nil {
		t.Fatalf("write persona file: %v", err)
	}

	if got := loadPersonaPrompt(path); got != "hello persona" {
		t.Fatalf("expected trimmed persona prompt, got %q", got)
	}
}

func TestLoadPersonaPromptReturnsEmptyForMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if got := loadPersonaPrompt(missing); got != "" {
		t.Fatalf("expected empty string for missing file, got %q", got)
	}
}
