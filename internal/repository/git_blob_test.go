package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadGitBlobHEAD(t *testing.T) {
	// 使用项目自身的 git repo 验证 blob 读取
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	// 读取 go.mod 的 HEAD 版本
	blobSpec := "HEAD:go.mod"
	data, err := readGitBlob(context.Background(), workdir, blobSpec)
	if err != nil {
		t.Fatalf("readGitBlob(%q) error: %v", blobSpec, err)
	}
	if len(data) == 0 {
		t.Fatal("blob content should not be empty")
	}
	if !strings.Contains(string(data), "module neo-code") {
		t.Fatalf("expected module declaration in go.mod blob, got %q", string(data)[:100])
	}
}

func TestReadGitBlobNotFound(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	_, err = readGitBlob(context.Background(), workdir, "HEAD:nonexistent_file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent blob")
	}
}

func TestReadGitBlobCancelledContext(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = readGitBlob(ctx, workdir, "HEAD:go.mod")
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestStatGitBlobHEAD(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	size, err := statGitBlob(context.Background(), workdir, "HEAD:go.mod")
	if err != nil {
		t.Fatalf("statGitBlob error: %v", err)
	}
	if size <= 0 {
		t.Fatalf("expected positive blob size, got %d", size)
	}
}

func TestStatGitBlobNotFound(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	_, err = statGitBlob(context.Background(), workdir, "HEAD:nonexistent_file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent blob")
	}
}

func TestStatGitBlobCancelledContext(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = statGitBlob(ctx, workdir, "HEAD:go.mod")
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestStatGitBlobInvalidOutput(t *testing.T) {
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}

	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	// A SHA ref like HEAD returns non-numeric output from git cat-file -s,
	// causing parse error.
	// Use a non-existent branch reference to trigger git error.
	_, err = statGitBlob(context.Background(), workdir, "refs/heads/nonexistent:go.mod")
	if err == nil {
		t.Fatal("expected error for invalid reference")
	}
}

func TestBuildGitHeadBlobSpec(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"go.mod", "HEAD:go.mod"},
		{"./go.mod", "HEAD:go.mod"},
		{"internal/tools/bash/tool.go", "HEAD:internal/tools/bash/tool.go"},
		{"  go.mod  ", "HEAD:go.mod"},
		{"", "HEAD:"},
	}
	for _, tt := range tests {
		got := buildGitHeadBlobSpec(tt.path)
		if got != tt.want {
			t.Errorf("buildGitHeadBlobSpec(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestBuildGitHeadBlobSpecWindowsBackslash(t *testing.T) {
	got := buildGitHeadBlobSpec("internal\\tools\\bash\\tool.go")
	if got != "HEAD:internal/tools/bash/tool.go" {
		t.Fatalf("expected forward slashes, got %q", got)
	}
}

func TestResolveGitDiffPaths(t *testing.T) {
	tests := []struct {
		status       ChangedFileStatus
		path         string
		oldPath      string
		wantOriginal string
		wantModified string
	}{
		{StatusModified, "file.go", "", "file.go", "file.go"},
		{StatusAdded, "file.go", "", "", "file.go"},
		{StatusDeleted, "file.go", "", "file.go", ""},
		{StatusRenamed, "new.go", "old.go", "old.go", "new.go"},
		{StatusCopied, "new.go", "old.go", "old.go", "new.go"},
		{StatusUntracked, "file.go", "", "", "file.go"},
		{StatusConflicted, "file.go", "", "file.go", "file.go"},
		{ChangedFileStatus("unknown"), "file.go", "", "file.go", "file.go"},
	}
	for _, tt := range tests {
		entry := gitChangedEntry{Status: tt.status, Path: tt.path, OldPath: tt.oldPath}
		orig, mod := resolveGitDiffPaths(entry)
		if orig != tt.wantOriginal || mod != tt.wantModified {
			t.Errorf("resolveGitDiffPaths(%s,%q,%q) = (%q,%q), want (%q,%q)",
				tt.status, tt.path, tt.oldPath, orig, mod, tt.wantOriginal, tt.wantModified)
		}
	}
}

func TestFindGitDiffEntry(t *testing.T) {
	normalized := func(p string) string {
		return filepathSlashClean(p)
	}
	entries := []gitChangedEntry{
		{Path: "pkg/a.go"},
		{Path: "pkg/b.go"},
	}
	_, ok := findGitDiffEntry(entries, "pkg/c.go")
	if ok {
		t.Fatal("expected not found for missing entry")
	}
	searchPath := normalized("pkg/a.go")
	entry, ok := findGitDiffEntry(entries, searchPath)
	if !ok {
		t.Fatalf("expected found for existing entry (searched with %q)", searchPath)
	}
	if entry.Path != "pkg/a.go" {
		t.Fatalf("path = %q", entry.Path)
	}
}

func isGitAvailable(t *testing.T) bool {
	t.Helper()
	_, err := exec.LookPath("git")
	return err == nil
}

// ensureInGitRepo 验证当前目录是 git 仓库，否则跳过测试。
func ensureInGitRepo(t *testing.T) string {
	t.Helper()
	gitAvailable := isGitAvailable(t)
	if !gitAvailable {
		t.Skip("git not available")
	}
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		t.Skip("not in a git repo")
	}
	return workdir
}
