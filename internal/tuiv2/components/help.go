package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/tuiv2/keymap"
	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

// HelpOverlay 是分组快捷键帮助浮层组件。
type HelpOverlay struct {
	state *state.ViewState
}

var _ tea.Model = (*HelpOverlay)(nil)

// NewHelpOverlay 创建帮助浮层组件。
func NewHelpOverlay(viewState *state.ViewState) *HelpOverlay {
	return &HelpOverlay{state: viewState}
}

// Init 不启动额外命令。
func (h *HelpOverlay) Init() tea.Cmd {
	return nil
}

// Update 处理帮助浮层内的键盘输入。
func (h *HelpOverlay) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}
	switch key.String() {
	case "esc", "ctrl+c", "q", "?":
		h.state.Overlay.Active = state.OverlayNone
		return h, nil
	}
	return h, nil
}

// View 渲染分组快捷键帮助浮层。
func (h *HelpOverlay) View() string {
	width := h.state.Layout.Width
	height := h.state.Layout.Height
	if width <= 0 {
		width = 60
	}
	if height <= 0 {
		height = 24
	}

	boxW := min(width-4, 56)
	groups := keymap.FullHelp()

	var lines []string
	lines = append(lines, theme.AccentStyle().Render("  Keyboard Shortcuts"))
	lines = append(lines, "")

	for _, group := range groups {
		lines = append(lines, theme.ToolNameStyle().Render("  "+group.Title))
		for _, entry := range group.Entries {
			keyText := theme.AccentStyle().Render(entry.Key)
			descText := theme.MutedStyle().Render(entry.Desc)
			line := "    " + padRight(keyText, 24) + " " + descText
			if dw := theme.DisplayWidth(line); dw > boxW-4 {
				line = theme.Truncate(line, boxW-4)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	// 约束内容高度到终端限制
	maxContentLines := height - 5 // border + padding overhead
	if maxContentLines < 4 {
		maxContentLines = 4
	}
	if len(lines) > maxContentLines {
		lines = lines[:maxContentLines]
	}

	hint := theme.MutedStyle().Render("  ␛ : close")
	lines = append(lines, hint)

	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Width(boxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(content)

	boxH := height - 2
	if boxH < 6 {
		boxH = 6
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(boxH).
		Align(lipgloss.Center, lipgloss.Center).
		Render(box)
}

// padRight 将文本补齐到指定显示宽度。
func padRight(text string, width int) string {
	dw := theme.DisplayWidth(text)
	if dw >= width {
		return text
	}
	return text + strings.Repeat(" ", width-dw)
}
