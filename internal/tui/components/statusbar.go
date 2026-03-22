package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type StatusBar struct {
	Model      string
	MemoryCnt  int
	Generating bool
	Width      int
}

func (s StatusBar) Render() string {
	modelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Background(lipgloss.Color("#282C34")).
		Padding(0, 1)

	memStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C678DD")).
		Background(lipgloss.Color("#282C34")).
		Padding(0, 1)

	status := "●"
	if s.Generating {
		status = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5C07B")).
			Render("◐")
	}

	timeStr := time.Now().Format("15:04")
	timestampStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5C6370"))

	memText := fmt.Sprintf("记忆: %d", s.MemoryCnt)
	space := s.Width - len(s.Model) - len(memText) - len(timeStr) - 10
	if space < 0 {
		space = 0
	}

	var b strings.Builder
	b.WriteString(modelStyle.Render(s.Model))
	b.WriteString("  ")
	b.WriteString(memStyle.Render(memText))
	b.WriteString("  ")
	b.WriteString(status)
	if space > 0 {
		b.WriteString(strings.Repeat(" ", space))
	}
	b.WriteString(timestampStyle.Render(timeStr))

	return b.String()
}
