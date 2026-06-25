package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

// PaletteItem 描述命令面板中的一个可选项。
type PaletteItem struct {
	Name        string
	Description string
}

// PaletteCommandMsg 表示用户选择了某个命令面板项。
type PaletteCommandMsg struct {
	Name string
}

var defaultPaletteItems = []PaletteItem{
	{Name: "/model", Description: "Change the current model"},
	{Name: "/mode", Description: "Switch between build and plan"},
	{Name: "/session", Description: "Browse and switch sessions"},
	{Name: "/compact", Description: "Compact current session"},
	{Name: "/checkpoint", Description: "Manage checkpoints"},
	{Name: "/skills", Description: "Manage session skills"},
	{Name: "/help", Description: "Show keyboard shortcuts"},
	{Name: "/exit", Description: "Quit NeoCode"},
}

// Palette 是 Telescope 风格的命令面板组件。
type Palette struct {
	state *state.ViewState
	items []PaletteItem
}

var _ tea.Model = (*Palette)(nil)

// NewPalette 创建命令面板组件。
func NewPalette(viewState *state.ViewState) *Palette {
	return &Palette{
		state: viewState,
		items: defaultPaletteItems,
	}
}

// Init 不启动额外命令。
func (p *Palette) Init() tea.Cmd {
	return nil
}

// Update 处理命令面板内的键盘、鼠标和导航。
func (p *Palette) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return p, p.handleKey(msg)
	case tea.MouseMsg:
		return p, p.handleMouse(msg)
	}
	return p, nil
}

func (p *Palette) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		p.state.Overlay.Active = state.OverlayNone
		p.state.Overlay.Query = ""
		p.state.Overlay.Selected = 0
		return nil
	case "enter", " ":
		matched := p.matchedItems()
		if len(matched) == 0 {
			return nil
		}
		idx := p.state.Overlay.Selected
		if idx >= len(matched) {
			idx = len(matched) - 1
		}
		selected := matched[idx]
		p.state.Overlay.Active = state.OverlayNone
		p.state.Overlay.Query = ""
		p.state.Overlay.Selected = 0
		return func() tea.Msg {
			return PaletteCommandMsg{Name: selected.Name}
		}
	case "up", "k":
		if p.state.Overlay.Selected > 0 {
			p.state.Overlay.Selected--
		}
		return nil
	case "down", "j":
		matched := p.matchedItems()
		if p.state.Overlay.Selected < len(matched)-1 {
			p.state.Overlay.Selected++
		}
		return nil
	case "backspace":
		if len(p.state.Overlay.Query) > 0 {
			p.state.Overlay.Query = p.state.Overlay.Query[:len(p.state.Overlay.Query)-1]
			p.state.Overlay.Selected = 0
		}
		return nil
	default:
		runes := msg.Runes
		if len(runes) > 0 && runes[0] >= 32 {
			p.state.Overlay.Query += string(runes)
			p.state.Overlay.Selected = 0
		}
		return nil
	}
}

// matchedItems 按确定性的优先级匹配命令：先精确名、再前缀、最后子串。
// 不使用模糊匹配，避免 "mode" 因为评分排序命中 /model 而不是 /mode。
func (p *Palette) matchedItems() []PaletteItem {
	query := strings.ToLower(strings.TrimPrefix(p.state.Overlay.Query, "/"))
	if query == "" {
		return p.items
	}
	var exact, prefix, substr []PaletteItem
	for _, item := range p.items {
		name := strings.ToLower(strings.TrimPrefix(item.Name, "/"))
		desc := strings.ToLower(item.Description)
		switch {
		case name == query:
			exact = append(exact, item)
		case strings.HasPrefix(name, query):
			prefix = append(prefix, item)
		case strings.Contains(name, query) || strings.Contains(desc, query):
			substr = append(substr, item)
		}
	}
	return append(append(exact, prefix...), substr...)
}

// handleMouse 处理鼠标滚轮和点击事件。
func (p *Palette) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if p.state.Overlay.Selected > 0 {
			p.state.Overlay.Selected--
		}
		return nil
	case tea.MouseButtonWheelDown:
		matched := p.matchedItems()
		if p.state.Overlay.Selected < len(matched)-1 {
			p.state.Overlay.Selected++
		}
		return nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		// query + 空行 = 2 行头部
		itemIdx := msg.Y - 2
		matched := p.matchedItems()
		if itemIdx >= 0 && itemIdx < len(matched) {
			selected := matched[itemIdx]
			p.state.Overlay.Active = state.OverlayNone
			p.state.Overlay.Query = ""
			p.state.Overlay.Selected = 0
			return func() tea.Msg {
				return PaletteCommandMsg{Name: selected.Name}
			}
		}
		return nil
	}
	return nil
}

// View 渲染 Telescope 风格的命令面板。
func (p *Palette) View() string {
	width := p.state.Layout.Width
	height := p.state.Layout.Height
	if width <= 0 {
		width = 60
	}
	if height <= 0 {
		height = 24
	}

	matched := p.matchedItems()
	boxW := min(width-4, 60)
	boxH := height - 4
	if boxH < 8 {
		boxH = 8
	}
	maxItems := boxH - 5 // title + query + hint + padding
	if maxItems < 1 {
		maxItems = 1
	}

	var lines []string

	// 搜索输入行
	queryLine := "> " + p.state.Overlay.Query
	queryLine = theme.AccentStyle().Render(queryLine)
	lines = append(lines, queryLine, "")

	// 选项列表
	for i, item := range matched {
		if i >= maxItems {
			break
		}
		prefix := "  "
		name := item.Name
		desc := theme.MutedStyle().Render(item.Description)

		if i == p.state.Overlay.Selected {
			prefix = theme.AccentStyle().Render("▎ ")
			name = theme.AccentStyle().Bold(true).Render(name)
		}
		line := prefix + name + "  " + desc
		if displayW := theme.DisplayWidth(line); displayW > boxW-2 {
			line = theme.Truncate(line, boxW-2)
		}
		lines = append(lines, line)
	}

	if len(matched) == 0 {
		lines = append(lines, theme.MutedStyle().Render("  No matches found"))
	}

	// 底部提示行
	hint := "  ⏎ / ␣ : execute   ␛ : dismiss"
	lines = append(lines, "", theme.MutedStyle().Render(hint))

	// 边框容器
	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Width(boxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(content)

	// 居中
	outerH := max(boxH, 10)
	return lipgloss.NewStyle().
		Width(width).
		Height(outerH).
		Align(lipgloss.Center, lipgloss.Center).
		Render(box)
}
