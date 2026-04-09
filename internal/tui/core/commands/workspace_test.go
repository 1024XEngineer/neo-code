package commands

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	tuiworkspace "neo-code/internal/tui/core/workspace"
)

func TestExecuteWorkspaceSwitchCommand(t *testing.T) {
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
		result := ExecuteWorkspaceSwitchCommand("", "", "/bad", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err == nil {
			t.Fatalf("expected parse error")
		}
	})

	t.Run("empty requested without current workdir", func(t *testing.T) {
		result := ExecuteWorkspaceSwitchCommand("", "", "/cwd", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "usage: /cwd <path>") {
			t.Fatalf("expected usage error, got %+v", result)
		}
	})

	t.Run("empty requested with current workdir", func(t *testing.T) {
		current := t.TempDir()
		result := ExecuteWorkspaceSwitchCommand(current, current, "/cwd", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current || !strings.Contains(result.Notice, "Current workspace is") {
			t.Fatalf("unexpected result: %+v", result)
		}
		if result.Relaunch {
			t.Fatalf("expected no relaunch for query only")
		}
	})

	t.Run("requested path resolves to relaunch target", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "sub")
		if err := ensureDir(target); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}

		result := ExecuteWorkspaceSwitchCommand(base, base, "/cwd sub", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !result.Relaunch || result.Workdir != target {
			t.Fatalf("expected relaunch to %q, got %+v", target, result)
		}
	})

	t.Run("requested path matching startup workdir is noop", func(t *testing.T) {
		startup := t.TempDir()
		current := filepath.Join(startup, "session-subdir")
		if err := ensureDir(current); err != nil {
			t.Fatalf("mkdir current: %v", err)
		}

		result := ExecuteWorkspaceSwitchCommand(current, startup, "/cwd .", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Relaunch {
			t.Fatalf("expected noop when switching to startup workdir, got %+v", result)
		}
		if result.Workdir != startup {
			t.Fatalf("expected startup workdir %q, got %q", startup, result.Workdir)
		}
		if !strings.Contains(result.Notice, "Workspace already set") {
			t.Fatalf("unexpected notice: %q", result.Notice)
		}
	})

	t.Run("relative path resolves from startup workdir", func(t *testing.T) {
		startup := t.TempDir()
		current := filepath.Join(startup, "session-subdir")
		target := filepath.Join(startup, "next-workspace")
		if err := ensureDir(current); err != nil {
			t.Fatalf("mkdir current: %v", err)
		}
		if err := ensureDir(target); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}

		result := ExecuteWorkspaceSwitchCommand(current, startup, "/cwd next-workspace", parse, tuiworkspace.ResolveWorkspacePath)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !result.Relaunch || result.Workdir != target {
			t.Fatalf("expected startup-relative relaunch to %q, got %+v", target, result)
		}
	})
}

func TestSameWorkspacePath(t *testing.T) {
	left := filepath.Clean("/tmp/project")
	right := filepath.Clean("/tmp/project")
	if goruntime.GOOS == "windows" {
		left = `C:\Repo`
		right = `c:\repo`
	}
	if !sameWorkspacePath(left, right) {
		t.Fatalf("expected %q and %q to match", left, right)
	}
	if sameWorkspacePath(left, "") {
		t.Fatalf("expected blank path to never match")
	}
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
