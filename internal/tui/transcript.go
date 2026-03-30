package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type messageSegmentKind int

const (
	segmentText messageSegmentKind = iota
	segmentCode
)

type messageSegment struct {
	Kind     messageSegmentKind
	Content  string
	Language string
}

type renderedContent struct {
	View       string
	Height     int
	CodeBlocks []codeBlockTarget
}

type renderedMessageBlock struct {
	View       string
	Height     int
	CodeBlocks []codeBlockTarget
}

func parseMessageSegments(content string) []messageSegment {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	segments := make([]messageSegment, 0, 4)
	buffer := make([]string, 0, len(lines))
	inCode := false
	language := ""

	flush := func(kind messageSegmentKind, lang string) {
		if len(buffer) == 0 {
			return
		}
		segments = append(segments, messageSegment{
			Kind:     kind,
			Content:  strings.Join(buffer, "\n"),
			Language: lang,
		})
		buffer = buffer[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flush(segmentCode, language)
				inCode = false
				language = ""
			} else {
				flush(segmentText, "")
				inCode = true
				language = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			}
			continue
		}
		buffer = append(buffer, line)
	}

	switch {
	case len(buffer) == 0:
	case inCode:
		flush(segmentCode, language)
	default:
		flush(segmentText, "")
	}

	if len(segments) == 0 {
		return []messageSegment{{Kind: segmentText, Content: content}}
	}
	return segments
}

func wrappedLineCount(text string, width int) int {
	if width <= 0 {
		return 1
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	total := 0
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			total++
			continue
		}
		total += (len(runes)-1)/width + 1
	}
	if total == 0 {
		return 1
	}
	return total
}

func padBetween(width int, left string, right string) string {
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	spaceWidth := max(1, width-leftWidth-rightWidth)
	return left + strings.Repeat(" ", spaceWidth) + right
}
