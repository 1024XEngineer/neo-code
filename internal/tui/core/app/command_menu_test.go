package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCommandMenuItem(t *testing.T) {
	item := commandMenuItem{
		title:           "Test Command",
		description:     "Test description",
		filter:          "test",
		highlight:       false,
		replacement:     "/test",
		useReplaceRange: false,
		replaceStart:    0,
		replaceEnd:      0,
		openFileBrowser: false,
	}

	if item.Title() != "Test Command" {
		t.Errorf("Title() = %v, want Test Command", item.Title())
	}
	if item.Description() != "Test description" {
		t.Errorf("Description() = %v, want Test description", item.Description())
	}
	if item.FilterValue() != "test" {
		t.Errorf("FilterValue() = %v, want test", item.FilterValue())
	}
}

func TestCommandMenuItemWithEmptyFilter(t *testing.T) {
	item := commandMenuItem{
		title:       "Command",
		description: "Description",
		filter:      "",
	}

	if item.FilterValue() != "command description" {
		t.Errorf("FilterValue() = %v, want command description", item.FilterValue())
	}
}

func TestCommandMenuItemFilterValueCase(t *testing.T) {
	item := commandMenuItem{
		title:       "UPPERCASE",
		description: "Description",
		filter:      "lowercase",
	}

	if !strings.Contains(item.FilterValue(), "lowercase") {
		t.Errorf("FilterValue() should contain lowercase, got %v", item.FilterValue())
	}
}

func TestSelectionItem(t *testing.T) {
	item := selectionItem{
		id:          "test-id",
		name:        "Test Name",
		description: "Test description",
	}

	if item.Title() != "Test Name" {
		t.Errorf("Title() = %v, want Test Name", item.Title())
	}
	if item.Description() != "Test description" {
		t.Errorf("Description() = %v, want Test description", item.Description())
	}
	if !strings.Contains(item.FilterValue(), "test-id") {
		t.Errorf("FilterValue() should contain test-id, got %v", item.FilterValue())
	}
}

func TestCommandMenuView(t *testing.T) {
	styles := newStyles()
	model := newCommandMenuModel(styles)

	v := model.View()
	if v == "" {
		t.Error("View() returned empty string")
	}
}

func TestBuildCommandMenuItemsForWorkspaceCommand(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentWorkdir = "/workspace/root"

	items, meta := app.buildCommandMenuItems("&", 80)
	if meta.Title != shellMenuTitle {
		t.Fatalf("expected shell menu title, got %q", meta.Title)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if !items[0].useReplaceRange || items[0].replacement != workspaceCommandPrefix+" " {
		t.Fatalf("expected workspace replace range")
	}
}

func TestBuildCommandMenuItemsForSlashCommands(t *testing.T) {
	app, _ := newTestApp(t)

	items, meta := app.buildCommandMenuItems("/he", 80)
	if meta.Title != commandMenuTitle {
		t.Fatalf("expected command menu title, got %q", meta.Title)
	}
	if len(items) == 0 {
		t.Fatalf("expected slash command suggestions")
	}
	found := false
	for _, item := range items {
		if item.replacement == slashUsageHelp {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected help suggestion to appear")
	}
}

func TestFileMenuSuggestionsEmptyQueryIncludesBrowse(t *testing.T) {
	app, _ := newTestApp(t)
	app.fileCandidates = []string{"README.md", "docs/guide.md"}

	items := app.fileMenuSuggestions("@")
	if len(items) == 0 || !items[0].openFileBrowser {
		t.Fatalf("expected browse file entry")
	}
}

func TestFileMenuSuggestionsMatchesQuery(t *testing.T) {
	app, _ := newTestApp(t)
	app.fileCandidates = []string{"README.md", "docs/guide.md"}

	items := app.fileMenuSuggestions("@read")
	if len(items) == 0 {
		t.Fatalf("expected file suggestions")
	}
	if items[0].replacement == "" {
		t.Fatalf("expected replacement to be set")
	}
}

func TestApplySelectedCommandSuggestionReplacesInput(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("/he")
	app.state.InputText = "/he"
	app.transcript.Width = 80
	app.refreshCommandMenu()

	if !app.commandMenuHasSuggestions() {
		t.Fatalf("expected suggestions")
	}
	if !app.applySelectedCommandSuggestion() {
		t.Fatalf("expected suggestion to apply")
	}
	if app.input.Value() == "/he" {
		t.Fatalf("expected input to change")
	}
}

func TestApplySelectedCommandSuggestionOpenFileBrowser(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentWorkdir = t.TempDir()
	app.fileCandidates = []string{"README.md"}
	app.input.SetValue("@")
	app.transcript.Width = 80
	app.refreshCommandMenu()

	if !app.commandMenuHasSuggestions() {
		t.Fatalf("expected suggestions")
	}
	applied := app.applySelectedCommandSuggestion()
	if !applied {
		t.Fatalf("expected browse action to apply")
	}
	if app.state.ActivePicker != pickerFile {
		t.Fatalf("expected file picker to open")
	}
}

func TestUpdateCommandMenuSelectionHandlesNavigationKeys(t *testing.T) {
	app, _ := newTestApp(t)
	app.input.SetValue("/he")
	app.transcript.Width = 80
	app.refreshCommandMenu()

	_, handled := app.updateCommandMenuSelection(tea.KeyMsg{Type: tea.KeyDown})
	if !handled {
		t.Fatalf("expected navigation key to be handled")
	}
	_, handled = app.updateCommandMenuSelection(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if handled {
		t.Fatalf("expected non-navigation key to be ignored")
	}
}

func TestOpenFileBrowserUsesAbsoluteWorkdir(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	app.openFileBrowser()

	expected, _ := filepath.Abs(root)
	if app.fileBrowser.CurrentDirectory != expected {
		t.Fatalf("expected absolute directory, got %q", app.fileBrowser.CurrentDirectory)
	}
	if app.state.ActivePicker != pickerFile {
		t.Fatalf("expected file picker to be active")
	}
}
