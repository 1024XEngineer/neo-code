package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
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
		Render(RenderStatusBar(m.activeModel, m.memoryStats.TotalItems, m.generating, m.width))

	padding := availableHeight - countLines(content)
	if padding > 0 {
		content += lipgloss.NewStyle().
			Height(padding).
			Render("")
	}

	inputArea := lipgloss.NewStyle().
		Height(inputHeight).
		Width(m.width).
		Render(RenderInput(m.inputBuffer, m.waitingCode, m.codeDelim, m.codeLines, m.width))

	return statusBar + content + inputArea
}

func (m Model) chatView() string {
	return RenderMessages(m.messages, m.width)
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

func RenderMessages(messages []Message, width int) string {
	if len(messages) == 0 {
		return ""
	}

	var b strings.Builder

	visibleMessages := messages
	startIdx := 0
	if len(messages) > 50 {
		startIdx = len(messages) - 50
		visibleMessages = messages[startIdx:]
	}

	for _, msg := range visibleMessages {
		idx := startIdx
		switch msg.Role {
		case "user":
			b.WriteString(userMsgStyle.Render(fmt.Sprintf("[%d] 你:", idx)))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")

		case "assistant":
			b.WriteString(assistantMsgStyle.Render(fmt.Sprintf("[%d] Neo:", idx)))
			b.WriteString("\n")
			b.WriteString(renderContent(msg.Content))
			b.WriteString("\n\n")

		case "system":
			b.WriteString(systemMsgStyle.Render("[系统]"))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}

		startIdx++
	}

	return b.String()
}

func renderContent(content string) string {
	if content == "" {
		return "..."
	}

	lines := strings.Split(content, "\n")
	var b strings.Builder

	inCodeBlock := false
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				b.WriteString(codeBlockStyle.Render("\n" + line + "\n"))
			} else {
				inCodeBlock = false
				b.WriteString(codeBlockStyle.Render(line + "\n"))
			}
			continue
		}

		if inCodeBlock {
			b.WriteString(codeBlockStyle.Render(line))
			b.WriteString("\n")
		} else {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

func RenderInput(buffer string, waitingCode bool, codeDelim string, codeLines []string, width int) string {
	var b strings.Builder

	if waitingCode {
		b.WriteString(helpStyle.Render(fmt.Sprintf("┌─ 代码输入 (%s ... %s) ─┐", codeDelim, codeDelim)))
		b.WriteString("\n")

		for i, line := range codeLines {
			b.WriteString(fmt.Sprintf("│ %2d │ %s\n", i+1, line))
		}

		b.WriteString("│    │ " + lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF")).Render(buffer))
		b.WriteString("\n")
		b.WriteString("└─ 双 Enter 发送 · Ctrl+C 取消 ─┘")
	} else {
		lines := strings.Split(buffer, "\n")
		hasMultipleLines := len(lines) > 1 || (len(lines) == 1 && lines[0] != "")

		if hasMultipleLines {
			b.WriteString(helpStyle.Render("┌─ 多行输入 ─┐"))
			b.WriteString("\n")
			for i, line := range lines {
				if i == len(lines)-1 {
					b.WriteString(fmt.Sprintf("│ %2d │ %s█\n", i+1, line))
				} else {
					b.WriteString(fmt.Sprintf("│ %2d │ %s\n", i+1, line))
				}
			}
			b.WriteString("└─ Enter 换行 · F5 发送 ─┘")
		} else {
			prompt := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#61AFEF")).
				Bold(true).Render("› ")

			b.WriteString(prompt)
			b.WriteString(buffer)
			b.WriteString("█")
		}
	}

	return b.String()
}

func RenderStatusBar(model string, memoryItems int, generating bool, width int) string {
	var b strings.Builder

	modelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Background(lipgloss.Color("#282C34")).
		Padding(0, 1)

	memStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C678DD")).
		Background(lipgloss.Color("#282C34")).
		Padding(0, 1)

	status := "●"
	if generating {
		status = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5C07B")).
			Render("◐")
	}

	timeStr := time.Now().Format("15:04")

	b.WriteString(modelStyle.Render(model))
	b.WriteString("  ")
	b.WriteString(memStyle.Render(fmt.Sprintf("记忆: %d", memoryItems)))
	b.WriteString("  ")
	b.WriteString(status)

	space := width - len(model) - len(fmt.Sprintf("记忆: %d", memoryItems)) - len(timeStr) - 10
	if space > 0 {
		b.WriteString(strings.Repeat(" ", space))
	}

	b.WriteString(timestampStyle.Render(timeStr))

	return b.String()
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
		{"/switch <model>", "切换模型"},
		{"/models", "列出可用模型"},
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

	for _, c := range commands {
		b.WriteString(cmdStyle.Render(c.cmd))
		b.WriteString(descStyle.Render(c.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("多行输入: Enter 换行，F5 发送"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("命令: /help 查看所有命令"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("取消: Ctrl+C"))

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("按 Esc 或 /help 关闭"))

	return b.String()
}
