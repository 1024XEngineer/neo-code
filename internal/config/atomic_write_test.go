package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestFsyncDirectoryWindowsSkipsDirectorySync(t *testing.T) {
	previousGOOS := atomicGOOS
	atomicGOOS = "windows"
	defer func() {
		atomicGOOS = previousGOOS
	}()

	missingDir := filepath.Join(t.TempDir(), "missing")
	if err := fsyncDirectory(missingDir); err != nil {
		t.Fatalf("fsyncDirectory() should be noop on windows, got %v", err)
	}
}

func TestFsyncDirectoryNonWindowsReturnsOpenErrorForMissingDirectory(t *testing.T) {
	previousGOOS := atomicGOOS
	atomicGOOS = "linux"
	defer func() {
		atomicGOOS = previousGOOS
	}()

	missingDir := filepath.Join(t.TempDir(), "missing")
	if err := fsyncDirectory(missingDir); err == nil {
		t.Fatalf("expected fsyncDirectory() to fail for missing directory on non-windows")
	}
}

func TestFsyncDirectoryNonWindowsSucceedsForExistingDirectory(t *testing.T) {
	previousGOOS := atomicGOOS
	atomicGOOS = "linux"
	defer func() {
		atomicGOOS = previousGOOS
	}()

	dir := t.TempDir()
	if err := fsyncDirectory(dir); err != nil {
		t.Fatalf("fsyncDirectory() error = %v", err)
	}
}

func TestIsBestEffortDirectorySyncError(t *testing.T) {
	t.Parallel()

	if !isBestEffortDirectorySyncError(syscall.EINVAL) {
		t.Fatalf("expected EINVAL to be treated as best-effort")
	}
	if !isBestEffortDirectorySyncError(syscall.EACCES) {
		t.Fatalf("expected EACCES to be treated as best-effort")
	}
	if !isBestEffortDirectorySyncError(&os.PathError{Op: "sync", Path: "/tmp", Err: syscall.EPERM}) {
		t.Fatalf("expected wrapped EPERM to be treated as best-effort")
	}
	if !isBestEffortDirectorySyncError(os.ErrInvalid) {
		t.Fatalf("expected os.ErrInvalid to be treated as best-effort")
	}
}
