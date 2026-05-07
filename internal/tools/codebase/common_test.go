package codebase

import (
	"os"
	"path/filepath"
	"testing"

	"neo-code/internal/tools"
)

func TestCodebaseCommonHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	child := filepath.Join(root, "subdir")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root) error = %v", err)
	}

	if got, err := tools.ResolveEffectiveRoot(root, " "); err != nil || got != canonicalRoot {
		t.Fatalf("effectiveRoot(default) = %q", got)
	}
	if got, err := tools.ResolveEffectiveRoot(root, "subdir"); err != nil || got != child {
		t.Fatalf("effectiveRoot(custom) = %q", got)
	}
	if got := itoa(0); got != "0" {
		t.Fatalf("itoa(0) = %q", got)
	}
	if got := itoa(-9); got != "-9" {
		t.Fatalf("itoa(-9) = %q", got)
	}
	if got := boolToString(true); got != "true" {
		t.Fatalf("boolToString(true) = %q", got)
	}
	if got := boolToString(false); got != "false" {
		t.Fatalf("boolToString(false) = %q", got)
	}
	if _, err := tools.ResolveEffectiveRoot(root, "../escape"); err == nil {
		t.Fatal("ResolveEffectiveRoot should reject escaping workdir")
	}
}
