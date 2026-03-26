package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderHelpContainsKeyCommands(t *testing.T) {
	rendered := RenderHelp(80, "/help toggle help\n/provider <name> switch provider")

	for _, want := range []string{"NeoCode Help", "/help", "/provider <name>", "Use /help, Esc, or q to close this panel."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected help to contain %q, got %q", want, rendered)
		}
	}
}

func TestInputBoxRenderShowsBodyAndStatusOnlyByDefault(t *testing.T) {
	rendered := InputBox{Body: "body", Status: "Thinking..."}.Render()
	if !strings.Contains(rendered, "body") {
		t.Fatalf("expected body content, got %q", rendered)
	}
	if !strings.Contains(rendered, "Thinking...") {
		t.Fatalf("expected status line, got %q", rendered)
	}
	if strings.Contains(rendered, "Ctrl+V: paste") {
		t.Fatalf("expected footer shortcuts to be omitted by default, got %q", rendered)
	}
}

func TestInputBoxRenderIncludesConsoleMetadataWhenProvided(t *testing.T) {
	rendered := InputBox{
		ModeLabel: "COMMAND",
		MetaText:  "1 line(s) | 12 chars",
		Body:      "body",
		Status:    "Ready",
		NoteText:  "Open the command and key reference.",
		Width:     40,
	}.Render()
	for _, want := range []string{"COMMAND", "1 line(s) | 12 chars", "Open the command and key reference."} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected console metadata to contain %q, got %q", want, rendered)
		}
	}
}

func TestInputBoxRenderKeepsMetadataSingleLineAndTrimsTrailingBodyNewlines(t *testing.T) {
	rendered := ansi.Strip(InputBox{
		Body:      "body\n\n",
		ModeLabel: "COMMAND",
		MetaText:  strings.Repeat("meta", 12),
		Status:    strings.Repeat("status", 12),
		NoteText:  strings.Repeat("note", 16),
		Width:     24,
	}.Render())

	if strings.Contains(rendered, "body\n\n\n") {
		t.Fatalf("expected trailing body newlines to be trimmed, got %q", rendered)
	}
	if strings.Count(rendered, "\n") != 3 {
		t.Fatalf("expected body plus three metadata lines, got %q", rendered)
	}
}

func TestMessageListRenderIncludesRoleSpecificLabels(t *testing.T) {
	rendered := MessageList{
		Width: 60,
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: time.Unix(1, 0)},
			{Role: "assistant", Content: "world", Timestamp: time.Unix(2, 0)},
			{Role: "system", Content: "note", Timestamp: time.Unix(3, 0)},
		},
	}.Render()

	for _, want := range []string{"You [1]:", "Neo [2]:", "[System]", "hello", "world", "note"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered list to contain %q, got %q", want, rendered)
		}
	}
}

func TestMessageListRenderReturnsEmptyForNoMessages(t *testing.T) {
	if got := (MessageList{Width: 40}).Render(); got != "" {
		t.Fatalf("expected empty render, got %q", got)
	}
}

func TestMessageListRenderLayoutIncludesCopyRegions(t *testing.T) {
	layout := MessageList{
		Width:    60,
		Messages: []Message{{Role: "assistant", Content: "```go\nfmt.Println(1)\n```", Timestamp: time.Unix(1, 0)}},
	}.RenderLayout()

	if !strings.Contains(layout.Content, "[Copy] go") {
		t.Fatalf("expected copy action in layout, got %q", layout.Content)
	}
	if len(layout.Regions) != 1 {
		t.Fatalf("expected one clickable region, got %d", len(layout.Regions))
	}
	region := layout.Regions[0]
	if region.Kind != "copy" || region.StartRow != 1 || region.StartCol != 1 || region.EndCol != len(CopyActionLabel()) {
		t.Fatalf("unexpected region: %+v", region)
	}
	if region.CodeBlock.Code != "fmt.Println(1)" {
		t.Fatalf("expected copied code, got %+v", region.CodeBlock)
	}
}

func TestMessageListRenderLayoutTrimsTrailingBlankLines(t *testing.T) {
	layout := MessageList{
		Width: 60,
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: time.Unix(1, 0)},
			{Role: "assistant", Content: "world", Timestamp: time.Unix(2, 0)},
		},
	}.RenderLayout()

	if strings.HasSuffix(layout.Content, "\n") {
		t.Fatalf("expected rendered layout to avoid trailing blank lines, got %q", layout.Content)
	}
}

func TestMessageListRenderWrapsLongWords(t *testing.T) {
	rendered := ansi.Strip(MessageList{
		Width: 20,
		Messages: []Message{
			{Role: "user", Content: strings.Repeat("a", 25), Timestamp: time.Unix(1, 0)},
		},
	}.Render())

	if strings.Contains(rendered, strings.Repeat("a", 25)) {
		t.Fatalf("expected long line to wrap, got %q", rendered)
	}
	if !strings.Contains(rendered, strings.Repeat("a", 16)+"\n"+strings.Repeat("a", 9)) {
		t.Fatalf("expected wrapped output, got %q", rendered)
	}
}

func TestRenderPanelKeepsHeaderSingleLineWhenHintIsLong(t *testing.T) {
	rendered := ansi.Strip(RenderPanel(
		"Context",
		"runtime snapshot | scroll 100% | this should stay on one line",
		"body",
		40,
		8,
		false,
		"#61AFEF",
	))

	if !strings.Contains(rendered, "Context") {
		t.Fatalf("expected panel title, got %q", rendered)
	}
	if strings.Contains(rendered, "Context\n") {
		t.Fatalf("expected header to stay on one line, got %q", rendered)
	}
}

func TestStatusBarRenderStaysSingleLineInNarrowWidth(t *testing.T) {
	rendered := ansi.Strip(StatusBar{
		Mode:      "chat",
		Focus:     "conversation",
		Model:     "very-long-model-name-for-testing",
		MemoryCnt: 42,
		Status:    "Thinking about a very long status message that should be clipped",
		Busy:      true,
		Width:     48,
	}.Render())

	if strings.Contains(rendered, "\n") {
		t.Fatalf("expected status bar to remain single-line, got %q", rendered)
	}
}
