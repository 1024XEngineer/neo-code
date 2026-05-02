package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderLoadReadsProjectAndGlobalAgents(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	projectRoot := t.TempDir()
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}

	projectPath := filepath.Join(projectRoot, agentsFileName)
	globalPath := filepath.Join(baseDir, agentsFileName)
	if err := os.WriteFile(projectPath, []byte("project-rules"), 0o644); err != nil {
		t.Fatalf("write project AGENTS.md: %v", err)
	}
	if err := os.WriteFile(globalPath, []byte("global-rules"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	snapshot, err := NewLoader(baseDir).Load(context.Background(), projectRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.ProjectAGENTS.Path != projectPath || snapshot.ProjectAGENTS.Content != "project-rules" {
		t.Fatalf("unexpected project snapshot: %+v", snapshot.ProjectAGENTS)
	}
	if snapshot.GlobalAGENTS.Path != globalPath || snapshot.GlobalAGENTS.Content != "global-rules" {
		t.Fatalf("unexpected global snapshot: %+v", snapshot.GlobalAGENTS)
	}
}

func TestLoaderLoadUsesParentDirectoryWhenProjectRootIsFile(t *testing.T) {
	projectRoot := t.TempDir()
	filePath := filepath.Join(projectRoot, "nested", "main.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(filePath), agentsFileName), []byte("wrong-scope"), 0o644); err != nil {
		t.Fatalf("write nested AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, agentsFileName), []byte("project-root"), 0o644); err != nil {
		t.Fatalf("write project root AGENTS.md: %v", err)
	}

	snapshot, err := NewLoader(filepath.Join(t.TempDir(), ".neocode")).Load(context.Background(), projectRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.ProjectAGENTS.Content != "project-root" {
		t.Fatalf("expected project root AGENTS.md, got %+v", snapshot.ProjectAGENTS)
	}
}

func TestLoaderLoadTruncatesLongDocument(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}
	large := strings.Repeat("规", documentRuneLimit+12)
	if err := os.WriteFile(filepath.Join(baseDir, agentsFileName), []byte(large), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	snapshot, err := NewLoader(baseDir).Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !snapshot.GlobalAGENTS.Truncated {
		t.Fatalf("expected truncated global document")
	}
	if runeCount(snapshot.GlobalAGENTS.Content) != documentRuneLimit {
		t.Fatalf("unexpected truncated length = %d", runeCount(snapshot.GlobalAGENTS.Content))
	}
}

func TestLoaderLoadReturnsEmptySnapshotWhenFilesAreMissing(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	snapshot, err := NewLoader(baseDir).Load(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.ProjectAGENTS != (Document{}) || snapshot.GlobalAGENTS != (Document{}) {
		t.Fatalf("expected empty snapshot, got %+v", snapshot)
	}
}

func TestLoaderLoadKeepsGlobalRulesWhenProjectRulesMissing(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	projectRoot := t.TempDir()
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, agentsFileName), []byte("global-only"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	snapshot, err := NewLoader(baseDir).Load(context.Background(), projectRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.ProjectAGENTS != (Document{}) {
		t.Fatalf("expected missing project rules, got %+v", snapshot.ProjectAGENTS)
	}
	if snapshot.GlobalAGENTS.Content != "global-only" {
		t.Fatalf("expected global-only rules, got %+v", snapshot.GlobalAGENTS)
	}
}

func TestLoaderLoadHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewLoader(filepath.Join(t.TempDir(), ".neocode")).Load(ctx, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestLoaderLoadUsesHomeFallbackWhenBaseDirEmpty(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	baseDir := filepath.Join(homeDir, defaultRulesDir)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, agentsFileName), []byte("global-home"), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	snapshot, err := NewLoader("").Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.GlobalAGENTS.Content != "global-home" {
		t.Fatalf("expected home fallback rules, got %+v", snapshot.GlobalAGENTS)
	}
}

func TestResolveBaseDirCleansExplicitBaseDir(t *testing.T) {
	got := resolveBaseDir(filepath.Join(t.TempDir(), "nested", "..", ".neocode"))
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute cleaned baseDir, got %q", got)
	}
	if filepath.Base(got) != defaultRulesDir {
		t.Fatalf("expected %q suffix, got %q", defaultRulesDir, got)
	}
}

func TestTruncateRunesWithZeroBudget(t *testing.T) {
	got, truncated := truncateRunes("规则", 0)
	if got != "" || !truncated {
		t.Fatalf("truncateRunes() = (%q, %v), want empty truncated result", got, truncated)
	}
}
