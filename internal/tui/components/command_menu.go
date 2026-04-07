package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CommandMenuData 描述命令建议菜单渲染所需的基础数据与样式。
type CommandMenuData struct {
	Title          string
	Body           string
	Width          int
	ContainerStyle lipgloss.Style
	TitleStyle     lipgloss.Style
}

// RenderCommandMenu 负责将命令菜单数据渲染为最终字符串。
func RenderCommandMenu(data CommandMenuData) string {
	if strings.TrimSpace(data.Body) == "" {
		return ""
	}

	return data.ContainerStyle.Width(data.Width).Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			data.TitleStyle.Render(data.Title),
			data.Body,
		),
	)
}
