package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

// ExCommandMsg 表示用户在 : 命令行提交了一条命令（已去除前缀 ":"）。
// 由 app 层解释具体命令（q/debug/help/compact/mode 等）并执行副作用。
type ExCommandMsg struct {
	Command string
}

// SearchSubmitMsg 表示用户在 / 搜索行提交了搜索词，由 app 层执行全量扫描。
type SearchSubmitMsg struct {
	Query string
}

// CmdLineCancelMsg 表示用户取消了 Ex/Search 输入（Esc），由 app 层关闭 overlay。
type CmdLineCancelMsg struct{}

// CmdLine 渲染并处理 Normal Mode 下的 : 命令行与 / 搜索输入。
//
// 组件本身无副作用：命令解释与搜索扫描由 app 层根据返回的 tea.Msg 完成，
// 保持组件职责单一（输入收集 + 渲染）。
type CmdLine struct {
	state *state.ViewState
}

var _ tea.Model = (*CmdLine)(nil)

// NewCmdLine 创建命令行/搜索输入组件。
func NewCmdLine(viewState *state.ViewState) *CmdLine {
	return &CmdLine{state: viewState}
}

// Init 不启动额外命令。
func (c *CmdLine) Init() tea.Cmd {
	return nil
}

// Update 处理 Ex/Search 输入的所有按键（app 在 overlay 激活时路由给它）。
//
// 行为约定：
//   - 可打印字符追加到对应输入（Ex.Input 或 Search.Query）
//   - Backspace 删除末尾字符
//   - Enter 提交：Ex 返回 ExCommandMsg，Search 返回 SearchSubmitMsg
//   - Esc/Ctrl+C 取消，返回 CmdLineCancelMsg
//   - 其余键（j/k 等导航）忽略，Ex/Search 输入时不滚动
func (c *CmdLine) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch key.String() {
	case "esc", "ctrl+c":
		return c, emitMsg(CmdLineCancelMsg{})
	case "enter":
		return c, c.handleSubmit()
	case "backspace", "ctrl+h":
		c.handleBackspace()
		return c, nil
	default:
		// 仅接受可打印字符（rune >= 32），忽略功能键与导航键。
		if len(key.Runes) > 0 && key.Runes[0] >= 32 {
			c.handleRune(key.Runes[0])
		}
		return c, nil
	}
}

// handleSubmit 根据当前 overlay 类型提交命令或搜索。
func (c *CmdLine) handleSubmit() tea.Cmd {
	switch c.state.Overlay.Active {
	case state.OverlayEx:
		cmd := strings.TrimSpace(c.state.Ex.Input)
		c.state.Ex.Input = ""
		return emitMsg(ExCommandMsg{Command: cmd})
	case state.OverlaySearch:
		query := c.state.Search.Query
		c.state.Search.Query = ""
		return emitMsg(SearchSubmitMsg{Query: query})
	}
	return nil
}

// handleBackspace 删除当前输入末尾一个 rune，按 rune 编辑以正确处理多字节 UTF-8。
//
// 早期实现按字节切片（Input[:len-1]）会破坏中文/emoji（如"错"3字节删1字节产生
// 无效 UTF-8），现改为 deleteLastRune（utf8.DecodeLastRuneInString），与主 prompt
// 编辑器行为一致。
func (c *CmdLine) handleBackspace() {
	switch c.state.Overlay.Active {
	case state.OverlayEx:
		c.state.Ex.Input = deleteLastRune(c.state.Ex.Input)
	case state.OverlaySearch:
		c.state.Search.Query = deleteLastRune(c.state.Search.Query)
	}
}

// handleRune 将可打印字符追加到当前输入。
func (c *CmdLine) handleRune(r rune) {
	switch c.state.Overlay.Active {
	case state.OverlayEx:
		c.state.Ex.Input += string(r)
	case state.OverlaySearch:
		c.state.Search.Query += string(r)
	}
}

// View 渲染底部单行输入（: 前缀或 / 前缀），并在搜索结果过时时追加 stale 提示。
func (c *CmdLine) View() string {
	var prefix, text, hint string
	switch c.state.Overlay.Active {
	case state.OverlayEx:
		prefix = ":"
		text = c.state.Ex.Input
	case state.OverlaySearch:
		prefix = "/"
		text = c.state.Search.Query
		if c.state.Search.Stale {
			hint = "\n" + theme.MutedStyle().Render("  results may be stale — press / to refresh")
		}
	default:
		return ""
	}
	cursor := "_"
	line := theme.AccentStyle().Render(prefix+" ") + theme.BaseStyle().Render(text+cursor)
	content := line + hint
	width := c.state.Layout.Width
	if width > 0 {
		return fitBlock(content, width, true)
	}
	return content
}

// RunSearch 全量扫描 Stream，返回匹配 entry 的全局索引（append-only 保证索引稳定）。
//
// 匹配规则：子串包含、忽略大小写。空 query 返回 nil（调用方按 no-op 处理）。
func RunSearch(stream []state.StreamEntry, query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	needle := strings.ToLower(query)
	matches := make([]int, 0)
	for i, entry := range stream {
		if strings.Contains(strings.ToLower(entry.Content), needle) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return matches
}
