package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CommandMenuRowData 描述命令建议菜单单行渲染所需数据。
type CommandMenuRowData struct {
	Title            string
	Description      string
	Highlight        bool
	Selected         bool
	Width            int
	UsageStyle       lipgloss.Style
	UsageMatchStyle  lipgloss.Style
	DescriptionStyle lipgloss.Style
}

// RenderCommandMenuRow 渲染命令建议菜单中的单行内容。
func RenderCommandMenuRow(data CommandMenuRowData) string {
	prefix := "  "
	if data.Selected {
		prefix = "> "
	}
	contentWidth := max(12, data.Width-lipgloss.Width(prefix))
	usageStyle := data.UsageStyle
	if data.Selected {
		usageStyle = data.UsageMatchStyle
	}
	descriptionStyle := data.DescriptionStyle
	if data.Selected {
		descriptionStyle = data.UsageMatchStyle
	}

	line := usageStyle.Render(data.Title)
	if description := strings.TrimSpace(data.Description); description != "" {
		descWidth := max(8, contentWidth-lipgloss.Width(data.Title)-2)
		line = lipgloss.JoinHorizontal(
			lipgloss.Top,
			line,
			lipgloss.NewStyle().Width(2).Render(""),
			descriptionStyle.Render(trimMiddle(description, descWidth)),
		)
	}

	return prefix + lipgloss.NewStyle().Width(contentWidth).Render(line)
}

// SessionRowData 描述会话列表单行渲染所需数据。
type SessionRowData struct {
	Title           string
	UpdatedAtLabel  string
	Active          bool
	Selected        bool
	Width           int
	RowStyle        lipgloss.Style
	RowActiveStyle  lipgloss.Style
	RowFocusStyle   lipgloss.Style
	MetaStyle       lipgloss.Style
	MetaActiveStyle lipgloss.Style
	MetaFocusStyle  lipgloss.Style
}

// RenderSessionRow 渲染会话列表单行内容。
func RenderSessionRow(data SessionRowData) string {
	width := max(18, data.Width-2)
	title := trimRunes(data.Title, max(8, width-10))

	prefix := "o"
	if data.Active {
		prefix = "*"
	}
	if data.Selected {
		prefix = ">"
	}

	style := data.RowStyle
	metaStyle := data.MetaStyle
	if data.Active {
		style = data.RowActiveStyle
		metaStyle = data.MetaActiveStyle
	}
	if data.Selected {
		style = data.RowFocusStyle
		metaStyle = data.MetaFocusStyle
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		fmt.Sprintf("%s %s", prefix, title),
		metaStyle.Render("  "+data.UpdatedAtLabel),
	)

	return style.Width(width).Render(content)
}

// trimRunes 在不破坏 rune 边界的前提下截断文本。
