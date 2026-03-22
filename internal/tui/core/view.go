package core

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go-llm-demo/internal/tui/components"
)

func (m Model) View() string {
	if m.width < 20 || m.height < 6 {
		return "窗口太小"
	}

	statusHeight := 1
	helpHeight := 0
	if m.mode == ModeHelp {
		helpHeight = minInt(20, m.height-statusHeight-3)
	}

	inputContent := m.renderInputArea()
	inputHeight := countLines(inputContent)
	if inputHeight < 4 {
		inputHeight = 4
	}

	contentHeight := m.height - statusHeight - inputHeight - helpHeight
	if contentHeight < 3 {
		contentHeight = 3
	}

	statusBar := lipgloss.NewStyle().
		Height(statusHeight).
		Width(m.width).
		Render(components.StatusBar{
			Model:      m.activeModel,
			MemoryCnt:  m.memoryStats.TotalItems,
			Generating: m.generating,
			Width:      m.width,
		}.Render())

	viewportView := m.viewport
	viewportView.SetContent(m.renderChatContent())
	chatArea := lipgloss.NewStyle().
		Width(m.width).
		Height(contentHeight).
		Render(viewportView.View())

	inputArea := lipgloss.NewStyle().
		Width(m.width).
		Render(inputContent)

	if m.mode == ModeHelp {
		help := lipgloss.NewStyle().
			Width(m.width).
			Height(helpHeight).
			Render(RenderHelp(m.width))
		return lipgloss.JoinVertical(lipgloss.Left, statusBar, chatArea, help, inputArea)
	}

	return lipgloss.JoinVertical(lipgloss.Left, statusBar, chatArea, inputArea)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func (m Model) renderInputArea() string {
	helpText := "[Enter换行 F5/F8发送 PgUp/PgDn滚动]"
	if !m.generating {
		helpText = "[Enter换行 F5/F8发送 Ctrl+V粘贴 PgUp/PgDn滚动]"
	}

	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C6370")).
		Render(helpText)

	return m.textarea.View() + "\n" + footer
}

func (m Model) renderChatContent() string {
	return components.MessageList{Messages: m.toComponentMessages(), Width: m.viewport.Width}.Render()
}

func (m Model) toComponentMessages() []components.Message {
	messages := make([]components.Message, len(m.messages))
	for i, msg := range m.messages {
		messages[i] = components.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			Streaming: msg.Streaming,
		}
	}
	return messages
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func RenderHelp(width int) string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF")).
		Bold(true).
		Render("NeoCode 帮助")

	b.WriteString(title)
	b.WriteString("\n\n")

	commands := []struct {
		cmd  string
		desc string
	}{
		{"/help", "显示帮助"},
		{"/apikey <env_name>", "切换 API Key 变量名"},
		{"/provider <name>", "切换模型提供商"},
		{"/switch <model>", "切换模型"},
		{"/models", "查看当前提供商模型列表"},
		{"/run <code>", "执行代码"},
		{"/explain <code>", "解释代码"},
		{"/memory", "显示记忆统计"},
		{"/clear-memory confirm", "清空长期记忆"},
		{"/clear-context", "清空会话上下文"},
		{"/exit", "退出程序"},
	}

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Width(22)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ABB2BF"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C6370"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF"))

	for _, c := range commands {
		b.WriteString(cmdStyle.Render(c.cmd))
		b.WriteString(descStyle.Render(c.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("输入框支持光标、粘贴、滚动，F5/F8 发送"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("聊天区支持 PgUp/PgDn 和鼠标滚轮"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("取消: Ctrl+C"))

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("按 Esc 或 /help 关闭"))

	return lipgloss.NewStyle().MaxWidth(width).Render(b.String())
}
