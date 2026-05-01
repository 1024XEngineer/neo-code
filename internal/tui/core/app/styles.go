package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	purpleBg      = "#1a1625"
	purpleBg2     = "#251f35"
	purpleSurface = "#352d47"

	lightText  = "#e8e6f0"
	lightText2 = "#c8c6d8"
	midGray    = "#7a7890"

	purpleAccent = "#a78bfa"
	purpleLight  = "#c4b5fd"
	coralAccent  = "#f09070"
	neoBadge     = "#bd93f9"
	youBadge     = "#8be9fd"
	neoText      = "#f5f7fb"
	youText      = "#e5e7eb"
	selectionBg  = "#355070"
	selectionFg  = "#f7fafc"

	errorRed      = "#f87171"
	successGreen  = "#34d399"
	warningYellow = "#fbbf24"

	charcoal   = "#4a4560"
	oliveGray  = "#6b6588"
	stoneGray  = "#9089a8"
	warmSilver = "#a9a4b8"

	borderDark  = "#3d3654"
	borderLight = "#4a4268"

	startupBackgroundColor = "#000000"
	startupLogoBaseColor   = "#f3f4f6"
	startupMetaColor       = "#6b7280"
	startupHeaderColor     = "#4b5563"
	startupMenuActionColor = "#9ca3af"
	startupKeyCapBGColor   = "#1a1a1a"
	startupDividerColor    = "#1f2937"
	startupPromptColor     = "#bd93f9"
	startupFooterColor     = "#4b5563"
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
	startupRoot       lipgloss.Style
	startupHeader     lipgloss.Style
	startupBrand      lipgloss.Style
	startupHeaderMeta lipgloss.Style
	startupSeparator  lipgloss.Style
	startupLogo       lipgloss.Style
	startupSubtitle   lipgloss.Style
	startupMenu       lipgloss.Style
	startupMenuItem   lipgloss.Style
	startupKeyCap     lipgloss.Style
	startupMenuAction lipgloss.Style
	startupInput      lipgloss.Style
	startupDivider    lipgloss.Style
	startupPrompt     lipgloss.Style
	startupTyping     lipgloss.Style
	startupCursor     lipgloss.Style
	startupFooter     lipgloss.Style
}

func newStyles() styles {
	headerAccent := lipgloss.AdaptiveColor{Light: coralAccent, Dark: purpleLight}

	panel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(borderDark)).
		Padding(0, 1)

	return styles{
		doc: lipgloss.NewStyle().
			Padding(1, 2, 0, 2).
			UnsetBackground(),
		headerBar: lipgloss.NewStyle().
			UnsetBackground(),
		headerBrand: lipgloss.NewStyle().
			Bold(true).
			Foreground(headerAccent).
			UnsetBackground().
			Padding(0, 1),
		panel: panel,
		panelFocused: panel.Copy().
			BorderForeground(lipgloss.Color(purpleAccent)),
		panelTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(lightText)),
		panelSubtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		panelBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)),
		empty: lipgloss.NewStyle().
			Foreground(lipgloss.Color(midGray)).
			Padding(1, 0),
		sessionRow: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(lightText)),
		sessionRowActive: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(lightText)),
		sessionRowFocused: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color(lightText)).
			Bold(true),
		sessionMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		sessionMetaActive: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)),
		sessionMetaFocus: lipgloss.NewStyle().
			Foreground(lipgloss.Color(purpleAccent)),
		streamTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(lightText)),
		streamMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		streamContent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)),
		messageUserTag:  tagStyle(youBadge),
		messageAgentTag: tagStyle(neoBadge),
		messageBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(neoText)).
			MarginLeft(3),
		messageUserBody: lipgloss.NewStyle().
			Foreground(lipgloss.Color(youText)).
			MarginLeft(3),
		inlineNotice: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)).
			Italic(true),
		inlineError: lipgloss.NewStyle().
			Foreground(lipgloss.Color(errorRed)).
			Bold(true),
		inlineSystem: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		codeBlock: lipgloss.NewStyle().
			MarginLeft(1).
			Padding(1, 0).
			UnsetBackground().
			Border(lipgloss.NormalBorder()).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderLight)),
		codeText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(warmSilver)),
		codeCopyButton: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(coralAccent)).
			Underline(true),
		commandMenu: lipgloss.NewStyle().
			MarginTop(1),
		commandMenuTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(purpleAccent)),
		commandUsage: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)),
		commandUsageMatch: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(purpleAccent)),
		commandDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(oliveGray)),
		inputPrefix: lipgloss.NewStyle().
			Foreground(lipgloss.Color(purpleAccent)).
			Bold(true),
		inputLine: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)),
		inputBox: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(borderDark)).
			Padding(0, 1),
		inputBoxFocused: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(purpleAccent)).
			Padding(0, 1),
		footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText2)).
			BorderTop(true).
			BorderForeground(lipgloss.Color(borderDark)).
			Align(lipgloss.Center).
			PaddingTop(1),
		badgeUser:    badge("", purpleAccent),
		badgeAgent:   badge("", coralAccent),
		badgeSuccess: badge("", successGreen),
		badgeWarning: badge("", warningYellow),
		badgeError:   badge("", errorRed),
		badgeMuted:   badge("", stoneGray),
		startupRoot: lipgloss.NewStyle().
			UnsetBackground(),
		startupHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupHeaderColor)),
		startupBrand: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupHeaderColor)).
			Bold(true),
		startupHeaderMeta: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupHeaderColor)),
		startupSeparator: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupHeaderColor)),
		startupLogo: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupLogoBaseColor)).
			Bold(true),
		startupSubtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupMetaColor)).
			Align(lipgloss.Center),
		startupMenu:     lipgloss.NewStyle(),
		startupMenuItem: lipgloss.NewStyle(),
		startupKeyCap: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			UnsetBackground().
			Padding(0, 1),
		startupMenuAction: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupMenuActionColor)),
		startupInput: lipgloss.NewStyle(),
		startupDivider: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupDividerColor)),
		startupPrompt: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupPromptColor)).
			Bold(true),
		startupTyping: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupMetaColor)),
		startupCursor: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			UnsetBackground().
			Reverse(true),
		startupFooter: lipgloss.NewStyle().
			Foreground(lipgloss.Color(startupFooterColor)).
			Align(lipgloss.Center),
	}
}

func tagStyle(fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
		Padding(0, 0)
}

func badge(_ string, fg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(fg)).
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
