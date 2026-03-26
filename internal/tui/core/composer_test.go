package core

import (
	"strings"
	"testing"

	"go-llm-demo/internal/tui/state"
)

func TestNewComposerUsesShiftEnterForNewline(t *testing.T) {
	composer := newComposerModel()
	keys := composer.KeyMap.InsertNewline.Keys()

	want := map[string]bool{
		"shift+enter": false,
		"alt+enter":   false,
		"ctrl+j":      false,
	}
	for _, key := range keys {
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}

	for key, seen := range want {
		if !seen {
			t.Fatalf("expected newline keymap to include %q, got %+v", key, keys)
		}
	}

	if composer.Height() != 1 {
		t.Fatalf("expected single-line composer to reserve 1 row, got %d", composer.Height())
	}
}

func TestComposerDesiredHeightUsesCompactSingleLineLayout(t *testing.T) {
	composer := newComposerModel()

	if got := composer.desiredHeight(); got != 1 {
		t.Fatalf("expected empty composer height 1, got %d", got)
	}

	composer.SetValue("hello")
	if got := composer.desiredHeight(); got != 1 {
		t.Fatalf("expected single-line draft height 1, got %d", got)
	}

	composer.SetValue("hello\nworld")
	if got := composer.desiredHeight(); got != 2 {
		t.Fatalf("expected two-line draft height 2, got %d", got)
	}

	composer.SetValue("a\nb\nc")
	if got := composer.desiredHeight(); got != 3 {
		t.Fatalf("expected three-line draft height 3, got %d", got)
	}
}

func TestVisibleComposerBodyKeepsOnlyVisibleTrailingLines(t *testing.T) {
	body := "works\n> nihao\n"
	if got := visibleComposerBody(body, 1); got != "> nihao" {
		t.Fatalf("expected only the visible prompt line, got %q", got)
	}
	if got := visibleComposerBody(body, 2); got != "works\n> nihao" {
		t.Fatalf("expected the last two lines, got %q", got)
	}
}

func TestComposerConsoleStateUsesCommandModeForSlashDraft(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 4, "config.yaml", "D:/neo-code")
	m.chat.APIKeyReady = true
	m.textarea.SetValue("/todo add fix viewport")

	state := m.composerConsoleState()
	if state.modeLabel != "COMMAND" {
		t.Fatalf("expected COMMAND mode, got %+v", state)
	}
	if state.noteText == "" || state.noteText == "Message mode. Enter sends immediately; Shift+Enter inserts a newline." {
		t.Fatalf("expected command-specific note, got %+v", state)
	}
}

func TestComposerConsoleStateUsesRecoveryModeWhenAPIKeyNotReady(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 4, "config.yaml", "D:/neo-code")
	m.chat.APIKeyReady = false
	m.textarea.SetValue("hello")

	state := m.composerConsoleState()
	if state.modeLabel != "RECOVERY" {
		t.Fatalf("expected RECOVERY mode, got %+v", state)
	}
	if !strings.Contains(state.noteText, "Available now:") {
		t.Fatalf("expected recovery guidance, got %+v", state)
	}
}

func TestComposerConsoleStateUsesApprovalModeBeforePlainMessage(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 4, "config.yaml", "D:/neo-code")
	m.chat.APIKeyReady = true
	m.chat.PendingApproval = &state.PendingApproval{}
	m.textarea.SetValue("continue")

	state := m.composerConsoleState()
	if state.modeLabel != "APPROVAL" {
		t.Fatalf("expected APPROVAL mode, got %+v", state)
	}
	if !strings.Contains(state.noteText, "Approval is pending") {
		t.Fatalf("expected approval guidance, got %+v", state)
	}
}
