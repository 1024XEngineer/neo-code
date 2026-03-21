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

func NewModel(client infra.ChatClient, persona string) Model {
	stats, _ := client.GetMemoryStats(context.Background())
	if stats == nil {
		stats = &infra.MemoryStats{}
	}

	return Model{
		mode:           ModeChat,
		focused:        "input",
		messages:       make([]Message, 0),
		historyTurns:   6,
		activeModel:    client.DefaultModel(),
		memoryStats:    *stats,
		commandHistory: make([]string, 0),
		cmdHistIndex:   -1,
		client:         client,
		persona:        persona,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetWidth(w int) {
	m.width = w
}

func (m *Model) SetHeight(h int) {
	m.height = h
}

func (m *Model) AddMessage(role, content string) {
	m.messages = append(m.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

func (m *Model) AppendLastMessage(content string) {
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].Content += content
	}
}

func (m *Model) FinishLastMessage() {
	if len(m.messages) > 0 {
		m.messages[len(m.messages)-1].Streaming = false
	}
}

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
