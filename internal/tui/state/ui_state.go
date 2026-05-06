package state

import agentsession "neo-code/internal/session"

// Panel 定义 TUI 中可聚焦的主面板。
type Panel int

const (
	PanelSessions Panel = iota
	PanelTranscript
	PanelActivity
	PanelTodo
	PanelInput
)

// PickerMode 定义当前激活的选择器类型。
type PickerMode int

const (
	PickerNone PickerMode = iota
	PickerProvider
	PickerModel
	PickerSession
	PickerFile
	PickerHelp
	PickerProviderAdd
	PickerModelScopeGuide
)

// UIState 保存顶层界面状态快照，仅作为数据容器使用。
type UIState struct {
	Sessions           []agentsession.Summary
	ActiveSessionID    string
	ActiveSessionTitle string
	ActiveRunID        string
	InputText          string
	IsAgentRunning     bool
	IsCompacting       bool
	StreamingReply     bool
	CurrentTool        string
	ToolStates         []ToolState
	RunContext         ContextWindowState
	TokenUsage         TokenUsageState
	ExecutionError     string
	StatusText         string
	CurrentProvider    string
	CurrentModel       string
	CurrentAgentMode   string
	CurrentWorkdir     string
	ShowHelp           bool
	ActivePicker       PickerMode
	Focus              Panel
}
