//go:build !windows

package ptyproxy

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathCoverageGapResolveLatestRunAndFind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runDir := filepath.Join(home, ".neocode", "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	socketPath := filepath.Join(runDir, diagSocketFilePrefix+"30001"+diagSocketFileSuffix)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	got, err := ResolveLatestRunDiagSocketPath()
	if err != nil {
		t.Fatalf("ResolveLatestRunDiagSocketPath() error = %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(socketPath) {
		t.Fatalf("got = %q, want %q", got, socketPath)
	}
}

func TestSocketPathCoverageGapResolvePIDAndFindFunctionEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := resolveDiagSocketPathForPID(0)
	if err != nil {
		t.Fatalf("resolveDiagSocketPathForPID() error = %v", err)
	}
	if !strings.Contains(path, diagSocketFilePrefix) || !strings.HasSuffix(path, diagSocketFileSuffix) {
		t.Fatalf("path = %q, want diag socket naming", path)
	}

	root := t.TempDir()
	socketPath := filepath.Join(root, diagSocketFilePrefix+"70001"+diagSocketFileSuffix)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	found, err := findLatestSocketByPattern(root, diagSocketFilePrefix+"*"+diagSocketFileSuffix)
	if err != nil {
		t.Fatalf("findLatestSocketByPattern() error = %v", err)
	}
	if filepath.Clean(found) != filepath.Clean(socketPath) {
		t.Fatalf("found = %q, want %q", found, socketPath)
	}
}
