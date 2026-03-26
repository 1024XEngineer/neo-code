package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
	Streaming bool
}

type MessageList struct {
	Messages []Message
	Width    int
}

func (ml MessageList) Render() string {
	return ml.RenderLayout().Content
}

func (ml MessageList) RenderLayout() RenderedChatLayout {
	if len(ml.Messages) == 0 {
		return RenderedChatLayout{}
	}
	contentWidth := ml.Width - 4
	if contentWidth <= 0 {
		contentWidth = ml.Width
	}

	userMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Bold(true)

	assistantMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5C07B"))

	systemMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C678DD"))

	var b strings.Builder
	regions := make([]ClickableRegion, 0)
	row := 0

	for i, msg := range ml.Messages {
		idx := i + 1
		switch msg.Role {
		case "user":
			line := renderMessageLine(fmt.Sprintf("You [%d]:", idx), msg.Content, userMsgStyle, contentWidth)
			b.WriteString(line)
			b.WriteString("\n\n")
			row += strings.Count(line, "\n") + 2

		case "assistant":
			rendered, assistantRegions, consumedRows := renderAssistantMessage(i, idx, msg.Content, contentWidth, row, assistantMsgStyle)
			b.WriteString(rendered)
			regions = append(regions, assistantRegions...)
			row += consumedRows

		case "system":
			line := renderMessageLine("[System]", msg.Content, systemMsgStyle, contentWidth)
			b.WriteString(line)
			b.WriteString("\n\n")
			row += strings.Count(line, "\n") + 2
		}
	}

	return RenderedChatLayout{
		Content: strings.TrimRight(b.String(), "\n"),
		Regions: regions,
	}
}

func renderMessageLine(label string, content string, labelStyle lipgloss.Style, width int) string {
	rendered := labelStyle.Render(label)
	if strings.TrimSpace(content) == "" {
		return rendered
	}
	return rendered + "\n" + wrapTextBlock(content, width)
}

func renderAssistantMessage(messageIndex, displayIndex int, content string, width int, startRow int, labelStyle lipgloss.Style) (string, []ClickableRegion, int) {
	var b strings.Builder
	regions := make([]ClickableRegion, 0)
	rows := 0

	b.WriteString(labelStyle.Render(fmt.Sprintf("Neo [%d]:", displayIndex)))
	b.WriteString("\n")
	rows++

	currentRow := startRow + rows
	blockIndex := 0
	for _, segment := range assistantSegments(content) {
		rendered, region, consumedRows := renderAssistantSegment(messageIndex, &blockIndex, segment, width, currentRow)
		b.WriteString(rendered)
		if region != nil {
			regions = append(regions, *region)
		}
		rows += consumedRows
		currentRow += consumedRows
	}

	b.WriteString("\n\n")
	rows += 2

	return b.String(), regions, rows
}

func assistantSegments(content string) []ContentSegment {
	segments := ParseContentSegments(content)
	if len(segments) == 0 {
		return []ContentSegment{{Type: SegmentText, Text: "..."}}
	}
	return segments
}

func renderAssistantSegment(messageIndex int, blockIndex *int, segment ContentSegment, width int, row int) (string, *ClickableRegion, int) {
	if segment.Type == SegmentCodeBlock {
		*blockIndex = *blockIndex + 1
		codeLang := resolveSegmentLanguage(segment)
		rendered := RenderCodeBlock(segment, width, CopyActionLabel())
		region := BuildCopyRegion(messageIndex, *blockIndex, row, segment.Code, codeLang)
		return rendered, &region, strings.Count(rendered, "\n")
	}

	rendered := renderTextSegment(segment.Text, width)
	return rendered, nil, strings.Count(rendered, "\n")
}

func resolveSegmentLanguage(segment ContentSegment) string {
	codeLang := strings.TrimSpace(segment.Lang)
	if codeLang == "" {
		codeLang = DetectLanguage(segment.Code)
	}
	if codeLang == "" {
		return "text"
	}
	return codeLang
}

func renderTextSegment(text string, width int) string {
	if text == "" {
		return ""
	}
	return wrapTextBlock(text, width) + "\n"
}
