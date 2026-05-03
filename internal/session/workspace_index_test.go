package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceIndex_LoadEmpty(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load on missing file should succeed, got %v", err)
	}
	if got := idx.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d records", len(got))
	}
}

func TestWorkspaceIndex_RegisterAndGet(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	wsRoot := filepath.Join(base, "alpha")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rec, err := idx.Register(wsRoot, "Alpha")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if rec.Hash != HashWorkspaceRoot(wsRoot) {
		t.Fatalf("hash mismatch: got %q", rec.Hash)
	}
	if rec.Name != "Alpha" {
		t.Fatalf("name = %q, want Alpha", rec.Name)
	}
	if rec.Path != NormalizeWorkspaceRoot(wsRoot) {
		t.Fatalf("path mismatch: got %q", rec.Path)
	}
	if rec.CreatedAt.IsZero() || rec.UpdatedAt.IsZero() {
		t.Fatalf("timestamps should be set")
	}

	got, ok := idx.Get(rec.Hash)
	if !ok {
		t.Fatalf("Get returned ok=false for just-registered hash")
	}
	if got.Hash != rec.Hash || got.Name != "Alpha" {
		t.Fatalf("Get returned unexpected record: %+v", got)
	}
}

func TestWorkspaceIndex_RegisterDefaultsName(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	wsRoot := filepath.Join(base, "my-project")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rec, err := idx.Register(wsRoot, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if rec.Name != "my-project" {
		t.Fatalf("name = %q, want my-project (filepath.Base)", rec.Name)
	}
}

func TestWorkspaceIndex_RegisterRejectsEmptyPath(t *testing.T) {
	idx := NewWorkspaceIndex(t.TempDir())
	if _, err := idx.Register("", "name"); err == nil {
		t.Fatalf("expected error for empty path")
	}
	if _, err := idx.Register("   ", "name"); err == nil {
		t.Fatalf("expected error for whitespace path")
	}
}

func TestWorkspaceIndex_RegisterUpdatesExisting(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	wsRoot := filepath.Join(base, "x")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	first, err := idx.Register(wsRoot, "First")
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	originalCreated := first.CreatedAt
	time.Sleep(2 * time.Millisecond)

	second, err := idx.Register(wsRoot, "Second")
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}
	if second.Hash != first.Hash {
		t.Fatalf("hash should be stable across re-registrations: %q vs %q", first.Hash, second.Hash)
	}
	if second.Name != "Second" {
		t.Fatalf("re-register should update name, got %q", second.Name)
	}
	if !second.CreatedAt.Equal(originalCreated) {
		t.Fatalf("CreatedAt should be preserved on update: %v vs %v", second.CreatedAt, originalCreated)
	}
	if !second.UpdatedAt.After(originalCreated) {
		t.Fatalf("UpdatedAt should advance: %v not after %v", second.UpdatedAt, originalCreated)
	}

	if list := idx.List(); len(list) != 1 {
		t.Fatalf("re-register must not duplicate; got %d records", len(list))
	}
}

func TestWorkspaceIndex_ListSortedByUpdatedAtDesc(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	for _, name := range []string{"a", "b", "c"} {
		ws := filepath.Join(base, name)
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		if _, err := idx.Register(ws, name); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	list := idx.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 records, got %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].UpdatedAt.Before(list[i].UpdatedAt) {
			t.Fatalf("List not sorted desc: %v before %v", list[i-1].UpdatedAt, list[i].UpdatedAt)
		}
	}
	// The last-registered "c" should be first.
	if list[0].Name != "c" {
		t.Fatalf("expected c first, got %q", list[0].Name)
	}
}

func TestWorkspaceIndex_GetMissing(t *testing.T) {
	idx := NewWorkspaceIndex(t.TempDir())
	if _, ok := idx.Get("not-a-hash"); ok {
		t.Fatalf("Get should return ok=false for missing hash")
	}
}

func TestWorkspaceIndex_RenameUpdatesNameAndTimestamp(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec, err := idx.Register(ws, "Original")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	originalUpdated := rec.UpdatedAt
	time.Sleep(2 * time.Millisecond)

	renamed, err := idx.Rename(rec.Hash, "Renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", renamed.Name)
	}
	if !renamed.UpdatedAt.After(originalUpdated) {
		t.Fatalf("UpdatedAt should advance on rename")
	}

	got, ok := idx.Get(rec.Hash)
	if !ok || got.Name != "Renamed" {
		t.Fatalf("Get after rename returned %+v ok=%v", got, ok)
	}
}

func TestWorkspaceIndex_RenameRejectsEmptyName(t *testing.T) {
	idx := NewWorkspaceIndex(t.TempDir())
	if _, err := idx.Rename("any", ""); err == nil {
		t.Fatalf("expected error for empty rename name")
	}
}

func TestWorkspaceIndex_RenameMissing(t *testing.T) {
	idx := NewWorkspaceIndex(t.TempDir())
	_, err := idx.Rename("missing", "X")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestWorkspaceIndex_DeleteRemovesRecord(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec, err := idx.Register(ws, "Doomed")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	deleted, err := idx.Delete(rec.Hash, false)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Hash != rec.Hash {
		t.Fatalf("Delete returned wrong record: %+v", deleted)
	}
	if _, ok := idx.Get(rec.Hash); ok {
		t.Fatalf("record should be gone after Delete")
	}
}

func TestWorkspaceIndex_DeleteMissing(t *testing.T) {
	idx := NewWorkspaceIndex(t.TempDir())
	_, err := idx.Delete("missing", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestWorkspaceIndex_DeleteRemovesDataDir(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec, err := idx.Register(ws, "Doomed")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	dataDir := filepath.Join(base, projectsDirName, rec.Hash)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	stamp := filepath.Join(dataDir, "session.db")
	if err := os.WriteFile(stamp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	if _, err := idx.Delete(rec.Hash, true); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir should be gone, stat err = %v", err)
	}
}

func TestWorkspaceIndex_DeleteKeepsDataDir(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rec, err := idx.Register(ws, "Keep")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	dataDir := filepath.Join(base, projectsDirName, rec.Hash)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	stamp := filepath.Join(dataDir, "session.db")
	if err := os.WriteFile(stamp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	if _, err := idx.Delete(rec.Hash, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Fatalf("data file should be preserved, stat err = %v", err)
	}
}

func TestWorkspaceIndex_SaveLoadRoundtrip(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	roots := []string{filepath.Join(base, "a"), filepath.Join(base, "b")}
	for i, r := range roots {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", r, err)
		}
		if _, err := idx.Register(r, []string{"alpha", "beta"}[i]); err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, workspaceIndexFileName)); err != nil {
		t.Fatalf("index file should exist, stat err = %v", err)
	}

	reloaded := NewWorkspaceIndex(base)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	original := idx.List()
	got := reloaded.List()
	if len(got) != len(original) {
		t.Fatalf("got %d records, want %d", len(got), len(original))
	}
	for i := range got {
		if got[i].Hash != original[i].Hash || got[i].Name != original[i].Name || got[i].Path != original[i].Path {
			t.Fatalf("record[%d] mismatch after roundtrip: got %+v want %+v", i, got[i], original[i])
		}
	}
}

func TestWorkspaceIndex_LoadParseError(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, workspaceIndexFileName), []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write bad index: %v", err)
	}
	idx := NewWorkspaceIndex(base)
	if err := idx.Load(); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestWorkspaceIndex_ListIsCopy(t *testing.T) {
	base := t.TempDir()
	idx := NewWorkspaceIndex(base)

	ws := filepath.Join(base, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := idx.Register(ws, "X"); err != nil {
		t.Fatalf("register: %v", err)
	}

	first := idx.List()
	first[0].Name = "Mutated"

	got, ok := idx.Get(first[0].Hash)
	if !ok {
		t.Fatalf("get failed")
	}
	if got.Name == "Mutated" {
		t.Fatalf("List should return a defensive copy; internal state mutated")
	}
}
