package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View 渲染当前 TUI 界面。
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
		Render(RenderInput(m.inputBuffer, m.waitingCode, m.codeDelim, m.codeLines, m.width, m.multilineMode, m.cursorLine, m.cursorCol))

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

// RenderMessages 渲染当前可见的聊天消息列表。
func RenderMessages(messages []Message, width int) string {
	if len(messages) == 0 {
		return ""
	}

	var b strings.Builder

	visibleMessages := messages
	if len(messages) > 30 {
		visibleMessages = messages[len(messages)-30:]
	}

	for i, msg := range visibleMessages {
		idx := len(messages) - len(visibleMessages) + i + 1
		switch msg.Role {
		case "user":
			b.WriteString(userMsgStyle.Render(fmt.Sprintf("你 [%d]:", idx)))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")

		case "assistant":
			b.WriteString(assistantMsgStyle.Render(fmt.Sprintf("Neo [%d]:", idx)))
			b.WriteString("\n")
			b.WriteString(renderContent(msg.Content))
			b.WriteString("\n\n")

		//系统消息不用展示给用户，直接推送到ai,如果需要可以复用
		case "system":
			b.WriteString(systemMsgStyle.Render("[系统]"))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}
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
	codeLang := ""
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimPrefix(line, "```")
				codeLang = strings.TrimSpace(codeLang)
				if codeLang == "" {
					codeLang = "go"
				}
				codeLines = []string{}
				b.WriteString("\n")
			} else {
				inCodeBlock = false
				highlighted := HighlightCodeBlock(codeLines, codeLang)
				b.WriteString(highlighted)
				b.WriteString(codeBlockStyle.Render("```\n"))
				codeLines = nil
			}
			continue
		}

		if inCodeBlock {
			codeLines = append(codeLines, line)
		} else {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

// HighlightCodeBlock 渲染带语法高亮的代码块。
func HighlightCodeBlock(lines []string, lang string) string {
	var b strings.Builder
	code := strings.Join(lines, "\n")

	b.WriteString(codeBlockStyle.Render("```" + lang + "\n"))

	highlighted := HighlightCode(code, lang)
	highlightedLines := strings.Split(highlighted, "\n")
	for _, line := range highlightedLines {
		b.WriteString(codeBlockStyle.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}

// RenderInput 渲染聊天和代码输入区域。
func RenderInput(buffer string, waitingCode bool, codeDelim string, codeLines []string, width int, multilineMode bool, cursorLine int, cursorCol int) string {
	var b strings.Builder

	if waitingCode {
		b.WriteString(helpStyle.Render(fmt.Sprintf("[代码输入] %s ... %s", codeDelim, codeDelim)))
		b.WriteString("\n")

		for i, line := range codeLines {
			highlighted := HighlightCodeInline(line, detectLang(buffer))
			b.WriteString(fmt.Sprintf("  %2d: %s\n", i+1, highlighted))
		}

		b.WriteString("  > " + lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF")).Render(buffer))
		b.WriteString("\n[F5发送 Ctrl+C取消]")
	} else {
		// 清理 \r 并将 tab 转换为 4 个空格以确保显示正确
		cleanBuffer := strings.ReplaceAll(buffer, "\r", "")
		cleanBuffer = strings.ReplaceAll(cleanBuffer, "\t", "    ")
		lines := strings.Split(cleanBuffer, "\n")
		hasMultipleLines := len(lines) > 1 || (len(lines) == 1 && lines[0] != "")

		if hasMultipleLines || multilineMode {
			lang := detectLang(buffer)
			_ = "[多行输入]"
			for i, line := range lines {
				lineNum := fmt.Sprintf("  %2d: ", i+1)
				b.WriteString(lineNum)

				runes := []rune(line)
				if multilineMode && i == cursorLine && cursorCol <= len(runes) {
					cursorStyle := lipgloss.NewStyle().
						Background(lipgloss.Color("#3E4451")).
						Foreground(lipgloss.Color("#ABB2BF"))

					var before, after string
					var char string
					if cursorCol < len(runes) {
						char = string(runes[cursorCol])
						before = string(runes[:cursorCol])
						after = string(runes[cursorCol+1:])
					} else {
						before = string(runes)
						char = " "
					}

					if before != "" {
						b.WriteString(HighlightCodeInline(before, lang))
					}
					b.WriteString(cursorStyle.Render(char))
					if after != "" {
						b.WriteString(HighlightCodeInline(after, lang))
					}
				} else {
					if line != "" {
						b.WriteString(HighlightCodeInline(line, lang))
					}
				}
				b.WriteString("\n")
			}
			if multilineMode {
				b.WriteString("[方向键移动 Enter换行 F5/F8发送 Del删除]")
			} else {
				b.WriteString("[Enter换行 F5/F8发送]")
			}
		} else {
			prompt := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#61AFEF")).
				Bold(true).Render("> ")

			b.WriteString(prompt)
			b.WriteString(buffer)
		}
	}

	return b.String()
}

// RenderStatusBar 渲染模型、记忆和状态指示信息。
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

// RenderHelp 渲染内置命令帮助视图。
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
	b.WriteString(helpStyle.Render("多行输入: Enter进入, 方向键移动, F5发送"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("命令: /help"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("取消: Ctrl+C"))

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("按 Esc 或 /help 关闭"))

	return b.String()
}
