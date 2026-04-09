package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentruntime "neo-code/internal/runtime"
)

type stubSessionWorkdirSetter struct {
	result agentruntime.WorkspaceSwitchResult
	err    error
	calls  int
}

func (s *stubSessionWorkdirSetter) SwitchWorkspace(ctx context.Context, input agentruntime.WorkspaceSwitchInput) (agentruntime.WorkspaceSwitchResult, error) {
	s.calls++
	if s.err != nil {
		return agentruntime.WorkspaceSwitchResult{}, s.err
	}
	return s.result, nil
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
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "/bad", parse)
		if result.Err == nil {
			t.Fatalf("expected parse error")
		}
	})

	t.Run("empty requested without current workdir", func(t *testing.T) {
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", "", "/cwd", parse)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "usage: /cwd <path>") {
			t.Fatalf("expected usage error, got %+v", result)
		}
	})

	t.Run("empty requested with current workdir", func(t *testing.T) {
		current := t.TempDir()
		result := ExecuteSessionWorkdirCommand(&stubSessionWorkdirSetter{}, "", current, "/cwd", parse)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current || !strings.Contains(result.Notice, "Current workspace is") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("draft session resolves requested path", func(t *testing.T) {
		base := t.TempDir()
		stub := &stubSessionWorkdirSetter{result: agentruntime.WorkspaceSwitchResult{Workdir: base}}
		result := ExecuteSessionWorkdirCommand(stub, "", base, "/cwd sub", parse)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !strings.Contains(result.Notice, "Draft workspace switched") {
			t.Fatalf("unexpected notice: %q", result.Notice)
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &stubSessionWorkdirSetter{err: errors.New("set workdir failed")}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", t.TempDir(), "/cwd sub", parse)
		if result.Err == nil || !strings.Contains(result.Err.Error(), "set workdir failed") {
			t.Fatalf("expected runtime error, got %+v", result)
		}
	})

	t.Run("runtime empty workdir fallback", func(t *testing.T) {
		current := t.TempDir()
		stub := &stubSessionWorkdirSetter{result: agentruntime.WorkspaceSwitchResult{Workdir: current}}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", current, "/cwd sub", parse)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Workdir != current {
			t.Fatalf("expected fallback workdir %q, got %q", current, result.Workdir)
		}
	})

	t.Run("cross workspace resets to draft", func(t *testing.T) {
		workdir := t.TempDir()
		stub := &stubSessionWorkdirSetter{result: agentruntime.WorkspaceSwitchResult{
			Workdir:          workdir,
			WorkspaceRoot:    workdir,
			WorkspaceChanged: true,
			ResetToDraft:     true,
		}}
		result := ExecuteSessionWorkdirCommand(stub, "session-1", t.TempDir(), "/cwd "+workdir, parse)
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if !result.ResetToDraft || !result.WorkspaceChanged {
			t.Fatalf("expected reset-to-draft result, got %+v", result)
		}
		if !strings.Contains(result.Notice, "Started a new draft") {
			t.Fatalf("unexpected notice: %q", result.Notice)
		}
	})
}
