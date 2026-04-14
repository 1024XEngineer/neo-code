package compact

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestTranscriptStoreSaveSanitizesSessionIDAndWritesJSONL(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	store := transcriptStore{
		now:         func() time.Time { return time.Unix(1712052000, 123456789) },
		randomToken: func() (string, error) { return "token1234", nil },
		userHomeDir: func() (string, error) { return home, nil },
		mkdirAll:    os.MkdirAll,
		writeFile:   os.WriteFile,
		rename:      os.Rename,
		remove:      os.Remove,
	}

	id, path, err := store.Save([]providertypes.Message{
		{Role: providertypes.RoleUser, Content: "hello"},
	}, "session with spaces", filepath.Join(home, "workspace"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if id == "" || path == "" {
		t.Fatalf("expected transcript metadata, got id=%q path=%q", id, path)
	}
	if filepath.Ext(path) != transcriptFileExtension {
		t.Fatalf("expected transcript extension %q, got %q", transcriptFileExtension, path)
	}
	if !strings.Contains(filepath.Base(path), "session_with_spaces") {
		t.Fatalf("expected sanitized session id in path, got %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected transcript content")
	}
}

func TestTranscriptFileModeForOS(t *testing.T) {
	t.Parallel()

	if got := transcriptFileModeForOS("windows"); got != 0o644 {
		t.Fatalf("expected windows mode 0644, got %#o", got)
	}
	if got := transcriptFileModeForOS("linux"); got != 0o600 {
		t.Fatalf("expected non-windows mode 0600, got %#o", got)
	}
}

func TestTranscriptStoreSaveReturnsHomeDirectoryError(t *testing.T) {
	t.Parallel()

	store := transcriptStore{
		userHomeDir: func() (string, error) { return "", errors.New("home boom") },
	}

	_, _, err := store.Save(nil, "session", "workspace")
	if err == nil || !strings.Contains(err.Error(), "home boom") {
		t.Fatalf("expected user home error, got %v", err)
	}
}

func TestTranscriptStoreSaveReturnsRandomTokenError(t *testing.T) {
	t.Parallel()

	store := transcriptStore{
		now:         func() time.Time { return time.Unix(1, 0) },
		userHomeDir: func() (string, error) { return t.TempDir(), nil },
		mkdirAll:    func(path string, perm os.FileMode) error { return nil },
		randomToken: func() (string, error) { return "", errors.New("token boom") },
	}

	_, _, err := store.Save(nil, "session", "workspace")
	if err == nil || !strings.Contains(err.Error(), "token boom") {
		t.Fatalf("expected token generation error, got %v", err)
	}
}

func TestTranscriptStoreSaveRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	written := ""
	removed := ""
	store := transcriptStore{
		now:         func() time.Time { return time.Unix(1, 0) },
		userHomeDir: func() (string, error) { return home, nil },
		mkdirAll:    func(path string, perm os.FileMode) error { return nil },
		randomToken: func() (string, error) { return "token1234", nil },
		writeFile: func(name string, data []byte, perm os.FileMode) error {
			written = name
			return nil
		},
		rename: func(oldPath, newPath string) error {
			return errors.New("rename boom")
		},
		remove: func(path string) error {
			removed = path
			return nil
		},
	}

	_, _, err := store.Save([]providertypes.Message{{Role: providertypes.RoleUser, Content: "hello"}}, "session", filepath.Join(home, "workspace"))
	if err == nil || !strings.Contains(err.Error(), "rename boom") {
		t.Fatalf("expected rename error, got %v", err)
	}
	if written == "" || removed != written {
		t.Fatalf("expected temp transcript cleanup, wrote %q removed %q", written, removed)
	}
}

func TestTranscriptStoreCleanupRemovesOldestFiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workdir := filepath.Join(home, "workspace")
	dir := transcriptDirectory(home, hashProject(workdir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 创建 5 个 transcript 文件（名字内嵌时间戳，字典序递增）
	names := []string{
		"transcript_1000_aaaa_s1.jsonl",
		"transcript_2000_bbbb_s1.jsonl",
		"transcript_3000_cccc_s1.jsonl",
		"transcript_4000_dddd_s1.jsonl",
		"transcript_5000_eeee_s1.jsonl",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	var removed []string
	store := transcriptStore{
		userHomeDir: func() (string, error) { return home, nil },
		readDir:     os.ReadDir,
		remove: func(path string) error {
			removed = append(removed, filepath.Base(path))
			return nil
		},
	}

	if err := store.Cleanup(workdir, 3); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if len(removed) != 2 {
		t.Fatalf("expected 2 files removed, got %d: %v", len(removed), removed)
	}
	if removed[0] != names[0] || removed[1] != names[1] {
		t.Fatalf("expected oldest files removed, got %v", removed)
	}
}

func TestTranscriptStoreCleanupNoopWhenUnderLimit(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workdir := filepath.Join(home, "workspace")
	dir := transcriptDirectory(home, hashProject(workdir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "transcript_1000_aaaa_s1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	removed := false
	store := transcriptStore{
		userHomeDir: func() (string, error) { return home, nil },
		readDir:     os.ReadDir,
		remove: func(path string) error {
			removed = true
			return nil
		},
	}

	if err := store.Cleanup(workdir, 10); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if removed {
		t.Fatalf("expected no files removed when under limit")
	}
}

func TestTranscriptStoreCleanupHandlesEmptyDirectory(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workdir := filepath.Join(home, "workspace")
	dir := transcriptDirectory(home, hashProject(workdir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := transcriptStore{
		userHomeDir: func() (string, error) { return home, nil },
		readDir:     os.ReadDir,
		remove: func(path string) error {
			t.Fatalf("unexpected remove call: %s", path)
			return nil
		},
	}

	if err := store.Cleanup(workdir, 3); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestTranscriptStoreCleanupHandlesMissingDirectory(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workdir := filepath.Join(home, "workspace")

	store := transcriptStore{
		userHomeDir: func() (string, error) { return home, nil },
		readDir:     os.ReadDir,
		remove: func(path string) error {
			t.Fatalf("unexpected remove call: %s", path)
			return nil
		},
	}

	if err := store.Cleanup(workdir, 3); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
}

func TestTranscriptStoreCleanupIgnoresNonTranscriptFiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	workdir := filepath.Join(home, "workspace")
	dir := transcriptDirectory(home, hashProject(workdir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 放一个非 transcript 文件
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	removed := false
	store := transcriptStore{
		userHomeDir: func() (string, error) { return home, nil },
		readDir:     os.ReadDir,
		remove: func(path string) error {
			removed = true
			return nil
		},
	}

	if err := store.Cleanup(workdir, 0); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if removed {
		t.Fatalf("expected non-transcript files to be ignored")
	}
}
