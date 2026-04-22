package tui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	tuiinfra "neo-code/internal/tui/infra"
)

type copyCodeButtonBinding struct {
	ID   int
	Code string
}

type markdownSegmentKind int

const (
	markdownSegmentText markdownSegmentKind = iota
	markdownSegmentCode
)

type markdownSegment struct {
	Kind   markdownSegmentKind
	Text   string
	Fenced string
	Code   string
}

var (
	copyCodeANSIPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	clipboardWriteAll   = tuiinfra.CopyText
)

func splitMarkdownSegments(content string) []markdownSegment {
	if !strings.Contains(content, "```") {
		return splitIndentedCodeSegments(content)
	}

	lines := strings.Split(content, "\n")
	segments := make([]markdownSegment, 0, 8)
	textLines := make([]string, 0, len(lines))
	codeLines := make([]string, 0, len(lines))
	inFence := false
	fenceInfo := ""
	sawFence := false

	flushText := func() {
		if len(textLines) == 0 {
			return
		}
		segments = append(segments, markdownSegment{
			Kind: markdownSegmentText,
			Text: strings.Join(textLines, "\n"),
		})
		textLines = textLines[:0]
	}
	flushCode := func() {
		if len(codeLines) == 0 {
			codeLines = codeLines[:0]
			return
		}
		code := strings.Join(codeLines, "\n")
		code = strings.TrimRight(code, "\n")
		if strings.TrimSpace(code) == "" {
			codeLines = codeLines[:0]
			return
		}
		fenced := "```"
		if fenceInfo != "" {
			fenced += fenceInfo
		}
		fenced += "\n" + code + "\n```"
		segments = append(segments, markdownSegment{
			Kind:   markdownSegmentCode,
			Fenced: fenced,
			Code:   code,
		})
		codeLines = codeLines[:0]
	}

	for _, line := range lines {
		if !inFence {
			if info, ok := parseFenceOpenLine(line); ok {
				sawFence = true
				flushText()
				inFence = true
				fenceInfo = info
				continue
			}
			textLines = append(textLines, line)
			continue
		}

		if isFenceCloseLine(line) {
			flushCode()
			inFence = false
			fenceInfo = ""
			continue
		}
		codeLines = append(codeLines, line)
	}

	if inFence {
		flushCode()
	}
	flushText()

	if sawFence && len(segments) > 0 {
		return segments
	}

	return splitIndentedCodeSegments(content)
}

func splitIndentedCodeSegments(content string) []markdownSegment {
	lines := strings.Split(content, "\n")
	segments := make([]markdownSegment, 0, 4)
	textLines := make([]string, 0, len(lines))
	codeLines := make([]string, 0, len(lines))
	inCode := false

	flushText := func() {
		if len(textLines) == 0 {
			return
		}
		segments = append(segments, markdownSegment{
			Kind: markdownSegmentText,
			Text: strings.Join(textLines, "\n"),
		})
		textLines = textLines[:0]
	}
	flushCode := func() {
		if len(codeLines) == 0 {
			return
		}
		code := strings.Join(codeLines, "\n")
		code = strings.TrimSpace(code)
		if code == "" {
			codeLines = codeLines[:0]
			return
		}
		segments = append(segments, markdownSegment{
			Kind:   markdownSegmentCode,
			Fenced: "```\n" + code + "\n```",
			Code:   code,
		})
		codeLines = codeLines[:0]
	}

	for _, line := range lines {
		indented := isIndentedCodeLine(line)
		if inCode {
			if indented {
				codeLines = append(codeLines, trimCodeIndent(line))
				continue
			}
			if strings.TrimSpace(line) == "" {
				codeLines = append(codeLines, "")
				continue
			}
			if len(codeLines) > 0 {
				flushCode()
			}
			inCode = false
		}

		if indented {
			if !inCode {
				flushText()
				inCode = true
			}
			codeLines = append(codeLines, trimCodeIndent(line))
			continue
		}

		textLines = append(textLines, line)
	}

	if inCode {
		flushCode()
	}
	flushText()

	if len(segments) == 0 {
		return []markdownSegment{{Kind: markdownSegmentText, Text: content}}
	}
	return segments
}

func extractFencedCodeBlocks(content string) []string {
	segments := splitMarkdownSegments(content)
	blocks := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Kind == markdownSegmentCode && strings.TrimSpace(segment.Code) != "" {
			blocks = append(blocks, segment.Code)
		}
	}
	return blocks
}

func parseFenceOpenLine(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "```") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "```")), true
}

func isFenceCloseLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return strings.TrimSpace(trimmed) == "```"
}

func isIndentedCodeLine(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

func trimCodeIndent(line string) string {
	if strings.HasPrefix(line, "\t") {
		return strings.TrimPrefix(line, "\t")
	}
	if strings.HasPrefix(line, "    ") {
		return line[4:]
	}
	return line
}

func (a App) selectionLines() []string {
	return strings.Split(a.transcriptContent, "\n")
}

func (a App) normalizeSelectionPosition(lines []string, line int, col int) (int, int, bool) {
	if len(lines) == 0 {
		return 0, 0, false
	}
	if line < 0 {
		line = 0
	}
	if line >= len(lines) {
		line = len(lines) - 1
	}
	plain := copyCodeANSIPattern.ReplaceAllString(lines[line], "")
	lineWidth := lipgloss.Width(plain)
	if col < 0 {
		col = 0
	}
	if col > lineWidth {
		col = lineWidth
	}
	return line, col, true
}

func (a App) selectionPositionAtMouse(msg tea.MouseMsg) (line int, col int, ok bool) {
	if !a.isMouseWithinTranscript(msg) {
		return 0, 0, false
	}

	x, y, _, _ := a.transcriptBounds()
	currentLine := a.transcript.YOffset + (msg.Y - y)
	currentCol := msg.X - x
	lines := a.selectionLines()
	if len(lines) == 0 || currentLine < 0 || currentLine >= len(lines) {
		return 0, 0, false
	}
	return a.normalizeSelectionPosition(lines, currentLine, currentCol)
}

func (a App) textSelectionRange(lines []string) (startLine int, startCol int, endLine int, endCol int, ok bool) {
	if !a.textSelection.active || len(lines) == 0 {
		return 0, 0, 0, 0, false
	}
	sLine, sCol, _ := a.normalizeSelectionPosition(lines, a.textSelection.startLine, a.textSelection.startCol)
	eLine, eCol, _ := a.normalizeSelectionPosition(lines, a.textSelection.endLine, a.textSelection.endCol)
	if sLine > eLine || (sLine == eLine && sCol > eCol) {
		sLine, eLine = eLine, sLine
		sCol, eCol = eCol, sCol
	}
	if sLine == eLine && sCol == eCol {
		return 0, 0, 0, 0, false
	}
	return sLine, sCol, eLine, eCol, true
}

func (a App) hasTextSelection() bool {
	_, _, _, _, ok := a.textSelectionRange(a.selectionLines())
	return ok
}

func (a *App) beginTextSelection(msg tea.MouseMsg) bool {
	line, col, ok := a.selectionPositionAtMouse(msg)
	if !ok {
		return false
	}
	a.textSelection.active = true
	a.textSelection.dragging = true
	a.textSelection.startLine = line
	a.textSelection.startCol = col
	a.textSelection.endLine = line
	a.textSelection.endCol = col
	a.refreshTranscriptHighlight()
	return true
}

func (a *App) updateTextSelection(msg tea.MouseMsg) bool {
	if !a.textSelection.dragging {
		return false
	}
	line, col, ok := a.selectionPositionAtMouse(msg)
	if !ok {
		return false
	}
	if a.textSelection.endLine == line && a.textSelection.endCol == col {
		return true
	}
	a.textSelection.endLine = line
	a.textSelection.endCol = col
	a.refreshTranscriptHighlight()
	return true
}

func (a *App) finishTextSelection() bool {
	if !a.textSelection.dragging {
		return false
	}
	a.textSelection.dragging = false
	if !a.hasTextSelection() {
		a.clearTextSelection()
		return true
	}
	a.refreshTranscriptHighlight()
	return true
}

func (a *App) refreshTranscriptHighlight() {
	if a.hasTextSelection() {
		highlighted := a.highlightTranscriptContent(a.transcriptContent)
		a.transcript.SetContent(highlighted)
		return
	}
	a.transcript.SetContent(a.transcriptContent)
}

func (a *App) copySelectionToClipboard() {
	lines := a.selectionLines()
	startLine, startCol, endLine, endCol, ok := a.textSelectionRange(lines)
	if !ok {
		return
	}

	selectedLines := make([]string, 0, endLine-startLine+1)
	for i := startLine; i <= endLine && i < len(lines); i++ {
		plain := copyCodeANSIPattern.ReplaceAllString(lines[i], "")
		lineWidth := lipgloss.Width(plain)
		from := 0
		to := lineWidth
		if i == startLine {
			from = startCol
		}
		if i == endLine {
			to = endCol
		}
		selectedLines = append(selectedLines, ansi.Cut(plain, from, to))
	}

	content := strings.Join(selectedLines, "\n")
	if err := clipboardWriteAll(content); err != nil {
		a.state.StatusText = "Failed to copy selection"
		return
	}

	a.state.StatusText = "Copied selected text"
	a.clearTextSelection()
}

func (a *App) clearTextSelection() {
	a.textSelection.active = false
	a.textSelection.dragging = false
	a.textSelection.startLine = 0
	a.textSelection.startCol = 0
	a.textSelection.endLine = 0
	a.textSelection.endCol = 0

	a.transcript.SetContent(a.transcriptContent)
}
