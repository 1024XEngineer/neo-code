package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	{Key: "Ctrl+N", Label: "New Chat            "},
	{Key: "     /", Label: "Open Command Palette"},
	{Key: "Ctrl+F", Label: "Full Access Mode    "},
	{Key: "Ctrl+L", Label: "Open Log Viewer     "},
	{Key: "Ctrl+U", Label: "Exit NeoCode        "},
}

const startupLayoutFullMinWidth = 73
const startupLayoutFullMinHeight = 30

func (a App) shouldRenderStartupScreen() bool {
	if a.state.ActivePicker != pickerNone || a.logViewerVisible || a.pendingPermission != nil || a.pendingUserQuestion != nil || a.pendingFullAccessPrompt != nil {
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
	if width <= 0 || height <= 0 {
		return ""
	}

	logoCanvasWidth := startupLogoCanvasWidth(startupLogo)
	maxWidth := max(1, width-4)
	panelWidth := min(maxWidth, startupPromptWidth(width))
	contentWidth := min(maxWidth, startupContentWidth(width))
	contentWidth = max(contentWidth, panelWidth)

	cardOuterWidth := min(contentWidth, 72)
	compactMenu := cardOuterWidth < 46
	cardInnerWidth := cardOuterWidth

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

	menuKeyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText)).
		Bold(true)

	menuLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText))

	menuLineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(lightText)).
		Width(contentWidth).
		Align(lipgloss.Center)

	logoContent := startupCenterPadLines(strings.Split(startupLogo, "\n"), logoCanvasWidth)
	if contentWidth < logoCanvasWidth {
		logoContent = startupCenterPadLine("NeoCode", contentWidth)
	}

	renderMenu := func() string {
		menuLines := make([]string, 0, len(startupQuickActions))
		for _, item := range startupQuickActions {
			if compactMenu {
				compactLine := item.Key + "  " + item.Label
				menuLines = append(menuLines, menuLineStyle.Render(compactStatusText(compactLine, max(8, cardInnerWidth))))
				continue
			}
			line := lipgloss.JoinHorizontal(
				lipgloss.Left,
				menuKeyStyle.Render(item.Key),
				"  ",
				menuLabelStyle.Render(item.Label),
			)
			menuLines = append(menuLines, menuLineStyle.Render(line))
		}
		return lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Center).
			Render(strings.Join(menuLines, "\n"))
	}

	showHints := width >= startupLayoutFullMinWidth && height >= startupLayoutFullMinHeight
	showLogo := true
	buildContent := func(includeLogo bool, includeHints bool) string {
		parts := make([]string, 0, 5)
		if includeLogo {
			parts = append(parts, logoStyle.Render(logoContent))
		}
		parts = append(parts, subtitleStyle.Render("AI-POWERED CLI WORKSPACE"))
		parts = append(parts, stateStyle.Render("Ready"))
		if includeHints {
			parts = append(parts, sectionTitleStyle.Render("Quick Actions"))
			parts = append(parts, renderMenu())
		}
		return lipgloss.JoinVertical(lipgloss.Center, parts...)
	}

	content := buildContent(showLogo, showHints)
	topOffset := startupTopOffset(height, showHints)

	// Prefer shrinking top offset first before removing startup sections.
	if usedHeight := lipgloss.Height(content) + topOffset; usedHeight > height {
		topOffset = max(0, height-lipgloss.Height(content))
	}
	if showHints && lipgloss.Height(content)+topOffset > height {
		showHints = false
		content = buildContent(showLogo, showHints)
		topOffset = startupTopOffset(height, showHints)
		if usedHeight := lipgloss.Height(content) + topOffset; usedHeight > height {
			topOffset = max(0, height-lipgloss.Height(content))
		}
	}
	if showLogo && lipgloss.Height(content)+topOffset > height {
		showLogo = false
		content = buildContent(showLogo, showHints)
		topOffset = startupTopOffset(height, showHints)
		if usedHeight := lipgloss.Height(content) + topOffset; usedHeight > height {
			topOffset = max(0, height-lipgloss.Height(content))
		}
	}

	paddedContent := lipgloss.NewStyle().
		MarginTop(topOffset).
		Render(content)
	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Top,
		paddedContent,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func startupTopOffset(height int, includeHints bool) int {
	if height <= 0 {
		return 0
	}
	if includeHints {
		return max(1, min(4, height/10))
	}
	if height >= 24 {
		return 2
	}
	return 1
}

func startupContentWidth(totalWidth int) int {
	maxWidth := max(1, totalWidth-4)
	if totalWidth <= 120 {
		return maxWidth
	}
	target := int(math.Round(float64(totalWidth) * 0.74))
	return max(40, min(maxWidth, target))
}

func startupPromptWidth(totalWidth int) int {
	maxWidth := max(1, totalWidth-2)
	if totalWidth <= 128 {
		return maxWidth
	}
	target := int(math.Round(float64(totalWidth) * 0.78))
	return max(72, min(maxWidth, target))
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
	if width > 0 && lipgloss.Width(line) > width {
		line = ansi.Cut(line, 0, width)
	}
	target := max(width, lipgloss.Width(line))
	w := lipgloss.Width(line)
	left := max(0, (target-w)/2)
	right := max(0, target-w-left)
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}

func startupQuickActionKeyWidth(cardWidth int) int {
	if cardWidth <= 0 {
		return 0
	}
	longest := 0
	for _, item := range startupQuickActions {
		longest = max(longest, lipgloss.Width(item.Key))
	}
	maxAllowed := max(4, cardWidth-12)
	return max(4, min(longest, maxAllowed))
}
