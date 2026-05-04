package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore returns a PerEditSnapshotStore rooted at t.TempDir() and a workdir under it.
func newTestStore(t *testing.T) (*PerEditSnapshotStore, string) {
	t.Helper()
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	workdir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	return NewPerEditSnapshotStore(projectDir, workdir), workdir
}

func writeWorkdirFile(t *testing.T, workdir, rel, content string) string {
	t.Helper()
	abs := filepath.Join(workdir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return abs
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestCapturePreWrite_AssignsMonotonicVersions: same path captured across turns gets v1, v2, v3...
func TestCapturePreWrite_AssignsMonotonicVersions(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "v0")

	for i := 1; i <= 3; i++ {
		v, err := store.CapturePreWrite(abs)
		if err != nil {
			t.Fatalf("capture %d: %v", i, err)
		}
		if v != i {
			t.Fatalf("capture %d: want version %d, got %d", i, i, v)
		}
		store.Reset()
	}
}

// TestCapturePreWrite_DedupesWithinTurn: same path within one turn returns first version every time.
func TestCapturePreWrite_DedupesWithinTurn(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "hello")

	v1, err := store.CapturePreWrite(abs)
	if err != nil || v1 != 1 {
		t.Fatalf("first capture: v=%d err=%v", v1, err)
	}
	v2, err := store.CapturePreWrite(abs)
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if v2 != v1 {
		t.Fatalf("dedupe failed: v1=%d v2=%d", v1, v2)
	}
	v3, err := store.CapturePreWrite(abs)
	if err != nil {
		t.Fatalf("third capture: %v", err)
	}
	if v3 != v1 {
		t.Fatalf("dedupe failed: v1=%d v3=%d", v1, v3)
	}
}

// TestCapturePreWrite_NewFileMarksExistedFalse: capturing a non-existent path stores Existed=false.
func TestCapturePreWrite_NewFileMarksExistedFalse(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := filepath.Join(workdir, "ghost.txt")

	v, err := store.CapturePreWrite(abs)
	if err != nil {
		t.Fatalf("capture missing file: %v", err)
	}

	hash := perEditPathHash(abs)
	meta, err := store.readVersionMeta(hash, v)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if meta.Existed {
		t.Fatalf("Existed should be false for missing file")
	}
	bin, err := store.readVersionBin(hash, v)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if len(bin) != 0 {
		t.Fatalf("bin should be empty, got %d bytes", len(bin))
	}
}

// TestRestore_UsesNextVersionAsTargetState: capture v1, modify, finalize cp1; capture v2, modify;
// Restore(cp1) should put v2.bin (== state right after v1's edit) on disk.
func TestRestore_UsesNextVersionAsTargetState(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "STATE_INITIAL")

	// Turn 1: capture preX, simulate tool edit to STATE_AFTER_TURN_1, finalize cp1.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("turn1 capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("STATE_AFTER_TURN_1"), 0o644); err != nil {
		t.Fatalf("turn1 edit: %v", err)
	}
	if written, err := store.Finalize("cp1"); err != nil || !written {
		t.Fatalf("turn1 finalize: written=%v err=%v", written, err)
	}
	store.Reset()

	// Turn 2: capture (current=STATE_AFTER_TURN_1), simulate edit to STATE_AFTER_TURN_2, finalize cp2.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("turn2 capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("STATE_AFTER_TURN_2"), 0o644); err != nil {
		t.Fatalf("turn2 edit: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("turn2 finalize: %v", err)
	}
	store.Reset()

	// Workdir is now STATE_AFTER_TURN_2.
	if got := mustReadFile(t, abs); got != "STATE_AFTER_TURN_2" {
		t.Fatalf("pre-restore: %q", got)
	}

	// Restore cp1: should write STATE_AFTER_TURN_1 (== v2.bin == content captured at start of turn 2).
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if got := mustReadFile(t, abs); got != "STATE_AFTER_TURN_1" {
		t.Fatalf("after restore cp1 want %q got %q", "STATE_AFTER_TURN_1", got)
	}
}

// TestRestore_NoNextVersionIsNoOp: restoring the latest checkpoint doesn't change workdir.
func TestRestore_NoNextVersionIsNoOp(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "BEFORE")

	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("AFTER"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	store.Reset()

	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := mustReadFile(t, abs); got != "AFTER" {
		t.Fatalf("workdir after restore should be unchanged AFTER, got %q", got)
	}
}

// TestRestore_PreservesUntrackedFiles: files not in cp.FileVersions stay untouched.
func TestRestore_PreservesUntrackedFiles(t *testing.T) {
	store, workdir := newTestStore(t)
	tracked := writeWorkdirFile(t, workdir, "tracked.txt", "TR_INITIAL")
	untracked := writeWorkdirFile(t, workdir, "untracked.txt", "UN_INITIAL")

	// Turn 1: only touch tracked.
	if _, err := store.CapturePreWrite(tracked); err != nil {
		t.Fatalf("capture tracked: %v", err)
	}
	if err := os.WriteFile(tracked, []byte("TR_AFTER_T1"), 0o644); err != nil {
		t.Fatalf("edit tracked: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	store.Reset()

	// Turn 2: edit tracked again so cp1 has a usable v_next.
	if _, err := store.CapturePreWrite(tracked); err != nil {
		t.Fatalf("capture tracked t2: %v", err)
	}
	if err := os.WriteFile(tracked, []byte("TR_AFTER_T2"), 0o644); err != nil {
		t.Fatalf("edit tracked t2: %v", err)
	}
	// External (non-agent) edit to untracked file at any time; should NOT be reverted.
	if err := os.WriteFile(untracked, []byte("UN_EXTERNAL"), 0o644); err != nil {
		t.Fatalf("edit untracked: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if got := mustReadFile(t, tracked); got != "TR_AFTER_T1" {
		t.Fatalf("tracked after restore want TR_AFTER_T1 got %q", got)
	}
	if got := mustReadFile(t, untracked); got != "UN_EXTERNAL" {
		t.Fatalf("untracked must stay UN_EXTERNAL, got %q", got)
	}
}

// TestDiff_EndToEnd_SameLineMultipleEdits: a→b→a→b→a sequence; Diff(first, last) is empty.
func TestDiff_EndToEnd_SameLineMultipleEdits(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "f.txt", "X\n")

	transitions := []string{"A\n", "B\n", "A\n", "B\n", "A\n"}
	for i, target := range transitions {
		if _, err := store.CapturePreWrite(abs); err != nil {
			t.Fatalf("capture turn %d: %v", i+1, err)
		}
		if err := os.WriteFile(abs, []byte(target), 0o644); err != nil {
			t.Fatalf("edit turn %d: %v", i+1, err)
		}
		cpID := "cp" + string(rune('0'+i+1))
		if _, err := store.Finalize(cpID); err != nil {
			t.Fatalf("finalize %s: %v", cpID, err)
		}
		store.Reset()
	}

	// State at cp1 (== content right after turn 1) should be "A".
	// State at cp5 (== current workdir, since v5 has no v_next) should also be "A".
	patch, err := store.Diff(context.Background(), "cp1", "cp5")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if strings.TrimSpace(patch) != "" {
		t.Fatalf("expected empty diff for endpoints both 'A', got:\n%s", patch)
	}
}

// TestDiff_NoNextVersionFallsBackToWorkdir: latest checkpoint uses current workdir for its content.
func TestDiff_NoNextVersionFallsBackToWorkdir(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "f.txt", "X")

	// Turn 1: X → A
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t1: %v", err)
	}
	if err := os.WriteFile(abs, []byte("A"), 0o644); err != nil {
		t.Fatalf("edit t1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: A → B
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t2: %v", err)
	}
	if err := os.WriteFile(abs, []byte("B"), 0o644); err != nil {
		t.Fatalf("edit t2: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// content_at_cp1 = v2.bin = "A"
	// content_at_cp2 = current workdir = "B"
	patch, err := store.Diff(context.Background(), "cp1", "cp2")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(patch, "-A") || !strings.Contains(patch, "+B") {
		t.Fatalf("expected diff A→B, got:\n%s", patch)
	}
}

// TestIndexReload_SurvivesProcessRestart: reconstruct store from disk, verify pathToVersions/displayPaths.
func TestIndexReload_SurvivesProcessRestart(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	workdir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	abs := filepath.Join(workdir, "a.txt")
	if err := os.WriteFile(abs, []byte("X"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	{
		store := NewPerEditSnapshotStore(projectDir, workdir)
		if _, err := store.CapturePreWrite(abs); err != nil {
			t.Fatalf("first capture: %v", err)
		}
		if err := os.WriteFile(abs, []byte("Y"), 0o644); err != nil {
			t.Fatalf("edit1: %v", err)
		}
		if _, err := store.Finalize("cp1"); err != nil {
			t.Fatalf("finalize: %v", err)
		}
		store.Reset()

		if _, err := store.CapturePreWrite(abs); err != nil {
			t.Fatalf("second capture: %v", err)
		}
	}

	// Simulate process restart: build fresh store from same dirs.
	revived := NewPerEditSnapshotStore(projectDir, workdir)
	hash := perEditPathHash(abs)
	versions := revived.pathToVersions[hash]
	if len(versions) != 2 || versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("revived versions = %v, want [1 2]", versions)
	}
	if revived.displayPaths[hash] != filepath.Clean(abs) {
		t.Fatalf("revived display = %q, want %q", revived.displayPaths[hash], filepath.Clean(abs))
	}

	// Restore on revived store should still work (verifies cp1.json + version files are usable).
	// Workdir is "Y" right now (we never edited again post second capture).
	// cp1 -> v_next(v1) = v2 -> meta.Existed=true, content="Y"
	// So Restore writes "Y" back which is no-op effectively.
	if err := revived.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("revived restore: %v", err)
	}
	if got := mustReadFile(t, abs); got != "Y" {
		t.Fatalf("post-restore want Y got %q", got)
	}
}

// TestFinalize_EmptyPendingReturnsFalse: Finalize with no captures should be a no-op.
func TestFinalize_EmptyPendingReturnsFalse(t *testing.T) {
	store, _ := newTestStore(t)
	written, err := store.Finalize("cp_empty")
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if written {
		t.Fatalf("written should be false on empty pending")
	}
	if _, err := os.Stat(store.checkpointMetaPath("cp_empty")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint meta should not exist, stat err=%v", err)
	}
}

// TestRestore_RemovesFileWhenVNextExistedFalse: capture-existing → delete → restore should NOT
// recreate the file because the next captured version has Existed=false.
func TestRestore_RemovesFileWhenVNextExistedFalse(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "doomed.txt", "I_LIVE")

	// Turn 1: edit
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t1: %v", err)
	}
	if err := os.WriteFile(abs, []byte("STILL_LIVE"), 0o644); err != nil {
		t.Fatalf("edit t1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: capture existing then delete; v2.bin contains "STILL_LIVE", v2.meta.Existed=true.
	// We need a v3 that has Existed=false to model "restore should delete".
	// So: turn 2 deletes, capture pre-delete: v2.bin="STILL_LIVE", Existed=true; remove file.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t2: %v", err)
	}
	if err := os.Remove(abs); err != nil {
		t.Fatalf("delete t2: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: re-create file; capture pre-create finds Existed=false.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t3: %v", err)
	}
	if err := os.WriteFile(abs, []byte("RECREATED"), 0o644); err != nil {
		t.Fatalf("recreate t3: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	// Restore cp2: v2 captured "STILL_LIVE"; v_next(v2)=v3 has Existed=false → delete file.
	if err := store.Restore(context.Background(), "cp2"); err != nil {
		t.Fatalf("restore cp2: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("file should be deleted, stat err=%v", err)
	}
}

// TestCaptureBatch_DedupesAndCaptures: batch is just sequential CapturePreWrite, dedupe works.
func TestCaptureBatch_DedupesAndCaptures(t *testing.T) {
	store, workdir := newTestStore(t)
	a := writeWorkdirFile(t, workdir, "a.txt", "A")
	b := writeWorkdirFile(t, workdir, "b.txt", "B")

	captured, err := store.CaptureBatch([]string{a, b, a, " ", "", b})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(captured) != 4 {
		t.Fatalf("captured paths len = %d, want 4 (empty/whitespace skipped)", len(captured))
	}

	// pending should have exactly two unique hashes.
	store.pendingMu.Lock()
	count := len(store.pending)
	store.pendingMu.Unlock()
	if count != 2 {
		t.Fatalf("pending count = %d, want 2", count)
	}
}

// TestCapturePreWrite_DirectoryMarksExistedTrue: capturing an existing directory stores Existed=true, IsDir=true.
func TestCapturePreWrite_DirectoryMarksExistedTrue(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := filepath.Join(workdir, "subdir")
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	v, err := store.CapturePreWrite(abs)
	if err != nil {
		t.Fatalf("capture dir: %v", err)
	}

	hash := perEditPathHash(abs)
	meta, err := store.readVersionMeta(hash, v)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	if !meta.Existed {
		t.Fatalf("Existed should be true for directory")
	}
	if !meta.IsDir {
		t.Fatalf("IsDir should be true for directory")
	}
	bin, err := store.readVersionBin(hash, v)
	if err != nil {
		t.Fatalf("read bin: %v", err)
	}
	if len(bin) != 0 {
		t.Fatalf("bin should be empty, got %d bytes", len(bin))
	}
}

// TestRestore_DirectoryRecreateAndDelete: per-edit restore uses v_next to determine directory state.
func TestRestore_DirectoryRecreateAndDelete(t *testing.T) {
	store, workdir := newTestStore(t)
	dir := filepath.Join(workdir, "foo")

	// Turn 1: create_dir — capture pre-create (does not exist), then create.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-create: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: remove_dir — capture pre-remove (exists, IsDir=true), then remove.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-remove: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: recreate_dir — capture pre-recreate (does not exist), then create.
	// This gives cp2 a v_next with Existed=false so restore can delete the directory.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-recreate: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir recreate: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	// Restore cp1: v_next=v2(Existed=true,IsDir=true) → MkdirAll. Dir should exist.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("manual remove before restore: %v", err)
	}
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("dir should exist after restore cp1, got %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("restored path should be a directory")
	}

	// Restore cp2: v_next=v3(Existed=false) → RemoveAll. Dir should be deleted.
	if err := store.Restore(context.Background(), "cp2"); err != nil {
		t.Fatalf("restore cp2: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir absent after restore cp2, stat err=%v", err)
	}
}

// TestRestore_DirectoryWithNestedFile: RemoveAll can delete a directory that later got nested files.
func TestRestore_DirectoryWithNestedFile(t *testing.T) {
	store, workdir := newTestStore(t)
	dir := filepath.Join(workdir, "foo")
	child := filepath.Join(dir, "bar.txt")

	// Turn 1: create_dir.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-create dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := store.Finalize("cp-dir"); err != nil {
		t.Fatalf("finalize cp-dir: %v", err)
	}
	store.Reset()

	// Turn 2: write file inside dir AND re-capture dir (so dir gets a v2 with Existed=true,IsDir=true).
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-touch dir: %v", err)
	}
	if _, err := store.CapturePreWrite(child); err != nil {
		t.Fatalf("capture pre-write child: %v", err)
	}
	if err := os.WriteFile(child, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	if _, err := store.Finalize("cp-child"); err != nil {
		t.Fatalf("finalize cp-child: %v", err)
	}
	store.Reset()

	// Turn 3: remove_dir — capture pre-remove (dir+child exist), then delete.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-remove dir: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if _, err := store.Finalize("cp-remove"); err != nil {
		t.Fatalf("finalize cp-remove: %v", err)
	}
	store.Reset()

	// Turn 4: recreate empty dir — gives cp-remove a v_next with Existed=false.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture pre-recreate dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir recreate: %v", err)
	}
	if _, err := store.Finalize("cp-recreate"); err != nil {
		t.Fatalf("finalize cp-recreate: %v", err)
	}
	store.Reset()

	// Restore cp-dir: v_next=v2(Existed=true,IsDir=true) → MkdirAll. Dir should exist (child won't be restored because child has its own chain).
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("manual remove before restore: %v", err)
	}
	if err := store.Restore(context.Background(), "cp-dir"); err != nil {
		t.Fatalf("restore cp-dir: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("dir should be recreated after restore cp-dir")
	}

	// Restore cp-remove: v_next=v4(Existed=false) → RemoveAll. Should delete even if non-empty.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir before restore: %v", err)
	}
	if err := os.WriteFile(child, []byte("new"), 0o644); err != nil {
		t.Fatalf("write child before restore: %v", err)
	}
	if err := store.Restore(context.Background(), "cp-remove"); err != nil {
		t.Fatalf("restore cp-remove: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir absent after restore cp-remove, stat err=%v", err)
	}
}

func TestChangedFiles(t *testing.T) {
	store, workdir := newTestStore(t)

	// Setup files for cp1.
	writeWorkdirFile(t, workdir, "a.txt", "alpha")
	writeWorkdirFile(t, workdir, "b.txt", "beta")

	// Turn 1: capture, finalize cp1.
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp1 a: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("capture cp1 b: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: capture all paths (including new c.txt), then edit.
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp2 a: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("capture cp2 b: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "c.txt")); err != nil {
		t.Fatalf("capture cp2 c: %v", err)
	}
	writeWorkdirFile(t, workdir, "a.txt", "alpha-v2")
	if err := os.Remove(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("remove b.txt: %v", err)
	}
	writeWorkdirFile(t, workdir, "c.txt", "gamma")
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: capture all paths again to create v_next for cp2 (needed for correct diff resolution).
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp3 a: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("capture cp3 b: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "c.txt")); err != nil {
		t.Fatalf("capture cp3 c: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	// Restore to cp1 so workdir fallback matches cp1 state.
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	// c.txt did not exist in cp1; Restore won't remove it because cp1 doesn't know about it.
	if err := os.Remove(filepath.Join(workdir, "c.txt")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove stray c.txt: %v", err)
	}

	changes, err := store.ChangedFiles(context.Background(), "cp1", "cp2")
	if err != nil {
		t.Fatalf("changed files cp1->cp2: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %+v", len(changes), changes)
	}

	want := map[string]FileChangeKind{
		"a.txt": FileChangeModified,
		"b.txt": FileChangeDeleted,
		"c.txt": FileChangeAdded,
	}
	for _, ch := range changes {
		if want[ch.Path] != ch.Kind {
			t.Errorf("path %s: expected kind %s, got %s", ch.Path, want[ch.Path], ch.Kind)
		}
		delete(want, ch.Path)
	}
	if len(want) > 0 {
		t.Errorf("missing expected changes: %+v", want)
	}
}

func TestChangedFiles_NoChange(t *testing.T) {
	store, workdir := newTestStore(t)
	writeWorkdirFile(t, workdir, "x.txt", "same")

	if _, err := store.CapturePreWrite(filepath.Join(workdir, "x.txt")); err != nil {
		t.Fatalf("capture cp1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	if _, err := store.CapturePreWrite(filepath.Join(workdir, "x.txt")); err != nil {
		t.Fatalf("capture cp2: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	changes, err := store.ChangedFiles(context.Background(), "cp1", "cp2")
	if err != nil {
		t.Fatalf("changed files cp1->cp2: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes, got %d: %+v", len(changes), changes)
	}
}

func TestChangedFiles_DirectoryToFile(t *testing.T) {
	store, workdir := newTestStore(t)
	path := filepath.Join(workdir, "target")

	// Turn 1: target is a directory.
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if _, err := store.CapturePreWrite(path); err != nil {
		t.Fatalf("capture cp1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: target becomes a file.
	if _, err := store.CapturePreWrite(path); err != nil {
		t.Fatalf("capture cp2: %v", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: capture again to give cp2 a v_next.
	if _, err := store.CapturePreWrite(path); err != nil {
		t.Fatalf("capture cp3: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	changes, err := store.ChangedFiles(context.Background(), "cp1", "cp2")
	if err != nil {
		t.Fatalf("changed files: %v", err)
	}
	if len(changes) != 1 || changes[0].Path != "target" || changes[0].Kind != FileChangeModified {
		t.Fatalf("expected target modified, got %+v", changes)
	}
}

func TestCapturePostDelete_CreatesExistedFalseVersion(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "hello")

	// Turn 1: capture pre-write (v1, Existed=true).
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture v1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Delete the file, then call CapturePostDelete to create v2(Existed=false).
	if err := os.Remove(abs); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := store.CapturePostDelete([]string{abs}); err != nil {
		t.Fatalf("CapturePostDelete: %v", err)
	}

	// Restore cp1: v_next should be v2(Existed=false) → file should be deleted.
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("expected file absent after restore, stat err=%v", err)
	}
}

func TestCapturePostDelete_DirectoryTreeRecovery(t *testing.T) {
	store, workdir := newTestStore(t)
	dir := filepath.Join(workdir, "foo")
	child1 := filepath.Join(dir, "a.txt")
	child2 := filepath.Join(dir, "sub", "b.txt")

	// Create nested tree.
	writeWorkdirFile(t, workdir, "foo/a.txt", "alpha")
	writeWorkdirFile(t, workdir, "foo/sub/b.txt", "beta")

	// Turn 1: pre-capture directory and all nested files.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture dir: %v", err)
	}
	if _, err := store.CapturePreWrite(child1); err != nil {
		t.Fatalf("capture child1: %v", err)
	}
	if _, err := store.CapturePreWrite(child2); err != nil {
		t.Fatalf("capture child2: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: pre-capture, then delete tree, then CapturePostDelete, then finalize cp2.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture dir t2: %v", err)
	}
	if _, err := store.CapturePreWrite(child1); err != nil {
		t.Fatalf("capture child1 t2: %v", err)
	}
	if _, err := store.CapturePreWrite(child2); err != nil {
		t.Fatalf("capture child2 t2: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removeAll: %v", err)
	}
	if err := store.CapturePostDelete([]string{dir, child1, child2}); err != nil {
		t.Fatalf("CapturePostDelete: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Restore cp1: v_next is v2(pre-delete, Existed=true) → tree recreated.
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if got := mustReadFile(t, child1); got != "alpha" {
		t.Fatalf("child1 want alpha got %q", got)
	}
	if got := mustReadFile(t, child2); got != "beta" {
		t.Fatalf("child2 want beta got %q", got)
	}

	// Restore cp2: v_next is v3(post-delete, Existed=false) → tree deleted.
	if err := store.Restore(context.Background(), "cp2"); err != nil {
		t.Fatalf("restore cp2: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir absent after restore cp2, stat err=%v", err)
	}
}

func TestRestore_RemoveDirWithNestedFiles(t *testing.T) {
	store, workdir := newTestStore(t)
	dir := filepath.Join(workdir, "foo")
	child := filepath.Join(dir, "bar.txt")

	// Turn 1: create tree.
	writeWorkdirFile(t, workdir, "foo/bar.txt", "hello")
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture dir t1: %v", err)
	}
	if _, err := store.CapturePreWrite(child); err != nil {
		t.Fatalf("capture child t1: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: remove tree with recursive pre-capture + post-delete.
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture dir t2: %v", err)
	}
	if _, err := store.CapturePreWrite(child); err != nil {
		t.Fatalf("capture child t2: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("removeAll: %v", err)
	}
	if err := store.CapturePostDelete([]string{dir, child}); err != nil {
		t.Fatalf("CapturePostDelete: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: recreate tree with different content.
	writeWorkdirFile(t, workdir, "foo/bar.txt", "world")
	if _, err := store.CapturePreWrite(dir); err != nil {
		t.Fatalf("capture dir t3: %v", err)
	}
	if _, err := store.CapturePreWrite(child); err != nil {
		t.Fatalf("capture child t3: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	// Restore cp2: should delete the tree.
	if err := store.Restore(context.Background(), "cp2"); err != nil {
		t.Fatalf("restore cp2: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir absent after restore cp2, stat err=%v", err)
	}

	// Restore cp1: should recreate the tree with original content.
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if got := mustReadFile(t, child); got != "hello" {
		t.Fatalf("child want hello got %q", got)
	}
}

func TestPerEditStoreHelperMethods(t *testing.T) {
	t.Run("availability and pending lifecycle", func(t *testing.T) {
		var nilStore *PerEditSnapshotStore
		if nilStore.IsAvailable() {
			t.Fatal("nil store should report unavailable")
		}

		store, workdir := newTestStore(t)
		if !store.IsAvailable() {
			t.Fatal("store should report available")
		}
		if store.HasPending() {
			t.Fatal("new store should not have pending captures")
		}

		abs := writeWorkdirFile(t, workdir, "pending.txt", "hello")
		if _, err := store.CapturePreWrite(abs); err != nil {
			t.Fatalf("CapturePreWrite() error = %v", err)
		}
		if !store.HasPending() {
			t.Fatal("capture should mark store pending")
		}

		store.Reset()
		if store.HasPending() {
			t.Fatal("Reset() should clear pending captures")
		}
	})

	t.Run("delete checkpoint and ref helpers", func(t *testing.T) {
		store, workdir := newTestStore(t)
		abs := writeWorkdirFile(t, workdir, "tracked.txt", "v1")
		if _, err := store.CapturePreWrite(abs); err != nil {
			t.Fatalf("CapturePreWrite() error = %v", err)
		}
		if written, err := store.Finalize("cp-delete"); err != nil || !written {
			t.Fatalf("Finalize() written=%v err=%v", written, err)
		}

		cpPath := store.checkpointMetaPath("cp-delete")
		if _, err := os.Stat(cpPath); err != nil {
			t.Fatalf("checkpoint meta missing before delete: %v", err)
		}
		if err := store.DeleteCheckpoint("cp-delete"); err != nil {
			t.Fatalf("DeleteCheckpoint() error = %v", err)
		}
		if _, err := os.Stat(cpPath); !os.IsNotExist(err) {
			t.Fatalf("checkpoint meta should be removed, err=%v", err)
		}
		if err := store.DeleteCheckpoint("cp-delete"); err != nil {
			t.Fatalf("DeleteCheckpoint() missing should be noop, got %v", err)
		}
		if err := store.DeleteCheckpoint(""); err != nil {
			t.Fatalf("DeleteCheckpoint(\"\") should be noop, got %v", err)
		}

		ref := RefForPerEditCheckpoint("cp-delete")
		if !IsPerEditRef(ref) {
			t.Fatalf("expected per-edit ref: %q", ref)
		}
		if got := PerEditCheckpointIDFromRef(ref); got != "cp-delete" {
			t.Fatalf("PerEditCheckpointIDFromRef() = %q, want cp-delete", got)
		}
		if IsPerEditRef("git:deadbeef") {
			t.Fatal("non per-edit ref should not match")
		}
		if got := PerEditCheckpointIDFromRef("git:deadbeef"); got != "" {
			t.Fatalf("non per-edit ref should return empty id, got %q", got)
		}
	})
}

func TestRestoreExact(t *testing.T) {
	store, workdir := newTestStore(t)
	abs := writeWorkdirFile(t, workdir, "a.txt", "hello")

	// Turn 1: capture, edit, finalize cp1.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := os.WriteFile(abs, []byte("world"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	store.Reset()

	// Turn 2: capture (v2="world"), edit again.
	if _, err := store.CapturePreWrite(abs); err != nil {
		t.Fatalf("capture t2: %v", err)
	}
	if err := os.WriteFile(abs, []byte("third"), 0o644); err != nil {
		t.Fatalf("edit t2: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// RestoreExact(cp2) should write back v2="world" (the exact version in cp2).
	if err := store.RestoreExact(context.Background(), "cp2"); err != nil {
		t.Fatalf("RestoreExact(cp2): %v", err)
	}
	if got := mustReadFile(t, abs); got != "world" {
		t.Fatalf("RestoreExact(cp2) want world got %q", got)
	}

	// RestoreExact(cp1) should write back v1="hello".
	if err := store.RestoreExact(context.Background(), "cp1"); err != nil {
		t.Fatalf("RestoreExact(cp1): %v", err)
	}
	if got := mustReadFile(t, abs); got != "hello" {
		t.Fatalf("RestoreExact(cp1) want hello got %q", got)
	}
}

func TestChangedFiles_NewFileDetectedAsAdded(t *testing.T) {
	store, workdir := newTestStore(t)

	// Turn 1: only a.txt exists.
	writeWorkdirFile(t, workdir, "a.txt", "alpha")
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp1 a: %v", err)
	}
	if _, err := store.Finalize("cp1"); err != nil {
		t.Fatalf("finalize cp1: %v", err)
	}
	store.Reset()

	// Turn 2: create b.txt.
	writeWorkdirFile(t, workdir, "b.txt", "beta")
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp2 a: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("capture cp2 b: %v", err)
	}
	if _, err := store.Finalize("cp2"); err != nil {
		t.Fatalf("finalize cp2: %v", err)
	}
	store.Reset()

	// Turn 3: capture again to give cp2 a v_next.
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "a.txt")); err != nil {
		t.Fatalf("capture cp3 a: %v", err)
	}
	if _, err := store.CapturePreWrite(filepath.Join(workdir, "b.txt")); err != nil {
		t.Fatalf("capture cp3 b: %v", err)
	}
	if _, err := store.Finalize("cp3"); err != nil {
		t.Fatalf("finalize cp3: %v", err)
	}
	store.Reset()

	// Restore to cp1 so workdir fallback matches cp1 state.
	if err := store.Restore(context.Background(), "cp1"); err != nil {
		t.Fatalf("restore cp1: %v", err)
	}
	if err := os.Remove(filepath.Join(workdir, "b.txt")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove stray b.txt: %v", err)
	}

	changes, err := store.ChangedFiles(context.Background(), "cp1", "cp2")
	if err != nil {
		t.Fatalf("changed files cp1->cp2: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %+v", len(changes), changes)
	}
	if changes[0].Path != "b.txt" || changes[0].Kind != FileChangeAdded {
		t.Fatalf("expected b.txt added, got %+v", changes[0])
	}
}
