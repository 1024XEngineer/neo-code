package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResultPathFilterEvaluateReasons(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "safe.txt"), "ok")
	mustWriteFile(t, filepath.Join(workspace, ".env"), "secret")
	mustWriteFile(t, filepath.Join(outside, "target.txt"), "outside")
	linkPath := filepath.Join(workspace, "escape.txt")
	if err := os.Symlink(filepath.Join(outside, "target.txt"), linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := os.Lstat(linkPath); err != nil {
		t.Skipf("symlink created but missing: %v", err)
	}

	filter, err := newResultPathFilter(workspace)
	if err != nil {
		t.Fatalf("create filter: %v", err)
	}

	relative, reason, allowed := filter.evaluate(filepath.Join(workspace, "safe.txt"))
	if !allowed || reason != "" || relative != "safe.txt" {
		t.Fatalf("expected safe path allowed, got relative=%q reason=%q allowed=%v", relative, reason, allowed)
	}

	_, reason, allowed = filter.evaluate(filepath.Join(workspace, ".env"))
	if allowed || reason != filterReasonSensitivePath {
		t.Fatalf("expected sensitive path rejection, got reason=%q allowed=%v", reason, allowed)
	}

	_, reason, allowed = filter.evaluate(filepath.Join(outside, "target.txt"))
	if allowed || reason != filterReasonOutsideWorkspace {
		t.Fatalf("expected outside workspace rejection, got reason=%q allowed=%v", reason, allowed)
	}

	_, reason, allowed = filter.evaluate(linkPath)
	if allowed || reason != filterReasonSymlinkEscape {
		t.Fatalf("expected symlink escape rejection, got reason=%q allowed=%v", reason, allowed)
	}
}
