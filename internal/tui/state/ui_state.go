package state

import agentruntime "neo-code/internal/runtime"

// Panel 定义 TUI 中可聚焦的主面板。
type Panel int

const (
	PanelSessions Panel = iota
	PanelTranscript
	PanelActivity
	PanelInput
)

// PickerMode 定义当前激活的选择器类型。
type PickerMode int

const (
	PickerNone PickerMode = iota
	PickerProvider
	PickerModel
	PickerFile
)

// UIState 保存顶层界面状态快照，仅作为数据容器使用。
type UIState struct {
	Sessions           []agentruntime.SessionSummary
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
	CurrentWorkdir     string
	ShowHelp           bool
	ActivePicker       PickerMode
	Focus              Panel
}
