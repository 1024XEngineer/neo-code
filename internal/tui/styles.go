package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	colorPrimary = "#CBA6F7"
	colorUser    = "#89B4FA"
	colorBorder  = "#45475A"
	colorError   = "#F38BA8"
	colorSuccess = "#A6E3A1"
	colorText    = "#CDD6F4"
	colorSubtle  = "#7F849C"
	colorBg      = "#11111B"
	colorPanel   = "#181825"
	colorCode    = "#1E1E2E"
	colorInk     = "#11111B"
	colorWarning = "#F9E2AF"
)

type styles struct {
	doc               lipgloss.Style
	headerBrand       lipgloss.Style
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
	inputMeta         lipgloss.Style
	inputLine         lipgloss.Style
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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorBorder)).
		Background(lipgloss.Color(colorPanel)).
		Padding(0, 1)

	return styles{
		doc: lipgloss.NewStyle().
			Padding(1, 2).
			Background(lipgloss.Color(colorBg)).
			Foreground(lipgloss.Color(colorText)),
		headerBrand: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorInk)).
			Background(lipgloss.Color(colorPrimary)).
			Padding(0, 1),
		headerSub: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		headerMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		headerSpacer: lipgloss.NewStyle().
			Width(2),
		panel: panel,
		panelFocused: panel.Copy().
			BorderForeground(lipgloss.Color(colorPrimary)),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorText)),
		panelSubtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		panelBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		empty: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			Padding(1, 0),
		sessionRow: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorText)),
		sessionRowActive: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorText)).
			Background(lipgloss.Color(colorCode)),
		sessionRowFocused: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(colorInk)).
			Background(lipgloss.Color(colorPrimary)).
			Bold(true),
		sessionMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		streamTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorText)),
		streamMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		streamContent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		messageUserTag:  tagStyle(colorUser, colorInk),
		messageAgentTag: tagStyle(colorPrimary, colorInk),
		messageToolTag:  tagStyle(colorSuccess, colorInk),
		messageBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			PaddingLeft(1),
		messageUserBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)).
			PaddingLeft(1),
		messageToolBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSuccess)).
			PaddingLeft(1),
		inlineNotice: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)).
			Italic(true),
		inlineError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorError)).
			Bold(true),
		inlineSystem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		codeBlock: lipgloss.NewStyle().
			MarginLeft(1).
			Padding(0, 1).
			Background(lipgloss.Color(colorCode)).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorBorder)),
		codeText: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#BAC2DE")),
		commandMenu: lipgloss.NewStyle().
			Background(lipgloss.Color(colorCode)).
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
		inputMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSubtle)),
		inputLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorText)),
		footer: lipgloss.NewStyle().
			PaddingTop(1).
			Foreground(lipgloss.Color(colorSubtle)),
		badgeUser:    badge(colorUser, colorInk),
		badgeAgent:   badge(colorPrimary, colorInk),
		badgeSuccess: badge(colorSuccess, colorInk),
		badgeWarning: badge(colorWarning, colorInk),
		badgeError:   badge(colorError, colorInk),
		badgeMuted:   badge(colorBorder, colorText),
	}
}

func tagStyle(bg string, fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
		Padding(0, 1)
}

func badge(bg string, fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(fg)).
		Background(lipgloss.Color(bg)).
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
