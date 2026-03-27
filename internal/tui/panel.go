package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func renderPanelFrame(
	title string,
	subtitle string,
	width int,
	height int,
	active bool,
	accent lipgloss.Color,
	content string,
) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	bodyWidth := max(8, width-4)
	bodyHeight := max(1, height-3)
	borderColor := themeBorder
	background := themePanel
	if active {
		borderColor = accent
		background = themePanelAlt
	}

	header := alignHeaderParts(
		bodyWidth,
		lipgloss.NewStyle().Bold(true).Foreground(accent).Render(strings.ToUpper(title)),
		lipgloss.NewStyle().Foreground(themeMuted).Render(truncateVisual(subtitle, max(10, bodyWidth/2))),
	)

	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Height(bodyHeight).
		Foreground(themeText).
		Background(background).
		Render(content)

	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(background).
		Padding(0, 1).
		Render(header + "\n" + body)
}

func renderSectionTitle(label string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(themeAccent2).Render(label)
}

func renderBadge(label string, color lipgloss.Color, background lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(background).
		Background(color).
		Padding(0, 1).
		Render(label)
}

func renderAction(label string, active bool, color lipgloss.Color) string {
	background := themePanelAlt
	foreground := color
	if active {
		background = color
		foreground = themeCanvas
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(foreground).
		Background(background).
		Padding(0, 1).
		Render(label)
}

type alignedHeaderParts struct {
	Text       string
	RightStart int
	RightWidth int
}

func layoutHeaderParts(width int, left string, right string) alignedHeaderParts {
	parts := alignedHeaderParts{RightStart: -1}
	if width <= 0 {
		return parts
	}
	if right == "" {
		parts.Text = fitLine(left, width)
		return parts
	}

	right = ansi.Truncate(right, width, "")
	rightWidth := ansi.StringWidth(right)
	if rightWidth == 0 {
		parts.Text = fitLine(left, width)
		return parts
	}
	if rightWidth >= width {
		parts.Text = fitLine(right, width)
		parts.RightStart = 0
		parts.RightWidth = width
		return parts
	}

	leftWidth := max(0, width-rightWidth-1)
	left = ansi.Truncate(left, leftWidth, "")
	spacing := max(1, width-ansi.StringWidth(left)-rightWidth)

	parts.Text = fitLine(left+strings.Repeat(" ", spacing)+right, width)
	parts.RightStart = width - rightWidth
	parts.RightWidth = rightWidth
	return parts
}

func alignHeaderParts(width int, left string, right string) string {
	return layoutHeaderParts(width, left, right).Text
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeScreen(content string, width int, height int) string {
	lines := strings.Split(content, "\n")
	normalized := make([]string, 0, max(height, len(lines)))
	for _, line := range lines {
		normalized = append(normalized, fitLine(line, width))
	}
	for len(normalized) < height {
		normalized = append(normalized, strings.Repeat(" ", width))
	}
	if len(normalized) > height {
		normalized = normalized[:height]
	}
	return strings.Join(normalized, "\n")
}

func overlayAt(base string, overlay string, x int, y int, width int, height int) string {
	baseLines := strings.Split(normalizeScreen(base, width, height), "\n")
	overlayLines := strings.Split(overlay, "\n")

	for idx, overlayLine := range overlayLines {
		target := y + idx
		if target < 0 || target >= len(baseLines) {
			continue
		}
		overlayWidth := ansi.StringWidth(overlayLine)
		if overlayWidth == 0 {
			continue
		}
		if x >= width {
			continue
		}
		if overlayWidth > width-x {
			overlayLine = ansi.Truncate(overlayLine, width-x, "")
			overlayWidth = ansi.StringWidth(overlayLine)
		}

		prefix := ansi.Cut(baseLines[target], 0, x)
		suffix := ""
		if x+overlayWidth < width {
			suffix = ansi.Cut(baseLines[target], x+overlayWidth, width)
		}
		baseLines[target] = fitLine(prefix+overlayLine+suffix, width)
	}

	return strings.Join(baseLines, "\n")
}

func boxTop(width int, color lipgloss.Color) string {
	if width < 2 {
		return strings.Repeat(" ", max(0, width))
	}
	return lipgloss.NewStyle().Foreground(color).Render("+" + strings.Repeat("-", width-2) + "+")
}

func boxBottom(width int, color lipgloss.Color) string {
	if width < 2 {
		return strings.Repeat(" ", max(0, width))
	}
	return lipgloss.NewStyle().Foreground(color).Render("+" + strings.Repeat("-", width-2) + "+")
}

func boxLine(width int, border lipgloss.Color, background lipgloss.Color, content string) string {
	if width < 2 {
		return ""
	}
	bodyWidth := width - 2
	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Background(background).
		Foreground(themeText).
		Render(fitLine(content, bodyWidth))
	return lipgloss.NewStyle().Foreground(border).Render("|") + body + lipgloss.NewStyle().Foreground(border).Render("|")
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	padding := width - ansi.StringWidth(line)
	if padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}
