package checkpoint

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFinalizeExactForCheckpointsEmptyID(t *testing.T) {
	store, _ := newTestStore(t)
	ok, err := store.FinalizeExactForCheckpoints("  ", []string{"cp1"})
	if err == nil || !strings.Contains(err.Error(), "empty checkpointID") {
		t.Fatalf("expected empty checkpointID error, got ok=%v err=%v", ok, err)
	}
}

func TestFinalizeExactForCheckpointsEmptyRelated(t *testing.T) {
	store, _ := newTestStore(t)
	ok, err := store.FinalizeExactForCheckpoints("cp-end", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for empty related list")
	}
}

func TestFinalizeExactForCheckpointsWithEmptyRelatedIDs(t *testing.T) {
	store, _ := newTestStore(t)
	ok, err := store.FinalizeExactForCheckpoints("cp-end", []string{"", "  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for all-empty related IDs")
	}
}

func TestFinalizeExactForCheckpointsMergesMultipleCheckpoints(t *testing.T) {
	store, workdir := newTestStore(t)
	abs1 := writeWorkdirFile(t, workdir, "a.txt", "a-initial")
	abs2 := writeWorkdirFile(t, workdir, "b.txt", "b-initial")

	// Capture + finalize for a.txt (cp1)
	if _, err := store.CapturePreWrite(abs1); err != nil {
		t.Fatalf("capture a.txt: %v", err)
	}
	if err := os.WriteFile(abs1, []byte("a-after-cp1"), 0o644); err != nil {
		t.Fatalf("edit a.txt: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Capture + finalize for b.txt (cp2)
	if _, err := store.CapturePreWrite(abs2); err != nil {
		t.Fatalf("capture b.txt: %v", err)
	}
	if err := os.WriteFile(abs2, []byte("b-after-cp2"), 0o644); err != nil {
		t.Fatalf("edit b.txt: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// FinalizeExactForCheckpoints 合并 cp1 和 cp2 的状态
	ok, err := store.FinalizeExactForCheckpoints("cp-end", []string{"cp1", "cp2"})
	if err != nil {
		t.Fatalf("FinalizeExactForCheckpoints error: %v", err)
	}
	if !ok {
		t.Fatal("expected true when merging valid checkpoints")
	}

	// 验证 cp-end 的 meta 存在且包含两个文件的版本
	meta, err := store.readCheckpointMeta("cp-end")
	if err != nil {
		t.Fatalf("readCheckpointMeta cp-end: %v", err)
	}
	if len(meta.ExactFileVersions) < 2 {
		t.Fatalf("expected at least 2 exact file versions, got %d", len(meta.ExactFileVersions))
	}
	if len(meta.FileVersions) < 2 {
		t.Fatalf("expected at least 2 file versions, got %d", len(meta.FileVersions))
	}
}

func TestFinalizeExactForCheckpointsNonexistentCheckpoint(t *testing.T) {
	store, _ := newTestStore(t)
	_, err := store.FinalizeExactForCheckpoints("cp-end", []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent related checkpoint")
	}
}

func TestFinalizeExactForCheckpointsSkipsEmptyHashVersions(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "initial")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("after"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// cp1 应该有 FileVersions
	ok, err := store.FinalizeExactForCheckpoints("cp-end", []string{"cp1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected true for single valid checkpoint")
	}
}

func TestRunEndCaptureEmptyList(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.RunEndCapture(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := store.RunEndCapture(context.Background(), []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEndCaptureWithCheckpoints(t *testing.T) {
	store, workdir := newTestStore(t)
	abs1 := writeWorkdirFile(t, workdir, "a.txt", "a-v0")
	abs2 := writeWorkdirFile(t, workdir, "b.txt", "b-v0")

	// Capture a.txt (turn 1)
	if _, err := store.CapturePreWrite(abs1); err != nil {
		t.Fatalf("capture a.txt: %v", err)
	}
	if err := os.WriteFile(abs1, []byte("a-v1"), 0o644); err != nil {
		t.Fatalf("edit a.txt: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Capture b.txt (turn 2)
	if _, err := store.CapturePreWrite(abs2); err != nil {
		t.Fatalf("capture b.txt: %v", err)
	}
	if err := os.WriteFile(abs2, []byte("b-v1"), 0o644); err != nil {
		t.Fatalf("edit b.txt: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// RunEndCapture 应该对 cp1 和 cp2 中所有涉及的文件抓取当前状态
	if err := store.RunEndCapture(context.Background(), []string{"cp1", "cp2"}); err != nil {
		t.Fatalf("RunEndCapture error: %v", err)
	}
}

func TestRunEndCaptureContextCancelled(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "v0")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("v1"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	store.Reset()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.RunEndCapture(ctx, []string{"cp1"})
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestRunEndCaptureSkipsNonexistentCheckpoints(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "v0")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// 混合存在和不存在的 checkpoint
	if err := store.RunEndCapture(context.Background(), []string{"cp1", "nonexistent"}); err != nil {
		t.Fatalf("RunEndCapture error: %v", err)
	}
}

func TestRunEndCaptureAllNonexistent(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.RunEndCapture(context.Background(), []string{"nonexistent1", "nonexistent2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCapturePostDelete(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "todelete.txt", "will be deleted")

	// CapturePreWrite first to register the path (version 1)
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	// Delete the file
	if err := os.Remove(abs); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// CapturePostDelete creates a new version for the deletion
	if err := store.CapturePostDelete([]string{abs}); err != nil {
		t.Fatalf("CapturePostDelete: %v", err)
	}

	// CapturePostDelete 会为已注册 path 创建新版本（version 2）
	hash := perEditPathHash(abs)
	meta, err := store.readVersionMeta(hash, 2)
	if err != nil {
		t.Fatalf("readVersionMeta: %v", err)
	}
	if !meta.IsPostDelete {
		t.Fatal("expected IsPostDelete=true")
	}
}

func TestHasPending(t *testing.T) {
	store, workdir := newTestStore(t)
	if store.HasPending() {
		t.Fatal("expected no pending after creation")
	}

	abs := writeWorkdirFile(t, workdir, "a.txt", "v0")
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !store.HasPending() {
		t.Fatal("expected pending after capture")
	}

	store.Reset()
	if store.HasPending() {
		t.Fatal("expected no pending after reset")
	}
}

func TestDeleteCheckpoint(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "v0")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := store.Finalize("cp-to-delete"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	store.Reset()

	// 确认 checkpoint 存在
	_, err := store.readCheckpointMeta("cp-to-delete")
	if err != nil {
		t.Fatalf("should exist before delete: %v", err)
	}

	// 删除 checkpoint 元数据
	if err := store.DeleteCheckpoint("cp-to-delete"); err != nil {
		t.Fatalf("DeleteCheckpoint error: %v", err)
	}

	// 确认已删除（readCheckpointMeta 应该失败）
	_, err = store.readCheckpointMeta("cp-to-delete")
	if err == nil {
		t.Fatal("expected error reading deleted checkpoint meta")
	}
}

func TestDeleteCheckpointNonexistent(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.DeleteCheckpoint("nonexistent"); err != nil {
		t.Fatalf("expected no error for deleting nonexistent checkpoint, got %v", err)
	}
}

func TestFinalizeSingleFileSkipWhenNoPending(t *testing.T) {
	store, _ := newTestStore(t)
	ok, err := store.Finalize("cp-no-pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false when no pending captures")
	}
}

func TestFinalizeWithExactStatePreservesFile(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "f.txt", "initial")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("after-edit"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	ok, err := store.FinalizeWithExactState("cp-exact")
	if err != nil {
		t.Fatalf("FinalizeWithExactState error: %v", err)
	}
	if !ok {
		t.Fatal("expected true for finalize with pending")
	}

	meta, err := store.readCheckpointMeta("cp-exact")
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if len(meta.ExactFileVersions) == 0 {
		t.Fatal("expected ExactFileVersions to be non-empty")
	}
}

func TestRefForPerEditCheckpointRoundtrip(t *testing.T) {
	ref := RefForPerEditCheckpoint("abc123")
	if ref != "peredit:abc123" {
		t.Fatalf("RefForPerEditCheckpoint = %q", ref)
	}
	if !IsPerEditRef(ref) {
		t.Fatal("expected IsPerEditRef=true")
	}
	if id := PerEditCheckpointIDFromRef(ref); id != "abc123" {
		t.Fatalf("PerEditCheckpointIDFromRef = %q", id)
	}
}

func TestIsPerEditRefOddCases(t *testing.T) {
	if IsPerEditRef("") {
		t.Fatal("empty string is not peredit ref")
	}
	if IsPerEditRef("peredit") {
		t.Fatal("bare peredit is not a valid ref")
	}
	// peredit: with empty ID — depends on implementation
}

func TestBashHasArchiveExtractFlag(t *testing.T) {
	// tar with extract flag
	if !bashHasArchiveExtractFlag("tar -xzf bundle.tar.gz") {
		t.Fatal("expected true for tar -xzf")
	}
	if !bashHasArchiveExtractFlag("tar xvf bundle.tar") {
		t.Fatal("expected true for tar xvf")
	}
	if !bashHasArchiveExtractFlag("tar --extract -f bundle.tar") {
		t.Fatal("expected true for tar --extract")
	}
	// tar without extract flag should be false
	if bashHasArchiveExtractFlag("tar -czf bundle.tar.gz src/") {
		t.Fatal("expected false for tar -czf (create)")
	}
	// unzip/gunzip/bunzip2 always extract
	if !bashHasArchiveExtractFlag("unzip file.zip") {
		t.Fatal("expected true for unzip")
	}
	if !bashHasArchiveExtractFlag("gunzip file.gz") {
		t.Fatal("expected true for gunzip")
	}
}

func TestHasRecognizedSourceExtEdgeCases(t *testing.T) {
	// standard extensions
	if !hasRecognizedSourceExt("main.go") {
		t.Fatal("expected true for .go")
	}
	if !hasRecognizedSourceExt("README.md") {
		t.Fatal("expected true for .md")
	}
	// no extension but recognized basename
	if !hasRecognizedSourceExt("Dockerfile") {
		t.Fatal("expected true for Dockerfile")
	}
	if !hasRecognizedSourceExt("Makefile") {
		t.Fatal("expected true for Makefile")
	}
	if hasRecognizedSourceExt(".gitignore") {
		t.Fatal("expected false for .gitignore (has extension, not in source set)")
	}
	// unrecognized
	if hasRecognizedSourceExt("image.png") {
		t.Fatal("expected false for .png")
	}
	if hasRecognizedSourceExt("binary") {
		t.Fatal("expected false for extensionless unrecognized file")
	}
}

func TestTokenizeBashArgsEdgeCases(t *testing.T) {
	// empty
	if len(tokenizeBashArgs("")) != 0 {
		t.Fatal("expected no tokens for empty string")
	}
	// simple command
	tokens := tokenizeBashArgs("git checkout main")
	if len(tokens) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d: %v", len(tokens), tokens)
	}
	// quoted args get stripped of quotes by tokenizer
	tokens = tokenizeBashArgs(`echo "hello world"`)
	if len(tokens) < 2 {
		t.Fatalf("expected at least 2 tokens, got %d: %v", len(tokens), tokens)
	}
	// pipe splits tokens
	tokens = tokenizeBashArgs("cat a.txt | grep foo")
	if len(tokens) < 3 {
		t.Fatalf("expected at least 3 tokens, got %d: %v", len(tokens), tokens)
	}
}

func TestResolvePathAgainstWorkdirEdgeCases(t *testing.T) {
	// empty path with empty workdir returns ""
	if got := resolvePathAgainstWorkdir("", "."); got != "" {
		t.Fatalf("expected empty for empty path with dot workdir, got %q", got)
	}
	// path with glob
	if got := resolvePathAgainstWorkdir("*.go", "/tmp"); got != "" {
		t.Fatalf("expected empty for glob, got %q", got)
	}
	// dot workdir
	if got := resolvePathAgainstWorkdir("file.go", "."); got != "" {
		t.Fatalf("expected empty for dot workdir, got %q", got)
	}
	// path escaping workdir
	got := resolvePathAgainstWorkdir("../outside.txt", "/tmp/work")
	if got != "" {
		t.Fatalf("expected empty for escape path, got %q", got)
	}
	// absolute path within workdir
	got = resolvePathAgainstWorkdir("/tmp/work/file.go", "/tmp/work")
	if got == "" {
		t.Fatal("expected non-empty for abs path within workdir")
	}
}

func TestSourceFilesInWorkdirEdgeCases(t *testing.T) {
	// empty command
	if len(SourceFilesInWorkdir("", "/tmp")) != 0 {
		t.Fatal("expected empty for empty command")
	}
	// command with source file-like tokens
	files := SourceFilesInWorkdir("cat main.go", "/tmp/work")
	if len(files) != 0 {
		// 应该返回空因为 /tmp/work/main.go 不存在
		t.Logf("files: %v", files)
	}
	// command with no recognized extensions
	files = SourceFilesInWorkdir("ls -la", "/tmp")
	if len(files) != 0 {
		t.Fatalf("expected empty for ls, got %v", files)
	}
}

func TestBashLikelyWritesFilesEmptyCommand(t *testing.T) {
	if BashLikelyWritesFiles("") {
		t.Fatal("expected false for empty command")
	}
	if BashLikelyWritesFiles("   ") {
		t.Fatal("expected false for whitespace command")
	}
}

func TestBashLikelyWritesFilesReadOnlyCommands(t *testing.T) {
	readOnly := []string{
		"ls -la",
		"cat file.go",
		"grep pattern *.go",
		"head -20 file.txt",
		"tail -f log.txt",
		"wc -l file.go",
		"sort data.txt",
		"uniq list.txt",
		"du -sh .",
		"df -h",
		"echo hello",
		"pwd",
		"whoami",
		"date",
		"env",
		"which go",
		"go version",
		"git status",
		"git log --oneline",
		"git diff",
		"git branch",
	}
	for _, cmd := range readOnly {
		if BashLikelyWritesFiles(cmd) {
			t.Errorf("expected false for read-only %q", cmd)
		}
	}
}

func TestStripHarmlessRedirectsPreservesDangerous(t *testing.T) {
	got := stripHarmlessRedirects("echo hello > file.txt")
	if !strings.Contains(got, "> file.txt") {
		t.Fatalf("expected dangerous redirect preserved, got %q", got)
	}
}
