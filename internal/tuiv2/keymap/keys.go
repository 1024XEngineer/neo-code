// Package keymap 定义 TUI v2 的三层键位系统：Input / Normal / Leader。
package keymap

import "github.com/charmbracelet/bubbles/key"

// Action 代表一个键位触发的动作。
type Action int

const (
	ActionNone Action = iota

	// Input Mode actions
	ActionSend        // Enter
	ActionNewline     // Shift+Enter
	ActionEscape      // Esc
	ActionCtrlC       // Ctrl+C (context-dependent)
	ActionOpenPalette // Ctrl+P
	ActionHelp        // ?
	ActionSlashMode   // / (when input empty)
	ActionFileRef     // @ (when input empty)
	ActionLogViewer   // Ctrl+L

	// Normal Mode actions
	ActionEnterInput    // i
	ActionScrollDown    // j
	ActionScrollUp      // k
	ActionHalfPageDown  // Ctrl+D
	ActionHalfPageUp    // Ctrl+U
	ActionFullPageDown  // Ctrl+F 整页下翻
	ActionFullPageUp    // Ctrl+B 整页上翻
	ActionScrollTop     // g
	ActionScrollBottom  // G
	ActionSearchForward // /
	ActionSearchNext    // n
	ActionSearchPrev    // N
	ActionExCommand     // :
	ActionQuit          // q
	ActionLeader        // Space (enters Leader mode)

	// Leader actions
	ActionLeaderPalette       // Space p
	ActionLeaderNewSession    // Space n
	ActionLeaderSwitchSession // Space s
	ActionLeaderHelp          // Space h
	ActionLeaderModelPicker   // Space m 模型选择器
	ActionLeaderFullAccess    // Space f
	ActionLeaderLog           // Space l
	ActionLeaderCancelRun     // Space c 取消当前运行
	ActionLeaderRetry         // Space r 重试上次运行
	ActionLeaderLastSession   // Space Space 切换上一会话
	ActionLeaderQuit          // Space q
)

// HelpEntry 描述一条键位帮助信息。
type HelpEntry struct {
	Key  string
	Desc string
}

// HelpGroup 是一组相关的键位帮助。
type HelpGroup struct {
	Title   string
	Entries []HelpEntry
}

// InputBindings 返回 Input Mode 的键位绑定。
func InputBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send message")),
		// Shift+Enter 仅在支持 kitty keyboard protocol 的终端（Kitty/WezTerm/Alacritty）
		// 可与 Enter 区分；多数终端（GNOME Terminal、tmux、screen、VS Code）不可区分，
		// 这里登记可用的 Alt+Enter / Ctrl+J 作为换行键兜底。
		key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"), key.WithHelp("alt+enter", "new line")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "normal mode")),
		key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "cancel/quit")),
		key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "command palette")),
	}
}

// InputHelp 返回 Input Mode 的分组帮助信息。
func InputHelp() []HelpGroup {
	return []HelpGroup{
		{
			Title: "Input Mode",
			Entries: []HelpEntry{
				{Key: "Enter", Desc: "Send message"},
				{Key: "Alt+Enter / Ctrl+J", Desc: "New line (Shift+Enter 仅 kitty 协议终端可用)"},
				{Key: "Ctrl+A / Ctrl+E", Desc: "行首 / 行尾"},
				{Key: "Ctrl+K", Desc: "删除到行尾"},
				{Key: "Ctrl+W", Desc: "删除前一个词"},
				{Key: "Ctrl+D", Desc: "空输入时 EOF 退出；非空时删除光标后字符"},
				{Key: "Ctrl+C", Desc: "Cancel agent (double to quit)"},
				{Key: "Ctrl+P", Desc: "Command palette"},
				{Key: "?", Desc: "This help"},
				{Key: "/", Desc: "Slash command (when empty)"},
				{Key: "@", Desc: "Attach file reference (when empty)"},
				{Key: "Esc", Desc: "Normal Mode"},
			},
		},
	}
}

// NormalHelp 返回 Normal Mode 的分组帮助信息。
func NormalHelp() []HelpGroup {
	return []HelpGroup{
		{
			Title: "Normal Mode (Esc)",
			Entries: []HelpEntry{
				{Key: "i", Desc: "Enter Input Mode"},
				{Key: "/", Desc: "Search in stream"},
				{Key: ":", Desc: "Command line (:q / :debug / :compact / :mode)"},
				{Key: "q", Desc: "Quit"},
			},
		},
	}
}

// LeaderHelp 返回 Leader Key 的分组帮助信息。
func LeaderHelp() []HelpGroup {
	return []HelpGroup{
		{
			Title: "Leader (Space)",
			Entries: []HelpEntry{
				{Key: "Space p", Desc: "Command palette"},
				{Key: "Space n", Desc: "New session"},
				{Key: "Space s", Desc: "Switch session"},
				{Key: "Space m", Desc: "Model picker"},
				{Key: "Space h", Desc: "Help"},
				{Key: "Space r", Desc: "Retry last run"},
				{Key: "Space c", Desc: "Cancel current run"},
				{Key: "Space Space", Desc: "Switch to last session"},
				{Key: "Space f", Desc: "Toggle Full Access"},
				{Key: "Space l", Desc: "Log viewer"},
				{Key: "Space q", Desc: "Quit"},
				{Key: "---", Desc: "已移除：compact → :compact，toggle-mode → :mode"},
			},
		},
	}
}

// NavigationHelp 返回导航键位的分组帮助信息。
func NavigationHelp() []HelpGroup {
	return []HelpGroup{
		{
			Title: "Navigation",
			Entries: []HelpEntry{
				{Key: "j / k", Desc: "Scroll down / up"},
				{Key: "Ctrl+D / U", Desc: "Half-page down / up"},
				{Key: "Ctrl+F / B", Desc: "Full-page down / up"},
				{Key: "g / G", Desc: "Jump to top / bottom"},
				{Key: "n / N", Desc: "Search next / previous (循环)"},
				{Key: "Mouse wheel", Desc: "Scroll"},
			},
		},
	}
}

// FullHelp 返回所有分组帮助信息。
func FullHelp() []HelpGroup {
	var groups []HelpGroup
	groups = append(groups, InputHelp()...)
	groups = append(groups, NormalHelp()...)
	groups = append(groups, LeaderHelp()...)
	groups = append(groups, NavigationHelp()...)
	return groups
}

// MatchInputKey 匹配 Input Mode 按键到动作。
//
// 注意：ctrl+d 不在此函数映射。Input Mode 下 Ctrl+D 的行为依赖输入框是否为空
// （空 → EOF 退出程序；非空 → 删除光标后字符），由 app.handleInputModeKey 层
// 按上下文决定。这里不映射可避免 keymap 层对上下文语义做错误假设。
func MatchInputKey(keyStr string) Action {
	switch keyStr {
	case "enter":
		return ActionSend
	case "shift+enter":
		return ActionNewline
	case "esc":
		return ActionEscape
	case "ctrl+c":
		return ActionCtrlC
	case "ctrl+p":
		return ActionOpenPalette
	case "ctrl+l":
		return ActionLogViewer
	}
	return ActionNone
}

// MatchNormalKey 匹配 Normal Mode 按键到动作。
func MatchNormalKey(keyStr string) Action {
	switch keyStr {
	case "i":
		return ActionEnterInput
	case "j":
		return ActionScrollDown
	case "k":
		return ActionScrollUp
	case "ctrl+d":
		return ActionHalfPageDown
	case "ctrl+u":
		return ActionHalfPageUp
	case "ctrl+f":
		return ActionFullPageDown
	case "ctrl+b":
		return ActionFullPageUp
	case "g":
		return ActionScrollTop
	case "G":
		return ActionScrollBottom
	case "/":
		return ActionSearchForward
	case "n":
		return ActionSearchNext
	case "N":
		return ActionSearchPrev
	case ":":
		return ActionExCommand
	case "q":
		return ActionQuit
	case " ", "space":
		return ActionLeader
	case "ctrl+c":
		return ActionCtrlC
	}
	return ActionNone
}

// MatchLeaderKey 匹配 Leader 后缀按键到动作。
func MatchLeaderKey(keyStr string) Action {
	switch keyStr {
	case "p":
		return ActionLeaderPalette
	case "n":
		return ActionLeaderNewSession
	case "s":
		return ActionLeaderSwitchSession
	case "h":
		return ActionLeaderHelp
	case "m":
		return ActionLeaderModelPicker
	case "f":
		return ActionLeaderFullAccess
	case "l":
		return ActionLeaderLog
	case "c":
		return ActionLeaderCancelRun
	case "r":
		return ActionLeaderRetry
	case " ", "space":
		return ActionLeaderLastSession
	case "q":
		return ActionLeaderQuit
	}
	return ActionNone
}

// IsLeaderSuffix 判断按键是否为有效的 Leader 后缀。
func IsLeaderSuffix(keyStr string) bool {
	return MatchLeaderKey(keyStr) != ActionNone
}
