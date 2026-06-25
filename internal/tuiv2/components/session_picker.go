package components

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"

	"github.com/sahilm/fuzzy"
)

// SessionSelectMsg 表示用户选择了某个会话。
type SessionSelectMsg struct {
	Session gateway.SessionSummary
}

// SessionDeleteMsg 表示用户请求删除某个会话。
type SessionDeleteMsg struct {
	SessionID string
}

// SessionPicker 是 Telescope 风格的会话选择器组件。
type SessionPicker struct {
	state *state.ViewState
}

var _ tea.Model = (*SessionPicker)(nil)

// NewSessionPicker 创建会话选择器组件。
func NewSessionPicker(viewState *state.ViewState) *SessionPicker {
	return &SessionPicker{state: viewState}
}

// Init 不启动额外命令。
func (s *SessionPicker) Init() tea.Cmd {
	return nil
}

// Update 处理会话选择器内的键盘和鼠标输入。
func (s *SessionPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return s, s.handleKey(msg)
	case tea.MouseMsg:
		return s, s.handleMouse(msg)
	}
	return s, nil
}

func (s *SessionPicker) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		s.state.Overlay.Active = state.OverlayNone
		s.state.Overlay.Query = ""
		s.state.Overlay.Selected = 0
		return nil
	case "enter", " ":
		matched := s.matchedSessions()
		if len(matched) == 0 {
			return nil
		}
		idx := s.state.Overlay.Selected
		if idx >= len(matched) {
			idx = len(matched) - 1
		}
		selected := matched[idx]
		s.state.Overlay.Active = state.OverlayNone
		s.state.Overlay.Query = ""
		s.state.Overlay.Selected = 0
		return func() tea.Msg {
			return SessionSelectMsg{Session: selected}
		}
	case "ctrl+d":
		matched := s.matchedSessions()
		if len(matched) == 0 {
			return nil
		}
		idx := s.state.Overlay.Selected
		if idx >= len(matched) {
			idx = len(matched) - 1
		}
		target := matched[idx]
		s.state.Overlay.Active = state.OverlayNone
		return func() tea.Msg {
			return SessionDeleteMsg{SessionID: target.ID}
		}
	case "up", "k":
		if s.state.Overlay.Selected > 0 {
			s.state.Overlay.Selected--
		}
		return nil
	case "down", "j":
		matched := s.matchedSessions()
		if s.state.Overlay.Selected < len(matched)-1 {
			s.state.Overlay.Selected++
		}
		return nil
	case "backspace":
		if len(s.state.Overlay.Query) > 0 {
			s.state.Overlay.Query = s.state.Overlay.Query[:len(s.state.Overlay.Query)-1]
			s.state.Overlay.Selected = 0
		}
		return nil
	default:
		runes := msg.Runes
		if len(runes) > 0 && runes[0] >= 32 {
			s.state.Overlay.Query += string(runes)
			s.state.Overlay.Selected = 0
		}
		return nil
	}
}

// matchedSessions 根据当前查询模糊匹配会话列表。
func (s *SessionPicker) matchedSessions() []gateway.SessionSummary {
	sessions := s.state.Gateway.Sessions
	query := strings.ToLower(s.state.Overlay.Query)
	if query == "" {
		return sessions
	}
	targets := make([]string, len(sessions))
	for i, sess := range sessions {
		targets[i] = strings.ToLower(sess.Title)
	}
	matches := fuzzy.Find(query, targets)
	result := make([]gateway.SessionSummary, 0, len(matches))
	for _, m := range matches {
		result = append(result, sessions[m.Index])
	}
	return result
}

// handleMouse 处理鼠标滚轮和点击事件。
func (s *SessionPicker) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if s.state.Overlay.Selected > 0 {
			s.state.Overlay.Selected--
		}
		return nil
	case tea.MouseButtonWheelDown:
		matched := s.matchedSessions()
		if s.state.Overlay.Selected < len(matched)-1 {
			s.state.Overlay.Selected++
		}
		return nil
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		// 标题 + 空行 + query + 空行 = 4 行头部
		itemIdx := msg.Y - 4
		matched := s.matchedSessions()
		if itemIdx >= 0 && itemIdx < len(matched) {
			selected := matched[itemIdx]
			s.state.Overlay.Active = state.OverlayNone
			s.state.Overlay.Query = ""
			s.state.Overlay.Selected = 0
			return func() tea.Msg {
				return SessionSelectMsg{Session: selected}
			}
		}
		return nil
	}
	return nil
}

// View 渲染会话选择器浮层。
func (s *SessionPicker) View() string {
	width := s.state.Layout.Width
	height := s.state.Layout.Height
	if width <= 0 {
		width = 60
	}
	if height <= 0 {
		height = 24
	}

	boxW := min(width-4, 56)
	boxH := height - 4
	if boxH < 8 {
		boxH = 8
	}
	maxItems := boxH - 7 // title + query + hint + separator lines
	if maxItems < 1 {
		maxItems = 1
	}
	matched := s.matchedSessions()

	var lines []string

	// 标题
	lines = append(lines, theme.AccentStyle().Render("  Sessions"))
	lines = append(lines, "")

	// 搜索输入
	queryLine := "> " + s.state.Overlay.Query
	lines = append(lines, theme.AccentStyle().Render(queryLine), "")

	// 会话列表
	for i, sess := range matched {
		if i >= maxItems {
			break
		}
		prefix := "  "
		title := sess.Title
		if title == "" {
			title = "untitled"
		}
		dateStr := formatSessionDate(sess.UpdatedAt)
		detail := theme.MutedStyle().Render(dateStr)

		if i == s.state.Overlay.Selected {
			prefix = theme.AccentStyle().Render("▎ ")
			title = theme.AccentStyle().Bold(true).Render(title)
		}
		line := prefix + title + "  " + detail
		if dw := theme.DisplayWidth(line); dw > boxW-4 {
			line = theme.Truncate(line, boxW-4)
		}
		lines = append(lines, line)
	}

	if len(matched) == 0 {
		lines = append(lines, theme.MutedStyle().Render("  No sessions found"))
	}

	// 提示行
	hint := "  ⏎ / ␣ : switch   Ctrl+D : delete   ␛ : cancel"
	lines = append(lines, "", theme.MutedStyle().Render(hint))

	content := strings.Join(lines, "\n")
	box := lipgloss.NewStyle().
		Width(boxW).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Render(content)

	return lipgloss.NewStyle().
		Width(width).
		Height(boxH).
		Align(lipgloss.Center, lipgloss.Center).
		Render(box)
}

// formatSessionDate 格式化会话日期显示。
func formatSessionDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}
