package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFrom(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	nested := filepath.Join(repoRoot, "pkg", "nested")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git marker: %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	resolved, err := ResolveFrom(repoRoot, "pkg/nested")
	if err != nil {
		t.Fatalf("ResolveFrom() error = %v", err)
	}
	if resolved.Workdir != nested {
		t.Fatalf("expected workdir %q, got %q", nested, resolved.Workdir)
	}
	if resolved.WorkspaceRoot != repoRoot {
		t.Fatalf("expected workspace root %q, got %q", repoRoot, resolved.WorkspaceRoot)
	}
}

func TestResolveFromFallsBackToTargetDirectoryOutsideGit(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "standalone")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	resolved, err := ResolveFrom(base, "standalone")
	if err != nil {
		t.Fatalf("ResolveFrom() error = %v", err)
	}
	if resolved.WorkspaceRoot != target {
		t.Fatalf("expected standalone workspace root %q, got %q", target, resolved.WorkspaceRoot)
	}
	if resolved.Workdir != target {
		t.Fatalf("expected workdir %q, got %q", target, resolved.Workdir)
	}
}

func TestResolveUsesCurrentDirectoryWhenBaseAndPathAreEmpty(t *testing.T) {
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	repoRoot := t.TempDir()
	nested := filepath.Join(repoRoot, "nested")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir git marker: %v", err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})

	resolved, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Workdir != nested {
		t.Fatalf("expected workdir %q, got %q", nested, resolved.Workdir)
	}
	if resolved.WorkspaceRoot != repoRoot {
		t.Fatalf("expected workspace root %q, got %q", repoRoot, resolved.WorkspaceRoot)
	}
}

func TestResolveRejectsInvalidTargets(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "note.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := ResolveFrom(base, "missing"); err == nil {
		t.Fatalf("expected missing path error")
	}
	if _, err := ResolveFrom(base, filePath); err == nil {
		t.Fatalf("expected file path error")
	}
}

func TestSameRoot(t *testing.T) {
	root := t.TempDir()
	if !SameRoot(root, filepath.Join(root, ".")) {
		t.Fatalf("expected same root match")
	}
	if SameRoot(root, filepath.Join(root, "child")) {
		t.Fatalf("expected different roots")
	}
	if SameRoot("", root) {
		t.Fatalf("expected empty root to never match")
	}
}
