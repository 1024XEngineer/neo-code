package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShadowRepoSnapshotRestoreAndConflictDetection(t *testing.T) {
	t.Parallel()

	if available, _ := CheckGitAvailability(context.Background()); !available {
		t.Skip("git is not available in test environment")
	}

	projectDir := t.TempDir()
	workdir := t.TempDir()
	repo := NewShadowRepo(projectDir, workdir)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	targetFile := filepath.Join(workdir, "main.go")
	if err := os.WriteFile(targetFile, []byte("package main\nconst version = 1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(version1) error = %v", err)
	}

	refOne := RefForCheckpoint("session-1", "cp-1")
	hashOne, err := repo.Snapshot(context.Background(), refOne, "snapshot one")
	if err != nil {
		t.Fatalf("Snapshot(first) error = %v", err)
	}
	if strings.TrimSpace(hashOne) == "" {
		t.Fatalf("Snapshot(first) returned empty hash")
	}

	if repo.HasCodeChanges(context.Background()) {
		t.Fatalf("expected clean worktree after first snapshot")
	}

	if err := os.WriteFile(targetFile, []byte("package main\nconst version = 2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(version2) error = %v", err)
	}
	if !repo.HasCodeChanges(context.Background()) {
		t.Fatalf("expected HasCodeChanges() to detect modified file")
	}

	refTwo := RefForCheckpoint("session-1", "cp-2")
	if _, err := repo.Snapshot(context.Background(), refTwo, "snapshot two"); err != nil {
		t.Fatalf("Snapshot(second) error = %v", err)
	}

	resolved, err := repo.ResolveRef(context.Background(), refOne)
	if err != nil {
		t.Fatalf("ResolveRef() error = %v", err)
	}
	if resolved != hashOne {
		t.Fatalf("ResolveRef() = %q, want %q", resolved, hashOne)
	}

	conflict, err := repo.DetectConflicts(context.Background(), hashOne)
	if err != nil {
		t.Fatalf("DetectConflicts() error = %v", err)
	}
	if !conflict.HasConflict || len(conflict.ModifiedFiles) != 1 || conflict.ModifiedFiles[0] != "main.go" {
		t.Fatalf("DetectConflicts() = %#v, want modified main.go", conflict)
	}

	if err := repo.Restore(context.Background(), hashOne); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile(restored) error = %v", err)
	}
	if !strings.Contains(string(content), "version = 1") {
		t.Fatalf("restored content = %q, want version 1", string(content))
	}

	if err := repo.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestShadowRepoInitRebuildsDamagedRepository(t *testing.T) {
	t.Parallel()

	if available, _ := CheckGitAvailability(context.Background()); !available {
		t.Skip("git is not available in test environment")
	}

	projectDir := t.TempDir()
	workdir := t.TempDir()
	shadowDir := filepath.Join(projectDir, ".shadow")
	if err := os.MkdirAll(shadowDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(shadowDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(shadowDir, "corrupted"), []byte("not a git dir"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupted) error = %v", err)
	}

	repo := NewShadowRepo(projectDir, workdir)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !repo.IsAvailable() {
		t.Fatalf("expected repo to be available after rebuild")
	}

	backups, err := filepath.Glob(shadowDir + ".bak.*")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(backups) == 0 {
		t.Fatalf("expected damaged shadow repo backup to be created")
	}

	if err := repo.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	backups, err = filepath.Glob(shadowDir + ".bak.*")
	if err != nil {
		t.Fatalf("Glob(after rebuild) error = %v", err)
	}
	if len(backups) < 2 {
		t.Fatalf("expected rebuild to create another backup, got %v", backups)
	}
}

func TestShadowRepoHelpers(t *testing.T) {
	t.Parallel()

	ref := RefForCheckpoint("session-a", "checkpoint-b")
	if ref != "refs/neocode/sessions/session-a/checkpoints/checkpoint-b" {
		t.Fatalf("RefForCheckpoint() = %q", ref)
	}

	repo := NewShadowRepo(t.TempDir(), t.TempDir())
	if repo.HasCodeChanges(context.Background()) != true {
		t.Fatalf("expected unavailable shadow repo to conservatively report code changes")
	}
	if err := repo.DeleteRef(context.Background(), "refs/unused"); err != nil {
		t.Fatalf("DeleteRef() on unavailable repo error = %v", err)
	}
}
