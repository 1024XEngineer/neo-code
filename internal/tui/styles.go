package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	colorPrimary  = "#7AA2F7"
	colorUser     = "#4FD6BE"
	colorBorder   = "#2B3342"
	colorError    = "#F7768E"
	colorSuccess  = "#73DACA"
	colorText     = "#DCE3F0"
	colorSubtle   = "#7E8AA3"
	colorBg       = "#090C12"
	colorPanel    = "#11161F"
	colorPanelAlt = "#151C27"
	colorCode     = "#0E131B"
	colorInk      = "#081018"
	colorWarning  = "#E0AF68"
)

type styles struct {
	doc               lipgloss.Style
	headerBar         lipgloss.Style
	headerBrand       lipgloss.Style
	headerLabel       lipgloss.Style
	headerPath        lipgloss.Style
	headerSub         lipgloss.Style
	headerMeta        lipgloss.Style
	headerSpacer      lipgloss.Style
	panel             lipgloss.Style
	panelFocused      lipgloss.Style
	panelTitle        lipgloss.Style
	panelSubtitle     lipgloss.Style
	panelBody         lipgloss.Style
	empty             lipgloss.Style
	sessionRow        lipgloss.Style
	sessionRowActive  lipgloss.Style
	sessionRowFocused lipgloss.Style
	sessionMeta       lipgloss.Style
	sessionMetaActive lipgloss.Style
	sessionMetaFocus  lipgloss.Style
	streamTitle       lipgloss.Style
	streamMeta        lipgloss.Style
	streamContent     lipgloss.Style
	messageUserTag    lipgloss.Style
	messageAgentTag   lipgloss.Style
	messageToolTag    lipgloss.Style
	messageBody       lipgloss.Style
	messageUserBody   lipgloss.Style
	messageToolBody   lipgloss.Style
	inlineNotice      lipgloss.Style
	inlineError       lipgloss.Style
	inlineSystem      lipgloss.Style
	codeBlock         lipgloss.Style
	codeText          lipgloss.Style
	commandMenu       lipgloss.Style
	commandMenuTitle  lipgloss.Style
	commandUsage      lipgloss.Style
	commandUsageMatch lipgloss.Style
	commandDesc       lipgloss.Style
	inputPrefix       lipgloss.Style
	inputLine         lipgloss.Style
	inputBox          lipgloss.Style
	inputBoxFocused   lipgloss.Style
	footer            lipgloss.Style
	badgeUser         lipgloss.Style
	badgeAgent        lipgloss.Style
	badgeSuccess      lipgloss.Style
	badgeWarning      lipgloss.Style
	badgeError        lipgloss.Style
	badgeMuted        lipgloss.Style
}

func newStyles() styles {
	panel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		UnsetBackground().
		Padding(0, 1)

	return styles{
		doc: lipgloss.NewStyle().
			Padding(1, 2, 0, 2).
			UnsetBackground().
			Foreground(lipgloss.Color(colorText)),
		headerBar: lipgloss.NewStyle().
			UnsetBackground().
			Foreground(lipgloss.Color(colorText)),
		headerBrand: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPrimary)).
			UnsetBackground().
			Padding(0, 1),
		headerLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		headerPath: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		headerSub: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		headerMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		headerSpacer: lipgloss.NewStyle().
			Width(1).
			UnsetBackground(),
		panel: panel,
		panelFocused: panel.Copy().
			BorderForeground(lipgloss.Color(colorPrimary)),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorText)),
		panelSubtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		panelBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			UnsetBackground(),
		empty: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			Padding(1, 0),
		sessionRow: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorText)).
			UnsetBackground(),
		sessionRowActive: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorText)).
			UnsetBackground(),
		sessionRowFocused: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorText)).
			UnsetBackground().
			Bold(true),
		sessionMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		sessionMetaActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A0B8")),
		sessionMetaFocus: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AFC3FF")),
		streamTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorText)),
		streamMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		streamContent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			UnsetBackground(),
		messageUserTag:  tagStyle(colorUser),
		messageAgentTag: tagStyle(colorPrimary),
		messageToolTag:  tagStyle(colorSuccess),
		messageBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			Background(lipgloss.Color(colorPanel)).
			Padding(0, 1),
		messageUserBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			Background(lipgloss.Color("#12202D")).
			Padding(0, 1),
		messageToolBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSuccess)).
			Background(lipgloss.Color("#0F1B1C")).
			Padding(0, 1),
		inlineNotice: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			Background(lipgloss.Color(colorPanel)).
			Padding(0, 1).
			Italic(true),
		inlineError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorError)).
			Background(lipgloss.Color("#21131A")).
			Padding(0, 1).
			Bold(true),
		inlineSystem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			Background(lipgloss.Color(colorPanel)).
			Padding(0, 1),
		codeBlock: lipgloss.NewStyle().
			MarginLeft(1).
			Padding(0, 1).
			Background(lipgloss.Color(colorCode)).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorPrimary)),
		codeText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		commandMenu: lipgloss.NewStyle().
			UnsetBackground().
			Padding(1, 1),
		commandMenuTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPrimary)),
		commandUsage: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		commandUsageMatch: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPrimary)),
		commandDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		inputPrefix: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorUser)).
			Bold(true),
		inputLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		inputBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)).
			UnsetBackground().
			Padding(0, 1),
		inputBoxFocused: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorPrimary)).
			UnsetBackground().
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			UnsetBackground(),
		badgeUser:    badge("#12202D", colorUser),
		badgeAgent:   badge("#16233A", colorPrimary),
		badgeSuccess: badge("#102018", colorSuccess),
		badgeWarning: badge("#241B10", colorWarning),
		badgeError:   badge("#26131A", colorError),
		badgeMuted:   badge(colorPanelAlt, colorSubtle),
	}
}

func tagStyle(fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(colorBg)).
		Padding(0, 1)
}

func badge(bg string, fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		UnsetBackground().
		Padding(0, 1)
}

func wrapPlain(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return strings.Join(out, "\n")
}

func wrapCodeBlock(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		expanded := strings.ReplaceAll(line, "\t", "    ")
		runes := []rune(expanded)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return strings.Join(out, "\n")
}

func trimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 4 {
		return text
	}
	return string(runes[:limit-3]) + "..."
}

func trimMiddle(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 7 {
		return text
	}
	left := (limit - 3) / 2
	right := limit - 3 - left
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func preview(text string, width int, lines int) string {
	rawLines := strings.Split(strings.TrimSpace(text), "\n")
	out := make([]string, 0, lines)
	for _, line := range rawLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, wrapPlain(line, width))
		if len(out) >= lines {
			break
		}
	}
	if len(out) == 0 {
		return "(empty)"
	}
	joined := strings.Join(out, "\n")
	runes := []rune(joined)
	if len(runes) > width*lines {
		return string(runes[:width*lines-3]) + "..."
	}
	return joined
}

func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
