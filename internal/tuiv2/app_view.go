// app_view.go 承载视图布局与尺寸适配辅助，从 app.go 拆分（plan-v4 Step 8）。
package tuiv2

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

// applyWindowSize 更新布局尺寸，并按 Focus-Only 断点计算 Soft Inspector 状态。
func (a *App) applyWindowSize(width int, height int) {
	a.state.Layout.Width = width
	a.state.Layout.Height = height
	switch {
	case width < inspectorHiddenWidth:
		a.state.Layout.ShowInspector = false
		a.state.Layout.InspectorWidth = 0
	case width < inspectorWideMin:
		a.state.Layout.ShowInspector = true
		a.state.Layout.InspectorWidth = width
	default:
		a.state.Layout.ShowInspector = true
		a.state.Layout.InspectorWidth = inspectorWideWidth
	}
}

// routeComponents 将全局消息转发给各静态布局组件。

// mainArea 渲染中部区域，按终端宽度决定 Inspector 右侧或纵向压缩显示。
func (a *App) mainArea() string {
	streamView := a.agentStream.View()
	if !a.state.Layout.ShowInspector {
		return streamView
	}
	inspectorView := a.softInspector.View()
	if a.state.Layout.Width >= inspectorWideMin {
		return lipgloss.JoinHorizontal(lipgloss.Top, streamView, "  ", inspectorView)
	}
	return lipgloss.JoinVertical(lipgloss.Left, streamView, "", a.separatorLine(), inspectorView)
}

// separatorLine 渲染单条细线，用于区分主要区域而不使用边框。
func (a *App) separatorLine() string {
	width := a.state.Layout.Width
	if width <= 0 {
		width = 48
	}
	return theme.SubtleStyle().Render(strings.Repeat("─", width))
}

// fitViewToTerminal 将视图约束到当前终端尺寸，避免 resize 后自动换行或旧行残留。
func (a *App) fitViewToTerminal(view string) string {
	width := a.state.Layout.Width
	height := a.state.Layout.Height
	if width <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = fitLine(line, width)
	}
	if height > 0 {
		switch {
		case len(lines) > height:
			lines = lines[:height]
		case len(lines) < height:
			for len(lines) < height {
				lines = append(lines, strings.Repeat(" ", width-1))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// fitLine 截断并补齐单行显示宽度，保留 ANSI 样式同时防止终端自动 wrap。
func fitLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	target := width - 1
	if target <= 0 {
		return ""
	}
	fitted := theme.Truncate(line, target)
	lineWidth := theme.DisplayWidth(fitted)
	if lineWidth < target {
		fitted += strings.Repeat(" ", target-lineWidth)
	}
	return fitted
}

// applyInitialLoaded 将 Gateway 初始 RPC 结果写入 ViewState。
func (a *App) applyInitialLoaded(msg initialLoadedMsg) {
	a.lastErr = msg.errText
	a.state.Gateway.Connected = msg.connected
	a.state.Gateway.Sessions = append([]gateway.SessionSummary(nil), msg.sessions...)
	a.state.Gateway.Models = append([]gateway.ModelInfo(nil), msg.models...)
	a.state.Gateway.ActiveModel = msg.activeModel
	a.eventCh = msg.eventCh
	if msg.errText != "" {
		a.state.Runtime.Phase = state.RuntimePhaseError
	}
	if len(msg.sessions) > 0 {
		active := msg.sessions[0]
		a.state.Gateway.ActiveSess = &active
	}
	if msg.detail != nil {
		a.state.Runtime.Tokens = state.TokenUsage{
			Input:  msg.detail.Usage.Input,
			Output: msg.detail.Usage.Output,
			Total:  msg.detail.Usage.Total,
		}
		for _, item := range msg.detail.Stream {
			a.appendStream(streamEntryFromItem(item))
		}
	}
}

// appendStream 以追加新 entry 的方式维护不可变 StreamEntry 序列。

// debugLine 渲染调试模式下的最小运行信息。
func (a *App) debugLine() string {
	size := defaultTerminal
	if a.state.Layout.Width > 0 || a.state.Layout.Height > 0 {
		size = fmt.Sprintf("%dx%d", a.state.Layout.Width, a.state.Layout.Height)
	}
	return fmt.Sprintf(
		"[debug] mode:%s  scenario:%s  events:%d  size:%s",
		inputModeName(a.state.Mode),
		a.scenario,
		len(a.state.Stream),
		size,
	)
}
