package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	providertypes "neo-code/internal/provider/types"
)

func TestRebuildTranscriptDoesNotCollapseAssistantAcrossToolBoundary(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 120
	app.height = 32
	app.applyComponentLayout(true)
	app.activeMessages = []providertypes.Message{
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before tool")}},
		{Role: roleTool, Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool output")}},
		{Role: roleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after tool")}},
	}

	app.rebuildTranscript()
	plain := copyCodeANSIPattern.ReplaceAllString(app.transcriptContent, "")
	if count := strings.Count(plain, messageTagAgent); count != 2 {
		t.Fatalf("expected two agent tags across tool boundary, got %d in %q", count, plain)
	}
}

func TestHandleTranscriptMouseDragMotionWithButtonNone(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent(strings.Repeat("line\n", 40))

	x, y, _, _ := app.transcriptBounds()
	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 2,
		Y:      y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected press to begin selection")
	}

	if !app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 6,
		Y:      y + 2,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionMotion,
		Type:   tea.MouseMotion,
	}) {
		t.Fatalf("expected motion with button none while dragging to be handled")
	}
	if app.textSelection.endLine != 2 || app.textSelection.endCol <= app.textSelection.startCol {
		t.Fatalf("expected selection to update on motion with button none, got line=%d col=%d", app.textSelection.endLine, app.textSelection.endCol)
	}
}

func TestHighlightTranscriptContentKeepsStyleWhenZeroWidthOnLine(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 1
	app.textSelection.endLine = 1
	app.textSelection.endCol = 0

	content := "\x1b[31mabc\x1b[0m\n\x1b[32mxyz\x1b[0m"
	highlighted := app.highlightTranscriptContent(content)
	lines := strings.Split(highlighted, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1], "\x1b[32m") {
		t.Fatalf("expected zero-width selected line to keep existing ANSI style, got %q", lines[1])
	}
}

func TestCopySelectionToClipboardFailureKeepsSelection(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("hello world")
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 0
	app.textSelection.endLine = 0
	app.textSelection.endCol = 5

	originalClipboard := clipboardWriteAll
	clipboardWriteAll = func(string) error {
		return fmt.Errorf("clipboard failed")
	}
	defer func() { clipboardWriteAll = originalClipboard }()

	app.copySelectionToClipboard()
	if app.state.StatusText != "Failed to copy selection" {
		t.Fatalf("expected status on copy error, got %q", app.state.StatusText)
	}
	if !app.textSelection.active {
		t.Fatalf("expected selection to remain active on copy failure")
	}
}

func TestHandleTranscriptMouseRightClickWithoutSelectionNoop(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("line")
	x, y, _, _ := app.transcriptBounds()
	if app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	}) {
		t.Fatalf("expected right click without selection to be ignored")
	}
}

func TestSelectionHelpersGuardAndClampBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if _, _, _, _, ok := app.textSelectionRange([]string{"x"}); ok {
		t.Fatalf("expected inactive selection to return false")
	}
	if _, _, ok := app.normalizeSelectionPosition(nil, 0, 0); ok {
		t.Fatalf("expected normalizeSelectionPosition to reject empty lines")
	}

	lines := []string{"abc", "de"}
	line, col, ok := app.normalizeSelectionPosition(lines, -3, 99)
	if !ok || line != 0 || col != 3 {
		t.Fatalf("expected clamp to first line end, got line=%d col=%d ok=%v", line, col, ok)
	}
	line, col, ok = app.normalizeSelectionPosition(lines, 9, -4)
	if !ok || line != 1 || col != 0 {
		t.Fatalf("expected clamp to last line start, got line=%d col=%d ok=%v", line, col, ok)
	}

	app.textSelection.active = true
	app.textSelection.startLine = 1
	app.textSelection.startCol = 2
	app.textSelection.endLine = 0
	app.textSelection.endCol = 1
	startLine, startCol, endLine, endCol, rangeOK := app.textSelectionRange(lines)
	if !rangeOK || startLine != 0 || startCol != 1 || endLine != 1 || endCol != 2 {
		t.Fatalf("expected reversed range to normalize ordering, got %d:%d -> %d:%d ok=%v", startLine, startCol, endLine, endCol, rangeOK)
	}

	app.textSelection.endLine = app.textSelection.startLine
	app.textSelection.endCol = app.textSelection.startCol
	if _, _, _, _, equalOK := app.textSelectionRange(lines); equalOK {
		t.Fatalf("expected empty range to be treated as no selection")
	}
}

func TestSplitMarkdownSegmentsFallbackWhenFenceHasNoCode(t *testing.T) {
	segments := splitMarkdownSegments("```go\n```")
	if len(segments) != 1 {
		t.Fatalf("expected fallback text segment count 1, got %d", len(segments))
	}
	if segments[0].Kind != markdownSegmentText {
		t.Fatalf("expected fallback text segment, got kind=%v", segments[0].Kind)
	}

	indented := splitIndentedCodeSegments("    \n")
	if len(indented) != 1 || indented[0].Kind != markdownSegmentText {
		t.Fatalf("expected blank indented content to stay text, got %+v", indented)
	}
}

func TestSelectionPositionAndDragGuardBranches(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("alpha\nbeta")

	if _, _, ok := app.selectionPositionAtMouse(tea.MouseMsg{X: -1, Y: -1}); ok {
		t.Fatalf("expected outside transcript mouse position to be rejected")
	}
	if app.beginTextSelection(tea.MouseMsg{X: -1, Y: -1}) {
		t.Fatalf("expected beginTextSelection outside transcript to fail")
	}
	if app.updateTextSelection(tea.MouseMsg{X: 0, Y: 0}) {
		t.Fatalf("expected updateTextSelection to fail when not dragging")
	}
	if app.finishTextSelection() {
		t.Fatalf("expected finishTextSelection to fail when not dragging")
	}

	x, y, _, _ := app.transcriptBounds()
	if !app.beginTextSelection(tea.MouseMsg{X: x + 1, Y: y + 1}) {
		t.Fatalf("expected beginTextSelection to succeed in transcript")
	}
	if app.updateTextSelection(tea.MouseMsg{X: x - 2, Y: y - 1}) {
		t.Fatalf("expected updateTextSelection to fail when mouse moved outside transcript")
	}

	app.textSelection.endLine = app.textSelection.startLine
	app.textSelection.endCol = app.textSelection.startCol
	if !app.finishTextSelection() {
		t.Fatalf("expected finishTextSelection to handle empty selection")
	}
	if app.textSelection.active {
		t.Fatalf("expected empty finished selection to be cleared")
	}
}

func TestSelectionPositionAtMouseRejectsBlankViewportRows(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("only-one-line")

	x, y, _, h := app.transcriptBounds()
	if h < 2 {
		t.Fatalf("expected transcript viewport with spare rows, got height=%d", h)
	}

	if _, _, ok := app.selectionPositionAtMouse(tea.MouseMsg{X: x + 1, Y: y + h - 1}); ok {
		t.Fatalf("expected blank viewport row to be ignored")
	}
}

func TestSetTranscriptContentClearsSelectionAfterContentChange(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("line-one")
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 0
	app.textSelection.endLine = 0
	app.textSelection.endCol = 4
	app.refreshTranscriptHighlight()

	app.setTranscriptContent("line-two")
	if app.textSelection.active || app.textSelection.dragging {
		t.Fatalf("expected selection to be cleared after transcript content changes")
	}
	if app.hasTextSelection() {
		t.Fatalf("expected no valid selection range after transcript content changes")
	}
}

func TestUpdateTextSelectionSkipsUnchangedPosition(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.setTranscriptContent("alpha\nbeta")

	x, y, _, _ := app.transcriptBounds()
	if !app.beginTextSelection(tea.MouseMsg{X: x + 1, Y: y + 1}) {
		t.Fatalf("expected beginTextSelection to succeed")
	}
	if !app.updateTextSelection(tea.MouseMsg{X: x + 2, Y: y + 1}) {
		t.Fatalf("expected first updateTextSelection to succeed")
	}

	app.transcript.SetContent("sentinel-marker")
	if !app.updateTextSelection(tea.MouseMsg{X: x + 2, Y: y + 1}) {
		t.Fatalf("expected unchanged motion to be handled")
	}
	if !strings.Contains(app.transcript.View(), "sentinel-marker") {
		t.Fatalf("expected unchanged motion to skip redraw")
	}
}

func TestHighlightTranscriptContentPreservesANSIOutsideSelection(t *testing.T) {
	app, _ := newTestApp(t)
	app.width = 100
	app.height = 24
	app.applyComponentLayout(true)
	app.textSelection.active = true
	app.textSelection.startLine = 0
	app.textSelection.startCol = 6
	app.textSelection.endLine = 0
	app.textSelection.endCol = 11

	highlighted := app.highlightTranscriptContent("\x1b[31mhello world\x1b[0m")
	if !strings.Contains(highlighted, "\x1b[31m") {
		t.Fatalf("expected highlighted content to preserve existing ANSI style runs")
	}
	if plain := copyCodeANSIPattern.ReplaceAllString(highlighted, ""); plain != "hello world" {
		t.Fatalf("expected highlighted content to preserve visible text, got %q", plain)
	}
}

func TestCopySelectionToClipboardNoSelectionNoop(t *testing.T) {
	app, _ := newTestApp(t)
	app.setTranscriptContent("hello")
	app.state.StatusText = "unchanged"
	app.copySelectionToClipboard()
	if app.state.StatusText != "unchanged" {
		t.Fatalf("expected no-selection copy to be noop, got status %q", app.state.StatusText)
	}
}
