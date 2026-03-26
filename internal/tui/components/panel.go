package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type uiTheme struct {
	canvas string
	panel  string
	ink    string
	cream  string
	muted  string
	teal   string
	gold   string
	coral  string
	sage   string
	ash    string

	panelBase  lipgloss.Style
	title      lipgloss.Style
	hint       lipgloss.Style
	body       lipgloss.Style
	footer     lipgloss.Style
	footerHelp lipgloss.Style
	footerMeta lipgloss.Style
}

type PanelSpec struct {
	Title   string
	Hint    string
	Body    string
	Width   int
	Height  int
	Focused bool
	Accent  string
}

func newTheme() uiTheme {
	panel := lipgloss.Color("#1C212B")

	return uiTheme{
		canvas: "#11161D",
		panel:  "#1C212B",
		ink:    "#11161D",
		cream:  "#E6EAF2",
		muted:  "#7F8794",
		teal:   "#61AFEF",
		gold:   "#E5C07B",
		coral:  "#D19A66",
		sage:   "#98C379",
		ash:    "#3B4252",
		panelBase: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			Background(panel).
			Padding(0, 1),
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E6EAF2")),
		hint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F8794")),
		body: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E6EAF2")),
		footer: lipgloss.NewStyle().
			Background(lipgloss.Color("#11161D")).
			Foreground(lipgloss.Color("#E6EAF2")),
		footerHelp: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#61AFEF")),
		footerMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7F8794")),
	}
}

var defaultTheme = newTheme()

func PanelHorizontalFrameSize() int {
	return defaultTheme.panelBase.GetHorizontalFrameSize()
}

func PanelVerticalFrameSize() int {
	return defaultTheme.panelBase.GetVerticalFrameSize()
}

func PanelInnerWidth(width int) int {
	return maxInt(1, width-PanelHorizontalFrameSize())
}

func PanelBodyHeight(height int) int {
	return maxInt(1, height-PanelVerticalFrameSize()-1)
}

func RenderPanel(title string, hint string, body string, width int, height int, focused bool, accent string) string {
	innerWidth := PanelInnerWidth(width)
	innerHeight := maxInt(1, height-PanelVerticalFrameSize())

	headerAccent := defaultTheme.ash
	if focused && strings.TrimSpace(accent) != "" {
		headerAccent = accent
	}

	titleStyle := defaultTheme.title.Copy().
		Foreground(lipgloss.Color(headerAccent))

	titleLabel := truncateText(strings.TrimSpace(title), innerWidth)
	titleText := titleStyle.Render(titleLabel)
	hintWidth := maxInt(0, innerWidth-lipgloss.Width(titleLabel)-1)
	hintLabel := truncateText(strings.TrimSpace(hint), hintWidth)
	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		titleText,
		" ",
		defaultTheme.hint.Copy().Width(hintWidth).MaxWidth(hintWidth).Align(lipgloss.Right).Render(hintLabel),
	)

	bodyHeight := maxInt(1, innerHeight-1)
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		defaultTheme.body.Copy().
			Width(innerWidth).
			Height(bodyHeight).
			Render(body),
	)

	panel := defaultTheme.panelBase.Copy().
		BorderForeground(lipgloss.Color(headerAccent)).
		Width(innerWidth).
		Height(innerHeight)

	return panel.Render(content)
}

func RenderPanelSpec(spec PanelSpec) string {
	return RenderPanel(
		spec.Title,
		spec.Hint,
		spec.Body,
		spec.Width,
		spec.Height,
		spec.Focused,
		spec.Accent,
	)
}

func RenderFooterLine(helpText string, statusText string, width int) string {
	helpText = strings.TrimSpace(helpText)
	statusText = strings.TrimSpace(statusText)
	if statusText == "" {
		statusText = "Ready"
	}

	rightWidth := minInt(maxInt(16, len([]rune(statusText))), maxInt(16, width-24))
	statusText = truncateText(statusText, rightWidth)
	helpWidth := maxInt(1, width-rightWidth-1)
	helpText = truncateText(helpText, helpWidth)

	left := defaultTheme.footerHelp.Render(helpText)
	right := defaultTheme.footerMeta.Render(statusText)
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		space = 1
	}

	return defaultTheme.footer.Copy().
		Width(maxInt(1, width)).
		Render(left + strings.Repeat(" ", space) + right)
}

func RenderHelp(width int, body string) string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF")).
		Bold(true).
		Render("NeoCode Help")

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C6370"))

	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("Use /help, Esc, or q to close this panel."))

	return lipgloss.NewStyle().MaxWidth(width).Render(b.String())
}

func RenderCanvas(width int, height int, body string) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(defaultTheme.canvas)).
		Foreground(lipgloss.Color(defaultTheme.cream)).
		Width(maxInt(1, width)).
		Height(maxInt(1, height)).
		Render(body)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateText(text string, width int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.TrimSpace(text)
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}
