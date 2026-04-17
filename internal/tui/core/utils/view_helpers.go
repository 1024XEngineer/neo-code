package utils

import (
	"strings"

	tuistate "neo-code/internal/tui/state"
)

// PickerLabelFromMode 将 picker 模式映射为状态快照展示标签。
func PickerLabelFromMode(mode tuistate.PickerMode) string {
	switch mode {
	case tuistate.PickerProvider:
		return "provider"
	case tuistate.PickerModel:
		return "model"
	case tuistate.PickerSession:
		return "session"
	case tuistate.PickerFile:
		return "file"
	case tuistate.PickerHelp:
		return "help"
	default:
		return "none"
	}
}

// RequestedWorkdirForRun 在发起 run 时返回应转发给 runtime 的工作目录。
// `/cwd` 移除后，TUI 内部始终以当前界面工作目录作为本次运行的覆盖值。
func RequestedWorkdirForRun(currentWorkdir string) string {
	return strings.TrimSpace(currentWorkdir)
}

// IsBusy 统一判断当前是否存在进行中的 agent 或 compact 操作。
func IsBusy(isAgentRunning bool, isCompacting bool) bool {
	return isAgentRunning || isCompacting
}

// FocusLabelFromPanel 将焦点面板枚举映射为界面展示标签。
func FocusLabelFromPanel(
	focus tuistate.Panel,
	sessionsLabel string,
	transcriptLabel string,
	activityLabel string,
	todoLabel string,
	composerLabel string,
) string {
	switch focus {
	case tuistate.PanelSessions:
		return sessionsLabel
	case tuistate.PanelTranscript:
		return transcriptLabel
	case tuistate.PanelActivity:
		return activityLabel
	case tuistate.PanelTodo:
		return todoLabel
	default:
		return composerLabel
	}
}

// TrimRunes 按 rune 数裁剪文本，超长时尾部追加省略号。
func TrimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 4 {
		return text
	}
	return string(runes[:limit-3]) + "..."
}

// TrimMiddle 在中间裁剪长文本，保留首尾并插入省略号。
func TrimMiddle(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 7 {
		return text
	}
	left := (limit - 3) / 2
	right := limit - 3 - left
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
}

// Fallback 当 value 为空白文本时返回 fallbackValue。
func Fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

// Clamp 将数值限制在 [minValue, maxValue] 范围内。
func Clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
