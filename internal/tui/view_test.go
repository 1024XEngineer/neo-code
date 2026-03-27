package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
	"neocode/internal/runtime"
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

func TestRenderPanelFrameFitsRequestedSize(t *testing.T) {
	panel := renderPanelFrame("Runtime", "tools + activity", 36, 8, true, themeAccent, "content")

	lines := strings.Split(panel, "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 rendered lines, got %d", len(lines))
	}
	expectedWidth := ansi.StringWidth(lines[0])
	if expectedWidth < 36 {
		t.Fatalf("expected panel width to be at least 36, got %d", expectedWidth)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width != expectedWidth {
			t.Fatalf("expected panel width %d, got %d for %q", expectedWidth, width, line)
		}
	}
}

func TestRenderHeaderIncludesRuntimeStatus(t *testing.T) {
	m := model{
		width:  100,
		height: 24,
		layout: computeLayout(100, 24),
		state: uiState{
			status: runtime.Status{
				Provider: "openai",
				Model:    "gpt-4.1-mini",
				Workdir:  "D:/workspace",
			},
			lastUpdatedAt: mustParseTime(t, "2026-03-27T10:08:09+08:00"),
		},
	}

	header := m.renderHeader()
	if !strings.Contains(header, "Provider openai") {
		t.Fatalf("expected header to contain provider, got %q", header)
	}
	if !strings.Contains(header, "Model gpt-4.1-mini") {
		t.Fatalf("expected header to contain model, got %q", header)
	}
	if !strings.Contains(header, "Workdir D:/workspace") {
		t.Fatalf("expected header to contain workdir, got %q", header)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}
