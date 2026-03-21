package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/tui/infra"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		m.SetHeight(msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case StreamChunkMsg:
		if m.generating {
			m.AppendLastMessage(msg.Content)
		}
		return m, nil

	case StreamDoneMsg:
		m.generating = false
		m.FinishLastMessage()
		return m, nil

	case StreamErrorMsg:
		m.generating = false
		m.AddMessage("assistant", fmt.Sprintf("错误: %v", msg.Err))
		return m, nil

	case ShowHelpMsg:
		m.mode = ModeHelp
		return m, nil

	case HideHelpMsg:
		m.mode = ModeChat
		return m, nil

	case RefreshMemoryMsg:
		stats, err := m.client.GetMemoryStats(context.Background())
		if err == nil && stats != nil {
			m.memoryStats = *stats
		}
		return m, nil

	case ExitMsg:
		return m, tea.Quit
	}

	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {

	case tea.KeyCtrlC:
		if m.waitingCode {
			m.waitingCode = false
			m.codeLines = nil
			m.codeDelim = ""
		}
		return *m, nil

	case tea.KeyCtrlD:
		if m.waitingCode {
			return *m, m.submitCode()
		}
		return *m, nil

	case tea.KeyEnter:
		m.lastKeyWasEnter = true
		return m.handleNewline()

	case tea.KeyF5:
		return m.handleSubmit()

	case tea.KeyUp:
		if len(m.commandHistory) > 0 {
			if m.cmdHistIndex < len(m.commandHistory)-1 {
				m.cmdHistIndex++
			}
			if m.cmdHistIndex >= 0 && m.cmdHistIndex < len(m.commandHistory) {
				m.inputBuffer = m.commandHistory[len(m.commandHistory)-1-m.cmdHistIndex]
			}
		}
		return *m, nil

	case tea.KeyDown:
		if m.cmdHistIndex > 0 {
			m.cmdHistIndex--
			m.inputBuffer = m.commandHistory[len(m.commandHistory)-1-m.cmdHistIndex]
		} else {
			m.cmdHistIndex = -1
			m.inputBuffer = ""
		}
		return *m, nil

	case tea.KeyRunes:
		if m.lastKeyWasEnter {
			m.lastKeyWasEnter = false
			runes := msg.Runes
			if len(runes) == 1 && runes[0] == 27 {
				m.lastKeyWasEnter = false
				return m.handleSubmit()
			}
		}
		r := string(msg.Runes)
		if msg.Type == tea.KeySpace && m.inputBuffer == "" {
			return *m, nil
		}
		m.inputBuffer += r
		m.lastKeyWasEnter = false
		m.cmdHistIndex = -1
		return *m, nil

	case tea.KeyBackspace:
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
		return *m, nil

	case tea.KeyEsc:
		if m.mode == ModeHelp {
			m.mode = ModeChat
		}
		return *m, nil
	}

	return *m, nil
}

func (m *Model) handleNewline() (tea.Model, tea.Cmd) {
	m.inputBuffer += "\n"
	return *m, nil
}

func (m *Model) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.inputBuffer)
	m.inputBuffer = ""

	if input == "" && !m.waitingCode {
		return *m, nil
	}

	switch m.mode {
	case ModeHelp:
		m.mode = ModeChat
		return *m, nil
	}

	if m.waitingCode {
		if isEndDelimiter(input, m.codeDelim) {
			return *m, m.submitCode()
		}
		m.codeLines = append(m.codeLines, input)
		return *m, nil
	}

	if isStartDelimiter(input) {
		m.waitingCode = true
		m.codeDelim = getDelimiter(input)
		m.codeLines = nil
		return *m, nil
	}

	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}

	m.AddMessage("user", input)
	m.AddMessage("assistant", "")
	m.generating = true

	m.commandHistory = append(m.commandHistory, input)
	m.cmdHistIndex = -1

	messages := m.buildMessages()
	return *m, m.streamResponse(messages)
}

func (m *Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return *m, nil
	}

	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/help":
		m.mode = ModeHelp
	case "/exit", "/quit", "/q":
		return *m, tea.Quit
	case "/switch":
		if len(args) == 0 {
			m.AddMessage("assistant", "用法: /switch <model>")
			return *m, nil
		}
		target := args[0]
		if !containsModel(m.client.ListModels(), target) {
			m.AddMessage("assistant", fmt.Sprintf("模型不可用: %s", target))
			return *m, nil
		}
		m.activeModel = target
		m.AddMessage("assistant", fmt.Sprintf("已切换到模型: %s", target))
	case "/models":
		models := m.client.ListModels()
		list := strings.Join(models, "\n  - ")
		m.AddMessage("assistant", fmt.Sprintf("可用模型:\n  - %s", list))
	case "/memory":
		stats, err := m.client.GetMemoryStats(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("读取记忆统计失败: %v", err))
			return *m, nil
		}
		m.memoryStats = *stats
		m.AddMessage("assistant", fmt.Sprintf(
			"记忆统计:\n  长期: %d\n  会话: %d\n  总计: %d\n  TopK: %d\n  最小分数: %.2f\n  文件: %s\n  类型: %s",
			stats.PersistentItems, stats.SessionItems, stats.TotalItems, stats.TopK, stats.MinScore, stats.Path, formatTypeStats(stats.ByType),
		))
	case "/clear-memory":
		if len(args) == 0 || args[0] != "confirm" {
			m.AddMessage("assistant", "此命令会清空长期记忆。请使用 /clear-memory confirm")
			return *m, nil
		}
		if err := m.client.ClearMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("清空长期记忆失败: %v", err))
			return *m, nil
		}
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.memoryStats = *stats
		}
		m.AddMessage("assistant", "已清空本地长期记忆")
	case "/clear-context":
		if err := m.client.ClearSessionMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("清空会话记忆失败: %v", err))
			return *m, nil
		}
		m.messages = nil
		if m.persona != "" {
			m.messages = append(m.messages, Message{
				Role:    "system",
				Content: m.persona,
			})
		}
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.memoryStats = *stats
		}
		m.AddMessage("assistant", "已清空当前会话上下文")
	case "/run":
		if len(args) > 0 {
			code := strings.Join(args, " ")
			return *m, tea.Batch(
				tea.Printf("\n--- 运行代码 ---\n"),
				runCodeCmd(code),
			)
		}
	case "/explain":
		if len(args) > 0 {
			code := strings.Join(args, " ")
			return *m, m.explainCode(code)
		}
		return *m, nil
	default:
		m.AddMessage("assistant", fmt.Sprintf("未知命令: %s，输入 /help 查看帮助", cmd))
	}

	return *m, nil
}

func containsModel(models []string, target string) bool {
	for _, model := range models {
		if model == target {
			return true
		}
	}
	return false
}

func formatTypeStats(byType map[string]int) string {
	if len(byType) == 0 {
		return "无"
	}
	ordered := []string{
		domain.TypeUserPreference,
		domain.TypeProjectRule,
		domain.TypeCodeFact,
		domain.TypeFixRecipe,
		domain.TypeSessionMemory,
	}
	parts := make([]string, 0, len(byType))
	for _, key := range ordered {
		if count := byType[key]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, count))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, ", ")
}

func (m *Model) buildMessages() []infra.Message {
	result := make([]infra.Message, 0, len(m.messages))

	for _, msg := range m.messages {
		if msg.Role == "system" {
			result = append(result, infra.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	for _, msg := range m.messages {
		if msg.Role != "system" {
			result = append(result, infra.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return result
}

func (m *Model) streamResponse(messages []infra.Message) tea.Cmd {
	return func() tea.Msg {
		stream, err := m.client.Chat(context.Background(), messages, m.activeModel)
		if err != nil {
			return StreamErrorMsg{Err: err}
		}

		for chunk := range stream {
			m.AppendLastMessage(chunk)
		}

		return StreamDoneMsg{}
	}
}

func (m *Model) submitCode() tea.Cmd {
	m.waitingCode = false
	code := strings.Join(m.codeLines, "\n")
	m.codeLines = nil
	m.codeDelim = ""

	m.AddMessage("user", fmt.Sprintf("```\n%s\n```", code))
	m.AddMessage("assistant", "")
	m.generating = true

	return tea.Batch(
		Chunk(""),
		m.sendCodeToAI(code),
	)
}

func (m *Model) sendCodeToAI(code string) tea.Cmd {
	prompt := fmt.Sprintf("请解释以下代码：\n```\n%s\n```", code)
	m.AddMessage("user", prompt)
	m.AddMessage("assistant", "")
	m.generating = true

	messages := m.buildMessages()
	return m.streamResponse(messages)
}

func (m *Model) explainCode(code string) tea.Cmd {
	m.AddMessage("user", fmt.Sprintf("请解释以下代码：\n```\n%s\n```", code))
	m.AddMessage("assistant", "")
	m.generating = true

	messages := m.buildMessages()
	return m.streamResponse(messages)
}

func isStartDelimiter(s string) bool {
	s = strings.TrimSpace(s)
	return s == "'''" || s == `"""` || s == "```"
}

func isEndDelimiter(line, delim string) bool {
	return strings.TrimSpace(line) == delim
}

func getDelimiter(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 3 {
		return s[:3]
	}
	return s
}

func runCodeCmd(code string) tea.Cmd {
	return func() tea.Msg {
		ext, runner := detectLanguage(code)
		if ext == "" {
			return StreamErrorMsg{Err: fmt.Errorf("无法识别代码语言")}
		}

		tmpFile, err := os.CreateTemp("", "neocode-*."+ext)
		if err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("创建临时文件失败: %w", err)}
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(code); err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("写入临时文件失败: %w", err)}
		}
		tmpFile.Close()

		var cmd *exec.Cmd
		if runner != "" {
			cmd = exec.Command(runner, tmpFile.Name())
		} else {
			cmd = exec.Command("go", "run", tmpFile.Name())
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return StreamErrorMsg{Err: err}
		}

		return StreamDoneMsg{}
	}
}

func detectLanguage(code string) (string, string) {
	code = strings.TrimSpace(code)

	if strings.HasPrefix(code, "#!/bin/bash") || strings.HasPrefix(code, "#!/bin/sh") {
		return "sh", "bash"
	}
	if strings.HasPrefix(code, "package main") || strings.Contains(code, "func main()") {
		return "go", ""
	}
	if strings.HasPrefix(code, "def ") || strings.HasPrefix(code, "class ") {
		return "py", "python"
	}
	if strings.HasPrefix(code, "fn ") || strings.HasPrefix(code, "impl ") {
		return "rs", "rustc"
	}
	if strings.HasPrefix(code, "console.log") || strings.Contains(code, "=>") {
		return "js", "node"
	}

	return "", ""
}
