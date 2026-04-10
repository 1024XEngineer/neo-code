package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentsession "neo-code/internal/session"
	tuiworkspace "neo-code/internal/tui/core/workspace"
	agentworkspace "neo-code/internal/workspace"
)

type stubSessionWorkdirSetter struct {
	session agentsession.Session
	err     error
	calls   int
}

func (s *stubSessionWorkdirSetter) UpdateSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	s.calls++
	if s.err != nil {
		return agentsession.Session{}, s.err
	}
	return s.session, nil
}

func TestExecuteSessionWorkdirCommand(t *testing.T) {
	parse := func(raw string) (string, error) {
		raw = strings.TrimSpace(raw)
		if raw == "/bad" {
			return "", errors.New("unknown command")
		}
		if raw == "/cwd" {
			return "", nil
		}
		if strings.HasPrefix(raw, "/cwd ") {
			return strings.TrimSpace(strings.TrimPrefix(raw, "/cwd ")), nil
		}
		return "", errors.New("unknown command")
	}

	t.Run("parse error", func(t *testing.T) {
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "", "/bad", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil {
			t.Fatalf("expected parse error")
		}
	})

	t.Run("empty requested without current workdir", func(t *testing.T) {
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "", "/cwd", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "usage: /cwd <path>") {
			t.Fatalf("expected usage error, got %+v", result)
		}
	})

	t.Run("empty requested with current workdir", func(t *testing.T) {
		current := t.TempDir()
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", current, current, "/cwd", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current || !strings.Contains(result.Notice, "Current workspace is") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("draft session resolves requested path", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "sub")
		if err := ensureDir(filepath.Join(base, ".git")); err != nil {
			t.Fatalf("mkdir git marker: %v", err)
		}
		if err := ensureDir(target); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", base, base, "/cwd sub", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !strings.Contains(result.Notice, "Draft workspace switched") {
			t.Fatalf("unexpected notice: %q", result.Notice)
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &stubSessionWorkdirSetter{err: errors.New("set workdir failed")}
		workdir := t.TempDir()
		if err := ensureDir(filepath.Join(workdir, ".git")); err != nil {
			t.Fatalf("mkdir git marker: %v", err)
		}
		if err := ensureDir(filepath.Join(workdir, "sub")); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", workdir, workdir, "/cwd sub", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "set workdir failed") {
			t.Fatalf("expected runtime error, got %+v", result)
		}
	})

	t.Run("runtime empty workdir fallback", func(t *testing.T) {
		current := t.TempDir()
		if err := ensureDir(filepath.Join(current, ".git")); err != nil {
			t.Fatalf("mkdir git marker: %v", err)
		}
		if err := ensureDir(filepath.Join(current, "sub")); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		stub := &stubSessionWorkdirSetter{session: agentsession.Session{ID: "session-1", Workdir: ""}}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", current, current, "/cwd sub", parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current {
			t.Fatalf("expected fallback workdir %q, got %q", current, result.Workdir)
		}
	})

	t.Run("cross workspace requests rebuild", func(t *testing.T) {
		current := t.TempDir()
		other := t.TempDir()
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", current, current, "/cwd "+other, parse, agentworkspace.ResolveFrom, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !result.RequiresRebuild {
			t.Fatalf("expected rebuild request")
		}
	})
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
