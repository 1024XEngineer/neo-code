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
	if err := os.WriteFile(filepath.Join(workspace, "CLAUDE.md"), []byte("Prefer concise review summaries."), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	svc := NewProjectMemoryService(workspace, []string{"AGENTS.md", "CLAUDE.md", "missing.md"}, 2400)

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
	if !strings.Contains(ctxText, "AGENTS.md") || !strings.Contains(ctxText, "CLAUDE.md") {
		t.Fatalf("expected project memory paths in context, got %q", ctxText)
	}
	if !strings.Contains(ctxText, "Use go test ./... before PR.") {
		t.Fatalf("expected project memory content in context, got %q", ctxText)
	}
	if !strings.Contains(ctxText, "precedence order") {
		t.Fatalf("expected precedence guidance in context, got %q", ctxText)
	}
	agentsIdx := strings.Index(ctxText, "AGENTS.md")
	claudeIdx := strings.Index(ctxText, "CLAUDE.md")
	if agentsIdx == -1 || claudeIdx == -1 || agentsIdx > claudeIdx {
		t.Fatalf("expected configured file order to be preserved, got %q", ctxText)
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
