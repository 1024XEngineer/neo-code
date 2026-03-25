package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectMemoryServiceLoadsConfiguredFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("Use go test ./... before PR."), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".neocode"), 0o755); err != nil {
		t.Fatalf("mkdir .neocode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".neocode", "memory.md"), []byte("Prefer Chinese explanations for teammates."), 0o644); err != nil {
		t.Fatalf("write .neocode/memory.md: %v", err)
	}

	svc := NewProjectMemoryService(workspace, []string{"AGENTS.md", ".neocode/memory.md", "missing.md"}, 2400)

	sources, err := svc.ListSources(context.Background())
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 project memory sources, got %+v", sources)
	}

	ctxText, err := svc.BuildContext(context.Background())
	if err != nil {
		t.Fatalf("build context: %v", err)
	}
	if !strings.Contains(ctxText, "AGENTS.md") || !strings.Contains(ctxText, ".neocode/memory.md") {
		t.Fatalf("expected project memory paths in context, got %q", ctxText)
	}
	if !strings.Contains(ctxText, "Use go test ./... before PR.") {
		t.Fatalf("expected project memory content in context, got %q", ctxText)
	}
}

func TestProjectMemoryServiceIgnoresPathsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	svc := NewProjectMemoryService(workspace, []string{outside}, 2400)
	sources, err := svc.ListSources(context.Background())
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected outside path to be ignored, got %+v", sources)
	}
}
