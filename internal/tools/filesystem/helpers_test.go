package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestToRelativePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "file.txt")
	outside := filepath.Join(filepath.Dir(root), "outside.txt")

	if got := toRelativePath(root, inside); got != filepath.Join("nested", "file.txt") {
		t.Fatalf("inside path = %q, want nested/file.txt", got)
	}
	if got := toRelativePath(root, outside); got != filepath.Join("..", "outside.txt") {
		t.Fatalf("outside path = %q, want ../outside.txt", got)
	}
}

func TestSkipDirEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustCreateDir(t, filepath.Join(root, ".git"))
	mustCreateDir(t, filepath.Join(root, "node_modules"))
	mustCreateDir(t, filepath.Join(root, "keep"))
	mustWriteTestFile(t, filepath.Join(root, ".vscode"), "not-a-dir")

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}

	got := map[string]bool{}
	for _, entry := range entries {
		got[entry.Name()] = skipDirEntry(filepath.Join(root, entry.Name()), entry)
	}

	if !got[".git"] {
		t.Fatalf(".git skip = false, want true")
	}
	if !got["node_modules"] {
		t.Fatalf("node_modules skip = false, want true")
	}
	if got["keep"] {
		t.Fatalf("keep skip = true, want false")
	}
	if got[".vscode"] {
		t.Fatalf(".vscode file skip = true, want false for non-directory")
	}
}

func TestIsCrossDeviceLinkError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "other", err: errors.New("permission denied"), want: false},
		{name: "cross-device", err: errors.New("invalid cross-device link"), want: true},
		{name: "exdev", err: errors.New("rename failed: EXDEV"), want: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := isCrossDeviceLinkError(tc.err); got != tc.want {
				t.Fatalf("isCrossDeviceLinkError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func mustCreateDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}

func mustWriteTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
