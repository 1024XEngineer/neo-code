package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestScanWorkdir_SkipsConfiguredDirs: skip dirs in opts are not scanned.
func TestScanWorkdir_SkipsConfiguredDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "src", "main.go"), "package main")
	mustWrite(t, filepath.Join(root, "node_modules", "lib", "x.js"), "x")
	mustWrite(t, filepath.Join(root, ".git", "config"), "[core]")
	mustWrite(t, filepath.Join(root, "vendor", "v.go"), "v")

	fp, truncated, err := ScanWorkdir(context.Background(), root, DefaultFingerprintOptions())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if truncated {
		t.Fatalf("should not be truncated")
	}
	if _, ok := fp[filepath.ToSlash(filepath.Join("src", "main.go"))]; !ok {
		t.Fatalf("src/main.go missing from fingerprint")
	}
	for k := range fp {
		if filepath.HasPrefix(k, "node_modules/") || filepath.HasPrefix(k, ".git/") || filepath.HasPrefix(k, "vendor/") {
			t.Fatalf("skipped dir leaked: %s", k)
		}
	}
}

// TestScanWorkdir_SkipsBinaryExtensions: extensions in SkipExts excluded.
func TestScanWorkdir_SkipsBinaryExtensions(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), "package a")
	mustWrite(t, filepath.Join(root, "b.exe"), "binary")
	mustWrite(t, filepath.Join(root, "c.zip"), "zip")

	fp, _, err := ScanWorkdir(context.Background(), root, DefaultFingerprintOptions())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, ok := fp["a.go"]; !ok {
		t.Fatalf("a.go missing")
	}
	if _, ok := fp["b.exe"]; ok {
		t.Fatalf(".exe should be skipped")
	}
	if _, ok := fp["c.zip"]; ok {
		t.Fatalf(".zip should be skipped")
	}
}

// TestScanWorkdir_TruncatesBeyondMaxFiles: MaxFiles enforces an upper bound and sets truncated=true.
func TestScanWorkdir_TruncatesBeyondMaxFiles(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		mustWrite(t, filepath.Join(root, "f", "x"+itoa(i)+".go"), "package x")
	}
	opts := DefaultFingerprintOptions()
	opts.MaxFiles = 5

	fp, truncated, err := ScanWorkdir(context.Background(), root, opts)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !truncated {
		t.Fatalf("expected truncated=true with MaxFiles=%d", opts.MaxFiles)
	}
	if len(fp) > opts.MaxFiles {
		t.Fatalf("got %d entries, MaxFiles=%d", len(fp), opts.MaxFiles)
	}
}

// TestDiffFingerprints_ClassifiesAddDeleteModify: Diff produces three categories correctly.
func TestDiffFingerprints_ClassifiesAddDeleteModify(t *testing.T) {
	before := WorkdirFingerprint{
		"keep.go":     {Size: 10, HeadHash: "AA"},
		"remove.go":   {Size: 20, HeadHash: "BB"},
		"modified.go": {Size: 30, HeadHash: "CC"},
	}
	after := WorkdirFingerprint{
		"keep.go":     {Size: 10, HeadHash: "AA"},
		"modified.go": {Size: 30, HeadHash: "DD"}, // hash differs
		"new.go":      {Size: 40, HeadHash: "EE"},
	}
	diff := DiffFingerprints(before, after)
	wantAdded := []string{"new.go"}
	wantDeleted := []string{"remove.go"}
	wantModified := []string{"modified.go"}
	if !equalStringSlice(diff.Added, wantAdded) {
		t.Fatalf("Added: got %v want %v", diff.Added, wantAdded)
	}
	if !equalStringSlice(diff.Deleted, wantDeleted) {
		t.Fatalf("Deleted: got %v want %v", diff.Deleted, wantDeleted)
	}
	if !equalStringSlice(diff.Modified, wantModified) {
		t.Fatalf("Modified: got %v want %v", diff.Modified, wantModified)
	}
}

// TestScanWorkdir_DetectsContentEdit: editing file content updates HeadHash.
func TestScanWorkdir_DetectsContentEdit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "f.go"), "v1")

	before, _, err := ScanWorkdir(context.Background(), root, DefaultFingerprintOptions())
	if err != nil {
		t.Fatalf("scan before: %v", err)
	}
	mustWrite(t, filepath.Join(root, "f.go"), "v2content_different_size")
	after, _, err := ScanWorkdir(context.Background(), root, DefaultFingerprintOptions())
	if err != nil {
		t.Fatalf("scan after: %v", err)
	}
	diff := DiffFingerprints(before, after)
	if !equalStringSlice(diff.Modified, []string{"f.go"}) {
		t.Fatalf("Modified=%v, want [f.go]", diff.Modified)
	}
}

// helpers

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
