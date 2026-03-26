package core

import (
	"strings"
	"testing"
	"time"

	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"

	"github.com/charmbracelet/x/ansi"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "single", in: "hello", want: 1},
		{name: "multi", in: "a\nb\nc", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.in); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestToComponentMessagesPreservesFields(t *testing.T) {
	ts := time.Unix(123, 0)
	m := Model{
		chat: state.ChatState{Messages: []state.Message{{
			Role:      "assistant",
			Content:   "hello",
			Timestamp: ts,
			Streaming: true,
		}}},
	}

	got := m.toComponentMessages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Role != "assistant" || got[0].Content != "hello" || !got[0].Timestamp.Equal(ts) || !got[0].Streaming {
		t.Fatalf("unexpected converted message: %+v", got[0])
	}
}

func TestViewShowsSmallWindowMessage(t *testing.T) {
	m := Model{}
	m.ui.Width = 10
	m.ui.Height = 5

	if got := m.View(); got != "Window too small" {
		t.Fatalf("expected small window warning, got %q", got)
	}
}

func TestViewRendersHelpPanelInHelpMode(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "workspace")
	m.ui.Width = 80
	m.ui.Height = 30
	m.ui.Mode = state.ModeHelp
	m.syncLayout()

	rendered := m.View()
	if !strings.Contains(rendered, "Help") {
		t.Fatalf("expected help modal title in view, got %q", rendered)
	}
	if !strings.Contains(rendered, "NeoCode Help") {
		t.Fatalf("expected help panel in view, got %q", rendered)
	}
	if !strings.Contains(rendered, "/help") {
		t.Fatalf("expected help commands in view, got %q", rendered)
	}
	for _, want := range []string{"Composer", "Context", "mode help"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected help mode to keep workbench shell visible and contain %q, got %q", want, rendered)
		}
	}
}

func TestViewRendersWorkbenchPanels(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "workspace")
	m.ui.Width = 140
	m.ui.Height = 36
	m.syncLayout()

	rendered := m.View()
	for _, want := range []string{"Conversation", "Context", "Composer"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected workbench view to contain %q, got %q", want, rendered)
		}
	}
}

func TestRefreshViewportKeepsShortConversationAtTop(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "workspace")
	m.ui.Width = 140
	m.ui.Height = 36
	m.chat.Messages = []state.Message{
		{Role: "user", Content: "nihao"},
		{Role: "assistant", Content: "你好，我在。"},
	}
	m.ui.AutoScroll = true
	m.refreshViewport()

	if m.viewport.YOffset != 0 {
		t.Fatalf("expected short conversation to stay at top, got offset %d", m.viewport.YOffset)
	}

	rendered := m.View()
	for _, want := range []string{"You [1]:", "nihao", "Neo [2]:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered workbench to contain %q, got %q", want, rendered)
		}
	}
}

func TestViewRendersStructuredContextPanel(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "D:/workspace/project")
	m.ui.Width = 140
	m.ui.Height = 50
	m.chat.ActiveModel = "qwen-plus"
	m.chat.Messages = []state.Message{
		{Role: "user", Content: "Please review the latest diff."},
		{Role: "assistant", Content: "I am checking the current worktree now."},
		{Role: "system", Content: formatToolStatusMessage("Edit", map[string]interface{}{"filePath": "internal/tui/core/view.go"})},
		{Role: "system", Content: toolContextPrefix + "\n" + "tool=Edit\n" + "success=true\n" + "output:\n" + "updated internal/tui/core/view.go"},
	}
	m.chat.ToolExecuting = true
	m.chat.PendingApproval = &state.PendingApproval{
		Call: services.ToolCall{
			Tool:   "Bash",
			Params: map[string]interface{}{"workdir": "D:/workspace/project"},
		},
		ToolType: "bash",
		Target:   "git status",
	}
	m.todo.setTodos([]services.Todo{
		{ID: "1", Content: "Refine context panel", Status: services.TodoInProgress},
		{ID: "2", Content: "Verify viewport sizing", Status: services.TodoPending},
		{ID: "3", Content: "Ship tests", Status: services.TodoCompleted},
	})
	m.syncLayout()

	rendered := m.View()
	for _, want := range []string{
		"Session",
		"Runtime",
		"Approval Required",
		"Recent Activity",
		"Todo Snapshot",
		"Tool: Bash",
		"Target: git status",
		"Last result: Edit (success)",
		"You: Please review",
		"Counts: 1 pending | 1 in progress",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected structured context panel to contain %q, got %q", want, rendered)
		}
	}
}

func TestViewRendersWorkbenchFooterMetadata(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "D:/workspace/project")
	m.ui.Width = 140
	m.ui.Height = 36
	m.ui.AutoScroll = true
	m.chat.ActiveModel = "qwen-plus"
	m.chat.MemoryStats.TotalItems = 9
	m.syncLayout()

	rendered := m.View()
	for _, want := range []string{
		"mode chat",
		"focus composer",
		"qwen-plus",
		"memory 9",
		"workspace D:/workspace/project",
		"auto-scroll on",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected footer metadata to contain %q, got %q", want, rendered)
		}
	}
}

func TestRenderShortHelpStaysSingleLine(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "D:/workspace/project")
	m.ui.Width = 80
	m.ui.Height = 24
	m.syncLayout()

	helpText := m.renderShortHelp()
	if strings.Contains(helpText, "\n") {
		t.Fatalf("expected short help to stay on one line, got %q", helpText)
	}
}

func TestFooterBarStaysTwoLinesWhenConversationFocused(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "C:/Users/tang/.codex/worktrees/485d/neo-code")
	m.ui.Width = 80
	m.ui.Height = 24
	m.ui.Focused = "conversation"
	m.ui.AutoScroll = false
	m.syncLayout()

	rendered := ansi.Strip(m.renderFooterBar(m.layout))
	if got := countLines(rendered); got != 2 {
		t.Fatalf("expected footer bar to stay two lines, got %d lines: %q", got, rendered)
	}
}

func TestViewRendersComposerConsoleHints(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "D:/workspace/project")
	m.ui.Width = 140
	m.ui.Height = 36
	m.chat.APIKeyReady = true
	m.textarea.SetValue("/todo add refine composer")
	m.syncLayout()

	rendered := m.View()
	for _, want := range []string{
		"COMMAND",
		"command",
		"palette",
		"View or manage the todo list.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected composer console to contain %q, got %q", want, rendered)
		}
	}
}

func TestViewKeepsWorkbenchWithinConfiguredWidth(t *testing.T) {
	m := NewModel(&fakeChatClient{}, "persona", 6, "config.yaml", "D:/workspace/project/with/a/very/long/path")
	m.ui.Width = 96
	m.ui.Height = 32
	m.chat.ActiveModel = "very-long-model-name-for-layout-checks"
	m.chat.MemoryStats.TotalItems = 17
	m.chat.PendingApproval = &state.PendingApproval{
		Call:   services.ToolCall{Tool: "Bash"},
		Target: "git status --short --branch --ahead-behind --verbose",
	}
	m.syncLayout()

	rendered := ansi.Strip(m.View())
	for _, line := range strings.Split(rendered, "\n") {
		if len([]rune(line)) > m.ui.Width {
			t.Fatalf("expected every rendered line to fit width %d, got %d in %q", m.ui.Width, len([]rune(line)), line)
		}
	}
}
