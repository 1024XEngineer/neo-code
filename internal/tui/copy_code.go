package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	copyCodeButtonPattern = regexp.MustCompile(`\[Copy code #([0-9]+)\]`)
	clipboardWriteAll     = clipboard.WriteAll
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
			flushCode()
			inCode = false
		}

		if indented {
			flushText()
			inCode = true
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

func (a *App) setCodeCopyBlocks(bindings []copyCodeButtonBinding) {
	a.codeCopyBlocks = make(map[int]string, len(bindings))
	for _, binding := range bindings {
		a.codeCopyBlocks[binding.ID] = binding.Code
	}
}

func parseCopyCodeButton(line string) (id int, startCol int, endCol int, ok bool) {
	clean := ansiEscapePattern.ReplaceAllString(line, "")
	matches := copyCodeButtonPattern.FindStringSubmatchIndex(clean)
	if len(matches) < 4 {
		return 0, 0, 0, false
	}

	buttonText := clean[matches[0]:matches[1]]
	idText := clean[matches[2]:matches[3]]
	id, err := strconv.Atoi(idText)
	if err != nil {
		return 0, 0, 0, false
	}

	startCol = lipgloss.Width(clean[:matches[0]])
	endCol = startCol + lipgloss.Width(buttonText)
	return id, startCol, endCol, true
}

func (a *App) copyButtonIDAtMouse(msg tea.MouseMsg) (int, bool) {
	line, relativeX, ok := a.transcriptLineAtMouse(msg)
	if !ok {
		return 0, false
	}

	buttonID, startCol, endCol, ok := parseCopyCodeButton(line)
	if !ok {
		return 0, false
	}
	if relativeX < startCol || relativeX >= endCol {
		return 0, false
	}
	return buttonID, true
}

func (a *App) copyCodeBlockByID(buttonID int) bool {
	code, ok := a.codeCopyBlocks[buttonID]
	if !ok {
		a.state.ExecutionError = statusCodeCopyError
		a.state.StatusText = statusCodeCopyError
		a.appendActivity("clipboard", statusCodeCopyError, fmt.Sprintf("button #%d", buttonID), true)
		return true
	}

	if err := clipboardWriteAll(code); err != nil {
		a.state.ExecutionError = err.Error()
		a.state.StatusText = statusCodeCopyError
		a.appendActivity("clipboard", statusCodeCopyError, err.Error(), true)
		return true
	}

	a.state.ExecutionError = ""
	a.state.StatusText = fmt.Sprintf(statusCodeCopied, buttonID)
	a.appendActivity("clipboard", "Copied code block", fmt.Sprintf("#%d", buttonID), false)
	return true
}

func (a App) transcriptLineAtMouse(msg tea.MouseMsg) (line string, relativeX int, ok bool) {
	if !a.isMouseWithinTranscript(msg) {
		return "", 0, false
	}

	x, y, _, _ := a.transcriptBounds()
	lineIndex := msg.Y - y
	if lineIndex < 0 {
		return "", 0, false
	}

	lines := strings.Split(a.transcript.View(), "\n")
	if lineIndex >= len(lines) {
		return "", 0, false
	}
	return lines[lineIndex], msg.X - x, true
}
