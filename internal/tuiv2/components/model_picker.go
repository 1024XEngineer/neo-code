package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"

	"github.com/sahilm/fuzzy"
)

// ModelSelectMsg 表示用户选择了某个模型。
type ModelSelectMsg struct {
	ModelID   string
	ModelName string
}

// ModelPicker 是 Telescope 风格的模型选择器组件。
type ModelPicker struct {
	state *state.ViewState
}

var _ tea.Model = (*ModelPicker)(nil)

// NewModelPicker 创建模型选择器组件。
func NewModelPicker(viewState *state.ViewState) *ModelPicker {
	return &ModelPicker{state: viewState}
}

// Init 不启动额外命令。
func (m *ModelPicker) Init() tea.Cmd {
	return nil
}

// Update 处理模型选择器内的键盘和鼠标输入。
func (m *ModelPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	}
	return m, nil
}

func (m *ModelPicker) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.state.Overlay.Active = state.OverlayNone
		m.state.Overlay.Query = ""
		m.state.Overlay.Selected = 0
		return nil
	case "enter", " ":
		matched := m.matchedModels()
		if len(matched) == 0 {
			return nil
		}
		idx := m.state.Overlay.Selected
		if idx >= len(matched) {
			idx = len(matched) - 1
		}
		selected := matched[idx]
		m.state.Overlay.Active = state.OverlayNone
		m.state.Overlay.Query = ""
		m.state.Overlay.Selected = 0
		return func() tea.Msg {
			return ModelSelectMsg{ModelID: selected.ID, ModelName: selected.Name}
		}
	case "up", "k":
		if m.state.Overlay.Selected > 0 {
			m.state.Overlay.Selected--
		}
		return nil
	case "down", "j":
		matched := m.matchedModels()
		if m.state.Overlay.Selected < len(matched)-1 {
			m.state.Overlay.Selected++
		}
		return nil
	case "backspace":
		if len(m.state.Overlay.Query) > 0 {
			m.state.Overlay.Query = m.state.Overlay.Query[:len(m.state.Overlay.Query)-1]
			m.state.Overlay.Selected = 0
		}
		return nil
	default:
		runes := msg.Runes
		if len(runes) > 0 && runes[0] >= 32 {
			m.state.Overlay.Query += string(runes)
			m.state.Overlay.Selected = 0
		}
		return nil
	}
}

func (m *ModelPicker) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.state.Overlay.Selected > 0 {
			m.state.Overlay.Selected--
		}
		return nil
	case tea.MouseButtonWheelDown:
		matched := m.matchedModels()
		if m.state.Overlay.Selected < len(matched)-1 {
			m.state.Overlay.Selected++
		}
		return nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		// 标题 + 空行 + query + 空行 = 4 行头部
		itemIdx := msg.Y - 4
		matched := m.matchedModels()
		if itemIdx >= 0 && itemIdx < len(matched) {
			selected := matched[itemIdx]
			m.state.Overlay.Active = state.OverlayNone
			m.state.Overlay.Query = ""
			m.state.Overlay.Selected = 0
			return func() tea.Msg {
				return ModelSelectMsg{ModelID: selected.ID, ModelName: selected.Name}
			}
		}
		return nil
	}
	return nil
}

// matchedModels 根据当前查询模糊匹配模型列表。
func (m *ModelPicker) matchedModels() []modelEntry {
	models := m.state.Gateway.Models
	query := strings.ToLower(m.state.Overlay.Query)
	if query == "" {
		result := make([]modelEntry, len(models))
		for i, mod := range models {
			result[i] = modelEntry{ID: mod.ID, Name: mod.Name, Provider: mod.Provider, Current: mod.Current}
		}
		return result
	}
	targets := make([]string, len(models))
	for i, mod := range models {
		targets[i] = strings.ToLower(mod.Name) + " " + strings.ToLower(mod.ID) + " " + strings.ToLower(mod.Provider)
	}
	matches := fuzzy.Find(query, targets)
	result := make([]modelEntry, 0, len(matches))
	for _, match := range matches {
		mod := models[match.Index]
		result = append(result, modelEntry{ID: mod.ID, Name: mod.Name, Provider: mod.Provider, Current: mod.Current})
	}
	return result
}

type modelEntry struct {
	ID       string
	Name     string
	Provider string
	Current  bool
}

// View 渲染 Telescope 风格的模型选择器浮层。
func (m *ModelPicker) View() string {
	width := m.state.Layout.Width
	height := m.state.Layout.Height
	if width <= 0 {
		width = 60
	}
	if height <= 0 {
		height = 24
	}

	matched := m.matchedModels()
	boxW := min(width-4, 56)
	boxH := height - 4
	if boxH < 8 {
		boxH = 8
	}
	maxItems := boxH - 5 // title + query + hint + padding
	if maxItems < 1 {
		maxItems = 1
	}

	var lines []string

	// 标题
	lines = append(lines, theme.AccentStyle().Render("  Models"))
	lines = append(lines, "")

	// 搜索输入
	queryLine := "> " + m.state.Overlay.Query
	lines = append(lines, theme.AccentStyle().Render(queryLine), "")

	// 模型列表
	for i, mod := range matched {
		if i >= maxItems {
			break
		}
		prefix := "  "
		name := mod.Name
		detail := theme.MutedStyle().Render(mod.Provider)
		if mod.Current {
			detail = theme.SuccessStyle().Render("● current")
		}

		if i == m.state.Overlay.Selected {
			prefix = theme.AccentStyle().Render("▎ ")
			name = theme.AccentStyle().Bold(true).Render(name)
		}
		line := prefix + name + "  " + detail
		if dw := theme.DisplayWidth(line); dw > boxW-4 {
			line = theme.Truncate(line, boxW-4)
		}
		lines = append(lines, line)
	}

	if len(matched) == 0 {
		lines = append(lines, theme.MutedStyle().Render("  No models found"))
	}

	// 提示行
	hint := "  ⏎ / ␣ : select   ␛ : cancel"
	lines = append(lines, "", theme.MutedStyle().Render(hint))

	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Width(boxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(content)

	outerH := max(boxH, 10)
	return lipgloss.NewStyle().
		Width(width).
		Height(outerH).
		Align(lipgloss.Center, lipgloss.Center).
		Render(box)
}
