package configs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePersonaFilePathFindsRepoLevelConfigsFromNestedWorkingDir(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tmpDir := t.TempDir()
	configsDir := filepath.Join(tmpDir, "configs")
	nestedDir := filepath.Join(tmpDir, "cmd", "tui")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("mkdir configs: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}
	personaPath := filepath.Join(configsDir, "persona.txt")
	if err := os.WriteFile(personaPath, []byte("persona"), 0o644); err != nil {
		t.Fatalf("write persona: %v", err)
	}
	if err := os.Chdir(nestedDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	resolved := ResolvePersonaFilePath("./configs/persona.txt")
	if resolved != personaPath {
		t.Fatalf("expected nested working dir to resolve %q, got %q", personaPath, resolved)
	}
}
