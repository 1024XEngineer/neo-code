package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	agentruntime "neo-code/internal/tui/services"
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

func TestHydrateSessionReplaysFoldRelatedPersistedLogs(t *testing.T) {
	app, runtime := newTestApp(t)
	sessionID := "session-hydrate-logs"
	runtime.loadSessions = map[string]agentsession.Session{
		sessionID: {
			ID:      sessionID,
			Title:   "Hydrated Logs Session",
			Workdir: t.TempDir(),
			Messages: []providertypes.Message{
				{
					Role:  roleUser,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
				},
				{
					Role:  roleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("final answer")},
				},
			},
		},
	}
	runtime.logEntriesBySID[sessionID] = []agentruntime.SessionLogEntry{
		{Timestamp: time.Unix(1_700_020_001, 0), Level: "info", Source: "verify", Message: "Verification started: completion_passed=true"},
		{Timestamp: time.Unix(1_700_020_002, 0), Level: "info", Source: "provider", Message: "Provider switched: openai"},
	}

	if err := app.HydrateSession(context.Background(), sessionID); err != nil {
		t.Fatalf("HydrateSession() error = %v", err)
	}

	joined := ""
	for _, message := range app.activeMessages {
		text := strings.TrimSpace(renderMessagePartsForDisplay(message.Parts))
		if joined != "" {
			joined += "\n"
		}
		joined += text
	}
	if !strings.Contains(joined, inlineLogMarker+"verify: Verification started: completion_passed=true") {
		t.Fatalf("expected verify session log to be replayed into transcript, got %q", joined)
	}
	if strings.Contains(joined, inlineLogMarker+"provider: Provider switched: openai") {
		t.Fatalf("expected non-fold provider log to stay out of transcript replay, got %q", joined)
	}
}

func TestHydrateSessionLogReplaySkipsDuplicateInlineMessages(t *testing.T) {
	app, runtime := newTestApp(t)
	sessionID := "session-hydrate-log-dedup"
	inline := inlineLogMarker + "verify: Verification finished: accepted"
	runtime.loadSessions = map[string]agentsession.Session{
		sessionID: {
			ID:      sessionID,
			Title:   "Hydrated Dedup Session",
			Workdir: t.TempDir(),
			Messages: []providertypes.Message{
				{
					Role:  roleSystem,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart(inline)},
				},
				{
					Role:  roleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("final answer")},
				},
			},
		},
	}
	runtime.logEntriesBySID[sessionID] = []agentruntime.SessionLogEntry{
		{Timestamp: time.Unix(1_700_020_011, 0), Level: "info", Source: "verify", Message: "Verification finished: accepted"},
	}

	if err := app.HydrateSession(context.Background(), sessionID); err != nil {
		t.Fatalf("HydrateSession() error = %v", err)
	}

	count := 0
	for _, message := range app.activeMessages {
		text := strings.TrimSpace(renderMessagePartsForDisplay(message.Parts))
		if text == inline {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected replay dedup to keep one inline message, got %d", count)
	}
}

func TestHydrateSessionReplaysPersistedInlineMessageRegardlessOfSource(t *testing.T) {
	app, runtime := newTestApp(t)
	sessionID := "session-hydrate-inline-source"
	runtime.loadSessions = map[string]agentsession.Session{
		sessionID: {
			ID:      sessionID,
			Title:   "Hydrated Inline Source Session",
			Workdir: t.TempDir(),
			Messages: []providertypes.Message{
				{
					Role:  roleAssistant,
					Parts: []providertypes.ContentPart{providertypes.NewTextPart("final answer")},
				},
			},
		},
	}
	runtime.logEntriesBySID[sessionID] = []agentruntime.SessionLogEntry{
		{
			Timestamp: time.Unix(1_700_020_021, 0),
			Level:     "info",
			Source:    "provider",
			Message:   "Provider switched: openai",
			Inline:    inlineLogMarker + "verify: Verification finished: accepted",
		},
	}

	if err := app.HydrateSession(context.Background(), sessionID); err != nil {
		t.Fatalf("HydrateSession() error = %v", err)
	}
	joined := ""
	for _, message := range app.activeMessages {
		text := strings.TrimSpace(renderMessagePartsForDisplay(message.Parts))
		if joined != "" {
			joined += "\n"
		}
		joined += text
	}
	if !strings.Contains(joined, inlineLogMarker+"verify: Verification finished: accepted") {
		t.Fatalf("expected persisted inline message to be replayed regardless of source, got %q", joined)
	}
}
