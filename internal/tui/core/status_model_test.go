package core

import (
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestSetThinkingStatusStartsSpinnerAndSyncsUIState(t *testing.T) {
	m := newTestModel(t, &fakeChatClient{})

	cmd := m.setThinkingStatus()
	if cmd == nil {
		t.Fatal("expected spinner tick command")
	}
	if !m.statusBusy() {
		t.Fatal("expected busy status after thinking starts")
	}
	if m.ui.StatusText != "Thinking..." {
		t.Fatalf("expected ui status text to sync, got %q", m.ui.StatusText)
	}

	msg := cmd()
	if _, ok := msg.(spinner.TickMsg); !ok {
		t.Fatalf("expected spinner.TickMsg, got %T", msg)
	}

	updated, next := m.Update(msg)
	got := updated.(Model)
	if !got.statusBusy() {
		t.Fatal("expected busy status to remain active after spinner tick")
	}
	if next == nil {
		t.Fatal("expected follow-up spinner command")
	}
}

func TestSetNoticeStatusUsesCopyCompatibilityField(t *testing.T) {
	m := newTestModel(t, &fakeChatClient{})

	m.setNoticeStatus("Copied block")
	if m.ui.CopyStatus != "Copied block" {
		t.Fatalf("expected copy compatibility field to sync, got %q", m.ui.CopyStatus)
	}
	if m.ui.StatusText != "" {
		t.Fatalf("expected status text to stay empty for notice status, got %q", m.ui.StatusText)
	}

	m.clearNotices()
	if m.ui.CopyStatus != "" || m.ui.StatusText != "" {
		t.Fatalf("expected transient notice fields to clear, got copy=%q status=%q", m.ui.CopyStatus, m.ui.StatusText)
	}
}
