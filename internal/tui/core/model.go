package core

import (
	"context"
	"strings"
	"sync"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	ui   state.UIState
	chat state.ChatState

	client  services.ChatClient
	persona string

	streamChan      <-chan string
	textarea        composerModel
	viewport        chatPaneModel
	context         contextPaneModel
	help            help.Model
	status          statusModel
	todo            todoPanelModel
	layout          layoutState
	copyToClipboard func(string) error
	thinkingInBlock bool
	thinkingCarry   string

	mu *sync.Mutex
}

const resumeSummaryPrefix = "[RESUME_SUMMARY]"

// NewModel constructs the top-level Bubble Tea model for the TUI.
func NewModel(client services.ChatClient, persona string, historyTurns int, configPath, workspaceRoot string) Model {
	stats, _ := client.GetMemoryStats(context.Background())
	if stats == nil {
		stats = &services.MemoryStats{}
	}
	if historyTurns <= 0 {
		historyTurns = 6
	}

	model := Model{
		ui: state.UIState{
			Mode:       state.ModeChat,
			Focused:    "composer",
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
		textarea:        newComposerModel(),
		viewport:        newChatPaneModel(),
		context:         newContextPaneModel(),
		help:            help.New(),
		status:          newStatusModel(),
		todo:            newTodoPanelModel(client),
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

func (m *Model) resetThinkingFilter() {
	m.thinkingInBlock = false
	m.thinkingCarry = ""
}

func (m *Model) consumeThinkingChunk(chunk string) string {
	if chunk == "" {
		return ""
	}

	m.thinkingCarry += chunk
	var out strings.Builder

	for len(m.thinkingCarry) > 0 {
		if m.thinkingInBlock {
			if end := strings.Index(m.thinkingCarry, "</think>"); end >= 0 {
				m.thinkingCarry = m.thinkingCarry[end+len("</think>"):]
				m.thinkingInBlock = false
				continue
			}

			if keep := partialTagSuffix(m.thinkingCarry, "</think>"); keep > 0 {
				m.thinkingCarry = m.thinkingCarry[len(m.thinkingCarry)-keep:]
			} else {
				m.thinkingCarry = ""
			}
			break
		}

		if start := strings.Index(m.thinkingCarry, "<think>"); start >= 0 {
			out.WriteString(m.thinkingCarry[:start])
			m.thinkingCarry = m.thinkingCarry[start+len("<think>"):]
			m.thinkingInBlock = true
			continue
		}

		if keep := partialTagSuffix(m.thinkingCarry, "<think>"); keep > 0 {
			safeLen := len(m.thinkingCarry) - keep
			out.WriteString(m.thinkingCarry[:safeLen])
			m.thinkingCarry = m.thinkingCarry[safeLen:]
		} else {
			out.WriteString(m.thinkingCarry)
			m.thinkingCarry = ""
		}
		break
	}

	return out.String()
}

func partialTagSuffix(content string, tag string) int {
	maxLen := len(tag) - 1
	if len(content) < maxLen {
		maxLen = len(content)
	}
	for size := maxLen; size > 0; size-- {
		if strings.HasSuffix(content, tag[:size]) {
			return size
		}
	}
	return 0
}

// Init returns the initial Bubble Tea command.
func (m Model) Init() tea.Cmd {
	return m.textarea.Focus()
}

// SetWidth stores the current viewport width.
func (m *Model) SetWidth(w int) {
	m.ui.Width = w
}

// SetHeight stores the current viewport height.
func (m *Model) SetHeight(h int) {
	m.ui.Height = h
}

// AddMessage appends a timestamped message to the chat history.
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

// AppendLastMessage appends streamed text onto the last message.
func (m *Model) AppendLastMessage(content string) {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	if len(m.chat.Messages) > 0 {
		m.chat.Messages[len(m.chat.Messages)-1].Content += content
	}
}

// FinishLastMessage marks the last message as no longer streaming.
func (m *Model) FinishLastMessage() {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	if len(m.chat.Messages) > 0 {
		m.chat.Messages[len(m.chat.Messages)-1].Streaming = false
	}
}

// TrimHistory keeps system messages and the latest non-system turns.
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
