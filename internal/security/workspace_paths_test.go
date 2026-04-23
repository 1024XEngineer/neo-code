package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspacePathResolvesInsideWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	resolvedRoot, resolvedTarget, err := ResolveWorkspacePath(root, "pkg")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath() error = %v", err)
	}
	if !samePathKey(resolvedRoot, root) {
		t.Fatalf("expected resolved root inside workspace, got %q", resolvedRoot)
	}
	if !samePathKey(resolvedTarget, targetDir) {
		t.Fatalf("expected resolved target %q, got %q", targetDir, resolvedTarget)
	}
}

func TestResolveWorkspacePathFromRootMatchesWorkspaceValidation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	resolvedRoot, _, err := ResolveWorkspacePath(root, ".")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath(root, dot) error = %v", err)
	}
	resolvedTarget, err := ResolveWorkspacePathFromRoot(resolvedRoot, "pkg")
	if err != nil {
		t.Fatalf("ResolveWorkspacePathFromRoot() error = %v", err)
	}
	if !samePathKey(resolvedTarget, targetDir) {
		t.Fatalf("expected resolved target %q, got %q", targetDir, resolvedTarget)
	}
}

func TestResolveWorkspacePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, _, err := ResolveWorkspacePath(root, "..\\outside.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
}

func TestResolveWorkspacePathRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	linkDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	linkPath := filepath.Join(linkDir, "secret.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if _, _, err := ResolveWorkspacePath(root, "pkg/secret.txt"); err == nil {
		t.Fatalf("expected symlink escape to be rejected")
	}
}

func TestResolveWorkspacePathRejectsEmptyRoot(t *testing.T) {
	t.Parallel()

	if _, _, err := ResolveWorkspacePath("   ", "a.txt"); err == nil {
		t.Fatalf("expected empty root to be rejected")
	}
}

func TestResolveWorkspacePathRejectsAbsoluteTargetOutsideWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if _, _, err := ResolveWorkspacePath(root, outside); err == nil {
		t.Fatalf("expected absolute outside path to be rejected")
	}
}

func TestResolveWorkspacePathRejectsRootThatIsNotDirectory(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	rootFile := filepath.Join(rootDir, "root.txt")
	if err := os.WriteFile(rootFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, _, err := ResolveWorkspacePath(rootFile, "a.txt"); err == nil {
		t.Fatalf("expected non-directory root to be rejected")
	}
}

func TestResolveWorkspacePathRejectsInvalidPathInput(t *testing.T) {
	t.Parallel()

	if _, _, err := ResolveWorkspacePath(string([]byte{0}), "a.txt"); err == nil {
		t.Fatalf("expected invalid root path to be rejected")
	}

	root := t.TempDir()
	if _, _, err := ResolveWorkspacePath(root, string([]byte{0})); err == nil {
		t.Fatalf("expected invalid target path to be rejected")
	}
}
