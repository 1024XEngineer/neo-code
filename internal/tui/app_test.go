package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
	"neocode/internal/runtime"
	"neocode/internal/tools"
)

type tuiTestProvider struct {
	response provider.ChatResponse
	err      error
}

func (p *tuiTestProvider) Name() string {
	return "fake"
}

func (p *tuiTestProvider) Chat(context.Context, provider.ChatRequest) (provider.ChatResponse, error) {
	return p.response, p.err
}

func newTestModelWithProvider(t *testing.T, modelProvider provider.Provider) *model {
	t.Helper()

	registry := tools.NewRegistry()
	runtimeSvc, err := runtime.New(modelProvider, registry, tools.NewExecutor(registry), "test-model", t.TempDir())
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	m := newModel(context.Background(), runtimeSvc)
	m.width = 120
	m.height = 40
	m.resize()
	return &m
}

func newTestModel(t *testing.T) *model {
	t.Helper()
	return newTestModelWithProvider(t, &tuiTestProvider{})
}

func TestHandleKeySlashStaysInComposer(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneCompose

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next := updated.(*model)

	if next.state.sidebarOpen {
		t.Fatalf("expected slash in composer to keep sidebar closed")
	}
	if got := next.composer.Value(); got != "/" {
		t.Fatalf("expected slash to be inserted into composer, got %q", got)
	}
}

func TestHandleKeyQuestionMarkStaysInComposer(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneCompose

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	next := updated.(*model)

	if next.state.showHelp {
		t.Fatalf("expected question mark in composer not to open help")
	}
	if got := next.composer.Value(); got != "?" {
		t.Fatalf("expected question mark to be inserted into composer, got %q", got)
	}
}

func TestHandleKeyF1OpensHelpInComposer(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneCompose

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyF1})
	next := updated.(*model)

	if !next.state.showHelp {
		t.Fatalf("expected F1 in composer to open help")
	}
	if got := next.composer.Value(); got != "" {
		t.Fatalf("expected F1 not to be inserted into composer, got %q", got)
	}
}

func TestHandleKeyF1OpensHelpInBrowse(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneBrowse

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyF1})
	next := updated.(*model)

	if !next.state.showHelp {
		t.Fatalf("expected F1 in browse mode to open help")
	}
}

func TestHandleKeyF1OpensHelpInSidebar(t *testing.T) {
	m := newTestModel(t)
	m.state.sidebarOpen = true
	m.state.pane = paneSidebar

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyF1})
	next := updated.(*model)

	if !next.state.showHelp {
		t.Fatalf("expected F1 in sidebar to open help")
	}
}

func TestHandleKeySlashOpensSidebarInBrowse(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneBrowse

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next := updated.(*model)

	if !next.state.sidebarOpen {
		t.Fatalf("expected slash in browse mode to open the sidebar")
	}
	if next.state.pane != paneSidebar {
		t.Fatalf("expected sidebar pane after opening search, got %s", next.state.pane.Label())
	}
}

func TestHandleCtrlVPasteMovesBrowseToComposer(t *testing.T) {
	m := newTestModel(t)
	m.state.pane = paneBrowse

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	next := updated.(*model)

	if next.state.sidebarOpen {
		t.Fatalf("expected ctrl+v not to open the sidebar")
	}
	if next.state.pane != paneCompose {
		t.Fatalf("expected ctrl+v to focus the composer, got %s", next.state.pane.Label())
	}
	if cmd == nil {
		t.Fatalf("expected ctrl+v to return a paste command")
	}
}

func TestRefreshZonesMatchRenderedHeaderActions(t *testing.T) {
	m := newTestModel(t)

	if len(m.state.headerZones) != 2 {
		t.Fatalf("expected 2 header zones, got %d", len(m.state.headerZones))
	}

	expectedSessionsX := 1 +
		ansi.StringWidth(renderBadge("NEOCODE", themeAccent, themeCanvas)) + 1 +
		ansi.StringWidth(renderBadge("IDLE", themeSuccess, themeCanvas)) + 1
	expectedSessionsWidth := ansi.StringWidth(renderAction("Sessions", false, themeAccent))
	expectedNewWidth := ansi.StringWidth(renderAction("New", false, themeAccent2))

	if zone := m.state.headerZones[0]; zone.Kind != actionToggleSidebar {
		t.Fatalf("expected first header zone to toggle sidebar, got %s", zone.Kind)
	} else {
		if zone.Rect.X != expectedSessionsX {
			t.Fatalf("expected sessions zone x=%d, got %d", expectedSessionsX, zone.Rect.X)
		}
		if zone.Rect.Width != expectedSessionsWidth {
			t.Fatalf("expected sessions zone width=%d, got %d", expectedSessionsWidth, zone.Rect.Width)
		}
	}

	if zone := m.state.headerZones[1]; zone.Kind != actionNewSession {
		t.Fatalf("expected second header zone to create session, got %s", zone.Kind)
	} else {
		expectedNewX := expectedSessionsX + expectedSessionsWidth + 1
		if zone.Rect.X != expectedNewX {
			t.Fatalf("expected new-session zone x=%d, got %d", expectedNewX, zone.Rect.X)
		}
		if zone.Rect.Width != expectedNewWidth {
			t.Fatalf("expected new-session zone width=%d, got %d", expectedNewWidth, zone.Rect.Width)
		}
	}
}

func TestHandleMouseClickCopyZoneSelectsTargetCodeBlock(t *testing.T) {
	m := newTestModelWithProvider(t, &tuiTestProvider{
		response: provider.ChatResponse{
			Message: provider.Message{
				Role: provider.RoleAssistant,
				Content: "```python\nprint('one')\n```\n\n" +
					"```go\nfmt.Println(\"two\")\n```",
			},
		},
	})

	if err := m.runtime.Run(context.Background(), runtime.UserInput{
		SessionID: m.state.activeSessionID,
		Content:   "show code",
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	m.sync(true)

	if len(m.rendered.CodeBlocks) != 2 {
		t.Fatalf("expected 2 code blocks, got %d", len(m.rendered.CodeBlocks))
	}
	if m.state.selectedCodeBlock != 1 {
		t.Fatalf("expected latest code block to be selected before click, got %d", m.state.selectedCodeBlock)
	}

	target := m.rendered.CodeBlocks[0]
	inner := m.layout.conversationRect.inner()
	x := inner.X + target.CopyX1 + max(0, (target.CopyX2-target.CopyX1)/2)
	y := inner.Y + target.HeaderLine - m.viewport.YOffset

	updated, cmd := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      x,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}))
	next := updated.(*model)

	if cmd == nil {
		t.Fatalf("expected clicking the copy zone to return a copy command")
	}
	if next.state.selectedCodeBlock != target.Index {
		t.Fatalf("expected selected code block %d after click, got %d", target.Index, next.state.selectedCodeBlock)
	}
	if next.state.selectedMessage != target.MessageIndex {
		t.Fatalf("expected selected message %d after click, got %d", target.MessageIndex, next.state.selectedMessage)
	}
}
