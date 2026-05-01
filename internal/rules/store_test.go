package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
