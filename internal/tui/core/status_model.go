package core

import (
	"fmt"
	"strings"

	"go-llm-demo/internal/tui/state"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type statusPhase int

const (
	statusPhaseIdle statusPhase = iota
	statusPhaseThinking
	statusPhaseToolRunning
	statusPhaseApprovalRequired
	statusPhaseDone
	statusPhaseError
	statusPhaseNotice
)

type statusModel struct {
	spinner spinner.Model
	phase   statusPhase
	text    string
}

func newStatusModel() statusModel {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))

	return statusModel{
		spinner: s,
		phase:   statusPhaseIdle,
	}
}

func (s statusModel) isBusy() bool {
	return s.phase == statusPhaseThinking || s.phase == statusPhaseToolRunning
}

func (s statusModel) plainText() string {
	if strings.TrimSpace(s.text) == "" {
		return "Ready"
	}
	return s.text
}

func (s statusModel) displayText() string {
	text := s.plainText()
	if !s.isBusy() {
		return text
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", s.spinner.View(), text))
}

func (s *statusModel) update(msg tea.Msg) tea.Cmd {
	if !s.isBusy() {
		return nil
	}
	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(msg)
	return cmd
}

func (s *statusModel) setPhase(phase statusPhase, text string) tea.Cmd {
	s.phase = phase
	s.text = strings.TrimSpace(text)
	if s.isBusy() {
		return s.spinner.Tick
	}
	return nil
}

func (s *statusModel) clearTransient() {
	switch s.phase {
	case statusPhaseDone, statusPhaseError, statusPhaseNotice:
		s.phase = statusPhaseIdle
		s.text = ""
	}
}

func (s statusModel) syncUIState(ui *state.UIState) {
	if ui == nil {
		return
	}
	switch s.phase {
	case statusPhaseNotice:
		ui.CopyStatus = s.plainText()
		ui.StatusText = ""
	case statusPhaseIdle:
		ui.CopyStatus = ""
		ui.StatusText = ""
	default:
		ui.CopyStatus = ""
		ui.StatusText = s.plainText()
	}
}

func (m *Model) statusText() string {
	return m.status.displayText()
}

func (m *Model) statusBusy() bool {
	return m.status.isBusy()
}

func (m *Model) clearNotices() {
	m.status.clearTransient()
	m.status.syncUIState(&m.ui)
}

func (m *Model) setStatusPhase(phase statusPhase, text string) tea.Cmd {
	cmd := m.status.setPhase(phase, text)
	m.status.syncUIState(&m.ui)
	return cmd
}

func (m *Model) setThinkingStatus() tea.Cmd {
	return m.setStatusPhase(statusPhaseThinking, "Thinking...")
}

func (m *Model) setToolRunningStatus(toolName string) tea.Cmd {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return m.setStatusPhase(statusPhaseToolRunning, "Executing tool...")
	}
	return m.setStatusPhase(statusPhaseToolRunning, fmt.Sprintf("Executing %s...", toolName))
}

func (m *Model) setApprovalRequiredStatus() tea.Cmd {
	return m.setStatusPhase(statusPhaseApprovalRequired, "Approval required")
}

func (m *Model) setDoneStatus(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		text = "Done"
	}
	return m.setStatusPhase(statusPhaseDone, text)
}

func (m *Model) setErrorStatus(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		text = "Request failed"
	}
	return m.setStatusPhase(statusPhaseError, text)
}

func (m *Model) setNoticeStatus(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		text = "Ready"
	}
	return m.setStatusPhase(statusPhaseNotice, text)
}
