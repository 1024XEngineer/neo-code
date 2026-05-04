package rules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobalRulePathUsesBaseDir(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	got := GlobalRulePath(baseDir)
	want := filepath.Join(baseDir, agentsFileName)
	if got != want {
		t.Fatalf("GlobalRulePath() = %q, want %q", got, want)
	}
}

func TestProjectRulePathUsesProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	got := ProjectRulePath(projectRoot)
	want := filepath.Join(projectRoot, agentsFileName)
	if got != want {
		t.Fatalf("ProjectRulePath() = %q, want %q", got, want)
	}
}

func TestProjectRulePathUsesFileParentDirectory(t *testing.T) {
	projectRoot := t.TempDir()
	filePath := filepath.Join(projectRoot, "nested", "main.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := ProjectRulePath(filePath)
	want := filepath.Join(filepath.Dir(filePath), agentsFileName)
	if got != want {
		t.Fatalf("ProjectRulePath(file) = %q, want %q", got, want)
	}
}

func TestProjectRulePathNormalizesRelativeRootToAbsolute(t *testing.T) {
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	relativeRoot := filepath.Join("relative-root-for-test", "nested")
	tempRoot := filepath.Join(workdir, relativeRoot)

	got := ProjectRulePath(relativeRoot)
	want := filepath.Join(tempRoot, agentsFileName)
	if got != want {
		t.Fatalf("ProjectRulePath(relative) = %q, want %q", got, want)
	}
}

func TestWriteGlobalRuleCreatesFileAndCanBeReadBack(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	path, err := WriteGlobalRule(context.Background(), baseDir, "默认使用中文输出")
	if err != nil {
		t.Fatalf("WriteGlobalRule() error = %v", err)
	}

	wantPath := filepath.Join(baseDir, agentsFileName)
	if path != wantPath {
		t.Fatalf("WriteGlobalRule() path = %q, want %q", path, wantPath)
	}

	document, err := ReadGlobalRule(context.Background(), baseDir)
	if err != nil {
		t.Fatalf("ReadGlobalRule() error = %v", err)
	}
	if document.Path != wantPath || document.Content != "默认使用中文输出" {
		t.Fatalf("unexpected global rule document: %+v", document)
	}
}

func TestWriteProjectRuleCreatesFileAndCanBeReadBack(t *testing.T) {
	projectRoot := t.TempDir()
	path, err := WriteProjectRule(context.Background(), projectRoot, "修改 runtime 必须补测试")
	if err != nil {
		t.Fatalf("WriteProjectRule() error = %v", err)
	}

	wantPath := filepath.Join(projectRoot, agentsFileName)
	if path != wantPath {
		t.Fatalf("WriteProjectRule() path = %q, want %q", path, wantPath)
	}

	document, err := ReadProjectRule(context.Background(), projectRoot)
	if err != nil {
		t.Fatalf("ReadProjectRule() error = %v", err)
	}
	if document.Path != wantPath || document.Content != "修改 runtime 必须补测试" {
		t.Fatalf("unexpected project rule document: %+v", document)
	}
}

func TestReadGlobalRuleHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ReadGlobalRule(ctx, filepath.Join(t.TempDir(), ".neocode"))
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestReadProjectRuleHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ReadProjectRule(ctx, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestWriteGlobalRuleHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := WriteGlobalRule(ctx, filepath.Join(t.TempDir(), ".neocode"), "ignored")
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestWriteProjectRuleHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := WriteProjectRule(ctx, t.TempDir(), "ignored")
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestWriteGlobalRuleRejectsInvalidUTF8(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	_, err := WriteGlobalRule(context.Background(), baseDir, string([]byte{0xff, 0xfe}))
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("expected invalid UTF-8 error, got %v", err)
	}
}

func TestReadGlobalRuleReturnsEmptyWhenMissing(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	document, err := ReadGlobalRule(context.Background(), baseDir)
	if err != nil {
		t.Fatalf("ReadGlobalRule() error = %v", err)
	}
	if document != (Document{}) {
		t.Fatalf("expected empty document, got %+v", document)
	}
}

func TestReadProjectRuleReturnsEmptyWhenMissing(t *testing.T) {
	document, err := ReadProjectRule(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("ReadProjectRule() error = %v", err)
	}
	if document != (Document{}) {
		t.Fatalf("expected empty document, got %+v", document)
	}
}

func TestReadProjectRuleReturnsReadError(t *testing.T) {
	projectRoot := t.TempDir()
	rulePath := filepath.Join(projectRoot, agentsFileName)
	if err := os.MkdirAll(rulePath, 0o755); err != nil {
		t.Fatalf("mkdir rule path: %v", err)
	}

	_, err := ReadProjectRule(context.Background(), projectRoot)
	if err == nil || !strings.Contains(err.Error(), "rules: read") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestReadGlobalRuleRejectsInvalidUTF8(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), ".neocode")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir baseDir: %v", err)
	}
	rulePath := filepath.Join(baseDir, agentsFileName)
	if err := os.WriteFile(rulePath, []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatalf("write invalid utf8 rule: %v", err)
	}

	_, err := ReadGlobalRule(context.Background(), baseDir)
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("expected invalid UTF-8 read error, got %v", err)
	}
}

func TestCommitWithRetryRetriesTransientRenameFailure(t *testing.T) {
	oldRenameFile := renameFile
	oldSleepFn := sleepFn
	t.Cleanup(func() {
		renameFile = oldRenameFile
		sleepFn = oldSleepFn
	})

	attempts := 0
	renameFile = func(_, _ string) error {
		attempts++
		if attempts < 3 {
			return errors.New("busy")
		}
		return nil
	}
	sleepFn = func(_ time.Duration) {}

	if err := commitWithRetry("temp", "target", false); err != nil {
		t.Fatalf("commitWithRetry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 rename attempts, got %d", attempts)
	}
}

func TestRenameWithReplaceRemovesExistingTargetWhenAllowed(t *testing.T) {
	oldRenameFile := renameFile
	oldRemoveFile := removeFile
	t.Cleanup(func() {
		renameFile = oldRenameFile
		removeFile = oldRemoveFile
	})

	attempts := 0
	removed := 0
	renameFile = func(_, _ string) error {
		attempts++
		if attempts == 1 {
			return errors.New("access denied")
		}
		return nil
	}
	removeFile = func(_ string) error {
		removed++
		return nil
	}

	if err := renameWithReplace("temp", "target", true); err != nil {
		t.Fatalf("renameWithReplace() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 rename attempts, got %d", attempts)
	}
	if removed != 1 {
		t.Fatalf("expected target removal before second rename, got %d", removed)
	}
}
