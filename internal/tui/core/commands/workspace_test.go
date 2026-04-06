package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "neo-code/internal/runtime"
	tuiworkspace "neo-code/internal/tui/core/workspace"
)

type stubSessionWorkdirSetter struct {
	session agentruntime.Session
	err     error
	calls   int
}

func (s *stubSessionWorkdirSetter) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentruntime.Session, error) {
	s.calls++
	if s.err != nil {
		return agentruntime.Session{}, s.err
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
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "/bad", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil {
			t.Fatalf("expected parse error")
		}
	})

	t.Run("empty requested without current workdir", func(t *testing.T) {
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "/cwd", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "usage: /cwd <path>") {
			t.Fatalf("expected usage error, got %+v", result)
		}
	})

	t.Run("empty requested with current workdir", func(t *testing.T) {
		current := t.TempDir()
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", current, "/cwd", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
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
		if err := ensureDir(target); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", base, "/cwd sub", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !strings.Contains(result.Notice, "Draft workspace switched") {
			t.Fatalf("unexpected notice: %q", result.Notice)
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &stubSessionWorkdirSetter{err: errors.New("set workdir failed")}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", t.TempDir(), "/cwd sub", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "set workdir failed") {
			t.Fatalf("expected runtime error, got %+v", result)
		}
	})

	t.Run("runtime empty workdir fallback", func(t *testing.T) {
		current := t.TempDir()
		stub := &stubSessionWorkdirSetter{session: agentruntime.Session{ID: "session-1", Workdir: ""}}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", current, "/cwd sub", parse, tuiworkspace.ResolveWorkspacePath, tuiworkspace.SelectSessionWorkdir)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current {
			t.Fatalf("expected fallback workdir %q, got %q", current, result.Workdir)
		}
	})
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
