package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

type stubSubmitter struct {
	lastInput PrepareInput
	err       error
}

func (s *stubSubmitter) Submit(ctx context.Context, input PrepareInput) error {
	s.lastInput = input
	return s.err
}

type stubCompactor struct {
	lastInput CompactInput
	err       error
}

func (s *stubCompactor) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	s.lastInput = input
	return CompactResult{}, s.err
}

type stubPermissionResolver struct {
	lastInput   PermissionResolutionInput
	err         error
	deadline    time.Time
	hasDeadline bool
}

func (s *stubPermissionResolver) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	s.lastInput = input
	s.deadline, s.hasDeadline = ctx.Deadline()
	return s.err
}

type stubSystemToolRunner struct {
	lastInput SystemToolInput
	result    tools.ToolResult
	err       error
}

func (s *stubSystemToolRunner) ExecuteSystemTool(ctx context.Context, input SystemToolInput) (tools.ToolResult, error) {
	s.lastInput = input
	return s.result, s.err
}

type stubProvider struct {
	selection configstate.Selection
	models    []providertypes.ModelDescriptor
	err       error
}

func (s *stubProvider) SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error) {
	return s.selection, s.err
}

func (s *stubProvider) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	return s.selection, s.err
}

func (s *stubProvider) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	return s.models, s.err
}

func TestListenForRuntimeEventCmd(t *testing.T) {
	ch := make(chan RuntimeEvent, 1)
	event := RuntimeEvent{Type: EventUserMessage}
	ch <- event

	msg := ListenForRuntimeEventCmd(
		ch,
		func(e RuntimeEvent) tea.Msg { return e },
		func() tea.Msg { return "closed" },
	)()
	got, ok := msg.(RuntimeEvent)
	if !ok || got.Type != EventUserMessage {
		t.Fatalf("expected runtime event msg, got %T %#v", msg, msg)
	}

	close(ch)
	msg = ListenForRuntimeEventCmd(
		ch,
		func(e RuntimeEvent) tea.Msg { return e },
		func() tea.Msg { return "closed" },
	)()
	if gotClosed, ok := msg.(string); !ok || gotClosed != "closed" {
		t.Fatalf("expected closed msg, got %T %#v", msg, msg)
	}
}

func TestRunSubmitCmd(t *testing.T) {
	runner := &stubSubmitter{err: errors.New("run failed")}
	prepareInput := PrepareInput{
		SessionID: "s1",
		RunID:     "run-1",
		Workdir:   "D:/",
		Text:      "hello",
		Images:    []UserImageInput{{Path: "C:/a.png", MimeType: "image/png"}},
	}
	msg := RunSubmitCmd(runner, prepareInput, func(err error) tea.Msg { return err })()
	if runner.lastInput.RunID != "run-1" || len(runner.lastInput.Images) != 1 {
		t.Fatalf("unexpected submit input: %+v", runner.lastInput)
	}
	if err, ok := msg.(error); !ok || err == nil || err.Error() != "run failed" {
		t.Fatalf("expected forwarded run error, got %T %#v", msg, msg)
	}
}

func TestRunCompactCmd(t *testing.T) {
	compactor := &stubCompactor{err: errors.New("compact failed")}
	input := CompactInput{SessionID: "s2"}
	msg := RunCompactCmd(compactor, input, func(err error) tea.Msg { return err })()
	if compactor.lastInput.SessionID != "s2" {
		t.Fatalf("unexpected compact input: %+v", compactor.lastInput)
	}
	if err, ok := msg.(error); !ok || err == nil || err.Error() != "compact failed" {
		t.Fatalf("expected forwarded compact error, got %T %#v", msg, msg)
	}
}

func TestRunResolvePermissionCmd(t *testing.T) {
	resolver := &stubPermissionResolver{err: errors.New("permission failed")}
	input := PermissionResolutionInput{
		RequestID: "perm-1",
		Decision:  DecisionAllowSession,
	}
	msg := RunResolvePermissionCmd(
		resolver,
		input,
		func(in PermissionResolutionInput, err error) tea.Msg {
			return struct {
				Input PermissionResolutionInput
				Err   error
			}{Input: in, Err: err}
		},
	)()

	got, ok := msg.(struct {
		Input PermissionResolutionInput
		Err   error
	})
	if !ok {
		t.Fatalf("expected wrapped permission result message, got %T %#v", msg, msg)
	}
	if got.Input.RequestID != "perm-1" || got.Input.Decision != DecisionAllowSession {
		t.Fatalf("unexpected permission input forwarded: %+v", got.Input)
	}
	if got.Err == nil || got.Err.Error() != "permission failed" {
		t.Fatalf("expected forwarded permission error, got %#v", got.Err)
	}
	if resolver.lastInput.RequestID != "perm-1" || resolver.lastInput.Decision != DecisionAllowSession {
		t.Fatalf("unexpected resolver input: %+v", resolver.lastInput)
	}
	if !resolver.hasDeadline {
		t.Fatalf("expected permission resolver context to carry a deadline")
	}
}

func TestRunSystemToolCmd(t *testing.T) {
	runner := &stubSystemToolRunner{
		result: tools.ToolResult{Name: "memo_read", Content: "ok"},
		err:    errors.New("tool failed"),
	}
	input := SystemToolInput{SessionID: "s1", ToolName: "memo_read"}
	msg := RunSystemToolCmd(
		runner,
		input,
		func(result tools.ToolResult, err error) tea.Msg {
			return struct {
				Result tools.ToolResult
				Err    error
			}{Result: result, Err: err}
		},
	)()
	got, ok := msg.(struct {
		Result tools.ToolResult
		Err    error
	})
	if !ok {
		t.Fatalf("expected wrapped tool result msg, got %T %#v", msg, msg)
	}
	if runner.lastInput.SessionID != "s1" || runner.lastInput.ToolName != "memo_read" {
		t.Fatalf("unexpected tool input: %#v", runner.lastInput)
	}
	if got.Result.Name != "memo_read" || got.Err == nil || got.Err.Error() != "tool failed" {
		t.Fatalf("unexpected tool msg payload: %#v", got)
	}
}

func TestProviderCmds(t *testing.T) {
	svc := &stubProvider{
		selection: configstate.Selection{ProviderID: "openai", ModelID: "gpt-5.4"},
		models:    []providertypes.ModelDescriptor{{ID: "gpt-5.4", Name: "GPT-5.4"}},
	}

	msg := SelectProviderCmd(svc, "openai", func(sel configstate.Selection, err error) tea.Msg { return sel })()
	if sel, ok := msg.(configstate.Selection); !ok || sel.ProviderID != "openai" {
		t.Fatalf("expected provider selection msg, got %T %#v", msg, msg)
	}

	msg = SelectModelCmd(svc, "gpt-5.4", func(sel configstate.Selection, err error) tea.Msg { return sel })()
	if sel, ok := msg.(configstate.Selection); !ok || sel.ModelID != "gpt-5.4" {
		t.Fatalf("expected model selection msg, got %T %#v", msg, msg)
	}

	msg = RefreshModelCatalogCmd(
		svc,
		"openai",
		func(providerID string, models []providertypes.ModelDescriptor, err error) tea.Msg {
			return providerID + ":" + models[0].ID
		},
	)()
	if got, ok := msg.(string); !ok || got != "openai:gpt-5.4" {
		t.Fatalf("expected catalog refresh msg, got %T %#v", msg, msg)
	}

	if cmd := RefreshModelCatalogCmd(svc, "", func(providerID string, models []providertypes.ModelDescriptor, err error) tea.Msg { return nil }); cmd != nil {
		t.Fatalf("expected nil cmd for empty provider id")
	}
}

func TestCommandCmds(t *testing.T) {
	localMsg := RunLocalCommandCmd(
		func(ctx context.Context) (string, error) { return "ok", nil },
		func(notice string, err error) tea.Msg { return notice },
	)()
	if got, ok := localMsg.(string); !ok || got != "ok" {
		t.Fatalf("expected local command notice msg, got %T %#v", localMsg, localMsg)
	}
}

func TestFileServices(t *testing.T) {
	matches := SuggestFileMatches("read", []string{
		"README.md",
		"internal/tui/update.go",
		"docs/internal-arch.md",
	}, 2)
	if len(matches) == 0 {
		t.Fatalf("expected at least one match, got %d (%v)", len(matches), matches)
	}
	if matches[0] != "README.md" {
		t.Fatalf("expected filename fuzzy match first, got %v", matches)
	}

	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	files, err := CollectWorkspaceFiles(root, 10)
	if err != nil {
		t.Fatalf("CollectWorkspaceFiles() error = %v", err)
	}
	if len(files) == 0 || files[0] != "a.txt" {
		t.Fatalf("unexpected collected files: %v", files)
	}

	if resolved := ResolveWorkspaceDirectory(root); resolved == "" {
		t.Fatalf("expected resolved workspace directory")
	}
	if resolved := ResolveWorkspaceDirectory(""); resolved != "" {
		t.Fatalf("expected empty resolved path for blank input, got %q", resolved)
	}
}

func TestSuggestFileMatchesBranches(t *testing.T) {
	candidates := []string{
		"internal/tui/update.go",
		"docs/internal-arch.md",
		"README.md",
	}

	if got := SuggestFileMatches("arch", candidates, 2); len(got) != 1 || got[0] != "docs/internal-arch.md" {
		t.Fatalf("expected contains-match branch, got %v", got)
	}
	if got := SuggestFileMatches("", candidates, 2); len(got) != 2 {
		t.Fatalf("expected empty query to return first items, got %v", got)
	}
	if got := SuggestFileMatches("any", candidates, 0); got != nil {
		t.Fatalf("expected zero limit to return nil, got %v", got)
	}
	if got := SuggestFileMatches("any", nil, 2); got != nil {
		t.Fatalf("expected nil candidates to return nil, got %v", got)
	}
	if got := SuggestFileMatches("itup", candidates, 2); len(got) == 0 || got[0] != "internal/tui/update.go" {
		t.Fatalf("expected fuzzy abbreviation match for itup, got %v", got)
	}
	if got := SuggestFileMatches("int", candidates, 2); len(got) == 0 || got[0] != "docs/internal-arch.md" {
		t.Fatalf("expected filename-priority fuzzy match for int, got %v", got)
	}
}

func TestResolveWorkspaceDirectoryInvalidPath(t *testing.T) {
	if resolved := ResolveWorkspaceDirectory("\x00"); resolved != "" {
		t.Fatalf("expected invalid path to resolve as empty string, got %q", resolved)
	}
}
