package core

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go-llm-demo/internal/tui/components"
)

func (m Model) View() string {
	if m.width < 20 || m.height < 5 {
		return "窗口太小"
	}

	var content string
	switch m.mode {
	case ModeHelp:
		content = RenderHelp(m.width)
	default:
		content = m.chatView()
	}

	statusHeight := 1
	inputHeight := 2
	helpHeight := 0

	if m.mode == ModeHelp {
		helpHeight = 20
	}

	availableHeight := m.height - statusHeight - inputHeight - helpHeight
	if availableHeight < 5 {
		availableHeight = 5
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

	padding := availableHeight - countLines(content)
	if padding > 0 {
		content += lipgloss.NewStyle().
			Height(padding).
			Render("")
	}

	inputArea := lipgloss.NewStyle().
		Height(inputHeight).
		Width(m.width).
		Render(components.Input{
			Buffer:     m.inputBuffer,
			Multiline:  m.multilineMode,
			CursorLine: m.cursorLine,
			CursorCol:  m.cursorCol,
		}.Render())

	return statusBar + content + inputArea
}

func (m Model) chatView() string {
	messages := make([]components.Message, len(m.messages))
	for i, msg := range m.messages {
		messages[i] = components.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			Streaming: msg.Streaming,
		}
	}
	return components.MessageList{Messages: messages, Width: m.width}.Render()
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := 1
	for _, c := range s {
		if c == '\n' {
			count++
		}
	}
	return count
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
	b.WriteString(helpStyle.Render("多行输入: Enter进入, 方向键移动, F5发送"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("命令: /help"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("取消: Ctrl+C"))

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("按 Esc 或 /help 关闭"))

	return b.String()
}
