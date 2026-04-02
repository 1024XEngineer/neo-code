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
	Kind markdownSegmentKind
	Text string
}

var (
	copyCodeButtonPattern = regexp.MustCompile(`\[Copy code #([0-9]+)\]`)
	clipboardWriteAll     = clipboard.WriteAll
)

func splitMarkdownSegments(content string) []markdownSegment {
	parts := strings.Split(content, "```")
	if len(parts) == 1 {
		return []markdownSegment{{Kind: markdownSegmentText, Text: content}}
	}

	segments := make([]markdownSegment, 0, len(parts))
	for i, part := range parts {
		if i%2 == 0 {
			if part != "" {
				segments = append(segments, markdownSegment{Kind: markdownSegmentText, Text: part})
			}
			continue
		}

		code := extractCodeBlockContent(part)
		if code == "" {
			continue
		}
		segments = append(segments, markdownSegment{Kind: markdownSegmentCode, Text: code})
	}
	if len(segments) == 0 {
		return []markdownSegment{{Kind: markdownSegmentText, Text: content}}
	}
	return segments
}

func extractFencedCodeBlocks(content string) []string {
	segments := splitMarkdownSegments(content)
	blocks := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment.Kind == markdownSegmentCode && strings.TrimSpace(segment.Text) != "" {
			blocks = append(blocks, strings.TrimSpace(segment.Text))
		}
	}
	return blocks
}

func extractCodeBlockContent(raw string) string {
	code := strings.Trim(raw, "\n")
	if code == "" {
		return ""
	}
	lines := strings.Split(code, "\n")
	if len(lines) > 1 && !strings.Contains(lines[0], " ") && !strings.Contains(lines[0], "\t") {
		code = strings.Join(lines[1:], "\n")
	}
	return strings.TrimSpace(code)
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

func (a *App) handleTranscriptCopyClick(msg tea.MouseMsg) bool {
	line, relativeX, ok := a.transcriptLineAtMouse(msg)
	if !ok {
		return false
	}

	buttonID, startCol, endCol, ok := parseCopyCodeButton(line)
	if !ok {
		return false
	}
	if relativeX < startCol || relativeX >= endCol {
		return false
	}

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
