package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureFileSnapshotMissingFileMarksAsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")

	snap := captureFileSnapshot(path)
	if !snap.WasNew() {
		t.Fatal("expected missing file snapshot to be treated as new")
	}

	diff, err := snap.Diff()
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff != "" {
		t.Fatalf("Diff() = %q, want empty", diff)
	}
}

func TestFileSnapshotDiffHandlesDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delete.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	snap := captureFileSnapshot(path)
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	diff, err := snap.Diff()
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !strings.Contains(diff, "--- "+path) || !strings.Contains(diff, "-before") {
		t.Fatalf("Diff() = %q, want deletion patch for %s", diff, path)
	}
}

func TestFileSnapshotDiffIgnoresUnchangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same.txt")
	if err := os.WriteFile(path, []byte("same\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	snap := captureFileSnapshot(path)
	diff, err := snap.Diff()
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff != "" {
		t.Fatalf("Diff() = %q, want empty", diff)
	}
}

func TestComputeUnifiedDiffTrimsTrailingNewline(t *testing.T) {
	diff, err := computeUnifiedDiff("one\n", "two\n", "sample.txt")
	if err != nil {
		t.Fatalf("computeUnifiedDiff() error = %v", err)
	}
	if strings.HasSuffix(diff, "\n") {
		t.Fatalf("diff should be trimmed, got %q", diff)
	}
	if !strings.Contains(diff, "@@") || !strings.Contains(diff, "+two") || !strings.Contains(diff, "-one") {
		t.Fatalf("diff = %q, want unified diff body", diff)
	}
}
