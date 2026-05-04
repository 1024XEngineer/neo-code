//go:build !windows

package ptyproxy

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveLatestRunDiagSocketPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runDir := filepath.Join(home, ".neocode", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	firstPath := filepath.Join(runDir, diagSocketFilePrefix+"111"+diagSocketFileSuffix)
	firstListener, err := net.Listen("unix", firstPath)
	if err != nil {
		t.Fatalf("net.Listen(first) error = %v", err)
	}
	defer firstListener.Close()

	time.Sleep(20 * time.Millisecond)

	secondPath := filepath.Join(runDir, diagSocketFilePrefix+"222"+diagSocketFileSuffix)
	secondListener, err := net.Listen("unix", secondPath)
	if err != nil {
		t.Fatalf("net.Listen(second) error = %v", err)
	}
	defer secondListener.Close()

	latest, err := ResolveLatestRunDiagSocketPath()
	if err != nil {
		t.Fatalf("ResolveLatestRunDiagSocketPath() error = %v", err)
	}
	if filepath.Clean(latest) != filepath.Clean(secondPath) {
		t.Fatalf("latest = %q, want %q", latest, secondPath)
	}
}

func TestResolveLatestRunDiagSocketPathNoSocketCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runDir := filepath.Join(home, ".neocode", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	regularFile := filepath.Join(runDir, diagSocketFilePrefix+"not-socket"+diagSocketFileSuffix)
	if err := os.WriteFile(regularFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ResolveLatestRunDiagSocketPath()
	if err == nil || !strings.Contains(err.Error(), "no socket candidate found") {
		t.Fatalf("ResolveLatestRunDiagSocketPath() err = %v, want no socket candidate", err)
	}
}

func TestResolveDiagSocketPathForPIDFallback(t *testing.T) {
	path, err := resolveDiagSocketPathForPID(0)
	if err != nil {
		t.Fatalf("resolveDiagSocketPathForPID() error = %v", err)
	}
	if !strings.Contains(path, diagSocketFilePrefix) || !strings.HasSuffix(path, diagSocketFileSuffix) {
		t.Fatalf("path = %q, want diag socket naming", path)
	}
}

func TestResolveDiagSocketPathForPIDExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := resolveDiagSocketPathForPID(12345)
	if err != nil {
		t.Fatalf("resolveDiagSocketPathForPID() error = %v", err)
	}
	if !strings.Contains(path, diagSocketFilePrefix+"12345"+diagSocketFileSuffix) {
		t.Fatalf("path = %q, want contains explicit pid suffix", path)
	}
}

func TestFindLatestSocketByPatternGlobError(t *testing.T) {
	_, err := findLatestSocketByPattern(t.TempDir(), "[")
	if err == nil || !strings.Contains(err.Error(), "glob diag socket path") {
		t.Fatalf("err = %v, want glob error", err)
	}
}

func TestResolveLatestRunDiagSocketPathMissingRunDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := ResolveLatestRunDiagSocketPath()
	if err == nil || !strings.Contains(err.Error(), "no diag socket found") {
		t.Fatalf("err = %v, want no diag socket found", err)
	}
}

func TestResolveLegacyTmpDiagSocketPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	socketPath := filepath.Join(tmp, diagSocketFilePrefix+"legacy"+diagSocketFileSuffix)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	legacy, err := ResolveLegacyTmpDiagSocketPath()
	if err != nil {
		t.Fatalf("ResolveLegacyTmpDiagSocketPath() error = %v", err)
	}
	if filepath.Clean(legacy) != filepath.Clean(socketPath) {
		t.Fatalf("legacy = %q, want %q", legacy, socketPath)
	}
}

func TestResolveLegacyTmpDiagSocketPathForPID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	firstPath := filepath.Join(tmp, diagSocketFilePrefix+"34567"+diagSocketFileSuffix)
	firstListener, err := net.Listen("unix", firstPath)
	if err != nil {
		t.Fatalf("net.Listen(first) error = %v", err)
	}
	defer firstListener.Close()

	secondPath := filepath.Join(tmp, diagSocketFilePrefix+"45678"+diagSocketFileSuffix)
	secondListener, err := net.Listen("unix", secondPath)
	if err != nil {
		t.Fatalf("net.Listen(second) error = %v", err)
	}
	defer secondListener.Close()

	legacy, err := ResolveLegacyTmpDiagSocketPathForPID(45678)
	if err != nil {
		t.Fatalf("ResolveLegacyTmpDiagSocketPathForPID() error = %v", err)
	}
	if filepath.Clean(legacy) != filepath.Clean(secondPath) {
		t.Fatalf("legacy = %q, want %q", legacy, secondPath)
	}
}

func TestResolveLegacyTmpDiagSocketPathForPIDRejectsInvalidPID(t *testing.T) {
	_, err := ResolveLegacyTmpDiagSocketPathForPID(0)
	if err == nil || !strings.Contains(err.Error(), "invalid diag socket pid") {
		t.Fatalf("err = %v, want invalid diag socket pid", err)
	}
}

func TestParseDiagSocketPIDFromPath(t *testing.T) {
	pid, err := parseDiagSocketPIDFromPath("/tmp/" + diagSocketFilePrefix + "12345" + diagSocketFileSuffix)
	if err != nil {
		t.Fatalf("parseDiagSocketPIDFromPath() error = %v", err)
	}
	if pid != 12345 {
		t.Fatalf("pid = %d, want 12345", pid)
	}
}

func TestParseDiagSocketPIDFromPathRejectsInvalidName(t *testing.T) {
	_, err := parseDiagSocketPIDFromPath("/tmp/diag.sock")
	if err == nil || !strings.Contains(err.Error(), "diag socket filename is invalid") {
		t.Fatalf("err = %v, want invalid filename error", err)
	}
}
