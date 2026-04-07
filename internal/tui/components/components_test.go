package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	tuistate "neo-code/internal/tui/state"
)

func TestActivityPreviewHeight(t *testing.T) {
	if got := ActivityPreviewHeight(0); got != 0 {
		t.Fatalf("expected empty activity height 0, got %d", got)
	}
	if got := ActivityPreviewHeight(1); got != 6 {
		t.Fatalf("expected non-empty activity height 6, got %d", got)
	}
}

func TestRenderActivityLine(t *testing.T) {
	fixed := time.Date(2026, 4, 5, 12, 34, 56, 0, time.UTC)
	line := RenderActivityLine(tuistate.ActivityEntry{
		Time:   fixed,
		Kind:   "",
		Title:  "single line",
		Detail: "",
	}, 80)
	if !strings.Contains(line, "12:34:56") {
		t.Fatalf("expected time label in line, got %q", line)
	}
	if !strings.Contains(line, "EVENT") {
		t.Fatalf("expected default kind label EVENT, got %q", line)
	}
	if !strings.Contains(line, "single line") {
		t.Fatalf("expected title text, got %q", line)
	}

	errorLine := RenderActivityLine(tuistate.ActivityEntry{
		Time:    fixed,
		Kind:    "tool",
		Title:   "failed task",
		Detail:  "boom",
		IsError: true,
	}, 80)
	if !strings.Contains(errorLine, "ERROR") {
		t.Fatalf("expected error kind label for IsError item, got %q", errorLine)
	}
}

func TestRenderCommandMenu(t *testing.T) {
	data := CommandMenuData{
		Title:          "Suggestions",
		Body:           "/help - show help",
		Width:          40,
		ContainerStyle: lipgloss.NewStyle(),
		TitleStyle:     lipgloss.NewStyle(),
	}
	output := RenderCommandMenu(data)
	if !strings.Contains(output, "Suggestions") {
		t.Fatalf("expected title in output, got %q", output)
	}
	if !strings.Contains(output, "/help - show help") {
		t.Fatalf("expected body in output, got %q", output)
	}

	empty := RenderCommandMenu(CommandMenuData{
		Title:          "Suggestions",
		Body:           "   ",
		Width:          40,
		ContainerStyle: lipgloss.NewStyle(),
		TitleStyle:     lipgloss.NewStyle(),
	})
	if empty != "" {
		t.Fatalf("expected empty output for blank body, got %q", empty)
	}
}

func TestCompactStatusText(t *testing.T) {
	if got := CompactStatusText("\n  hello   world \n", 0); got != "hello world" {
		t.Fatalf("expected compacted line, got %q", got)
	}
	if got := CompactStatusText("\n \n", 10); got != "" {
		t.Fatalf("expected empty compact status text, got %q", got)
	}
}

func TestCodeBlockHelpers(t *testing.T) {
	if got := TrimRenderedTrailingWhitespace("a \t\nb  "); got != "a\nb" {
		t.Fatalf("unexpected trimmed result: %q", got)
	}

	normalized := NormalizeBlockRightEdge("a\nbb", 8)
	lines := strings.Split(normalized, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %d", len(lines))
	}
	if lipgloss.Width(lines[0]) != lipgloss.Width(lines[1]) {
		t.Fatalf("expected equal visual widths, got %d and %d", lipgloss.Width(lines[0]), lipgloss.Width(lines[1]))
	}
}

func TestRenderCommandMenuRow(t *testing.T) {
	row := RenderCommandMenuRow(CommandMenuRowData{
		Title:            "/status",
		Description:      "show status",
		Highlight:        true,
		Selected:         false,
		Width:            30,
		UsageStyle:       lipgloss.NewStyle(),
		UsageMatchStyle:  lipgloss.NewStyle().Bold(true),
		DescriptionStyle: lipgloss.NewStyle(),
	})
	if !strings.Contains(row, "/status") || !strings.Contains(row, "show status") {
		t.Fatalf("unexpected command menu row: %q", row)
	}
}

func TestRenderSessionRow(t *testing.T) {
	row := RenderSessionRow(SessionRowData{
		Title:           "My session title",
		UpdatedAtLabel:  "04-05 12:34",
		Active:          true,
		Selected:        true,
		Width:           40,
		RowStyle:        lipgloss.NewStyle(),
		RowActiveStyle:  lipgloss.NewStyle(),
		RowFocusStyle:   lipgloss.NewStyle(),
		MetaStyle:       lipgloss.NewStyle(),
		MetaActiveStyle: lipgloss.NewStyle(),
		MetaFocusStyle:  lipgloss.NewStyle(),
	})
	if !strings.Contains(row, ">") {
		t.Fatalf("expected selected prefix in row, got %q", row)
	}
	if !strings.Contains(row, "04-05 12:34") {
		t.Fatalf("expected updated-at label in row, got %q", row)
	}
}

func TestViewHelperBranches(t *testing.T) {
	if got := fallback("primary", "fallback"); got != "primary" {
		t.Fatalf("expected primary fallback value, got %q", got)
	}
	if got := fallback("", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}

	if got := trimMiddle("abcdef", 0); got != "" {
		t.Fatalf("expected empty string for non-positive limit, got %q", got)
	}
	if got := trimMiddle("abcdef", 3); got != "abc" {
		t.Fatalf("expected hard truncate for short limit, got %q", got)
	}
	if got := trimMiddle("abcdefghij", 7); got != "ab...ij" {
		t.Fatalf("expected middle trim output, got %q", got)
	}

	if got := trimRunes("abcdef", 3); got != "abcdef" {
		t.Fatalf("expected original text when limit < 4, got %q", got)
	}
	if got := trimRunes("abcdef", 5); got != "ab..." {
		t.Fatalf("expected rune-safe ellipsis trim, got %q", got)
	}

	if got := clamp(-1, 0, 10); got != 0 {
		t.Fatalf("expected clamp to min, got %d", got)
	}
	if got := clamp(20, 0, 10); got != 10 {
		t.Fatalf("expected clamp to max, got %d", got)
	}
	if got := clamp(6, 0, 10); got != 6 {
		t.Fatalf("expected clamp to keep in-range value, got %d", got)
	}
}

func TestNormalizeBlockRightEdgeBlankContent(t *testing.T) {
	blank := "   \n\t"
	if got := NormalizeBlockRightEdge(blank, 20); got != blank {
		t.Fatalf("expected blank content passthrough, got %q", got)
	}
}

func TestCompactStatusTextWithLimit(t *testing.T) {
	text := "\n   first   useful   line   \nsecond line"
	if got := CompactStatusText(text, 9); got != "fir...ine" {
		t.Fatalf("expected first non-empty line compacted with ellipsis, got %q", got)
	}
}
