// app_leader.go 承载 Leader Mode 的按键路由与 Leader 动作辅助函数，
// 从 app.go 拆分以控制单文件行数（plan-v4 Step 8）。
package tuiv2

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/components"
	"neo-code/internal/tuiv2/keymap"
	"neo-code/internal/tuiv2/state"
)

// handleLeaderKey 处理 Leader Key 后缀。
//
// 行为约定（plan-v4）：Leader 是独占捕获，非后缀键或超时(1s)时立即静默回到
// Normal（不泄漏给 Normal handler）。后缀键执行动作后回到 Normal，除非打开了
// 需要保持的面板（palette/session_picker/help/model_picker）。
func (a *App) handleLeaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()
	if keyStr == "esc" || keyStr == "ctrl+c" {
		a.state.Mode = state.NormalMode
		return a, nil
	}
	action := keymap.MatchLeaderKey(keyStr)
	// 非后缀键：静默回到 Normal，不执行任何动作。
	if action == keymap.ActionNone {
		a.state.Mode = state.NormalMode
		return a, nil
	}
	a.state.Mode = state.NormalMode // Leader 后总是回到 Normal
	switch action {
	case keymap.ActionLeaderQuit:
		return a, tea.Quit
	case keymap.ActionLeaderPalette:
		a.openOverlay(state.OverlayPalette)
		return a, nil
	case keymap.ActionLeaderHelp:
		a.openOverlay(state.OverlayHelp)
		return a, nil
	case keymap.ActionLeaderSwitchSession:
		a.openOverlay(state.OverlaySessionPicker)
		return a, nil
	case keymap.ActionLeaderModelPicker:
		a.openOverlay(state.OverlayModelPicker)
		return a, nil
	case keymap.ActionLeaderNewSession:
		if a.client != nil {
			return a, createSessionCmd(a.client)
		}
		return a, nil
	case keymap.ActionLeaderFullAccess:
		return a, a.toggleFullAccess()
	case keymap.ActionLeaderLog:
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("log-hint-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   "Log viewer not yet available",
			Metadata:  map[string]any{"done": true},
		})
		return a, nil
	case keymap.ActionLeaderCancelRun:
		return a, a.cancelCurrentRun()
	case keymap.ActionLeaderRetry:
		return a, a.retryLastRun()
	case keymap.ActionLeaderLastSession:
		return a, a.switchToLastSession()
	default:
		return a, nil
	}
}

// cancelCurrentRun 取消当前运行中的 Agent；无运行中任务时静默 no-op。
func (a *App) cancelCurrentRun() tea.Cmd {
	phase := a.state.Runtime.Phase
	if phase != state.RuntimePhaseRunning &&
		phase != state.RuntimePhaseWaitingPermission &&
		phase != state.RuntimePhaseWaitingUser {
		// 空闲态：静默 no-op，避免打扰用户。
		return nil
	}
	if a.client != nil {
		return cancelRunCmd(a.client, a.activeSessionID(), a.state.Runtime.RunID)
	}
	a.state.Runtime.Phase = state.RuntimePhaseCancelled
	return nil
}

// retryLastRun 重试最近一次用户输入；无历史输入时提示。
func (a *App) retryLastRun() tea.Cmd {
	if strings.TrimSpace(a.lastUserText) == "" {
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("retry-hint-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   "No previous run to retry",
			Metadata:  map[string]any{"done": true},
		})
		return nil
	}
	return a.handleSubmitMessage(components.SubmitMessageMsg{Text: a.lastUserText})
}

// switchToLastSession 切换到上一个会话；无上一会话时提示。
func (a *App) switchToLastSession() tea.Cmd {
	if a.prevSessionID == "" {
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("last-sess-hint-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   "No previous session to switch",
			Metadata:  map[string]any{"done": true},
		})
		return nil
	}
	if a.client == nil {
		return nil
	}
	return loadSessionCmd(a.client, a.prevSessionID)
}

// handleCtrlC 实现 Ctrl+C 双退保护：运行中取消、空闲双退。
