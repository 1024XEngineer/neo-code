package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/config"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providercatalog "neo-code/internal/provider/catalog"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/tools"
)

type stubRuntime struct {
	runInputs     []agentruntime.UserInput
	compactInputs []agentruntime.CompactInput
	events        chan agentruntime.RuntimeEvent
	sessions      []agentruntime.SessionSummary
	loads         map[string]agentruntime.Session
	runErr        error
	compactErr    error
	compactResult agentruntime.CompactResult
	listErr       error
	loadErr       error
	setWorkdirErr error
	setResult     *agentruntime.Session
	setCalls      int
	resolveInputs []agentruntime.PermissionResolutionInput
	resolveErr    error
	cancelCalls   int
	cancelResult  bool
}

type stubMarkdownRenderer struct {
	output string
	err    error
	calls  int
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (r *stubMarkdownRenderer) Render(content string, width int) (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	if r.output != "" {
		return r.output, nil
	}
	return content, nil
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

func (r *stubRuntime) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	r.compactInputs = append(r.compactInputs, input)
	return r.compactResult, r.compactErr
}

func (r *stubRuntime) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	r.resolveInputs = append(r.resolveInputs, input)
	return r.resolveErr
}

func (r *stubRuntime) Events() <-chan agentruntime.RuntimeEvent {
	return r.events
}

func (r *stubRuntime) CancelActiveRun() bool {
	r.cancelCalls++
	return r.cancelResult
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

func (r *stubRuntime) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentruntime.Session, error) {
	r.setCalls++
	if r.setWorkdirErr != nil {
		return agentruntime.Session{}, r.setWorkdirErr
	}
	if r.setResult != nil {
		return *r.setResult, nil
	}
	session, ok := r.loads[sessionID]
	if !ok {
		session = agentruntime.Session{ID: sessionID}
	}
	session.Workdir = strings.TrimSpace(workdir)
	r.loads[sessionID] = session
	return session, nil
}

func TestAppUpdateComposerCommands(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		assert func(t *testing.T, beforeRuntime *stubRuntime, manager *config.Manager, app App)
	}{
		{
			name:  "unknown slash command stays local and surfaces error",
			input: "/unknown",
			assert: func(t *testing.T, runtime *stubRuntime, manager *config.Manager, app App) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected runtime not to run, got %d calls", len(runtime.runInputs))
				}
				if !strings.Contains(app.state.StatusText, "unknown command") {
					t.Fatalf("expected unknown command error, got %q", app.state.StatusText)
				}
				if app.state.IsAgentRunning {
					t.Fatalf("expected agent to stay idle")
				}
			},
		},
		{
			name:  "provider command opens picker and does not start runtime",
			input: "/provider\n",
			assert: func(t *testing.T, runtime *stubRuntime, manager *config.Manager, app App) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected runtime not to run, got %d calls", len(runtime.runInputs))
				}
				if app.state.ActivePicker != pickerProvider {
					t.Fatalf("expected provider picker to open")
				}
				if app.state.StatusText != statusChooseProvider {
					t.Fatalf("expected status %q, got %q", statusChooseProvider, app.state.StatusText)
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
				if app.state.ActivePicker != pickerModel {
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

			app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
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

func TestAppUpdateWorkspaceSlashCommands(t *testing.T) {
	t.Run("draft workspace command updates local current workdir", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		target := t.TempDir()
		app.input.SetValue("/cwd " + target)
		app.state.InputText = app.input.Value()

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		for _, msg := range collectTeaMessages(cmd) {
			model, follow := app.Update(msg)
			app = model.(App)
			_ = collectTeaMessages(follow)
		}

		if len(runtime.runInputs) != 0 {
			t.Fatalf("expected slash command not to call runtime.Run, got %+v", runtime.runInputs)
		}
		if runtime.setCalls != 0 {
			t.Fatalf("expected draft workspace change not to call SetSessionWorkdir")
		}
		if app.state.CurrentWorkdir != target {
			t.Fatalf("expected current workdir %q, got %q", target, app.state.CurrentWorkdir)
		}
		if !strings.Contains(app.state.StatusText, "Draft workspace switched") {
			t.Fatalf("expected draft workspace switch status, got %q", app.state.StatusText)
		}
	})

	t.Run("session workspace command updates runtime session workdir", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		sessionID := "session-workdir"
		runtime.loads[sessionID] = agentruntime.Session{ID: sessionID, Workdir: t.TempDir()}

		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		app.state.ActiveSessionID = sessionID
		target := t.TempDir()

		app.input.SetValue("/cwd " + target)
		app.state.InputText = app.input.Value()

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		for _, msg := range collectTeaMessages(cmd) {
			model, follow := app.Update(msg)
			app = model.(App)
			_ = collectTeaMessages(follow)
		}

		if runtime.setCalls != 1 {
			t.Fatalf("expected SetSessionWorkdir to be called once, got %d", runtime.setCalls)
		}
		if app.state.CurrentWorkdir != target {
			t.Fatalf("expected current workdir %q, got %q", target, app.state.CurrentWorkdir)
		}
		if !strings.Contains(app.state.StatusText, "Session workspace switched") {
			t.Fatalf("expected session workspace switch status, got %q", app.state.StatusText)
		}
	})
}

func TestRunSessionWorkdirCommandBranches(t *testing.T) {
	t.Run("invalid slash command returns parser error", func(t *testing.T) {
		msg := runSessionWorkdirCommand(newStubRuntime(), "", "", "/status")()
		result, ok := msg.(sessionWorkdirResultMsg)
		if !ok {
			t.Fatalf("expected sessionWorkdirResultMsg, got %T", msg)
		}
		if result.err == nil || !strings.Contains(result.err.Error(), "unknown command") {
			t.Fatalf("expected unknown command error, got %+v", result)
		}
	})

	t.Run("empty workspace query without current workdir returns usage", func(t *testing.T) {
		msg := runSessionWorkdirCommand(newStubRuntime(), "", "", "/cwd")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err == nil || !strings.Contains(result.err.Error(), "usage: /cwd <path>") {
			t.Fatalf("expected usage error, got %+v", result)
		}
	})

	t.Run("empty workspace query with current workdir shows current directory", func(t *testing.T) {
		current := t.TempDir()
		msg := runSessionWorkdirCommand(newStubRuntime(), "", current, "/cwd")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err != nil {
			t.Fatalf("unexpected error: %v", result.err)
		}
		if result.workdir != current || !strings.Contains(result.notice, "Current workspace is") {
			t.Fatalf("expected current workspace message, got %+v", result)
		}
	})

	t.Run("session workdir change surfaces runtime error", func(t *testing.T) {
		runtime := newStubRuntime()
		runtime.setWorkdirErr = errors.New("set workdir failed")
		msg := runSessionWorkdirCommand(runtime, "session-1", t.TempDir(), "/cwd ./subdir")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err == nil || !strings.Contains(result.err.Error(), "set workdir failed") {
			t.Fatalf("expected set workdir error, got %+v", result)
		}
	})

	t.Run("session workdir fallback uses current workdir when runtime returns empty", func(t *testing.T) {
		current := t.TempDir()
		runtime := newStubRuntime()
		runtime.setResult = &agentruntime.Session{ID: "session-1", Workdir: ""}
		msg := runSessionWorkdirCommand(runtime, "session-1", current, "/cwd ./subdir")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err != nil {
			t.Fatalf("unexpected error: %v", result.err)
		}
		if result.workdir != current {
			t.Fatalf("expected fallback workdir %q, got %q", current, result.workdir)
		}
	})

	t.Run("session workdir change uses runtime returned workdir when available", func(t *testing.T) {
		current := t.TempDir()
		target := t.TempDir()
		runtime := newStubRuntime()
		runtime.setResult = &agentruntime.Session{ID: "session-1", Workdir: target}
		msg := runSessionWorkdirCommand(runtime, "session-1", current, "/cwd ./subdir")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err != nil {
			t.Fatalf("unexpected error: %v", result.err)
		}
		if result.workdir != target || !strings.Contains(result.notice, "Session workspace switched") {
			t.Fatalf("expected runtime returned workdir %q, got %+v", target, result)
		}
	})

	t.Run("draft workspace change returns resolve error for missing path", func(t *testing.T) {
		msg := runSessionWorkdirCommand(newStubRuntime(), "", t.TempDir(), "/cwd ./missing-path")()
		result := msg.(sessionWorkdirResultMsg)
		if result.err == nil || !strings.Contains(strings.ToLower(result.err.Error()), "resolve path") {
			t.Fatalf("expected resolve path error, got %+v", result)
		}
	})
}

func TestResolveWorkspacePathAndSelector(t *testing.T) {
	t.Run("resolve from empty base falls back to process cwd", func(t *testing.T) {
		resolved, err := resolveWorkspacePath("", ".")
		if err != nil {
			t.Fatalf("resolveWorkspacePath() error = %v", err)
		}
		expected, err := filepath.Abs(".")
		if err != nil {
			t.Fatalf("filepath.Abs(.) error = %v", err)
		}
		if resolved != filepath.Clean(expected) {
			t.Fatalf("expected %q, got %q", filepath.Clean(expected), resolved)
		}
	})

	t.Run("resolve relative path from base", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "sub")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		resolved, err := resolveWorkspacePath(base, "sub")
		if err != nil {
			t.Fatalf("resolveWorkspacePath() error = %v", err)
		}
		if resolved != filepath.Clean(target) {
			t.Fatalf("expected %q, got %q", filepath.Clean(target), resolved)
		}
	})

	t.Run("resolve workspace path rejects non-directory", func(t *testing.T) {
		base := t.TempDir()
		file := filepath.Join(base, "note.txt")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		_, err := resolveWorkspacePath(base, "note.txt")
		if err == nil || !strings.Contains(err.Error(), "is not a directory") {
			t.Fatalf("expected non-directory error, got %v", err)
		}
	})

	t.Run("resolve workspace path returns error for missing target", func(t *testing.T) {
		base := t.TempDir()
		_, err := resolveWorkspacePath(base, "missing-dir")
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "resolve path") {
			t.Fatalf("expected missing path error, got %v", err)
		}
	})

	t.Run("select session workdir prefers session value", func(t *testing.T) {
		if got := selectSessionWorkdir("  /session  ", "/default"); got != "/session" {
			t.Fatalf("expected trimmed session workdir, got %q", got)
		}
		if got := selectSessionWorkdir("", " /default "); got != "/default" {
			t.Fatalf("expected fallback default workdir, got %q", got)
		}
	})
}

func TestAppUpdateSessionWorkdirResultMessage(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	model, cmd := app.Update(sessionWorkdirResultMsg{
		notice:  "ok",
		workdir: filepath.Join(t.TempDir(), "missing-dir"),
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.state.ExecutionError == "" || !strings.Contains(strings.ToLower(app.state.ExecutionError), "missing-dir") {
		t.Fatalf("expected refresh workspace file error, got %q", app.state.ExecutionError)
	}
	if app.state.StatusText == "ok" {
		t.Fatalf("expected status text to switch to refresh error, got %q", app.state.StatusText)
	}
}

func TestRunAgentWorkdirForwarding(t *testing.T) {
	t.Run("draft run forwards current workdir", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		target := t.TempDir()
		app.state.CurrentWorkdir = target
		app.input.SetValue("hello")
		app.state.InputText = "hello"

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 1 {
			t.Fatalf("expected one runtime input, got %d", len(runtime.runInputs))
		}
		if runtime.runInputs[0].Workdir != target {
			t.Fatalf("expected draft run workdir %q, got %q", target, runtime.runInputs[0].Workdir)
		}
	})

	t.Run("session run uses persisted session workdir", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		app.state.ActiveSessionID = "session-1"
		app.state.CurrentWorkdir = t.TempDir()
		app.input.SetValue("hello")
		app.state.InputText = "hello"

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 1 {
			t.Fatalf("expected one runtime input, got %d", len(runtime.runInputs))
		}
		if runtime.runInputs[0].Workdir != "" {
			t.Fatalf("expected session run to omit workdir override, got %q", runtime.runInputs[0].Workdir)
		}
	})
}

func TestHandlePermissionDecisionKey(t *testing.T) {
	t.Parallel()

	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.pendingPermission = &pendingPermissionPrompt{
		RequestID: "perm-1",
		ToolName:  "webfetch",
	}

	tests := []struct {
		name     string
		key      tea.KeyMsg
		wantSent agentruntime.PermissionResolutionDecision
		handled  bool
	}{
		{
			name:     "y maps to allow once",
			key:      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}},
			wantSent: agentruntime.PermissionResolutionAllowOnce,
			handled:  true,
		},
		{
			name:     "a maps to allow session",
			key:      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}},
			wantSent: agentruntime.PermissionResolutionAllowSession,
			handled:  true,
		},
		{
			name:     "n maps to reject",
			key:      tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}},
			wantSent: agentruntime.PermissionResolutionReject,
			handled:  true,
		},
		{
			name:    "other key ignored",
			key:     tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}},
			handled: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			app.pendingPermission.Submitted = false
			runtime.resolveInputs = nil

			cmd, handled := app.handlePermissionDecisionKey(tt.key)
			if handled != tt.handled {
				t.Fatalf("expected handled=%v, got %v", tt.handled, handled)
			}
			if !tt.handled {
				if cmd != nil {
					t.Fatalf("expected nil cmd for unhandled key")
				}
				return
			}
			if cmd == nil {
				t.Fatalf("expected resolve cmd")
			}
			msg := cmd()
			result, ok := msg.(permissionResolveResultMsg)
			if !ok {
				t.Fatalf("expected permissionResolveResultMsg, got %T", msg)
			}
			if result.err != nil {
				t.Fatalf("expected nil resolve error, got %v", result.err)
			}
			if len(runtime.resolveInputs) == 0 {
				t.Fatalf("expected runtime resolve inputs")
			}
			last := runtime.resolveInputs[len(runtime.resolveInputs)-1]
			if last.Decision != tt.wantSent {
				t.Fatalf("expected decision %q, got %q", tt.wantSent, last.Decision)
			}
			if strings.TrimSpace(last.RequestID) != "perm-1" {
				t.Fatalf("expected request id perm-1, got %q", last.RequestID)
			}
		})
	}
}

func TestHandlePermissionDecisionKeyIgnoresRepeatedSubmission(t *testing.T) {
	t.Parallel()

	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	app.pendingPermission = &pendingPermissionPrompt{
		RequestID: "perm-repeat",
		ToolName:  "webfetch",
	}

	cmd, handled := app.handlePermissionDecisionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !handled || cmd == nil {
		t.Fatalf("expected first permission key to be handled with cmd")
	}
	msg := cmd()
	result, ok := msg.(permissionResolveResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("expected successful permissionResolveResultMsg, got %#v", msg)
	}
	if len(runtime.resolveInputs) != 1 {
		t.Fatalf("expected one resolve call after first submission, got %d", len(runtime.resolveInputs))
	}

	cmd, handled = app.handlePermissionDecisionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !handled {
		t.Fatalf("expected repeated permission key to be consumed")
	}
	if cmd != nil {
		t.Fatalf("expected repeated submission to skip runtime command")
	}
	if len(runtime.resolveInputs) != 1 {
		t.Fatalf("expected resolve call count unchanged after repeat, got %d", len(runtime.resolveInputs))
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
				app.state.ActivePicker = pickerModel
				app.focus = panelInput
			},
			msg: tea.KeyMsg{Type: tea.KeyEsc},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if app.state.ActivePicker != pickerNone {
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
			name: "runtime canceled clears running state without error",
			setup: func(t *testing.T, app *App, runtime *stubRuntime) {
				app.state.IsAgentRunning = true
				app.state.ActiveSessionID = "session-cancel"
			},
			msg: RuntimeMsg{Event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventRunCanceled,
				SessionID: "session-cancel",
			}},
			assert: func(t *testing.T, runtime *stubRuntime, app App, msgs []tea.Msg) {
				t.Helper()
				if app.state.IsAgentRunning {
					t.Fatalf("expected agent to stop running")
				}
				if app.state.ExecutionError != "" || app.state.StatusText != statusCanceled {
					t.Fatalf("expected canceled status without error, got %+v", app.state)
				}
				if len(app.activeMessages) != 0 {
					t.Fatalf("expected cancel notice to stay out of transcript, got %+v", app.activeMessages)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Canceled current run" {
					t.Fatalf("expected cancel notice in activity, got %+v", app.activities)
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
				if len(app.activeMessages) == 0 || app.activeMessages[len(app.activeMessages)-1].Role != roleTool {
					t.Fatalf("expected tool result to stay in transcript, got %+v", app.activeMessages)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Tool error" {
					t.Fatalf("expected tool error in activity, got %+v", app.activities)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()
			app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
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

	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.Init() == nil {
		t.Fatalf("expected init command")
	}

	app.openModelPicker()
	app.closePicker()
	app.selectCurrentModel(config.OpenAIDefaultModel)
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
	app.state.ActivePicker = pickerModel
	if app.renderPicker(48, 12) == "" || app.renderWaterfall(80, 20) == "" {
		t.Fatalf("expected model picker rendering")
	}
	app.state.ActivePicker = pickerNone
	app.refreshCommandMenu()
	if app.renderCommandMenu(80) == "" {
		app.input.SetValue("/")
		app.state.InputText = "/"
		app.refreshCommandMenu()
		if app.renderCommandMenu(80) == "" {
			t.Fatalf("expected slash command menu when input starts with slash")
		}
	}
	app.input.SetValue("/status")
	app.state.InputText = "/status"
	app.refreshCommandMenu()
	if menu := app.renderCommandMenu(80); menu != "" {
		t.Fatalf("expected complete slash command to hide menu, got %q", menu)
	}
	if app.renderPrompt(80) == "" || app.renderHelp(80) == "" {
		t.Fatalf("expected prompt and help output")
	}
	app.state.StatusText = "Status:\nSession: Draft\nProvider: openll"
	if lipgloss.Height(app.renderHeader(app.computeLayout().contentWidth)) != 1 {
		t.Fatalf("expected header to remain a single line even with multiline status text")
	}
	if lipgloss.Width(app.renderPrompt(80)) != 80 {
		t.Fatalf("expected prompt width 80, got %d", lipgloss.Width(app.renderPrompt(80)))
	}
	if got := newKeyMap().Send.Help().Key; got != "Enter" {
		t.Fatalf("expected send shortcut help to use Enter, got %q", got)
	}
	if got := newKeyMap().Send.Keys(); len(got) != 1 || got[0] != "enter" {
		t.Fatalf("expected send binding to use enter, got %+v", got)
	}
	if got := newKeyMap().Newline.Help().Key; got != "Ctrl+J" {
		t.Fatalf("expected newline shortcut help to use Ctrl+J, got %q", got)
	}
	if got := newKeyMap().Newline.Keys(); len(got) != 1 || got[0] != "ctrl+j" {
		t.Fatalf("expected newline binding to use ctrl+j, got %+v", got)
	}
	if !strings.Contains(app.renderHelp(80), "Ctrl+J") {
		t.Fatalf("expected footer help to render newline shortcut")
	}
	sidebar := app.renderSidebar(26, 12)
	if lipgloss.Width(sidebar) != 26 || lipgloss.Height(sidebar) != 12 {
		t.Fatalf("expected sidebar to respect requested dimensions, got %dx%d", lipgloss.Width(sidebar), lipgloss.Height(sidebar))
	}
	if !strings.Contains(app.renderSidebar(26, 12), sidebarTitle) || !strings.Contains(app.renderSidebar(26, 12), sidebarOpenHint) {
		t.Fatalf("expected updated sidebar header text")
	}
	if strings.Contains(app.renderPrompt(80), "Enter sends, Ctrl+J inserts a newline") {
		t.Fatalf("expected keyboard hint to move out of placeholder text")
	}
	if strings.TrimSpace(app.renderPrompt(80)) == "" {
		t.Fatalf("expected prompt to render a visible border")
	}
	app.input.SetValue("one")
	app.state.InputText = "one"
	app.applyComponentLayout(true)
	if app.input.Height() != 1 {
		t.Fatalf("expected single-line composer height 1, got %d", app.input.Height())
	}
	if strings.Count(app.renderPrompt(80), "> ") < 1 {
		t.Fatalf("expected single-line prompt to render composer prefix")
	}
	app.input.SetValue("one\ntwo")
	app.state.InputText = app.input.Value()
	app.applyComponentLayout(true)
	if app.input.Height() != 2 {
		t.Fatalf("expected two-line composer height 2, got %d", app.input.Height())
	}
	if strings.Count(app.renderPrompt(80), "> ") < 2 {
		t.Fatalf("expected multi-line prompt to repeat composer prefix")
	}
	app.input.SetValue(strings.Join([]string{"1", "2", "3", "4", "5", "6"}, "\n"))
	app.state.InputText = app.input.Value()
	app.applyComponentLayout(true)
	if app.input.Height() != composerMaxHeight {
		t.Fatalf("expected composer height capped at %d, got %d", composerMaxHeight, app.input.Height())
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
	if rendered, _ := app.renderMessageBlockWithCopy(provider.Message{Role: roleError, Content: "boom"}, 80, 1); rendered == "" {
		t.Fatalf("expected error message block")
	}
	if rendered, _ := app.renderMessageBlockWithCopy(provider.Message{
		Role: roleAssistant,
		ToolCalls: []provider.ToolCall{
			{Name: "filesystem_edit"},
		},
	}, 80, 1); rendered == "" {
		t.Fatalf("expected tool call message block")
	}
	renderedCodeOnly, _ := app.renderMessageContentWithCopy("```go\nfmt.Println(\"x\")\n```", 80, app.styles.messageBody, 1)
	if renderedCodeOnly == "" {
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

	mItem := selectionItem{name: "gpt-5.4", description: "Frontier"}
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
	model := newSelectionPickerItems(mapModelItems([]config.ModelDescriptor{{ID: "gpt-4.1", Name: "gpt-4.1"}}))
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
	runMsg := runAgent(runtime, "session-x", "", "hello")()
	if _, ok := runMsg.(runFinishedMsg); !ok {
		t.Fatalf("expected runFinishedMsg")
	}
	if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "hello" {
		t.Fatalf("expected runtime run input to be captured")
	}

	manager := newTestConfigManager(t)
	msg := runModelSelection(newTestProviderService(t, manager), config.OpenAIDefaultModel)()
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
			name: "run finished canceled is not surfaced as error",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.state.IsAgentRunning = true
				app.state.StatusText = statusCanceling
			},
			msg: runFinishedMsg{err: context.Canceled},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.IsAgentRunning || app.state.ExecutionError != "" || app.state.StatusText != statusCanceled {
					t.Fatalf("expected canceled run to stop without error, got %+v", app.state)
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
			name: "model selection success updates state",
			msg:  localCommandResultMsg{notice: "[System] ok"},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.StatusText != "[System] ok" {
					t.Fatalf("expected success notice, got %q", app.state.StatusText)
				}
			},
		},
		{
			name: "model selection error updates state",
			msg:  localCommandResultMsg{err: context.Canceled},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.state.ExecutionError == "" || app.state.StatusText == "" {
					t.Fatalf("expected local command error state")
				}
			},
		},
		{
			name: "cancel shortcut interrupts running agent",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.state.IsAgentRunning = true
				app.state.StatusText = statusThinking
				runtime.cancelResult = true
				app.keys.CancelAgent.SetKeys("ctrl+@")
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlAt},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if runtime.cancelCalls != 1 {
					t.Fatalf("expected cancel to be called once, got %d", runtime.cancelCalls)
				}
				if app.state.StatusText != statusCanceling {
					t.Fatalf("expected canceling status, got %q", app.state.StatusText)
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
			name: "tab in non-empty input inserts indentation instead of switching panel",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.focus = panelInput
				app.applyFocus()
				app.input.SetValue("func main() {\n")
				app.state.InputText = app.input.Value()
				app.applyComponentLayout(true)
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.focus != panelInput {
					t.Fatalf("expected focus to stay in input, got %v", app.focus)
				}
				if !strings.Contains(app.state.InputText, "func main() {") {
					t.Fatalf("expected existing code to be preserved, got %q", app.state.InputText)
				}
				if !strings.HasSuffix(app.state.InputText, "\n\t") && !strings.HasSuffix(app.state.InputText, "\n    ") {
					t.Fatalf("expected tab indentation at the end, got %q", app.state.InputText)
				}
			},
		},
		{
			name: "previous panel moves focus backward",
			msg:  tea.KeyMsg{Type: tea.KeyShiftTab},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.focus != panelActivity {
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
			name: "ctrl+j inserts newline without sending",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue("inspect repo")
				app.state.InputText = "inspect repo"
				app.applyComponentLayout(true)
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlJ},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected ctrl+j not to send input, got %+v", runtime.runInputs)
				}
				if app.state.InputText != "inspect repo\n" {
					t.Fatalf("expected newline to be inserted, got %q", app.state.InputText)
				}
				if app.input.Height() != 2 {
					t.Fatalf("expected composer height to grow to 2, got %d", app.input.Height())
				}
				prompt := app.renderPrompt(80)
				if !strings.Contains(prompt, "inspect repo") {
					t.Fatalf("expected first line to remain visible after newline, got %q", prompt)
				}
				if strings.Count(prompt, "> ") < 2 {
					t.Fatalf("expected both lines to keep prompt prefix, got %q", prompt)
				}
			},
		},
		{
			name: "second ctrl+j grows composer to third line",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue("line1\nline2")
				app.state.InputText = app.input.Value()
				app.applyComponentLayout(true)
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlJ},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if len(runtime.runInputs) != 0 {
					t.Fatalf("expected second ctrl+j not to send input")
				}
				if app.input.Height() != 3 {
					t.Fatalf("expected composer height to grow to 3, got %d", app.input.Height())
				}
				prompt := app.renderPrompt(80)
				if !strings.Contains(prompt, "line1") || !strings.Contains(prompt, "line2") {
					t.Fatalf("expected previous lines to remain visible, got %q", prompt)
				}
				if strings.Count(prompt, "> ") < 3 {
					t.Fatalf("expected all three lines to keep prompt prefix, got %q", prompt)
				}
			},
		},
		{
			name: "plain multiline input enter starts runtime",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue("inspect repo\nwith details")
				app.state.InputText = "inspect repo\nwith details"
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
				if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "inspect repo\nwith details" {
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
			name: "blank input enter stays local",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue(" \n ")
				app.state.InputText = " \n "
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if len(runtime.runInputs) != 0 || app.state.IsAgentRunning {
					t.Fatalf("expected blank input enter not to send")
				}
				if app.state.InputText != " \n " {
					t.Fatalf("expected blank input to stay unchanged on enter, got %q", app.state.InputText)
				}
			},
		},
		{
			name: "blank input ctrl+j inserts newline locally",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue(" \n ")
				app.state.InputText = " \n "
				app.applyComponentLayout(true)
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlJ},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if len(runtime.runInputs) != 0 || app.state.IsAgentRunning {
					t.Fatalf("expected blank input ctrl+j not to send")
				}
				if app.state.InputText != " \n \n" {
					t.Fatalf("expected ctrl+j to insert newline into blank input, got %q", app.state.InputText)
				}
				if app.input.Height() != 3 {
					t.Fatalf("expected composer height to grow to 3, got %d", app.input.Height())
				}
			},
		},
		{
			name: "delete newline shrinks composer height",
			setup: func(t *testing.T, app *App, runtime *stubRuntime, manager *config.Manager) {
				app.input.SetValue("line1\n")
				app.state.InputText = app.input.Value()
				app.applyComponentLayout(true)
			},
			msg: tea.KeyMsg{Type: tea.KeyBackspace},
			assert: func(t *testing.T, app App, runtime *stubRuntime, manager *config.Manager, msgs []tea.Msg) {
				t.Helper()
				if app.input.Height() != 1 {
					t.Fatalf("expected composer height to shrink to 1, got %d", app.input.Height())
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
			app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
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

func TestAppUpdatePasteEnterGuard(t *testing.T) {
	t.Run("paste-like burst keeps enter as newline", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		now := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
		app.nowFn = func() time.Time { return now }

		for _, r := range []rune("function_name") {
			model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			app = model.(App)
			_ = collectTeaMessages(cmd)
			now = now.Add(15 * time.Millisecond)
		}

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 0 {
			t.Fatalf("expected enter to stay local during paste-like burst, got %+v", runtime.runInputs)
		}
		if app.state.InputText != "function_name\n" {
			t.Fatalf("expected enter to insert newline, got %q", app.state.InputText)
		}
		if app.state.IsAgentRunning {
			t.Fatalf("expected agent to remain idle")
		}
	})

	t.Run("normal typing enter still sends", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		now := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
		app.nowFn = func() time.Time { return now }

		for _, r := range []rune("hello") {
			model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
			app = model.(App)
			_ = collectTeaMessages(cmd)
			now = now.Add(300 * time.Millisecond)
		}

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		msgs := collectTeaMessages(cmd)

		if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "hello" {
			t.Fatalf("expected enter to send normal input once, got %+v", runtime.runInputs)
		}
		if app.state.InputText != "" {
			t.Fatalf("expected input to reset after send, got %q", app.state.InputText)
		}
		for _, msg := range msgs {
			model, follow := app.Update(msg)
			app = model.(App)
			_ = collectTeaMessages(follow)
		}
	})

	t.Run("explicit paste enter inserts newline", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		app.input.SetValue("before")
		app.state.InputText = "before"

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 0 {
			t.Fatalf("expected paste enter not to send, got %+v", runtime.runInputs)
		}
		if app.state.InputText != "before\n" {
			t.Fatalf("expected paste enter to insert newline, got %q", app.state.InputText)
		}
	})

	t.Run("segmented long paste keeps enter as newline across chunks", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		now := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
		app.nowFn = func() time.Time { return now }

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("package main")})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		now = now.Add(80 * time.Millisecond)
		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		now = now.Add(80 * time.Millisecond)
		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("func main() {}")})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		now = now.Add(80 * time.Millisecond)
		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 0 {
			t.Fatalf("expected segmented paste not to trigger send, got %+v", runtime.runInputs)
		}
		if app.state.InputText != "package main\nfunc main() {}\n" {
			t.Fatalf("expected multiline pasted content, got %q", app.state.InputText)
		}
	})

	t.Run("long gap after paste-like input allows immediate send", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		now := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
		app.nowFn = func() time.Time { return now }

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1")})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		now = now.Add(2 * time.Second)
		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "line1" {
			t.Fatalf("expected enter to send after long gap, got %+v", runtime.runInputs)
		}
	})

	t.Run("enter sends after paste session expires", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		now := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
		app.nowFn = func() time.Time { return now }

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\nline2")})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		now = now.Add(pasteSessionGuard + 100*time.Millisecond)
		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)

		if len(runtime.runInputs) != 1 || runtime.runInputs[0].Content != "line1\nline2" {
			t.Fatalf("expected send after paste session expiry, got %+v", runtime.runInputs)
		}
	})

	t.Run("enter sends right after tab indentation in normal input", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		app.input.SetValue("hello")
		app.state.InputText = app.input.Value()
		app.focus = panelInput
		app.applyFocus()

		model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyTab})
		app = model.(App)
		_ = collectTeaMessages(cmd)
		if !strings.Contains(app.state.InputText, "hello") {
			t.Fatalf("expected tab to keep current input, got %q", app.state.InputText)
		}

		model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		app = model.(App)
		_ = collectTeaMessages(cmd)
		if len(runtime.runInputs) != 1 {
			t.Fatalf("expected enter to send after tab indentation, got %+v", runtime.runInputs)
		}
	})
}

func TestAppUpdateModelPickerEnterAppliesSelection(t *testing.T) {
	manager := newTestConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("set unsupported current model: %v", err)
	}
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.openModelPicker()
	if len(app.modelPicker.Items()) == 0 {
		t.Fatalf("expected model picker catalog")
	}
	selected := app.modelPicker.Items()[0].(selectionItem).id
	app.modelPicker.Select(0)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close after selection")
	}

	if cmd != nil {
		if msg := cmd(); msg != nil {
			model, follow := app.Update(msg)
			app = model.(App)
			_ = collectTeaMessages(follow)
		}
	}

	cfg := manager.Get()
	if cfg.CurrentModel != selected {
		t.Fatalf("expected current model %q, got %q", selected, cfg.CurrentModel)
	}
}

func TestAppUpdateProviderPickerEnterAppliesSelection(t *testing.T) {
	manager := newTestConfigManager(t)
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.CurrentModel = "unsupported-current"
		return nil
	}); err != nil {
		t.Fatalf("set unsupported current model: %v", err)
	}

	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.openProviderPicker()
	if len(app.providerPicker.Items()) < 2 {
		t.Fatalf("expected provider picker catalog")
	}
	selectedIndex := -1
	selected := ""
	for idx, item := range app.providerPicker.Items() {
		candidate, ok := item.(selectionItem)
		if !ok {
			continue
		}
		if candidate.id == config.QiniuName {
			selectedIndex = idx
			selected = candidate.id
			break
		}
	}
	if selectedIndex < 0 {
		t.Fatalf("expected provider picker to include %s", config.QiniuName)
	}
	app.providerPicker.Select(selectedIndex)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.state.ActivePicker != pickerNone {
		t.Fatalf("expected picker to close after selection")
	}

	for _, msg := range collectTeaMessages(cmd) {
		model, follow := app.Update(msg)
		app = model.(App)
		_ = collectTeaMessages(follow)
	}

	cfg := manager.Get()
	if cfg.SelectedProvider != selected {
		t.Fatalf("expected selected provider %q, got %q", selected, cfg.SelectedProvider)
	}
	if cfg.CurrentModel != config.QiniuDefaultModel {
		t.Fatalf("expected current model to follow provider default, got %q", cfg.CurrentModel)
	}
}

func TestRefreshPickerKeepsSizeAfterRebuild(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.providerPicker.SetSize(32, 9)
	app.modelPicker.SetSize(28, 7)

	if err := app.refreshProviderPicker(); err != nil {
		t.Fatalf("refreshProviderPicker() error = %v", err)
	}
	if err := app.refreshModelPicker(); err != nil {
		t.Fatalf("refreshModelPicker() error = %v", err)
	}

	if app.providerPicker.Width() != 32 || app.providerPicker.Height() != 9 {
		t.Fatalf("expected provider picker size 32x9, got %dx%d", app.providerPicker.Width(), app.providerPicker.Height())
	}
	if app.modelPicker.Width() != 28 || app.modelPicker.Height() != 7 {
		t.Fatalf("expected model picker size 28x7, got %dx%d", app.modelPicker.Width(), app.modelPicker.Height())
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
				if len(app.activeMessages) != 0 {
					t.Fatalf("expected tool start to stay out of transcript, got %+v", app.activeMessages)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Running tool" {
					t.Fatalf("expected tool start in activity, got %+v", app.activities)
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
				if len(app.activeMessages) == 0 || app.activeMessages[len(app.activeMessages)-1].Role != roleTool {
					t.Fatalf("expected tool result message in transcript, got %+v", app.activeMessages)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Completed tool" {
					t.Fatalf("expected tool completion in activity, got %+v", app.activities)
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
				if len(app.activeMessages) != 0 {
					t.Fatalf("expected runtime error to stay out of transcript, got %+v", app.activeMessages)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Runtime error" {
					t.Fatalf("expected runtime error in activity, got %+v", app.activities)
				}
			},
		},
		{
			name: "tool call thinking is tracked as activity",
			event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventToolCallThinking,
				SessionID: "s1",
				Payload:   "filesystem_edit",
			},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.CurrentTool != "filesystem_edit" {
					t.Fatalf("expected current tool to be populated, got %+v", app.state)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Planning tool call" {
					t.Fatalf("expected planning activity, got %+v", app.activities)
				}
			},
		},
		{
			name: "provider retry is tracked as activity",
			event: agentruntime.RuntimeEvent{
				Type:      agentruntime.EventProviderRetry,
				SessionID: "s1",
				Payload:   "retrying provider call (attempt 1/2, wait=1.0s)...",
			},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.StatusText != statusThinking {
					t.Fatalf("expected provider retry to preserve thinking status, got %+v", app.state)
				}
				if len(app.activities) == 0 || app.activities[len(app.activities)-1].Title != "Retrying provider call" {
					t.Fatalf("expected provider retry activity, got %+v", app.activities)
				}
			},
		},
		{
			name: "run canceled event clears current tool and error state",
			setup: func(app *App) {
				app.state.IsAgentRunning = true
				app.state.CurrentTool = "filesystem_edit"
				app.state.ExecutionError = "old"
			},
			event: agentruntime.RuntimeEvent{Type: agentruntime.EventRunCanceled, SessionID: "s1"},
			assert: func(t *testing.T, app App) {
				t.Helper()
				if app.state.IsAgentRunning || app.state.CurrentTool != "" || app.state.ExecutionError != "" || app.state.StatusText != statusCanceled {
					t.Fatalf("unexpected canceled state: %+v", app.state)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			manager := newTestConfigManager(t)
			runtime := newStubRuntime()
			app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
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

		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
		if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("expected list session error during New, got %v", err)
		}
		_ = app
	})

	t.Run("refresh messages returns load error", func(t *testing.T) {
		manager := newTestConfigManager(t)
		runtime := newStubRuntime()
		runtime.loadErr = context.Canceled

		app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
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

func TestImmediateSlashCommandsAndLayoutBranches(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	handled, cmd := app.handleImmediateSlashCommand("/help")
	if handled || cmd != nil {
		t.Fatalf("expected /help to stay on normal slash flow")
	}

	handled, cmd = app.handleImmediateSlashCommand("/clear")
	if !handled || cmd != nil {
		t.Fatalf("expected /clear to be handled locally")
	}
	if app.state.ActiveSessionID != "" || len(app.activeMessages) != 0 {
		t.Fatalf("expected /clear to reset draft state")
	}

	handled, cmd = app.handleImmediateSlashCommand("/compact")
	if !handled || cmd != nil {
		t.Fatalf("expected /compact without session to be handled locally")
	}
	if !strings.Contains(app.state.StatusText, "compact requires an existing session") {
		t.Fatalf("expected missing-session compact hint, got %q", app.state.StatusText)
	}
	if len(runtime.compactInputs) != 0 {
		t.Fatalf("expected no runtime compact call without session, got %+v", runtime.compactInputs)
	}

	runtime.compactResult = agentruntime.CompactResult{
		Applied:        true,
		BeforeChars:    100,
		AfterChars:     40,
		SavedRatio:     0.6,
		TriggerMode:    string(contextcompact.ModeManual),
		TranscriptPath: "/tmp/transcript.jsonl",
	}
	app.state.ActiveSessionID = "session-compact"
	handled, cmd = app.handleImmediateSlashCommand("/compact")
	if !handled || cmd == nil {
		t.Fatalf("expected /compact to trigger compact cmd")
	}
	if app.state.StatusText != statusCompacting {
		t.Fatalf("expected compact status %q, got %q", statusCompacting, app.state.StatusText)
	}
	if !app.state.IsCompacting {
		t.Fatalf("expected /compact to mark UI as compacting")
	}
	msgs := collectTeaMessages(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one compact command message, got %d", len(msgs))
	}
	if _, ok := msgs[0].(compactFinishedMsg); !ok {
		t.Fatalf("expected compact finished msg, got %T", msgs[0])
	}
	if len(runtime.compactInputs) != 1 || runtime.compactInputs[0].SessionID != "session-compact" {
		t.Fatalf("expected runtime compact call with active session, got %+v", runtime.compactInputs)
	}

	handled, cmd = app.handleImmediateSlashCommand("/compact")
	if !handled || cmd != nil {
		t.Fatalf("expected re-entrant /compact to be rejected while compacting")
	}
	if !strings.Contains(strings.ToLower(app.state.StatusText), "compact is already running") {
		t.Fatalf("expected compact busy hint, got %q", app.state.StatusText)
	}

	handled, cmd = app.handleImmediateSlashCommand("/compact now")
	if !handled || cmd != nil {
		t.Fatalf("expected /compact with args to be handled locally with usage error")
	}
	if !strings.Contains(app.state.StatusText, "usage: /compact") {
		t.Fatalf("expected /compact usage hint, got %q", app.state.StatusText)
	}

	handled, cmd = app.handleImmediateSlashCommand("/exit")
	if !handled || cmd == nil {
		t.Fatalf("expected /exit to return a quit cmd")
	}
	foundQuit := false
	for _, msg := range collectTeaMessages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			foundQuit = true
		}
	}
	if !foundQuit {
		t.Fatalf("expected quit msg from /exit")
	}

	app.state.IsAgentRunning = false
	app.transcript.Width = 40
	app.transcript.Height = 4
	app.transcript.SetContent(strings.Repeat("line\n", 20))
	app.transcript.GotoBottom()
	app.applyComponentLayout(false)
	if app.transcript.Width <= 0 || app.transcript.Height <= 0 {
		t.Fatalf("expected resizeComposerLayout to keep transcript dimensions positive")
	}

	snapshot := app.currentStatusSnapshot()
	if snapshot.FocusLabel == "" || snapshot.CurrentProvider == "" || snapshot.CurrentModel == "" {
		t.Fatalf("expected non-empty status snapshot fields, got %+v", snapshot)
	}
}

func TestCompactEventAndBusyBranches(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.state.ActiveSessionID = "s1"
	dirty := app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventCompactDone,
		SessionID: "s1",
		Payload: agentruntime.CompactDonePayload{
			Applied:        true,
			BeforeChars:    100,
			AfterChars:     60,
			SavedRatio:     0.4,
			TriggerMode:    string(contextcompact.ModeManual),
			TranscriptPath: "/tmp/t.jsonl",
		},
	})
	if !dirty {
		t.Fatalf("expected compact done to dirty transcript")
	}
	if !strings.Contains(app.state.StatusText, "Compact(manual)") {
		t.Fatalf("expected compact status text, got %q", app.state.StatusText)
	}
	if len(app.activeMessages) == 0 || !strings.Contains(app.activeMessages[len(app.activeMessages)-1].Content, "Compact(manual)") {
		t.Fatalf("expected compact inline notice, got %+v", app.activeMessages)
	}

	dirty = app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventCompactError,
		SessionID: "s1",
		Payload: agentruntime.CompactErrorPayload{
			TriggerMode: string(contextcompact.ModeManual),
			Message:     "disk full",
		},
	})
	if !dirty {
		t.Fatalf("expected compact error to dirty transcript")
	}
	if !strings.Contains(app.state.ExecutionError, "Compact(manual) failed: disk full") {
		t.Fatalf("expected compact error in state, got %q", app.state.ExecutionError)
	}

	app.state.IsCompacting = true
	app.state.ActiveSessionID = "session-existing"
	app.state.ActiveSessionTitle = "Existing"
	app.input.SetValue("hello")
	app.state.InputText = "hello"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if len(runtime.runInputs) != 0 {
		t.Fatalf("expected send to be blocked while compacting, got %+v", runtime.runInputs)
	}
	if app.state.InputText != "hello" {
		t.Fatalf("expected input unchanged while compacting, got %q", app.state.InputText)
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.state.ActiveSessionID != "session-existing" {
		t.Fatalf("expected new-session shortcut to be blocked while compacting")
	}

	model, cmd = app.Update(compactFinishedMsg{err: nil})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.state.IsCompacting {
		t.Fatalf("expected compact finished message to clear busy compacting state")
	}
}

func TestAdditionalRenderingAndToolChunkBranches(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.state.ActiveSessionID = "session-tool"
	app.handleRuntimeEvent(agentruntime.RuntimeEvent{
		Type:      agentruntime.EventToolChunk,
		SessionID: "session-tool",
		Payload:   "chunk output",
	})
	if app.state.StatusText != statusRunningTool {
		t.Fatalf("expected tool chunk to keep running status, got %q", app.state.StatusText)
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected tool chunk to stay out of transcript, got %+v", app.activeMessages)
	}
	if len(app.activities) == 0 || !strings.Contains(app.activities[len(app.activities)-1].Detail, "chunk output") {
		t.Fatalf("expected tool chunk preview in activity, got %+v", app.activities)
	}

	if got := wrapCodeBlock("a\tb", 3); !strings.Contains(got, "\n") {
		t.Fatalf("expected tabs to expand and wrap, got %q", got)
	}
	if got := wrapCodeBlock("abc", 0); got != "abc" {
		t.Fatalf("expected width<=0 to return original text, got %q", got)
	}

	rendered, _ := app.renderMessageContentWithCopy("```\n```", 20, app.styles.messageBody, 1)
	if !strings.Contains(stripANSI(rendered), emptyMessageText) {
		t.Fatalf("expected empty code block placeholder, got %q", rendered)
	}
}

func TestHandleViewportKeysPageScrollingUsesFullPage(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.transcript.SetContent(strings.Repeat("line\n", 120))
	app.transcript.Height = 10
	app.transcript.GotoTop()

	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyPgDown})
	if app.transcript.YOffset != 10 {
		t.Fatalf("expected page down to move a full page, got offset %d", app.transcript.YOffset)
	}

	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyPgUp})
	if app.transcript.YOffset != 0 {
		t.Fatalf("expected page up to return a full page, got offset %d", app.transcript.YOffset)
	}

	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyEnd})
	if !app.transcript.AtBottom() {
		t.Fatalf("expected end to jump to bottom")
	}

	app.handleViewportKeys(&app.transcript, tea.KeyMsg{Type: tea.KeyHome})
	if !app.transcript.AtTop() {
		t.Fatalf("expected home to jump to top")
	}
}

func TestTranscriptMouseWheelScrollsOnlyInsideTranscript(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.width = 128
	app.height = 40
	app.applyComponentLayout(true)
	app.transcript.SetContent(strings.Repeat("line\n", 160))
	app.transcript.GotoTop()

	x, y, _, _ := app.transcriptBounds()
	model, cmd := app.Update(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonWheelDown,
		Type:   tea.MouseWheelDown,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.transcript.YOffset != mouseWheelStepLines {
		t.Fatalf("expected wheel down to scroll transcript by %d lines, got %d", mouseWheelStepLines, app.transcript.YOffset)
	}

	model, cmd = app.Update(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonWheelUp,
		Type:   tea.MouseWheelUp,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.transcript.YOffset != 0 {
		t.Fatalf("expected wheel up to scroll transcript back to top, got %d", app.transcript.YOffset)
	}

	model, cmd = app.Update(tea.MouseMsg{
		X:      0,
		Y:      0,
		Button: tea.MouseButtonWheelDown,
		Type:   tea.MouseWheelDown,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.transcript.YOffset != 0 {
		t.Fatalf("expected wheel event outside transcript to be ignored, got %d", app.transcript.YOffset)
	}

	app.transcript.Height = 0
	if app.isMouseWithinTranscript(tea.MouseMsg{X: x + 1, Y: y + 1}) {
		t.Fatalf("expected zero-height transcript bounds to reject mouse hits")
	}

	app.transcript.Height = 10
	if app.handleTranscriptMouse(tea.MouseMsg{X: x + 1, Y: y + 1, Button: tea.MouseButtonLeft}) {
		t.Fatalf("expected non-wheel mouse button to be ignored")
	}

	app.width = 100
	app.height = 32
	app.applyComponentLayout(true)
	stackX, stackY, _, stackH := app.transcriptBounds()
	if stackH <= 0 || !app.isMouseWithinTranscript(tea.MouseMsg{X: stackX + 1, Y: stackY + 1}) {
		t.Fatalf("expected stacked layout transcript bounds to accept mouse hits")
	}
}

func TestInputMouseWheelScrollsComposer(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.width = 128
	app.height = 40
	app.input.SetValue(strings.Join([]string{
		"line1", "line2", "line3", "line4", "line5",
		"line6", "line7", "line8", "line9", "line10",
		"line11", "line12",
	}, "\n"))
	app.state.InputText = app.input.Value()
	app.applyComponentLayout(true)
	app.focus = panelTranscript
	app.applyFocus()

	initialLine := app.input.Line()
	if initialLine < 1 {
		t.Fatalf("expected cursor line to be >=1 for multiline input, got %d", initialLine)
	}
	pageStep := max(1, app.input.Height()-1)

	x, y, _, _ := app.inputBounds()
	model, cmd := app.Update(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonWheelUp,
		Type:   tea.MouseWheelUp,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.focus != panelInput {
		t.Fatalf("expected input wheel to focus input panel, got %v", app.focus)
	}
	if initialLine-app.input.Line() < pageStep-1 {
		t.Fatalf("expected wheel up in input to page-scroll by ~%d lines, got from %d to %d", pageStep, initialLine, app.input.Line())
	}

	lineAfterUp := app.input.Line()
	model, cmd = app.Update(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonWheelDown,
		Type:   tea.MouseWheelDown,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.input.Line()-lineAfterUp < pageStep-1 {
		t.Fatalf("expected wheel down in input to page-scroll by ~%d lines, got from %d to %d", pageStep, lineAfterUp, app.input.Line())
	}

	lineBeforeOutside := app.input.Line()
	model, cmd = app.Update(tea.MouseMsg{
		X:      0,
		Y:      0,
		Button: tea.MouseButtonWheelUp,
		Type:   tea.MouseWheelUp,
	})
	app = model.(App)
	_ = collectTeaMessages(cmd)
	if app.input.Line() != lineBeforeOutside {
		t.Fatalf("expected wheel outside input to be ignored, got line=%d", app.input.Line())
	}
}

func TestInputCharLimitIsUnlimited(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.input.CharLimit != 0 {
		t.Fatalf("expected unlimited input char limit, got %d", app.input.CharLimit)
	}
}

func TestViewActivityPreviewAndStatusHelpers(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.activityPreviewHeight() != 0 || app.renderActivityPreview(80) != "" {
		t.Fatalf("expected empty activity state to render nothing")
	}

	fixed := time.Date(2026, 4, 2, 9, 30, 0, 0, time.UTC)
	app.activities = []activityEntry{
		{Time: fixed, Kind: "tool", Title: "first", Detail: "alpha"},
		{Time: fixed, Kind: "", Title: "second", Detail: ""},
		{Time: fixed, Kind: "provider", Title: "third", Detail: "retry"},
		{Time: fixed, Kind: "run", Title: "fourth", Detail: "done"},
	}
	app.applyComponentLayout(true)
	app.rebuildActivity()
	app.focus = panelActivity
	if app.focusLabel() != focusLabelActivity {
		t.Fatalf("expected activity focus label, got %q", app.focusLabel())
	}
	if app.activityPreviewHeight() != 6 {
		t.Fatalf("expected fixed activity preview height, got %d", app.activityPreviewHeight())
	}

	preview := app.renderActivityPreview(64)
	if !strings.Contains(preview, "second") || !strings.Contains(preview, "third") || !strings.Contains(preview, "fourth") {
		t.Fatalf("expected latest activity rows in preview, got %q", preview)
	}
	if strings.Contains(preview, "first") {
		t.Fatalf("expected oldest activity row to be clipped from current viewport, got %q", preview)
	}

	line := app.renderActivityLine(activityEntry{Time: fixed, Kind: "", Title: "single line", Detail: ""}, 80)
	if !strings.Contains(line, "EVENT") || strings.Contains(line, "single line:") {
		t.Fatalf("expected fallback kind without detail suffix, got %q", line)
	}

	rendered, _ := app.renderMessageContentWithCopy("before\n```go\nfmt.Println(1)\n```\nafter", 30, app.styles.messageBody, 1)
	rendered = stripANSI(rendered)
	if !strings.Contains(rendered, "before") || !strings.Contains(rendered, "fmt.Println(") || !strings.Contains(rendered, "1)") || !strings.Contains(rendered, "after") {
		t.Fatalf("expected mixed prose and code to render, got %q", rendered)
	}
	if !strings.Contains(rendered, "[Copy code #1]") {
		t.Fatalf("expected copy button alongside code block, got %q", rendered)
	}

	if got := compactStatusText("\n  hello   world \n", 0); got != "hello world" {
		t.Fatalf("expected compact status without truncation, got %q", got)
	}
	if got := compactStatusText("\n \n", 10); got != "" {
		t.Fatalf("expected empty compact status for blank input, got %q", got)
	}

	if app.statusBadge("failed request") == "" || app.statusBadge("canceled") == "" || app.statusBadge("ready") == "" {
		t.Fatalf("expected status badge branches to render non-empty output")
	}
}

func TestRenderMessageContentUsesMarkdownRenderer(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stub := &stubMarkdownRenderer{output: "markdown-rendered"}
	app.markdownRenderer = stub

	rendered, _ := app.renderMessageContentWithCopy("# Title\n\n- item", 40, app.styles.messageBody, 1)
	if !strings.Contains(rendered, "markdown-rendered") {
		t.Fatalf("expected markdown renderer output, got %q", rendered)
	}
	if stub.calls != 1 {
		t.Fatalf("expected markdown renderer to be called once, got %d", stub.calls)
	}
}

func TestRenderMessageBlockUserContentAlignsWithUserTag(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	renderedMessage, _ := app.renderMessageBlockWithCopy(provider.Message{Role: roleUser, Content: "nihao"}, 80, 1)
	rendered := stripANSI(renderedMessage)
	lines := strings.Split(rendered, "\n")

	tagLine := ""
	bodyLine := ""
	for _, line := range lines {
		if strings.Contains(line, messageTagUser) {
			tagLine = line
		}
		if strings.Contains(line, "nihao") {
			bodyLine = line
		}
	}
	if tagLine == "" || bodyLine == "" {
		t.Fatalf("expected user tag and body lines, got %q", rendered)
	}

	tagCol := strings.Index(tagLine, messageTagUser)
	bodyCol := strings.Index(bodyLine, "nihao")
	if tagCol < 0 || bodyCol < 0 {
		t.Fatalf("expected valid columns for user tag/body, got tag=%d body=%d", tagCol, bodyCol)
	}
	if bodyCol+6 < tagCol {
		t.Fatalf("expected user body to align near user tag, got tagCol=%d bodyCol=%d rendered=%q", tagCol, bodyCol, rendered)
	}
}

func TestRenderMessageContentNormalizesRightEdge(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.markdownRenderer = &stubMarkdownRenderer{
		output: "very long line\nshort\nmid",
	}

	renderedRaw, _ := app.renderMessageContentWithCopy("ignored", 40, app.styles.messageBody, 1)
	rendered := stripANSI(renderedRaw)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected multiline output, got %q", rendered)
	}

	firstWidth := len([]rune(lines[0]))
	for i, line := range lines[1:] {
		if len([]rune(line)) != firstWidth {
			t.Fatalf("expected aligned right edge, line %d width=%d first=%d rendered=%q", i+1, len([]rune(line)), firstWidth, rendered)
		}
	}
}

func TestRenderMessageContentShowsPlaceholderWhenMarkdownFails(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.markdownRenderer = &stubMarkdownRenderer{err: errors.New("render failed")}

	content := "before\n```go\nfmt.Println(1)\n```\nafter"
	rendered, _ := app.renderMessageContentWithCopy(content, 50, app.styles.messageBody, 1)
	if !strings.Contains(rendered, "fmt.Println(1)") {
		t.Fatalf("expected code block to keep rendering when markdown prose fails, got %q", rendered)
	}
	if !strings.Contains(rendered, "[Copy code #1]") {
		t.Fatalf("expected copy button for rendered code block, got %q", rendered)
	}
}

func TestRenderMessageContentShowsPlaceholderWhenRendererMissing(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.markdownRenderer = nil

	rendered, _ := app.renderMessageContentWithCopy("content", 50, app.styles.messageBody, 1)
	if !strings.Contains(rendered, emptyMessageText) {
		t.Fatalf("expected placeholder when markdown renderer is missing, got %q", rendered)
	}
}

func TestWorkspaceCommandAndFileReferenceFlow(t *testing.T) {
	previousExecutor := workspaceCommandExecutor
	t.Cleanup(func() { workspaceCommandExecutor = previousExecutor })
	workspaceCommandExecutor = func(ctx context.Context, cfg config.Config, workdir string, command string) (string, error) {
		return "stubbed output for " + command, nil
	}

	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.input.SetValue("& git status")
	app.state.InputText = app.input.Value()
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	for _, msg := range collectTeaMessages(cmd) {
		model, follow := app.Update(msg)
		app = model.(App)
		_ = collectTeaMessages(follow)
	}

	if len(runtime.runInputs) != 0 {
		t.Fatalf("expected & command not to hit agent runtime, got %+v", runtime.runInputs)
	}
	if app.state.StatusText != statusCommandDone {
		t.Fatalf("expected command done status, got %q", app.state.StatusText)
	}
	if len(app.activeMessages) != 0 {
		t.Fatalf("expected workspace command flow to stay out of transcript, got %+v", app.activeMessages)
	}
	if len(app.activities) < 2 {
		t.Fatalf("expected running event and command result in activity, got %+v", app.activities)
	}
	first := app.activities[0]
	if first.Title != "Running command" || !strings.Contains(first.Detail, "git status") {
		t.Fatalf("expected running command activity, got %+v", first)
	}
	last := app.activities[len(app.activities)-1]
	if last.Title != "Command finished" || !strings.Contains(last.Detail, "Command: & git status") || !strings.Contains(last.Detail, "stubbed output for git status") {
		t.Fatalf("expected command output in activity, got %+v", last)
	}

	app.fileCandidates = []string{"README.md", "internal/tui/update.go", "internal/tui/view.go"}
	app.input.SetValue("inspect @internal/tui/upd")
	app.state.InputText = app.input.Value()
	app.refreshCommandMenu()
	menu := app.renderCommandMenu(80)
	if !strings.Contains(menu, fileMenuTitle) || !strings.Contains(menu, "@internal/tui/update.go") {
		t.Fatalf("expected file suggestion menu, got %q", menu)
	}
	if strings.Count(menu, "\n") > 6 {
		t.Fatalf("expected compact file suggestion menu, got %q", menu)
	}

	model, cmd = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	app = model.(App)
	if cmd != nil {
		_ = collectTeaMessages(cmd)
	}
	if app.focus != panelInput {
		t.Fatalf("expected tab completion to keep focus in input, got %v", app.focus)
	}
	if app.state.InputText != "inspect @internal/tui/update.go" {
		t.Fatalf("expected @ suggestion to be applied, got %q", app.state.InputText)
	}

	app.input.SetValue("& go test ./...")
	app.state.InputText = app.input.Value()
	app.refreshCommandMenu()
	menu = app.renderCommandMenu(80)
	if !strings.Contains(menu, shellMenuTitle) || !strings.Contains(menu, workspaceCommandUsage) {
		t.Fatalf("expected shell hint menu, got %q", menu)
	}
	if strings.Count(menu, "\n") > 3 {
		t.Fatalf("expected compact shell menu, got %q", menu)
	}
}

func TestActivityMouseFilePickerAndProgressRendering(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	app.width = 120
	app.height = 38
	app.applyComponentLayout(true)
	x0, y0, _, _ := app.activityBounds()
	if app.isMouseWithinActivity(tea.MouseMsg{X: x0, Y: y0}) {
		t.Fatalf("expected empty activity panel hit test to be false")
	}
	if app.handleActivityMouse(tea.MouseMsg{X: x0, Y: y0, Type: tea.MouseWheelDown, Button: tea.MouseButtonWheelDown}) {
		t.Fatalf("expected mouse handling to be false without activities")
	}

	app.appendActivity("tool", "Running tool", "detail", false)
	app.applyComponentLayout(true)
	ax, ay, aw, ah := app.activityBounds()
	if aw <= 0 || ah <= 0 {
		t.Fatalf("expected visible activity bounds, got width=%d height=%d", aw, ah)
	}
	inside := tea.MouseMsg{
		X:      ax + 1,
		Y:      ay + 1,
		Type:   tea.MouseWheelDown,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	}
	app.focus = panelTranscript
	if !app.handleActivityMouse(inside) {
		t.Fatalf("expected activity wheel event to be handled")
	}
	if app.focus != panelActivity {
		t.Fatalf("expected focus to move to activity panel, got %v", app.focus)
	}

	app.state.ActivePicker = pickerModel
	if app.handleActivityMouse(inside) {
		t.Fatalf("expected activity mouse ignored when picker is active")
	}
	app.state.ActivePicker = pickerNone
	if app.isMouseWithinActivity(tea.MouseMsg{X: ax + aw + 2, Y: ay}) {
		t.Fatalf("expected out-of-bound mouse point to be rejected")
	}

	app.state.ActivePicker = pickerFile
	if pickerView := stripANSI(app.renderPicker(48, 12)); !strings.Contains(pickerView, filePickerTitle) {
		t.Fatalf("expected file picker title, got %q", pickerView)
	}
	model, cmd := app.updatePicker(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)
	if cmd != nil {
		_ = collectTeaMessages(cmd)
	}

	app.state.IsAgentRunning = true
	app.state.StatusText = "thinking"
	app.runProgressKnown = true
	app.runProgressValue = 0.45
	app.runProgressLabel = "Planning"
	header := stripANSI(app.renderHeader(100))
	if !strings.Contains(header, "Planning") {
		t.Fatalf("expected progress label in header, got %q", header)
	}
	app.runProgressLabel = ""
	app.state.StatusText = ""
	header = stripANSI(app.renderHeader(100))
	if !strings.Contains(header, statusRunning) {
		t.Fatalf("expected running fallback text in header, got %q", header)
	}

	app.state.ActivePicker = pickerFile
	snapshot := app.currentStatusSnapshot()
	if snapshot.PickerLabel != "file" {
		t.Fatalf("expected picker label file, got %q", snapshot.PickerLabel)
	}

	app.runProgressKnown = true
	modelAny, _ := app.Update(RuntimeClosedMsg{})
	closed := modelAny.(App)
	if closed.runProgressKnown {
		t.Fatalf("expected RuntimeClosedMsg to clear run progress")
	}
}

func newTestConfigManager(t *testing.T) *config.Manager {
	t.Helper()
	manager := config.NewManager(config.NewLoader(t.TempDir(), config.DefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return manager
}

func newTestProviderService(t *testing.T, manager *config.Manager) *config.SelectionService {
	t.Helper()

	registry := provider.NewRegistry()
	err := registry.Register(provider.DriverDefinition{
		Name: config.OpenAIName,
		Build: func(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error) {
			return tUItestProvider{}, nil
		},
	})
	if err != nil {
		t.Fatalf("register provider drivers: %v", err)
	}
	modelCatalogs := providercatalog.NewService("", registry, newTUItestCatalogStore())
	return config.NewSelectionService(manager, registry, registry, modelCatalogs)
}

type tUItestProvider struct{}

func (tUItestProvider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) error {
	return nil
}

type tUItestCatalogStore struct {
	catalogs map[string]providercatalog.ModelCatalog
	mu       sync.Mutex
}

func newTUItestCatalogStore() *tUItestCatalogStore {
	return &tUItestCatalogStore{
		catalogs: map[string]providercatalog.ModelCatalog{},
	}
}

func (s *tUItestCatalogStore) Load(ctx context.Context, identity config.ProviderIdentity) (providercatalog.ModelCatalog, error) {
	if err := ctx.Err(); err != nil {
		return providercatalog.ModelCatalog{}, err
	}

	catalog, ok := s.catalogs[identity.Key()]
	if !ok {
		return providercatalog.ModelCatalog{}, providercatalog.ErrCatalogNotFound
	}
	return catalog, nil
}

func (s *tUItestCatalogStore) Save(ctx context.Context, catalog providercatalog.ModelCatalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.catalogs[catalog.Identity.Key()] = catalog
	return nil
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
	case <-time.After(250 * time.Millisecond):
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

func stripANSI(input string) string {
	return ansiPattern.ReplaceAllString(input, "")
}
