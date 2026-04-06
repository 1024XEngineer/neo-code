package status

import (
	"fmt"
	"strings"

	tuiutils "neo-code/internal/tui/core/utils"
	tuistate "neo-code/internal/tui/state"
)

// Snapshot 表示 /status 命令所需的界面状态快照。
type Snapshot struct {
	ActiveSessionID    string
	ActiveSessionTitle string
	ActiveRunID        string
	IsAgentRunning     bool
	IsCompacting       bool
	CurrentProvider    string
	CurrentModel       string
	CurrentWorkdir     string
	CurrentTool        string
	ToolStateCount     int
	RunTotalTokens     int
	SessionTotalTokens int
	ExecutionError     string
	FocusLabel         string
	PickerLabel        string
	MessageCount       int
}

// BuildFromUIState 根据 UIState 与附加上下文构建 /status 所需快照。
func BuildFromUIState(
	state tuistate.UIState,
	messageCount int,
	focusLabel string,
	pickerLabel string,
) Snapshot {
	return Snapshot{
		ActiveSessionID:    state.ActiveSessionID,
		ActiveSessionTitle: state.ActiveSessionTitle,
		ActiveRunID:        state.ActiveRunID,
		IsAgentRunning:     state.IsAgentRunning,
		IsCompacting:       state.IsCompacting,
		CurrentProvider:    state.CurrentProvider,
		CurrentModel:       state.CurrentModel,
		CurrentWorkdir:     state.CurrentWorkdir,
		CurrentTool:        state.CurrentTool,
		ToolStateCount:     len(state.ToolStates),
		RunTotalTokens:     state.TokenUsage.RunTotalTokens,
		SessionTotalTokens: state.TokenUsage.SessionTotalTokens,
		ExecutionError:     state.ExecutionError,
		FocusLabel:         focusLabel,
		PickerLabel:        pickerLabel,
		MessageCount:       messageCount,
	}
}

// Format 将状态快照格式化为多行文本，用于 /status 命令输出。
func Format(snapshot Snapshot, draftSessionTitle string) string {
	sessionID := snapshot.ActiveSessionID
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "<draft>"
	}
	sessionTitle := snapshot.ActiveSessionTitle
	if strings.TrimSpace(sessionTitle) == "" {
		sessionTitle = draftSessionTitle
	}
	running := "no"
	if snapshot.IsAgentRunning || snapshot.IsCompacting {
		running = "yes"
	}
	currentTool := snapshot.CurrentTool
	if strings.TrimSpace(currentTool) == "" {
		currentTool = "<none>"
	}
	errorText := snapshot.ExecutionError
	if strings.TrimSpace(errorText) == "" {
		errorText = "<none>"
	}
	picker := snapshot.PickerLabel
	if strings.TrimSpace(picker) == "" {
		picker = "none"
	}

	lines := []string{
		"Status:",
		"Session: " + sessionTitle,
		"Session ID: " + sessionID,
		"Run ID: " + tuiutils.Fallback(strings.TrimSpace(snapshot.ActiveRunID), "<none>"),
		"Running: " + running,
		"Provider: " + snapshot.CurrentProvider,
		"Model: " + snapshot.CurrentModel,
		"Workdir: " + snapshot.CurrentWorkdir,
		"Focus: " + snapshot.FocusLabel,
		"Picker: " + picker,
		"Current Tool: " + currentTool,
		fmt.Sprintf("Tool States: %d", snapshot.ToolStateCount),
		fmt.Sprintf("Run Tokens: %d", snapshot.RunTotalTokens),
		fmt.Sprintf("Session Tokens: %d", snapshot.SessionTotalTokens),
		fmt.Sprintf("Messages: %d", snapshot.MessageCount),
		"Error: " + errorText,
	}
	return strings.Join(lines, "\n")
}
