package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type InputBox struct {
	ModeLabel string
	ModeColor string
	MetaText  string
	Body      string
	NoteText  string
	Status    string
	Width     int
}

func (i InputBox) Render() string {
	parts := []string{strings.TrimRight(i.Body, "\n")}
	lineWidth := i.Width

	modeText := i.ModeLabel
	if modeText != "" {
		modeColor := i.ModeColor
		if modeColor == "" {
			modeColor = "#61AFEF"
		}
		modeBadge := lipgloss.NewStyle().
			Foreground(lipgloss.Color(modeColor)).
			Background(lipgloss.Color("#282C34")).
			Bold(true).
			Padding(0, 1).
			Render(modeText)

		line := modeBadge
		metaText := strings.TrimSpace(i.MetaText)
		if metaText != "" {
			if lineWidth > 0 {
				badgeWidth := len([]rune(strings.TrimSpace(modeText))) + 2
				metaText = truncateText(metaText, maxInt(0, lineWidth-badgeWidth-1))
			}
			if metaText != "" {
				line += " " + lipgloss.NewStyle().
					Foreground(lipgloss.Color("#5C6370")).
					Render(metaText)
			}
		}
		parts = append(parts, line)
	}

	statusText := i.Status
	if statusText == "" {
		statusText = "Ready"
	}

	status := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF")).
		Render(singleLineText(statusText, lineWidth))

	parts = append(parts, status)

	if i.NoteText != "" {
		note := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F8794")).
			Render(singleLineText(i.NoteText, lineWidth))
		parts = append(parts, note)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func singleLineText(text string, width int) string {
	if width <= 0 {
		return strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	}
	return truncateText(text, width)
}
