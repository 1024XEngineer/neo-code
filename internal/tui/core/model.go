package core

import (
	"context"
	"strings"
	"sync"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/components"
	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	ui   state.UIState
	chat state.ChatState

	client  services.ChatClient
	persona string

	streamChan      <-chan services.ChatEvent
	textarea        textarea.Model
	viewport        viewport.Model
	chatLayout      components.RenderedChatLayout
	copyToClipboard func(string) error

	mu *sync.Mutex

	// Todo 相关状态
	todos      []services.Todo
	todoCursor int
}

const resumeSummaryPrefix = "[RESUME_SUMMARY]"

// NewModel 创建 TUI 状态模型。
// historyTurns 用于限制发送给后端的短期对话轮数，避免原始消息无限增长。
func NewModel(client services.ChatClient, persona string, historyTurns int, configPath, workspaceRoot string) Model {
	stats, _ := client.GetMemoryStats(context.Background())
	if stats == nil {
		stats = &services.MemoryStats{}
	}
	if historyTurns <= 0 {
		historyTurns = 6
	}

	input := textarea.New()
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF"))
	blurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#5C6370"))
	focusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EAF2"))
	blurredStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAB2C0"))
	input.FocusedStyle = focusedStyle
	input.BlurredStyle = blurredStyle
	input.Placeholder = "Type a message..."
	input.Focus()
	input.ShowLineNumbers = false
	input.SetHeight(3)
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline.SetEnabled(true)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF"))
	input.Cursor.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EAF2"))
	_ = input.Cursor.SetMode(cursor.CursorBlink)

	vp := viewport.New(0, 0)
	vp.SetContent("")

	model := Model{
		ui: state.UIState{
			Mode:       state.ModeChat,
			Focused:    "input",
			AutoScroll: true,
		},
		chat: state.ChatState{
			Messages:       make([]state.Message, 0),
			HistoryTurns:   historyTurns,
			ActiveModel:    client.DefaultModel(),
			MemoryStats:    *stats,
			CommandHistory: make([]string, 0),
			CmdHistIndex:   -1,
			WorkspaceRoot:  workspaceRoot,
			APIKeyReady:    configs.RuntimeAPIKey() != "",
			ConfigPath:     configPath,
		},
		client:          client,
		persona:         persona,
		textarea:        input,
		viewport:        vp,
		copyToClipboard: clipboard.WriteAll,
		mu:              &sync.Mutex{},
	}
	if provider, ok := client.(services.WorkingSessionSummaryProvider); ok {
		if summary, err := provider.GetWorkingSessionSummary(context.Background()); err == nil && strings.TrimSpace(summary) != "" {
			model.chat.Messages = append(model.chat.Messages, state.Message{
				Role:      "system",
				Content:   resumeSummaryPrefix + "\n" + summary,
				Timestamp: time.Now(),
			})
		}
	}
	return model
}

func (m *Model) mutex() *sync.Mutex {
	if m.mu == nil {
		m.mu = &sync.Mutex{}
	}
	return m.mu
}

// Init 返回 Bubble Tea 的初始命令。
func (m Model) Init() tea.Cmd {
	return m.textarea.Focus()
}

// SetWidth 更新当前视口宽度。
func (m *Model) SetWidth(w int) {
	m.ui.Width = w
}

// SetHeight 更新当前视口高度。
func (m *Model) SetHeight(h int) {
	m.ui.Height = h
}

// AddMessage 向聊天历史追加一条带时间戳的消息。
func (m *Model) AddMessage(role, content string) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	m.chat.Messages = append(m.chat.Messages, state.Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

func (m *Model) AddToolCallMessage(calls []services.ChatToolCall) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	m.chat.Messages = append(m.chat.Messages, state.Message{
		Role:      "assistant",
		ToolCalls: calls,
		Timestamp: time.Now(),
	})
}

func (m *Model) AddToolMessage(callID, content string) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	m.chat.Messages = append(m.chat.Messages, state.Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: callID,
		Timestamp:  time.Now(),
	})
}

// AppendLastMessage 将流式内容追加到最后一条消息中。
func (m *Model) AppendLastMessage(content string) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	if len(m.chat.Messages) > 0 {
		m.chat.Messages[len(m.chat.Messages)-1].Content += content
	}
}

// FinishLastMessage 将最后一条消息标记为结束流式输出。
func (m *Model) FinishLastMessage() {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	if len(m.chat.Messages) > 0 {
		m.chat.Messages[len(m.chat.Messages)-1].Streaming = false
	}
}

// TrimHistory 在保留系统消息的同时裁剪最近的非系统对话轮次。
func (m *Model) TrimHistory(maxTurns int) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	if len(m.chat.Messages) <= maxTurns*2 {
		return
	}

	var system []state.Message
	var others []state.Message

	for _, msg := range m.chat.Messages {
		if msg.Role == "system" {
			system = append(system, msg)
		} else {
			others = append(others, msg)
		}
	}

	if len(others) > maxTurns*2 {
		others = others[len(others)-maxTurns*2:]
	}

	m.chat.Messages = append(system, others...)
}

func isResumeSummaryMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), resumeSummaryPrefix)
}
