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
	agentruntime "neo-code/internal/runtime"
	approvalflow "neo-code/internal/runtime/approval"
)

type stubRunner struct {
	lastInput agentruntime.UserInput
	err       error
}

func (s *stubRunner) Run(ctx context.Context, input agentruntime.UserInput) error {
	s.lastInput = input
	return s.err
}

type stubCompactor struct {
	lastInput agentruntime.CompactInput
	err       error
}

func (s *stubCompactor) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	s.lastInput = input
	return agentruntime.CompactResult{}, s.err
}

type stubPermissionResolver struct {
	lastInput   agentruntime.PermissionResolutionInput
	err         error
	deadline    time.Time
	hasDeadline bool
}

func (s *stubPermissionResolver) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	s.lastInput = input
	s.deadline, s.hasDeadline = ctx.Deadline()
	return s.err
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
	ch := make(chan agentruntime.RuntimeEvent, 1)
	event := agentruntime.RuntimeEvent{Type: agentruntime.EventUserMessage}
	ch <- event

	msg := ListenForRuntimeEventCmd(
		ch,
		func(e agentruntime.RuntimeEvent) tea.Msg { return e },
		func() tea.Msg { return "closed" },
	)()
	got, ok := msg.(agentruntime.RuntimeEvent)
	if !ok || got.Type != agentruntime.EventUserMessage {
		t.Fatalf("expected runtime event msg, got %T %#v", msg, msg)
	}

	close(ch)
	msg = ListenForRuntimeEventCmd(
		ch,
		func(e agentruntime.RuntimeEvent) tea.Msg { return e },
		func() tea.Msg { return "closed" },
	)()
	if gotClosed, ok := msg.(string); !ok || gotClosed != "closed" {
		t.Fatalf("expected closed msg, got %T %#v", msg, msg)
	}
}

func TestRunAgentCmd(t *testing.T) {
	runner := &stubRunner{err: errors.New("boom")}
	input := agentruntime.UserInput{SessionID: "s1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}, Workdir: "D:/"}
	msg := RunAgentCmd(runner, input, func(err error) tea.Msg { return err })()
	if runner.lastInput.SessionID != "s1" || renderPartsForTest(runner.lastInput.Parts) != "hello" {
		t.Fatalf("unexpected runner input: %+v", runner.lastInput)
	}
	if err, ok := msg.(error); !ok || err == nil || err.Error() != "boom" {
		t.Fatalf("expected forwarded error message, got %T %#v", msg, msg)
	}
}

func TestRunCompactCmd(t *testing.T) {
	compactor := &stubCompactor{err: errors.New("compact failed")}
	input := agentruntime.CompactInput{SessionID: "s2"}
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
	input := agentruntime.PermissionResolutionInput{
		RequestID: "perm-1",
		Decision:  approvalflow.DecisionAllowSession,
	}
	msg := RunResolvePermissionCmd(
		resolver,
		input,
		func(in agentruntime.PermissionResolutionInput, err error) tea.Msg {
			return struct {
				Input agentruntime.PermissionResolutionInput
				Err   error
			}{Input: in, Err: err}
		},
	)()

	got, ok := msg.(struct {
		Input agentruntime.PermissionResolutionInput
		Err   error
	})
	if !ok {
		t.Fatalf("expected wrapped permission result message, got %T %#v", msg, msg)
	}
	if got.Input.RequestID != "perm-1" || got.Input.Decision != approvalflow.DecisionAllowSession {
		t.Fatalf("unexpected permission input forwarded: %+v", got.Input)
	}
	if got.Err == nil || got.Err.Error() != "permission failed" {
		t.Fatalf("expected forwarded permission error, got %#v", got.Err)
	}
	if resolver.lastInput.RequestID != "perm-1" || resolver.lastInput.Decision != approvalflow.DecisionAllowSession {
		t.Fatalf("unexpected resolver input: %+v", resolver.lastInput)
	}
	if !resolver.hasDeadline {
		t.Fatalf("expected permission resolver context to carry a deadline")
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

	workspaceMsg := RunWorkspaceCommandCmd(
		func(ctx context.Context) (string, string, error) { return "git status", "clean", nil },
		func(command string, output string, err error) tea.Msg { return command + ":" + output },
	)()
	if got, ok := workspaceMsg.(string); !ok || got != "git status:clean" {
		t.Fatalf("expected workspace command msg, got %T %#v", workspaceMsg, workspaceMsg)
	}
}

func TestFileServices(t *testing.T) {
	matches := SuggestFileMatches("int", []string{
		"README.md",
		"internal/tui/update.go",
		"docs/internal-arch.md",
	}, 2)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d (%v)", len(matches), matches)
	}
	if matches[0] != "internal/tui/update.go" {
		t.Fatalf("expected prefix match first, got %v", matches)
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
		t.Fatalf("expected empty query to return prefix-priority items, got %v", got)
	}
	if got := SuggestFileMatches("any", candidates, 0); got != nil {
		t.Fatalf("expected zero limit to return nil, got %v", got)
	}
	if got := SuggestFileMatches("any", nil, 2); got != nil {
		t.Fatalf("expected nil candidates to return nil, got %v", got)
	}
}

func TestResolveWorkspaceDirectoryInvalidPath(t *testing.T) {
	if resolved := ResolveWorkspaceDirectory("\x00"); resolved != "" {
		t.Fatalf("expected invalid path to resolve as empty string, got %q", resolved)
	}
}
