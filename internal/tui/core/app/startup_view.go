package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const startupLogo = `
███╗   ██╗███████╗ ██████╗ ██████╗  ██████╗ ██████╗ ███████╗
████╗  ██║██╔════╝██╔═══██╗██╔═══╝ ██╔═══██╗██╔══██╗██╔════╝
██╔██╗ ██║█████╗  ██║   ██║██║     ██║   ██║██║  ██║█████╗
██║╚██╗██║██╔══╝  ██║   ██║██║     ██║   ██║██║  ██║██╔══╝
██║ ╚████║███████╗╚██████╔╝██████╗ ╚██████╔╝██████╔╝███████╗
╚═╝  ╚═══╝╚══════╝ ╚═════╝ ╚═════╝  ╚═════╝ ╚═════╝ ╚══════╝`

type startupQuickActionItem struct {
	Key   string
	Label string
}

var startupQuickActions = []startupQuickActionItem{
	{Key: "Ctrl+N", Label: "New Chat"},
	{Key: "/", Label: "Open Command Palette"},
	{Key: "Ctrl+L", Label: "Open Log Viewer"},
	{Key: "Ctrl+U", Label: "Exit NeoCode"},
}

const startupGoldenRatio = 1.61803398875

const (
	startupMinimalKeyBG = "#2a2538"
)

func (a App) shouldRenderStartupScreen() bool {
	if a.state.ActivePicker != pickerNone || a.logViewerVisible || a.pendingPermission != nil {
		return false
	}
	if a.state.IsAgentRunning || a.state.IsCompacting {
		return false
	}
	if !a.startupScreenLocked {
		return false
	}
	if len(a.activeMessages) > 0 {
		return false
	}
	return true
}

func (a App) renderStartupScreen(width int, height int) string {
	logoCanvasWidth := startupLogoCanvasWidth(startupLogo)
	maxWidth := max(44, width-6)
	panelWidth := min(maxWidth, startupPromptWidth(width))
	contentWidth := min(maxWidth, max(startupContentWidth(width), logoCanvasWidth+2))
	contentWidth = max(contentWidth, panelWidth)

	cardWidth := max(46, min(contentWidth, 72))
	keyWidth := 9
	labelWidth := max(20, cardWidth-keyWidth-4)

	logoStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(selectionFg)).
		Width(contentWidth).
		Align(lipgloss.Center).
		MarginBottom(1)
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText2)).
		Width(contentWidth).
		Align(lipgloss.Center).
		MarginBottom(1)
	stateStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(midGray)).
		Width(contentWidth).
		Align(lipgloss.Center)
	sectionTitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(oliveGray)).
		Width(contentWidth).
		Align(lipgloss.Center).
		MarginBottom(1)

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderDark)).
		Padding(0, 1)
	menuKeyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText)).
		UnsetBackground().
		Bold(true).
		Padding(0, 1).
		Align(lipgloss.Center).
		Width(keyWidth)
	menuLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText)).
		Width(labelWidth)

	logoContent := startupCenterPadLines(strings.Split(startupLogo, "\n"), logoCanvasWidth)

	menuLines := make([]string, 0, len(startupQuickActions))
	for _, item := range startupQuickActions {
		menuLines = append(menuLines, lipgloss.JoinHorizontal(
			lipgloss.Left,
			menuKeyStyle.Render(item.Key),
			"  ",
			menuLabelStyle.Render(item.Label),
		))
	}

	card := cardStyle.Width(cardWidth).Render(strings.Join(menuLines, "\n"))
	menuBlock := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(card)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		logoStyle.Render(logoContent),
		subtitleStyle.Render("AI-POWERED CLI WORKSPACE"),
		stateStyle.Render("Ready"),
		sectionTitleStyle.Render("Quick Actions"),
		menuBlock,
	)

	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		content,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func startupContentWidth(totalWidth int) int {
	maxWidth := max(44, totalWidth-6)
	target := int(math.Round(float64(totalWidth) * 0.62))
	return max(56, min(maxWidth, target))
}

func startupPromptWidth(totalWidth int) int {
	maxWidth := max(24, totalWidth-2)
	target := int(math.Round(float64(totalWidth) / startupGoldenRatio))
	return max(60, min(maxWidth, target))
}

func startupLogoCanvasWidth(logo string) int {
	lines := strings.Split(logo, "\n")
	maxWidth := 0
	for _, line := range lines {
		maxWidth = max(maxWidth, lipgloss.Width(line))
	}
	return maxWidth + 2
}

func startupCenterPadLines(lines []string, width int) string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, startupCenterPadLine(line, width))
	}
	return strings.Join(out, "\n")
}

func startupCenterPadLine(line string, width int) string {
	target := max(width, lipgloss.Width(line))
	w := lipgloss.Width(line)
	left := max(0, (target-w)/2)
	right := max(0, target-w-left)
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}
