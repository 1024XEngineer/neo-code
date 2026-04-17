package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTaskStateEstablishedAndTruncateHelpers(t *testing.T) {
	t.Parallel()

	if (TaskState{}).Established() {
		t.Fatalf("empty task state should not be established")
	}
	if !(TaskState{Goal: "ship"}).Established() {
		t.Fatalf("goal should mark task state as established")
	}
	if !(TaskState{Progress: []string{"done"}}).Established() {
		t.Fatalf("progress should mark task state as established")
	}
	if truncateRunes("abc", 0) != "" {
		t.Fatalf("truncateRunes with zero limit should return empty string")
	}
	if truncateRunes("abc", 3) != "abc" {
		t.Fatalf("truncateRunes should keep exact-length string")
	}
}

func TestWorkspaceHelpersAndPathKey(t *testing.T) {
	t.Parallel()

	if NormalizeWorkspaceRoot("   ") != "" {
		t.Fatalf("blank workspace root should normalize to empty")
	}
	dir := t.TempDir()
	key := WorkspacePathKey(dir)
	if key == "" {
		t.Fatalf("workspace path key should not be empty")
	}
	if runtime.GOOS == "windows" && key != strings.ToLower(key) {
		t.Fatalf("windows workspace path key should be lower-cased, got %q", key)
	}
	if got := EffectiveWorkdir(" session ", "default"); got != "session" {
		t.Fatalf("EffectiveWorkdir should prefer session workdir, got %q", got)
	}
}

func TestStorageHelpers(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	inside := filepath.Join(baseDir, "sub", "file.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := ensurePathWithinBase(baseDir, inside); err != nil {
		t.Fatalf("ensurePathWithinBase() inside error = %v", err)
	}
	if err := ensurePathWithinBase(baseDir, filepath.Dir(baseDir)); err == nil {
		t.Fatalf("expected path escape error")
	}

	if _, _, err := createTempFile(filepath.Join(baseDir, "missing"), "tmp-*", "temp"); err == nil {
		t.Fatalf("expected createTempFile() error for missing dir")
	}

	tempFile, tempPath, err := createTempFile(baseDir, "tmp-*", "temp")
	if err != nil {
		t.Fatalf("createTempFile() error = %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("tempFile.Close() error = %v", err)
	}
	target := filepath.Join(baseDir, "target.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := replaceFileWithTemp(tempPath, target, "target"); err != nil {
		t.Fatalf("replaceFileWithTemp() error = %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected replaced target to exist, got %v", err)
	}
	if err := replaceFileWithTemp(filepath.Join(baseDir, "missing.tmp"), filepath.Join(baseDir, "missing.txt"), "missing"); err == nil {
		t.Fatalf("expected replaceFileWithTemp() error for missing temp file")
	}
}
