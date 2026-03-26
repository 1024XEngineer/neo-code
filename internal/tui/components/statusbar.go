package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type StatusBar struct {
	Mode      string
	Focus     string
	Model     string
	MemoryCnt int
	Status    string
	Busy      bool
	Width     int
}

func (s StatusBar) Render() string {
	statusText := strings.TrimSpace(s.Status)
	if statusText == "" {
		statusText = "Ready"
	}
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Background(lipgloss.Color("#282C34")).
		Padding(0, 1)
	if s.Busy {
		statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5C07B")).
			Background(lipgloss.Color("#282C34")).
			Padding(0, 1)
	}

	badgeStyle := func(fg string) lipgloss.Style {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(fg)).
			Background(lipgloss.Color("#282C34")).
			Padding(0, 1)
	}

	timeStr := time.Now().Format("15:04")
	timestampStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5C6370"))

	modeText := strings.TrimSpace(s.Mode)
	if modeText == "" {
		modeText = "chat"
	}
	focusText := strings.TrimSpace(s.Focus)
	if focusText == "" {
		focusText = "composer"
	}
	modelText := strings.TrimSpace(s.Model)
	if modelText == "" {
		modelText = "unknown"
	}
	memText := fmt.Sprintf("memory %d", s.MemoryCnt)
	statusWidth := minInt(maxInt(12, s.Width/4), maxInt(12, s.Width-10))
	statusText = truncateText(statusText, statusWidth)
	right := statusStyle.Render(statusText) + " " + timestampStyle.Render(timeStr)

	leftBudget := maxInt(1, s.Width-lipgloss.Width(right)-1)
	leftContentBudget := maxInt(1, leftBudget-11)
	modeLabel := truncateText("mode "+modeText, maxInt(4, leftContentBudget/5))
	focusLabel := truncateText("focus "+focusText, maxInt(4, leftContentBudget/5))
	modelLabel := truncateText(modelText, maxInt(4, leftContentBudget/3))
	memLabel := truncateText(memText, maxInt(4, leftContentBudget/5))

	left := strings.Join([]string{
		badgeStyle("#61AFEF").Render(modeLabel),
		badgeStyle("#D19A66").Render(focusLabel),
		badgeStyle("#98C379").Render(modelLabel),
		badgeStyle("#C678DD").Render(memLabel),
	}, " ")

	space := s.Width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 0 {
		space = 0
	}

	var b strings.Builder
	b.WriteString(left)
	if space > 0 {
		b.WriteString(strings.Repeat(" ", space))
	}
	b.WriteString(right)

	return b.String()
}
