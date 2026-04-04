package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandMenuItemAndDelegateHelpers(t *testing.T) {
	item := commandMenuItem{
		title:       "/status",
		description: "show status",
		filter:      " custom FILTER ",
	}
	if item.Title() != "/status" || item.Description() != "show status" {
		t.Fatalf("unexpected title/description: %+v", item)
	}
	if got := item.FilterValue(); got != "custom filter" {
		t.Fatalf("expected trimmed lowercase filter, got %q", got)
	}

	item.filter = ""
	if got := item.FilterValue(); got != "/status show status" {
		t.Fatalf("expected fallback filter text, got %q", got)
	}

	delegate := commandMenuDelegate{styles: newStyles()}
	if delegate.Height() != 1 || delegate.Spacing() != 0 {
		t.Fatalf("unexpected delegate size: height=%d spacing=%d", delegate.Height(), delegate.Spacing())
	}
	if cmd := delegate.Update(nil, nil); cmd != nil {
		t.Fatalf("expected nil update cmd, got %v", cmd)
	}
}

func TestCommandMenuBehaviorPaths(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.state.CurrentWorkdir = t.TempDir()
	app.fileCandidates = []string{"internal/tui/update.go", "internal/tui/view.go"}
	app.input.SetValue("inspect @")
	app.state.InputText = app.input.Value()
	app.refreshCommandMenu()
	if !app.commandMenuHasSuggestions() || app.commandMenuMeta.Title != fileMenuTitle {
		t.Fatalf("expected file menu suggestions, meta=%+v items=%d", app.commandMenuMeta, len(app.commandMenu.Items()))
	}
	if !app.applySelectedCommandSuggestion() {
		t.Fatalf("expected browse suggestion to open file browser")
	}
	if app.state.ActivePicker != pickerFile {
		t.Fatalf("expected pickerFile after browse suggestion, got %v", app.state.ActivePicker)
	}

	app.closePicker()
	app.input.SetValue("/")
	app.state.InputText = app.input.Value()
	app.refreshCommandMenu()
	if len(app.commandMenu.Items()) == 0 {
		t.Fatalf("expected slash command menu items")
	}
	if len(app.commandMenu.Items()) > 1 {
		selectedTitle := app.commandMenu.Items()[1].(commandMenuItem).title
		app.commandMenu.Select(1)
		app.refreshCommandMenu()
		currentTitle := app.commandMenu.SelectedItem().(commandMenuItem).title
		if currentTitle != selectedTitle {
			t.Fatalf("expected selection retained, got %q want %q", currentTitle, selectedTitle)
		}
	}

	if _, handled := app.updateCommandMenuSelection(tea.KeyMsg{Type: tea.KeyDown}); !handled {
		t.Fatalf("expected key down to be handled")
	}
	if _, handled := app.updateCommandMenuSelection(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}); handled {
		t.Fatalf("expected rune key not to be handled by command menu")
	}

	app.state.ActivePicker = pickerModel
	app.refreshCommandMenu()
	if app.commandMenuHasSuggestions() || strings.TrimSpace(app.commandMenuMeta.Title) != "" {
		t.Fatalf("expected menu cleared while picker active")
	}
	app.state.ActivePicker = pickerNone

	app.commandMenu.SetItems([]list.Item{selectionItem{id: "x", name: "not command item"}})
	app.commandMenu.Select(0)
	if app.applySelectedCommandSuggestion() {
		t.Fatalf("expected false when selected item type is invalid")
	}

	app.input.SetValue("abc")
	app.state.InputText = app.input.Value()
	app.commandMenu.SetItems([]list.Item{commandMenuItem{
		replacement:     "ignored",
		useReplaceRange: true,
		replaceStart:    -1,
		replaceEnd:      1,
	}})
	app.commandMenu.Select(0)
	if app.applySelectedCommandSuggestion() {
		t.Fatalf("expected false for invalid replace range")
	}

	app.commandMenu.SetItems([]list.Item{commandMenuItem{replacement: "/status"}})
	app.commandMenu.Select(0)
	if !app.applySelectedCommandSuggestion() || app.state.InputText != "/status" {
		t.Fatalf("expected direct replacement, got %q", app.state.InputText)
	}
	app.commandMenu.SetItems([]list.Item{commandMenuItem{replacement: "/status"}})
	app.commandMenu.Select(0)
	if app.applySelectedCommandSuggestion() {
		t.Fatalf("expected no-op replacement to return false")
	}
}

func TestBuildCommandMenuItemsVariants(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.state.CurrentWorkdir = t.TempDir()

	items, meta := app.buildCommandMenuItems("&", 80)
	if len(items) != 1 || meta.Title != shellMenuTitle || !items[0].useReplaceRange {
		t.Fatalf("expected bare workspace command helper, got meta=%+v items=%+v", meta, items)
	}

	items, meta = app.buildCommandMenuItems("& git status", 80)
	if len(items) != 1 || meta.Title != shellMenuTitle || items[0].useReplaceRange {
		t.Fatalf("expected concrete workspace command helper, got meta=%+v items=%+v", meta, items)
	}

	items, meta = app.buildCommandMenuItems("not-a-command", 80)
	if len(items) != 0 || strings.TrimSpace(meta.Title) != "" {
		t.Fatalf("expected no suggestions for plain input, got meta=%+v items=%+v", meta, items)
	}

	app.fileCandidates = []string{"internal/tui/update.go"}
	fileItems := app.fileMenuSuggestions("inspect @")
	if len(fileItems) == 0 || !fileItems[0].openFileBrowser {
		t.Fatalf("expected browse item for empty file query, got %+v", fileItems)
	}

	app.fileCandidates = nil
	if suggestions := app.fileMenuSuggestions("inspect @missing"); len(suggestions) != 0 {
		t.Fatalf("expected empty suggestions when query misses all candidates, got %+v", suggestions)
	}

	app.state.CurrentWorkdir = ""
	app.openFileBrowser()
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected openFileBrowser to no-op when workdir is empty")
	}
}
