package core

import (
	"testing"

	"go-llm-demo/internal/tui/services"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTodoPanelRefreshClampsCursor(t *testing.T) {
	client := &fakeChatClient{
		todos: []services.Todo{
			{ID: "1", Content: "one"},
			{ID: "2", Content: "two"},
		},
	}

	panel := newTodoPanelModel(client)
	panel.cursor = 5
	if err := panel.refresh(); err != nil {
		t.Fatalf("expected refresh to succeed, got %v", err)
	}

	if len(panel.items()) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(panel.items()))
	}
	if panel.selectedIndex() != 1 {
		t.Fatalf("expected cursor to clamp to last item, got %d", panel.selectedIndex())
	}
}

func TestTodoPanelUpdateTogglesStatusAndRefreshes(t *testing.T) {
	client := &fakeChatClient{
		todos: []services.Todo{
			{ID: "1", Content: "one", Status: services.TodoPending},
		},
	}

	panel := newTodoPanelModel(client)
	if err := panel.refresh(); err != nil {
		t.Fatalf("expected refresh to succeed, got %v", err)
	}

	action, err := panel.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if err != nil {
		t.Fatalf("expected toggle to succeed, got %v", err)
	}
	if action != todoPanelRefreshed {
		t.Fatalf("expected refreshed action, got %v", action)
	}
	if got := panel.items()[0].Status; got != services.TodoInProgress {
		t.Fatalf("expected toggled status to be in progress, got %q", got)
	}
}

func TestTodoPanelUpdateDeleteRefreshesAndAdjustsCursor(t *testing.T) {
	client := &fakeChatClient{
		todos: []services.Todo{
			{ID: "1", Content: "one"},
			{ID: "2", Content: "two"},
		},
	}

	panel := newTodoPanelModel(client)
	if err := panel.refresh(); err != nil {
		t.Fatalf("expected refresh to succeed, got %v", err)
	}
	panel.cursor = 1

	action, err := panel.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if err != nil {
		t.Fatalf("expected delete to succeed, got %v", err)
	}
	if action != todoPanelRefreshed {
		t.Fatalf("expected refreshed action, got %v", action)
	}
	if len(panel.items()) != 1 {
		t.Fatalf("expected 1 todo after delete, got %d", len(panel.items()))
	}
	if panel.selectedIndex() != 0 {
		t.Fatalf("expected cursor to move back to 0, got %d", panel.selectedIndex())
	}
}

func TestTodoPanelUpdateReturnsPromptAddAction(t *testing.T) {
	panel := newTodoPanelModel(&fakeChatClient{})

	action, err := panel.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if err != nil {
		t.Fatalf("expected add shortcut to succeed, got %v", err)
	}
	if action != todoPanelPromptAdd {
		t.Fatalf("expected prompt-add action, got %v", action)
	}
}
