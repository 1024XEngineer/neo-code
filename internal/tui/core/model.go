package core

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go-llm-demo/internal/tui/infra"
)

type Mode int

const (
	ModeChat Mode = iota
	ModeCodeInput
	ModeHelp
	ModeMemory
)

type Model struct {
	width   int
	height  int
	mode    Mode
	focused string

	messages     []Message
	historyTurns int

	codeLines   []string
	codeDelim   string
	waitingCode bool

	generating  bool
	activeModel string

	memoryStats infra.MemoryStats

	commandHistory []string
	cmdHistIndex   int
	inputBuffer    string

	client          infra.ChatClient
	persona         string
	lastKeyWasEnter bool

	cursorLine    int
	cursorCol     int
	multilineMode bool
}

type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
	Streaming bool
}

var (
	accentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#61AFEF")).
			Bold(true)

	userMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#98C379")).
			Bold(true)

	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5C07B"))

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C678DD"))

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5C6370"))

	codeBlockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ABB2BF")).
			Background(lipgloss.Color("#282C34")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#61AFEF"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5C6370"))
)

// NewModel 创建 TUI 状态模型。
// historyTurns 用于限制发送给后端的短期对话轮数，避免原始消息无限增长。
func NewModel(client infra.ChatClient, persona string, historyTurns int) Model {
	stats, _ := client.GetMemoryStats(context.Background())
	if stats == nil {
		stats = &infra.MemoryStats{}
	}
	if historyTurns <= 0 {
		historyTurns = 6
	}

	return Model{
		mode:           ModeChat,
		focused:        "input",
		messages:       make([]Message, 0),
		historyTurns:   historyTurns,
		activeModel:    client.DefaultModel(),
		memoryStats:    *stats,
		commandHistory: make([]string, 0),
		cmdHistIndex:   -1,
		client:         client,
		persona:        persona,
	}
}

// Init 返回 Bubble Tea 的初始命令。
func (m Model) Init() tea.Cmd {
	return nil
}

// SetWidth 更新当前视口宽度。
func (m *Model) SetWidth(w int) {
	m.width = w
}

// SetHeight 更新当前视口高度。
func (m *Model) SetHeight(h int) {
	m.height = h
}

// AddMessage 向聊天历史追加一条带时间戳的消息。
func (m *Model) AddMessage(role, content string) {
	m.messages = append(m.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// AppendLastMessage 将流式内容追加到最后一条消息中。
func (m *Model) AppendLastMessage(content string) {
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].Content += content
	}
}

// FinishLastMessage 将最后一条消息标记为结束流式输出。
func (m *Model) FinishLastMessage() {
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].Streaming = false
	}
}

// TrimHistory 在保留系统消息的同时裁剪最近的非系统对话轮次。
func (m *Model) TrimHistory(maxTurns int) {
	if len(m.messages) <= maxTurns*2 {
		return
	}

	var system []Message
	var others []Message

	for _, msg := range m.messages {
		if msg.Role == "system" {
			system = append(system, msg)
		} else {
			others = append(others, msg)
		}
	}

	if len(others) > maxTurns*2 {
		others = others[len(others)-maxTurns*2:]
	}

	m.messages = append(system, others...)
}
