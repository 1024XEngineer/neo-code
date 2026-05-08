package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tuiservices "neo-code/internal/tui/services"
)

type checkpointRuntimeStub struct {
	tuiservices.Runtime

	listInput  tuiservices.CheckpointListInput
	listResult []tuiservices.CheckpointEntry
	listErr    error

	restoreInput  tuiservices.CheckpointRestoreInput
	restoreResult tuiservices.CheckpointRestoreResult
	restoreErr    error

	undoSessionID string
	undoResult    tuiservices.CheckpointRestoreResult
	undoErr       error

	diffSessionID string
	diffID        string
	diffResult    tuiservices.CheckpointDiffResult
	diffErr       error
}

func (s *checkpointRuntimeStub) ListCheckpoints(
	_ context.Context,
	input tuiservices.CheckpointListInput,
) ([]tuiservices.CheckpointEntry, error) {
	s.listInput = input
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]tuiservices.CheckpointEntry(nil), s.listResult...), nil
}

func (s *checkpointRuntimeStub) RestoreCheckpoint(
	_ context.Context,
	input tuiservices.CheckpointRestoreInput,
) (tuiservices.CheckpointRestoreResult, error) {
	s.restoreInput = input
	if s.restoreErr != nil {
		return tuiservices.CheckpointRestoreResult{}, s.restoreErr
	}
	return s.restoreResult, nil
}

func (s *checkpointRuntimeStub) UndoRestoreCheckpoint(
	_ context.Context,
	sessionID string,
) (tuiservices.CheckpointRestoreResult, error) {
	s.undoSessionID = strings.TrimSpace(sessionID)
	if s.undoErr != nil {
		return tuiservices.CheckpointRestoreResult{}, s.undoErr
	}
	return s.undoResult, nil
}

func (s *checkpointRuntimeStub) CheckpointDiff(
	_ context.Context,
	sessionID string,
	checkpointID string,
) (tuiservices.CheckpointDiffResult, error) {
	s.diffSessionID = strings.TrimSpace(sessionID)
	s.diffID = strings.TrimSpace(checkpointID)
	if s.diffErr != nil {
		return tuiservices.CheckpointDiffResult{}, s.diffErr
	}
	return s.diffResult, nil
}

func TestHandleCheckpointCommandRequiresActiveSession(t *testing.T) {
	app, _ := newTestApp(t)
	handled, cmd := app.handleImmediateSlashCommand("/checkpoint")
	if !handled {
		t.Fatalf("expected /checkpoint to be recognized")
	}
	if cmd != nil {
		t.Fatalf("expected no cmd when active session is missing")
	}
	if !strings.Contains(app.state.StatusText, "requires an active session") {
		t.Fatalf("expected active session hint, got %q", app.state.StatusText)
	}
}

func TestHandleCheckpointCommandUnsupportedRuntime(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	handled, cmd := app.handleImmediateSlashCommand("/checkpoint")
	if !handled {
		t.Fatalf("expected /checkpoint to be recognized")
	}
	if cmd != nil {
		t.Fatalf("expected no cmd when checkpoint runtime is unavailable")
	}
	if !strings.Contains(app.state.StatusText, "unavailable") {
		t.Fatalf("expected unavailable hint, got %q", app.state.StatusText)
	}
}

func TestHandleCheckpointSlashCommands(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"

	checkpointRuntime := &checkpointRuntimeStub{
		Runtime: runtime,
		listResult: []tuiservices.CheckpointEntry{
			{CheckpointID: "cp-1", Reason: "pre_write", Status: "ready", Restorable: true, CreatedAtMS: 1700000000000},
		},
		restoreResult: tuiservices.CheckpointRestoreResult{CheckpointID: "cp-1", SessionID: "session-1"},
		undoResult:    tuiservices.CheckpointRestoreResult{CheckpointID: "cp-guard", SessionID: "session-1"},
		diffResult: tuiservices.CheckpointDiffResult{
			CheckpointID: "cp-1",
			Files:        tuiservices.CheckpointDiffFiles{Modified: []string{"a.txt"}},
			Patch:        "diff --git a/a.txt b/a.txt",
		},
	}
	app.runtime = checkpointRuntime

	handled, cmd := app.handleImmediateSlashCommand("/checkpoint")
	if !handled || cmd == nil {
		t.Fatalf("expected /checkpoint to return async cmd")
	}
	model, _ := app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Restorable checkpoints:") {
		t.Fatalf("unexpected list status text: %q", app.state.StatusText)
	}
	if checkpointRuntime.listInput.SessionID != "session-1" || checkpointRuntime.listInput.Limit != 20 || !checkpointRuntime.listInput.RestorableOnly {
		t.Fatalf("unexpected list input: %#v", checkpointRuntime.listInput)
	}

	handled, cmd = app.handleImmediateSlashCommand("/checkpoint restore cp-1")
	if !handled || cmd == nil {
		t.Fatalf("expected /checkpoint restore to return async cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Checkpoint restored: cp-1") {
		t.Fatalf("unexpected restore status text: %q", app.state.StatusText)
	}
	if checkpointRuntime.restoreInput.SessionID != "session-1" || checkpointRuntime.restoreInput.CheckpointID != "cp-1" {
		t.Fatalf("unexpected restore input: %#v", checkpointRuntime.restoreInput)
	}

	handled, cmd = app.handleImmediateSlashCommand("/checkpoint diff cp-1")
	if !handled || cmd == nil {
		t.Fatalf("expected /checkpoint diff to return async cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Checkpoint diff: cp-1") {
		t.Fatalf("unexpected diff status text: %q", app.state.StatusText)
	}
	if checkpointRuntime.diffSessionID != "session-1" || checkpointRuntime.diffID != "cp-1" {
		t.Fatalf("unexpected diff inputs: sid=%q checkpoint=%q", checkpointRuntime.diffSessionID, checkpointRuntime.diffID)
	}

	handled, cmd = app.handleImmediateSlashCommand("/checkpoint undo")
	if !handled || cmd == nil {
		t.Fatalf("expected /checkpoint undo to return async cmd")
	}
	model, _ = app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "Checkpoint restore undo applied") {
		t.Fatalf("unexpected undo status text: %q", app.state.StatusText)
	}
	if checkpointRuntime.undoSessionID != "session-1" {
		t.Fatalf("unexpected undo session id: %q", checkpointRuntime.undoSessionID)
	}
}

func TestHandleCheckpointCommandUsageAndErrorBranches(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	checkpointRuntime := &checkpointRuntimeStub{
		Runtime: runtime,
	}
	app.runtime = checkpointRuntime

	if handled, cmd := app.handleImmediateSlashCommand("/checkpoint restore"); !handled || cmd != nil {
		t.Fatalf("expected restore usage branch")
	}
	if !strings.Contains(app.state.StatusText, slashUsageCheckpointRestore) {
		t.Fatalf("expected restore usage text, got %q", app.state.StatusText)
	}

	if handled, cmd := app.handleImmediateSlashCommand("/checkpoint undo now"); !handled || cmd != nil {
		t.Fatalf("expected undo usage branch")
	}
	if !strings.Contains(app.state.StatusText, slashUsageCheckpointUndo) {
		t.Fatalf("expected undo usage text, got %q", app.state.StatusText)
	}

	checkpointRuntime.listErr = errors.New("list failed")
	handled, cmd := app.handleImmediateSlashCommand("/checkpoint")
	if !handled || cmd == nil {
		t.Fatalf("expected /checkpoint to return cmd")
	}
	model, _ := app.Update(cmd())
	app = model.(App)
	if !strings.Contains(app.state.StatusText, "list failed") {
		t.Fatalf("expected list error passthrough, got %q", app.state.StatusText)
	}
}
