package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
)

func TestTruncateVisualUsesDisplayWidth(t *testing.T) {
	value := "generate a python example that is wider than the viewport"
	got := truncateVisual(value, 12)

	if got == value {
		t.Fatalf("expected value to be truncated")
	}
	if width := ansi.StringWidth(got); width > 12 {
		t.Fatalf("expected truncated width <= 12, got %d for %q", width, got)
	}
}

func TestRenderBadgeStaysSingleLine(t *testing.T) {
	got := renderBadge("PYTHON", themeAccent2, themeCode)

	if strings.Contains(got, "\n") {
		t.Fatalf("expected badge to stay on one line, got %q", got)
	}
}

func TestRenderActionStaysSingleLine(t *testing.T) {
	got := renderAction("Copy", false, themeAccent)

	if strings.Contains(got, "\n") {
		t.Fatalf("expected action to stay on one line, got %q", got)
	}
}

func TestAlignHeaderPartsFitsVisualWidth(t *testing.T) {
	header := alignHeaderParts(20, "very long session title", "status")

	if width := ansi.StringWidth(header); width != 20 {
		t.Fatalf("expected header width 20, got %d for %q", width, header)
	}
}

func TestLayoutHeaderPartsKeepsRightPartVisible(t *testing.T) {
	right := renderAction("Copy", false, themeAccent)
	parts := layoutHeaderParts(24, strings.Repeat("L", 40), right)

	if width := ansi.StringWidth(parts.Text); width != 24 {
		t.Fatalf("expected header width 24, got %d for %q", width, parts.Text)
	}
	if parts.RightStart != 24-ansi.StringWidth(right) {
		t.Fatalf("expected right segment to start at %d, got %d", 24-ansi.StringWidth(right), parts.RightStart)
	}
	if !strings.Contains(parts.Text, right) {
		t.Fatalf("expected header text to keep the right segment visible")
	}
}

func TestRenderMessageCardTracksCopyHotspot(t *testing.T) {
	lines, _, blocks, _ := renderMessageCard(
		provider.Message{
			Role:    provider.RoleAssistant,
			Content: "```python\nprint('hello')\n```",
		},
		40,
		0,
		false,
		-1,
		0,
		0,
	)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(blocks))
	}

	block := blocks[0]
	if block.CopyX2 <= block.CopyX1 {
		t.Fatalf("expected positive copy hotspot width, got [%d,%d)", block.CopyX1, block.CopyX2)
	}
	if width := ansi.StringWidth(lines[block.HeaderLine]); width != 40 {
		t.Fatalf("expected code header width 40, got %d", width)
	}
}

func TestBoxLineFitsLongContent(t *testing.T) {
	line := boxLine(18, themeAccent, themePanel, strings.Repeat("x", 20))

	if width := ansi.StringWidth(line); width != 18 {
		t.Fatalf("expected box line width 18, got %d for %q", width, line)
	}
}
