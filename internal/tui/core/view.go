package core

import (
	"strings"

	"go-llm-demo/internal/tui/components"
	"go-llm-demo/internal/tui/state"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.ui.Width < 20 || m.ui.Height < 6 {
		return "Window too small"
	}

	statusHeight := 1
	helpHeight := 0
	if m.ui.Mode == state.ModeHelp {
		helpHeight = minInt(20, m.ui.Height-statusHeight-3)
	}

	inputContent := m.renderInputArea()
	inputHeight := countLines(inputContent)
	if inputHeight < 4 {
		inputHeight = 4
	}

	contentHeight := m.ui.Height - statusHeight - inputHeight - helpHeight
	if contentHeight < 3 {
		contentHeight = 3
	}

	statusBar := lipgloss.NewStyle().
		Height(statusHeight).
		Width(m.ui.Width).
		Render(components.StatusBar{
			Model:      m.chat.ActiveModel,
			MemoryCnt:  m.chat.MemoryStats.TotalItems,
			Generating: m.chat.Generating,
			Width:      m.ui.Width,
		}.Render())

	viewportView := m.viewport
	viewportView.SetContent(m.renderChatContent())
	chatArea := lipgloss.NewStyle().
		Width(m.ui.Width).
		Height(contentHeight).
		Render(viewportView.View())

	inputArea := lipgloss.NewStyle().
		Width(m.ui.Width).
		Render(inputContent)

	if m.ui.Mode == state.ModeHelp {
		help := lipgloss.NewStyle().
			Width(m.ui.Width).
			Height(helpHeight).
			Render(components.RenderHelp(m.ui.Width))
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
	return components.InputBox{
		Body:       m.textarea.View(),
		Generating: m.chat.Generating,
		Status:     m.ui.CopyStatus,
	}.Render()
}

func (m *Model) renderChatContent() string {
	if m.ui.Mode == state.ModeTodo {
		m.chatLayout = components.RenderedChatLayout{}
		return components.TodoList{
			Todos:   m.todos,
			Cursor:  m.todoCursor,
			Width:   m.viewport.Width,
			Focused: true,
		}.Render()
	}
	layout := components.MessageList{Messages: m.toComponentMessages(), Width: m.viewport.Width}.RenderLayout()
	m.chatLayout = layout
	return layout.Content
}

func (m Model) toComponentMessages() []components.Message {
	messages := make([]components.Message, 0, len(m.chat.Messages))
	for _, msg := range m.chat.Messages {
		if msg.Role == "tool" {
			continue
		}
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) > 0 {
			continue
		}
		messages = append(messages, components.Message{
			Role:      msg.Role,
			Content:   displayMessageContent(msg.Role, msg.Content),
			Timestamp: msg.Timestamp,
			Streaming: msg.Streaming,
		})
	}
	return messages
}

func displayMessageContent(role, content string) string {
	if role == "system" && isResumeSummaryMessage(content) {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(content), resumeSummaryPrefix))
	}
	return content
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
