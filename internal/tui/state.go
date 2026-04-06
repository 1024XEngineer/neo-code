package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	agentruntime "neo-code/internal/runtime"
)

type panel int

const (
	panelSessions panel = iota
	panelTranscript
	panelActivity
	panelInput
)

type pickerMode int

const (
	pickerNone pickerMode = iota
	pickerProvider
	pickerModel
	pickerFile
)

type UIState struct {
	Sessions           []agentruntime.SessionSummary
	ActiveSessionID    string
	ActiveSessionTitle string
	InputText          string
	IsAgentRunning     bool
	IsCompacting       bool
	StreamingReply     bool
	CurrentTool        string
	ExecutionError     string
	StatusText         string
	CurrentProvider    string
	CurrentModel       string
	CurrentWorkdir     string
	ShowHelp           bool
	ActivePicker       pickerMode
	Focus              panel
}

type activityEntry struct {
	Time    time.Time
	Kind    string
	Title   string
	Detail  string
	IsError bool
}

type pendingPermissionPrompt struct {
	RequestID    string
	ToolCallID   string
	ToolName     string
	ToolCategory string
	Target       string
	Submitted    bool
}

type commandMenuMeta struct {
	Title string
}

type commandMenuItem struct {
	title           string
	description     string
	filter          string
	highlight       bool
	replacement     string
	useReplaceRange bool
	replaceStart    int
	replaceEnd      int
	openFileBrowser bool
}

func (c commandMenuItem) Title() string {
	return c.title
}

func (c commandMenuItem) Description() string {
	return c.description
}

func (c commandMenuItem) FilterValue() string {
	base := strings.TrimSpace(c.filter)
	if base != "" {
		return strings.ToLower(base)
	}
	return strings.ToLower(c.title + " " + c.description)
}

type commandMenuDelegate struct {
	styles styles
}

func (d commandMenuDelegate) Height() int {
	return 1
}

func (d commandMenuDelegate) Spacing() int {
	return 0
}

func (d commandMenuDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d commandMenuDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(commandMenuItem)
	if !ok {
		return
	}

	contentWidth := max(12, m.Width()-2)
	usageStyle := d.styles.commandUsage
	if entry.highlight || index == m.Index() {
		usageStyle = d.styles.commandUsageMatch
	}

	line := usageStyle.Render(entry.title)
	if description := strings.TrimSpace(entry.description); description != "" {
		descWidth := max(8, contentWidth-lipgloss.Width(entry.title)-2)
		line = lipgloss.JoinHorizontal(
			lipgloss.Top,
			line,
			lipgloss.NewStyle().Width(2).Render(""),
			d.styles.commandDesc.Render(trimMiddle(description, descWidth)),
		)
	}

	fmt.Fprint(w, lipgloss.NewStyle().Width(contentWidth).Render(line))
}

type sessionItem struct {
	Summary agentruntime.SessionSummary
	Active  bool
}

func (s sessionItem) FilterValue() string {
	return strings.ToLower(s.Summary.Title)
}

type selectionItem struct {
	id          string
	name        string
	description string
}

func (s selectionItem) Title() string {
	return s.name
}

func (s selectionItem) Description() string {
	return s.description
}

func (s selectionItem) FilterValue() string {
	return strings.ToLower(s.id + " " + s.name + " " + s.description)
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

	style := d.styles.sessionRow
	metaStyle := d.styles.sessionMeta
	if session.Active {
		style = d.styles.sessionRowActive
		metaStyle = d.styles.sessionMetaActive
	}
	if index == m.Index() {
		style = d.styles.sessionRowFocused
		metaStyle = d.styles.sessionMetaFocus
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		fmt.Sprintf("%s %s", prefix, title),
		metaStyle.Render("  "+meta),
	)

	fmt.Fprint(w, style.Width(width).Render(content))
}
