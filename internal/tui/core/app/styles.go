package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	parchment   = "#f5f4ed"
	ivory       = "#faf9f5"
	pureWhite   = "#ffffff"
	warmSand    = "#e8e6dc"
	darkSurface = "#30302e"
	deepDark    = "#141413"

	anthropicNearBlack = "#141413"
	terracotta         = "#c96442"
	coralAccent        = "#d97757"

	errorCrimson = "#b53333"
	focusBlue    = "#3898ec"

	charcoalWarm = "#4d4c48"
	oliveGray    = "#5e5d59"
	stoneGray    = "#87867f"
	darkWarm     = "#3d3d3a"
	warmSilver   = "#b0aea5"

	borderCream = "#f0eee6"
	borderWarm  = "#e8e6dc"
	borderDark  = "#30302e"
	ringWarm    = "#d1cfc5"
	ringSubtle  = "#dedc01"
	ringDeep    = "#c2c0b6"
)

type styles struct {
	doc               lipgloss.Style
	headerBar         lipgloss.Style
	headerBrand       lipgloss.Style
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
	messageBody       lipgloss.Style
	messageUserBody   lipgloss.Style
	inlineNotice      lipgloss.Style
	inlineError       lipgloss.Style
	inlineSystem      lipgloss.Style
	codeBlock         lipgloss.Style
	codeText          lipgloss.Style
	codeCopyButton    lipgloss.Style
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
		BorderForeground(lipgloss.Color(borderWarm)).
		UnsetBackground().
		Padding(0, 1)

	return styles{
		doc: lipgloss.NewStyle().
			Padding(1, 2, 0, 2).
			UnsetBackground().
			Foreground(lipgloss.Color(anthropicNearBlack)),
		headerBar: lipgloss.NewStyle().
			UnsetBackground().
			Foreground(lipgloss.Color(anthropicNearBlack)).
			Underline(true),
		headerBrand: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(terracotta)).
			UnsetBackground().
			Padding(0, 1),
		panel: panel,
		panelFocused: panel.Copy().
			BorderForeground(lipgloss.Color(terracotta)),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(anthropicNearBlack)),
		panelSubtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		panelBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground(),
		empty: lipgloss.NewStyle().
			Foreground(lipgloss.Color(stoneGray)).
			Padding(1, 0),
		sessionRow: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground(),
		sessionRowActive: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground(),
		sessionRowFocused: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground().
			Bold(true),
		sessionMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		sessionMetaActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)),
		sessionMetaFocus: lipgloss.NewStyle().
			Foreground(lipgloss.Color(terracotta)),
		streamTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(anthropicNearBlack)),
		streamMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		streamContent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground(),
		messageUserTag:  tagStyle(charcoalWarm),
		messageAgentTag: tagStyle(terracotta),
		messageBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderWarm)).
			Padding(0, 0),
		messageUserBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(terracotta)).
			Padding(0, 0),
		inlineNotice: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderWarm)).
			Padding(0, 0).
			Italic(true),
		inlineError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(errorCrimson)).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(errorCrimson)).
			Padding(0, 0).
			Bold(true),
		inlineSystem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderWarm)).
			Padding(0, 0),
		codeBlock: lipgloss.NewStyle().
			MarginLeft(1).
			Padding(0, 0).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderDark)),
		codeText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(warmSilver)),
		codeCopyButton: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(coralAccent)).
			Underline(true),
		commandMenu: lipgloss.NewStyle().
			UnsetBackground().
			Padding(1, 1),
		commandMenuTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(terracotta)),
		commandUsage: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)),
		commandUsageMatch: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(terracotta)),
		commandDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		inputPrefix: lipgloss.NewStyle().
			Foreground(lipgloss.Color(terracotta)).
			Bold(true),
		inputLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color(anthropicNearBlack)),
		inputBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderWarm)).
			UnsetBackground().
			Padding(0, 1),
		inputBoxFocused: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(focusBlue)).
			UnsetBackground().
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			UnsetBackground(),
		badgeUser:    badge("", charcoalWarm),
		badgeAgent:   badge("", coralAccent),
		badgeSuccess: badge("", oliveGray),
		badgeWarning: badge("", charcoalWarm),
		badgeError:   badge("", errorCrimson),
		badgeMuted:   badge("", stoneGray),
	}
}

func tagStyle(fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		UnsetBackground().
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
