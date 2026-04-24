package tui

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	agentsession "neo-code/internal/session"
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

func TestSessionItemAccessors(t *testing.T) {
	updatedAt := time.Date(2026, 4, 22, 8, 30, 0, 0, time.UTC)
	item := sessionItem{Summary: agentsession.Summary{Title: "Session A", UpdatedAt: updatedAt}}
	if item.Title() != "Session A" {
		t.Fatalf("Title() = %q, want Session A", item.Title())
	}
	if item.Description() != "04-22 08:30" {
		t.Fatalf("Description() = %q, want 04-22 08:30", item.Description())
	}
	if item.FilterValue() != "session a" {
		t.Fatalf("FilterValue() = %q, want session a", item.FilterValue())
	}
}

type titledOnlyItem struct{ title string }

func (i titledOnlyItem) Title() string { return i.title }
func (i titledOnlyItem) FilterValue() string {
	return i.title
}

type describedOnlyItem struct{ description string }

func (i describedOnlyItem) Description() string { return i.description }
func (i describedOnlyItem) FilterValue() string {
	return i.description
}

type invalidListItem struct{}

func (invalidListItem) FilterValue() string { return "invalid" }

func TestPickerItemTextFallbackBranches(t *testing.T) {
	title, subtitle := pickerItemText(titledOnlyItem{title: "  only-title  "})
	if title != "only-title" || subtitle != "" {
		t.Fatalf("unexpected title-only result: title=%q subtitle=%q", title, subtitle)
	}
	title, subtitle = pickerItemText(describedOnlyItem{description: "  only-desc  "})
	if title != "" || subtitle != "only-desc" {
		t.Fatalf("unexpected desc-only result: title=%q subtitle=%q", title, subtitle)
	}
}

func TestPickerSelectionDelegateMethods(t *testing.T) {
	delegate := newPickerSelectionDelegate()
	if delegate.Height() != 2 {
		t.Fatalf("Height() = %d, want 2", delegate.Height())
	}
	if delegate.Spacing() != 0 {
		t.Fatalf("Spacing() = %d, want 0", delegate.Spacing())
	}
	if cmd := delegate.Update(tea.KeyMsg{Type: tea.KeyDown}, nil); cmd != nil {
		t.Fatalf("expected nil cmd from delegate update, got %T", cmd)
	}
}

func TestPickerSelectionDelegateRenderBranches(t *testing.T) {
	delegate := newPickerSelectionDelegate()
	model := list.New([]list.Item{
		selectionItem{id: "m1", name: "Model A", description: "desc"},
	}, delegate, 24, 2)
	model.Select(0)

	var out bytes.Buffer
	delegate.Render(&out, model, 0, selectionItem{id: "m1", name: "Model A", description: "desc"})
	if !strings.Contains(out.String(), "|") {
		t.Fatalf("expected selected row indicator, got %q", out.String())
	}

	out.Reset()
	delegate.Render(&out, model, 1, selectionItem{id: "m2", name: "Model B", description: ""})
	if strings.TrimSpace(out.String()) == "" {
		t.Fatalf("expected non-selected row to render")
	}

	out.Reset()
	delegate.Render(io.Discard, model, 0, invalidListItem{})
}

func TestSessionDelegateMethodsAndRenderGuard(t *testing.T) {
	delegate := sessionDelegate{styles: newStyles()}
	if delegate.Height() != 3 {
		t.Fatalf("Height() = %d, want 3", delegate.Height())
	}
	if delegate.Spacing() != 1 {
		t.Fatalf("Spacing() = %d, want 1", delegate.Spacing())
	}
	if cmd := delegate.Update(tea.KeyMsg{Type: tea.KeyUp}, nil); cmd != nil {
		t.Fatalf("expected nil cmd from session delegate update, got %T", cmd)
	}

	model := list.New(nil, delegate, 24, 3)
	var out bytes.Buffer
	delegate.Render(&out, model, 0, invalidListItem{})
	if out.Len() != 0 {
		t.Fatalf("expected guard branch to skip invalid item render, got %q", out.String())
	}
}
