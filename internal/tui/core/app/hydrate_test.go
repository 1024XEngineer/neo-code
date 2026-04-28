package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestHydrateSessionLoadsHistoryAndWorkdir(t *testing.T) {
	app, runtime := newTestApp(t)

	sessionWorkdir := t.TempDir()
	runtime.loadSessions = map[string]agentsession.Session{
		"session-hydrate": {
			ID:      "session-hydrate",
			Title:   "Hydrated Session",
			Workdir: sessionWorkdir,
			Messages: []providertypes.Message{
				{
					Role:  roleUser,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello hydrate")},
				},
			},
		},
	}

	if err := app.HydrateSession(context.Background(), "session-hydrate"); err != nil {
		t.Fatalf("HydrateSession() error = %v", err)
	}
	if app.state.ActiveSessionID != "session-hydrate" {
		t.Fatalf("active session id = %q, want %q", app.state.ActiveSessionID, "session-hydrate")
	}
	if app.state.ActiveSessionTitle != "Hydrated Session" {
		t.Fatalf("active session title = %q, want %q", app.state.ActiveSessionTitle, "Hydrated Session")
	}
	if len(app.activeMessages) != 1 || messageText(app.activeMessages[0]) != "hello hydrate" {
		t.Fatalf("active messages = %#v, want one hydrated message", app.activeMessages)
	}
	if app.state.CurrentWorkdir != sessionWorkdir {
		t.Fatalf("current workdir = %q, want %q", app.state.CurrentWorkdir, sessionWorkdir)
	}
	if app.startupScreenLocked {
		t.Fatal("expected startup screen to be unlocked after hydration")
	}
}

func TestHydrateSessionKeepsCurrentWorkdirWhenSessionPathMissing(t *testing.T) {
	app, runtime := newTestApp(t)
	originalWorkdir := app.state.CurrentWorkdir

	missingWorkdir := filepath.Join(t.TempDir(), "missing")
	runtime.loadSessions = map[string]agentsession.Session{
		"session-missing-workdir": {
			ID:      "session-missing-workdir",
			Title:   "Missing Workdir",
			Workdir: missingWorkdir,
		},
	}

	if err := app.HydrateSession(context.Background(), "session-missing-workdir"); err != nil {
		t.Fatalf("HydrateSession() error = %v", err)
	}
	if app.state.CurrentWorkdir != originalWorkdir {
		t.Fatalf("current workdir = %q, want keep %q", app.state.CurrentWorkdir, originalWorkdir)
	}
	if !strings.Contains(app.footerErrorText, sessionWorkdirMissingWarning) {
		t.Fatalf("footer warning = %q, want contains %q", app.footerErrorText, sessionWorkdirMissingWarning)
	}
}

func TestHydrateSessionRejectsEmptySessionID(t *testing.T) {
	app, _ := newTestApp(t)
	if err := app.HydrateSession(context.Background(), "   "); err == nil {
		t.Fatal("expected empty session id error")
	}
}

func TestHydrateSessionReturnsLoadError(t *testing.T) {
	app, runtime := newTestApp(t)
	runtime.loadSessionErr = errors.New("load failed")

	err := app.HydrateSession(context.Background(), "session-load-failed")
	if err == nil || !strings.Contains(err.Error(), "load failed") {
		t.Fatalf("HydrateSession() error = %v, want contains %q", err, "load failed")
	}
}
