package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	agentruntime "github.com/dust/neo-code/internal/runtime"
)

type panel int

const (
	panelSessions panel = iota
	panelTranscript
	panelActivity
	panelInput
)

type UIState struct {
	Sessions           []agentruntime.SessionSummary
	ActiveSessionID    string
	ActiveSessionTitle string
	InputText          string
	IsAgentRunning     bool
	StreamingReply     bool
	CurrentTool        string
	ExecutionError     string
	StatusText         string
	CurrentProvider    string
	CurrentModel       string
	CurrentWorkdir     string
	ShowHelp           bool
	ShowModelPicker    bool
	Focus              panel
}

type activityEntry struct {
	Time    time.Time
	Kind    string
	Title   string
	Detail  string
	IsError bool
}

type sessionItem struct {
	Summary agentruntime.SessionSummary
	Active  bool
}

func (s sessionItem) FilterValue() string {
	return strings.ToLower(s.Summary.Title)
}

type modelItem struct {
	name        string
	description string
}

func (m modelItem) Title() string {
	return m.name
}

func (m modelItem) Description() string {
	return m.description
}

func (m modelItem) FilterValue() string {
	return strings.ToLower(m.name + " " + m.description)
}

type sessionDelegate struct {
	styles styles
}

func (d sessionDelegate) Height() int {
	return 3
}

func (d sessionDelegate) Spacing() int {
	return 1
}

func (d sessionDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d sessionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	session, ok := item.(sessionItem)
	if !ok {
		return
	}

	width := max(18, m.Width()-2)
	title := trimRunes(session.Summary.Title, max(8, width-10))
	meta := session.Summary.UpdatedAt.Format("01-02 15:04")

	prefix := "o"
	if session.Active {
		prefix = "*"
	}
	if index == m.Index() {
		prefix = ">"
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		fmt.Sprintf("%s %s", prefix, title),
		d.styles.sessionMeta.Render("  "+meta),
	)

	style := d.styles.sessionRow
	if session.Active {
		style = d.styles.sessionRowActive
	}
	if index == m.Index() {
		style = d.styles.sessionRowFocused
	}

	fmt.Fprint(w, style.Width(width).Render(content))
}
