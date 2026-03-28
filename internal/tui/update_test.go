package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	agentruntime "github.com/dust/neo-code/internal/runtime"
	"github.com/dust/neo-code/internal/tools"
)

type stubRuntime struct {
	runInputs []agentruntime.UserInput
	events    chan agentruntime.RuntimeEvent
	sessions  []agentruntime.SessionSummary
	loads     map[string]agentruntime.Session
	runErr    error
	listErr   error
	loadErr   error
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{
		events: make(chan agentruntime.RuntimeEvent, 16),
		loads:  map[string]agentruntime.Session{},
	}
}

func (r *stubRuntime) Run(ctx context.Context, input agentruntime.UserInput) error {
	r.runInputs = append(r.runInputs, input)
	return r.runErr
}

func (r *stubRuntime) Events() <-chan agentruntime.RuntimeEvent {
	return r.events
}

func (r *stubRuntime) ListSessions(ctx context.Context) ([]agentruntime.SessionSummary, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]agentruntime.SessionSummary(nil), r.sessions...), nil
}

func (r *stubRuntime) LoadSession(ctx context.Context, id string) (agentruntime.Session, error) {
	if r.loadErr != nil {
		return agentruntime.Session{}, r.loadErr
	}
	if session, ok := r.loads[id]; ok {
		return session, nil
	}
	return agentruntime.Session{}, nil
}

func TestAppUpdateComposerCommands(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		assert func(t *testing.T, beforeRuntime *stubRuntime, manager *config.Manager, app App)
	}{
		{
			name:  "slash set url updates config and does not start runtime",
			input: "/set url https://test.com",
			assert: func(t *testing.T, runtime *stubRuntime, manager *config.Manager, app App) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected runtime not to run, got %d calls", len(runtime.runInputs))
				}
				cfg := manager.Get()
				selected, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("SelectedProviderConfig() error = %v", err)
				}
				if selected.BaseURL != "https://test.com" {
					t.Fatalf("expected base url updated to https://test.com, got %q", selected.BaseURL)
				}
				if app.state.IsAgentRunning {
					t.Fatalf("expected agent to stay idle")
				}
			},
		},
		{
			name:  "model command opens picker and does not start runtime",
			input: "/model",
			assert: func(t *testing.T, runtime *stubRuntime, manager *config.Manager, app App) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected runtime not to run, got %d calls", len(runtime.runInputs))
				}
				if !app.state.ShowModelPicker {
					t.Fatalf("expected model picker to open")
				}
				if app.state.StatusText != statusChooseModel {
					t.Fatalf("expected status %q, got %q", statusChooseModel, app.state.StatusText)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()

			app, err := New(nil, manager, runtime)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			app.input.SetValue(tt.input)
			app.state.InputText = tt.input

			model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			app = model.(App)

			for _, msg := range collectTeaMessages(cmd) {
				model, followCmd := app.Update(msg)
				app = model.(App)
				_ = collectTeaMessages(followCmd)
			}

			tt.assert(t, runtime, manager, app)
		})
	}
}

func TestAppUpdateModelPickerAndRuntimeMessages(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, app *App, runtime *stubRuntime)
		msg    tea.Msg
		assert func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg)
	}{
		{
			name: "escape closes model picker",
			setup: func(t *testing.T, app *App, runtime *stubRuntime) {
				app.state.ShowModelPicker = true
				app.focus = panelInput
			},
			msg: tea.KeyMsg{Type: tea.KeyEsc},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if app.state.ShowModelPicker {
					t.Fatalf("expected model picker to close")
				}
				if app.state.Focus != panelInput {
					t.Fatalf("expected focus to return to input")
				}
			},
		},
		{
			name: "runtime chunk appends assistant draft",
			setup: func(t *testing.T, app *App, runtime *stubRuntime) {
				app.state.ActiveSessionID = "session-1"
			},
			msg: RuntimeMsg{Event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventAgentChunk,
				SessionID: "session-1",
				Payload:   "hello",
			}},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if len(app.activeMessages) == 0 {
					t.Fatalf("expected assistant draft message")
				}
				last := app.activeMessages[len(app.activeMessages)-1]
				if last.Role != roleAssistant || last.Content != "hello" {
					t.Fatalf("unexpected last assistant draft: %+v", last)
				}
			},
		},
		{
			name: "runtime done appends final assistant and clears running state",
			setup: func(t *testing.T, app *App, runtime *stubRuntime) {
				app.state.IsAgentRunning = true
				app.state.ActiveSessionID = "session-2"
			},
			msg: RuntimeMsg{Event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventAgentDone,
				SessionID: "session-2",
				Payload: provider.Message{
					Role:    roleAssistant,
					Content: "final",
				},
			}},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if app.state.IsAgentRunning {
					t.Fatalf("expected agent to stop running")
				}
				if app.state.StatusText != statusReady {
					t.Fatalf("expected status ready, got %q", app.state.StatusText)
				}
				last := app.activeMessages[len(app.activeMessages)-1]
				if last.Content != "final" {
					t.Fatalf("expected final assistant message, got %+v", last)
				}
			},
		},
		{
			name: "runtime tool result error is surfaced",
			setup: func(t *testing.T, app *App, runtime *stubRuntime) {
				app.state.ActiveSessionID = "session-3"
			},
			msg: RuntimeMsg{Event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventToolResult,
				SessionID: "session-3",
				Payload: tools.ToolResult{
					Name:    "filesystem_edit",
					Content: "boom",
					IsError: true,
				},
			}},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if app.state.ExecutionError != "boom" {
					t.Fatalf("expected execution error boom, got %q", app.state.ExecutionError)
				}
				if app.state.StatusText != statusToolError {
					t.Fatalf("expected status tool error, got %q", app.state.StatusText)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()
			app, err := New(nil, manager, runtime)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if tt.setup != nil {
				tt.setup(t, &app, runtime)
			}

			model, cmd := app.Update(tt.msg)
			app = model.(App)
			tt.assert(t, runtime, app, collectTeaMessages(cmd))
		})
	}
}

func TestAppHelpersAndRenderingSmoke(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	now := agentruntime.Session{
		ID:    "session-1",
		Title: "Existing Session",
		Messages: []provider.Message{
			{Role: roleUser, Content: "hi"},
			{Role: roleAssistant, Content: "hello"},
		},
	}
	runtime.sessions = []agentruntime.SessionSummary{
		{ID: now.ID, Title: now.Title, UpdatedAt: now.UpdatedAt},
	}
	runtime.loads[now.ID] = now

	app, err := New(nil, manager, runtime)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.Init() == nil {
		t.Fatalf("expected init command")
	}

	app.openModelPicker()
	app.closeModelPicker()
	app.selectCurrentModel(config.DefaultOpenAIModel)
	app.appendAssistantChunk("hello")
	app.appendAssistantChunk(" world")
	if !app.lastAssistantMatches("hello world") {
		t.Fatalf("expected assistant draft to match")
	}
	app.appendInlineMessage(roleSystem, "notice")

	app.focusNext()
	app.focusPrev()
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyDown})
	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyUp})

	if err := app.refreshSessions(); err != nil {
		t.Fatalf("refreshSessions() error = %v", err)
	}
	if err := app.activateSelectedSession(); err != nil {
		t.Fatalf("activateSelectedSession() error = %v", err)
	}
	app.syncActiveSessionTitle()
	app.syncConfigState(manager.Get())
	app.rebuildTranscript()

	view := app.View()
	if view == "" {
		t.Fatalf("expected non-empty View()")
	}
	if lipgloss.Height(view) > app.height+1 {
		t.Fatalf("expected view height to stay within window bounds, got %d", lipgloss.Height(view))
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "Ctrl+U") {
		t.Fatalf("expected footer help to render on the last visible line")
	}
	if app.renderHeader(app.computeLayout().contentWidth) == "" || app.renderBody(app.computeLayout()) == "" {
		t.Fatalf("expected non-empty render output")
	}
	app.state.ShowModelPicker = true
	if app.renderModelPicker(48, 12) == "" || app.renderWaterfall(80, 20) == "" {
		t.Fatalf("expected model picker rendering")
	}
	app.state.ShowModelPicker = false
	if app.renderCommandMenu(80) == "" {
		app.input.SetValue("/")
		app.state.InputText = "/"
		if app.renderCommandMenu(80) == "" {
			t.Fatalf("expected slash command menu when input starts with slash")
		}
	}
	if app.renderPrompt(80) == "" || app.renderHelp(80) == "" {
		t.Fatalf("expected prompt and help output")
	}
	if lipgloss.Width(app.renderPrompt(80)) != 80 {
		t.Fatalf("expected prompt width 80, got %d", lipgloss.Width(app.renderPrompt(80)))
	}
	sidebar := app.renderSidebar(26, 12)
	if lipgloss.Width(sidebar) != 26 || lipgloss.Height(sidebar) != 12 {
		t.Fatalf("expected sidebar to respect requested dimensions, got %dx%d", lipgloss.Width(sidebar), lipgloss.Height(sidebar))
	}
	if !strings.Contains(app.renderSidebar(26, 12), sidebarTitle) || !strings.Contains(app.renderSidebar(26, 12), sidebarOpenHint) {
		t.Fatalf("expected updated sidebar header text")
	}
	if strings.Contains(app.renderPrompt(80), "Enter/Ctrl+S") {
		t.Fatalf("expected composer hint line to be removed")
	}
	if strings.TrimSpace(app.renderPrompt(80)) == "" {
		t.Fatalf("expected prompt to render a visible border")
	}
	if app.focusLabel() == "" || app.statusBadge("ready") == "" {
		t.Fatalf("expected status helpers to render")
	}
	app.focus = panelSessions
	if app.focusLabel() != focusLabelSessions {
		t.Fatalf("expected session focus label")
	}
	app.focus = panelTranscript
	if app.focusLabel() != focusLabelTranscript {
		t.Fatalf("expected transcript focus label")
	}
	app.focus = panelInput
	if app.statusBadge("error: boom") == "" || app.statusBadge("running now") == "" {
		t.Fatalf("expected status badge variants")
	}
	if app.renderMessageBlock(provider.Message{Role: roleError, Content: "boom"}, 80) == "" {
		t.Fatalf("expected error message block")
	}
	if app.renderMessageBlock(provider.Message{
		Role: roleAssistant,
		ToolCalls: []provider.ToolCall{
			{Name: "filesystem_edit"},
		},
	}, 80) == "" {
		t.Fatalf("expected tool call message block")
	}
	if app.renderMessageContent("```go\nfmt.Println(\"x\")\n```", 80, app.styles.messageBody) == "" {
		t.Fatalf("expected code block rendering")
	}
	if app.computeLayout().contentWidth == 0 {
		t.Fatalf("expected computed layout")
	}
	app.width = 90
	app.height = 26
	compact := app.computeLayout()
	if !compact.stacked {
		t.Fatalf("expected compact layout to stack")
	}
	app.sessions.SetFilterState(list.Filtering)
	if !app.isFilteringSessions() {
		t.Fatalf("expected filtering state")
	}
	if app.sessions.ShowPagination() {
		t.Fatalf("expected sessions pagination to stay hidden")
	}
}

func TestTUIStandaloneHelpers(t *testing.T) {
	t.Parallel()

	if len(newKeyMap().ShortHelp()) == 0 || len(newKeyMap().FullHelp()) == 0 {
		t.Fatalf("expected key help bindings")
	}

	if wrapPlain("abcdef", 3) == "" || trimRunes("abcdef", 4) == "" || trimMiddle("abcdefgh", 5) == "" {
		t.Fatalf("expected string helpers to return content")
	}
	if fallback("", "x") != "x" {
		t.Fatalf("expected fallback to use replacement")
	}
	if preview("line1\nline2\nline3", 8, 2) == "" {
		t.Fatalf("expected preview output")
	}
	if clamp(10, 0, 5) != 5 || max(2, 3) != 3 {
		t.Fatalf("expected numeric helpers to work")
	}

	sItem := sessionItem{Summary: agentruntime.SessionSummary{Title: "My Session"}}
	if sItem.FilterValue() != "my session" {
		t.Fatalf("unexpected session item filter value")
	}

	mItem := modelItem{name: "gpt-5.4", description: "Frontier"}
	if mItem.Title() == "" || mItem.Description() == "" || mItem.FilterValue() == "" {
		t.Fatalf("expected model item helpers to return values")
	}

	delegate := sessionDelegate{styles: newStyles()}
	if delegate.Height() == 0 || delegate.Spacing() == 0 {
		t.Fatalf("expected delegate sizing")
	}
	if delegate.Update(nil, nil) != nil {
		t.Fatalf("expected delegate update to return nil")
	}
	var buf bytes.Buffer
	model := newModelPicker()
	sessionList := []list.Item{sItem}
	listModel := list.New(sessionList, delegate, 30, 10)
	delegate.Render(&buf, listModel, 0, sItem)
	if buf.Len() == 0 {
		t.Fatalf("expected delegate render output")
	}

	eventCh := make(chan agentruntime.RuntimeEvent, 1)
	eventCh <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentChunk, Payload: "x"}
	if msg := ListenForRuntimeEvent(eventCh)(); msg == nil {
		t.Fatalf("expected runtime event message")
	}
	close(eventCh)
	if _, ok := ListenForRuntimeEvent(eventCh)().(RuntimeClosedMsg); !ok {
		t.Fatalf("expected runtime closed message")
	}

	runtime := newStubRuntime()
	runMsg := runAgent(runtime, "session-x", "hello")()
	if _, ok := runMsg.(runFinishedMsg); !ok {
		t.Fatalf("expected runFinishedMsg")
	}
	if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "hello" {
		t.Fatalf("expected runtime run input to be captured")
	}

	manager := newTestConfigManager(t)
	msg := runModelSelection(manager, "gpt-5.4")()
	if result, ok := msg.(localCommandResultMsg); !ok || result.err != nil {
		t.Fatalf("expected successful localCommandResultMsg, got %+v", msg)
	}

	_ = model
}

func TestAppUpdateAdditionalTransitions(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager)
		msg    tea.Msg
		assert func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg)
	}{
		{
			name: "window resize updates dimensions",
			msg:  tea.WindowSizeMsg{Width: 100, Height: 32},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.width != 100 || app.height != 32 {
					t.Fatalf("expected updated dimensions, got %dx%d", app.width, app.height)
				}
			},
		},
		{
			name: "runtime closed stops agent",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.state.IsAgentRunning = true
				app.state.StatusText = ""
			},
			msg: RuntimeClosedMsg{},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.IsAgentRunning || app.state.StatusText != statusRuntimeClosed {
					t.Fatalf("expected runtime closed state, got %+v", app.state)
				}
			},
		},
		{
			name: "run finished error is surfaced",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.state.IsAgentRunning = true
			},
			msg: runFinishedMsg{err: context.DeadlineExceeded},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.IsAgentRunning || app.state.ExecutionError == "" {
					t.Fatalf("expected execution error to be set")
				}
			},
		},
		{
			name: "local command success updates state",
			msg:  localCommandResultMsg{notice: "[System] ok"},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.StatusText != "[System] ok" {
					t.Fatalf("expected success notice, got %q", app.state.StatusText)
				}
			},
		},
		{
			name: "local command error updates state",
			msg:  localCommandResultMsg{err: context.Canceled},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.ExecutionError == "" || app.state.StatusText == "" {
					t.Fatalf("expected local command error state")
				}
			},
		},
		{
			name: "toggle help flips state",
			msg:  tea.KeyMsg{Type: tea.KeyCtrlQ},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if !app.state.ShowHelp || !app.help.ShowAll {
					t.Fatalf("expected help to be visible")
				}
			},
		},
		{
			name: "next panel moves focus",
			msg:  tea.KeyMsg{Type: tea.KeyTab},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.focus != panelSessions {
					t.Fatalf("expected focus to move to sessions, got %v", app.focus)
				}
			},
		},
		{
			name: "previous panel moves focus backward",
			msg:  tea.KeyMsg{Type: tea.KeyShiftTab},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.focus != panelTranscript {
					t.Fatalf("expected focus to move backward, got %v", app.focus)
				}
			},
		},
		{
			name: "new session clears active draft",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.state.ActiveSessionID = "existing"
				app.state.ActiveSessionTitle = "Existing"
				app.activeMessages = []provider.Message{{Role: roleUser, Content: "hello"}}
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlN},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.ActiveSessionID != "" || len(app.activeMessages) != 0 || app.state.StatusText != statusDraft {
					t.Fatalf("expected new draft state, got %+v", app.state)
				}
			},
		},
		{
			name: "session enter activates selected session",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				runtime.sessions = []agentruntime.SessionSummary{{ID: "s1", Title: "One"}}
				runtime.loads["s1"] = agentruntime.Session{
					ID:       "s1",
					Title:    "One",
					Messages: []provider.Message{{Role: roleAssistant, Content: "loaded"}},
				}
				if err := app.refreshSessions(); err != nil {
					t.Fatalf("refresh sessions: %v", err)
				}
				app.focus = panelSessions
				app.applyFocus()
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.ActiveSessionID != "s1" || len(app.activeMessages) != 1 {
					t.Fatalf("expected selected session to load, got %+v / %+v", app.state, app.activeMessages)
				}
			},
		},
		{
			name: "transcript focus handles scroll keys",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.focus = panelTranscript
				app.transcript.SetContent(strings.Repeat("line\n", 80))
				app.transcript.Height = 5
				app.transcript.GotoBottom()
			},
			msg: tea.KeyMsg{Type: tea.KeyUp},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.transcript.YOffset < 0 {
					t.Fatalf("expected non-negative offset")
				}
			},
		},
		{
			name: "input typing updates composer text",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.InputText != "h" {
					t.Fatalf("expected input text to update, got %q", app.state.InputText)
				}
			},
		},
		{
			name: "plain input send starts runtime",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue("inspect repo")
				app.state.InputText = "inspect repo"
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if !app.state.IsAgentRunning {
					t.Fatalf("expected agent to start running")
				}
				if len(app.activeMessages) == 0 || app.activeMessages[len(app.activeMessages)-1].Role != roleUser {
					t.Fatalf("expected user message appended")
				}
				if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "inspect repo" {
					t.Fatalf("expected runtime command to execute once, got %+v", runtime.runInputs)
				}
				finished := false
				for _, msg := range msgs {
					if _, ok := msg.(runFinishedMsg); ok {
						finished = true
					}
				}
				if !finished {
					t.Fatalf("expected runFinishedMsg from command")
				}
			},
		},
		{
			name: "quit returns quit command",
			msg:  tea.KeyMsg{Type: tea.KeyCtrlU},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				foundQuit := false
				for _, msg := range msgs {
					if _, ok := msg.(tea.QuitMsg); ok {
						foundQuit = true
					}
				}
				if !foundQuit {
					t.Fatalf("expected quit message")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()
			app, err := New(nil, manager, runtime)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if tt.setup != nil {
				tt.setup(t, &app, runtime, manager)
			}

			model, cmd := app.Update(tt.msg)
			app = model.(App)
			tt.assert(t, app, runtime, manager, collectTeaMessages(cmd))
		})
	}
}

func TestAppUpdateModelPickerEnterAppliesSelection(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.openModelPicker()
	if len(app.modelPicker.Items()) < 2 {
		t.Fatalf("expected model picker catalog")
	}
	selected := app.modelPicker.Items()[1].(modelItem).name
	app.modelPicker.Select(1)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ShowModelPicker {
		t.Fatalf("expected picker to close after selection")
	}

	for _, msg := range collectTeaMessages(cmd) {
		model, follow := app.Update(msg)
		app = model.(App)
		_ = collectTeaMessages(follow)
	}

	cfg := manager.Get()
	if cfg.CurrentModel != selected {
		t.Fatalf("expected current model %q, got %q", selected, cfg.CurrentModel)
	}
}

func TestAppHandleRuntimeEventAdditionalBranches(t *testing.T) {
	tests := []struct {
		name   string
		event  agentruntime.RuntimeEvent
		setup  func(*App)
		assert func(t *testing.T, app App)
	}{
		{
			name: "user message resets execution state",
			setup: func(app *App) {
				app.state.ExecutionError = "old"
				app.state.CurrentTool = "bash"
			},
			event: agentruntime.RuntimeEvent{Type: agentruntime.EventUserMessage, SessionID: "s1"},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.ExecutionError != "" || app.state.CurrentTool != "" || app.state.StatusText != statusThinking {
					t.Fatalf("unexpected user message state: %+v", app.state)
				}
			},
		},
		{
			name: "tool start stores current tool",
			event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventToolStart,
				SessionID: "s1",
				Payload: provider.ToolCall{
					Name: "filesystem_edit",
				},
			},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.CurrentTool != "filesystem_edit" || app.state.StatusText != statusRunningTool {
					t.Fatalf("unexpected tool start state: %+v", app.state)
				}
			},
		},
		{
			name: "tool success appends completion event",
			setup: func(app *App) {
				app.state.CurrentTool = "filesystem_edit"
			},
			event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventToolResult,
				SessionID: "s1",
				Payload: tools.ToolResult{
					Name: "filesystem_edit",
				},
			},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.CurrentTool != "" || app.state.StatusText != statusToolFinished {
					t.Fatalf("unexpected tool success state: %+v", app.state)
				}
			},
		},
		{
			name: "error event appends inline error",
			event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventError,
				SessionID: "s1",
				Payload:   "boom",
			},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.ExecutionError != "boom" || app.state.IsAgentRunning {
					t.Fatalf("unexpected error state: %+v", app.state)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()
			app, err := New(nil, manager, runtime)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if tt.setup != nil {
				tt.setup(&app)
			}
			app.handleRuntimeEvent(tt.event)
			tt.assert(t, app)
		})
	}
}

func TestAppRefreshErrorPaths(t *testing.T) {
	t.Run("refresh sessions returns runtime error", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		runtime.listErr = context.DeadlineExceeded

		app, err := New(nil, manager, runtime)
		if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("expected list session error during New, got %v", err)
		}
		_ = app
	})

	t.Run("refresh messages returns load error", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		runtime.loadErr = context.Canceled

		app, err := New(nil, manager, runtime)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		app.state.ActiveSessionID = "broken"

		err = app.refreshMessages()
		if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
			t.Fatalf("expected load session error, got %v", err)
		}
	})
}

func newTestConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	manager := config.NewManager(config.NewLoader(t.TempDir()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}

func collectTeaMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msgCh := make(chan tea.Msg, 1)
	go func() {
		msgCh <- cmd()
	}()

	var msg tea.Msg
	select {
	case msg = <-msgCh:
	case <-time.After(25 * time.Millisecond):
		return nil
	}
	if msg == nil {
		return nil
	}
	switch typed := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, child := range typed {
			out = append(out, collectTeaMessages(child)...)
		}
		return out
	default:
		return []tea.Msg{typed}
	}
}
