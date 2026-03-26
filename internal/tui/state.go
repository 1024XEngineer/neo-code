package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"neocode/internal/provider"
	"neocode/internal/runtime"
	"neocode/internal/tools"
)

const maxActivityEntries = 24

type paneMode int

const (
	paneBrowse paneMode = iota
	paneCompose
	paneSidebar
)

func (p paneMode) Label() string {
	switch p {
	case paneCompose:
		return "compose"
	case paneSidebar:
		return "sessions"
	default:
		return "browse"
	}
}

type activityTone string

const (
	toneInfo    activityTone = "info"
	toneRunning activityTone = "running"
	toneSuccess activityTone = "success"
	toneError   activityTone = "error"
)

type activityEntry struct {
	At        time.Time
	SessionID string
	Title     string
	Detail    string
	Tone      activityTone
}

type activeTool struct {
	SessionID string
	Call      provider.ToolCall
	StartedAt time.Time
}

type sessionStats struct {
	UserMessages      int
	AssistantMessages int
	ToolMessages      int
	ToolRequests      int
}

type rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func (r rect) contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

func (r rect) inner() rect {
	inner := rect{
		X:      r.X + 2,
		Y:      r.Y + 2,
		Width:  max(0, r.Width-4),
		Height: max(0, r.Height-3),
	}
	if inner.Width < 0 {
		inner.Width = 0
	}
	if inner.Height < 0 {
		inner.Height = 0
	}
	return inner
}

type screenActionKind string

const (
	actionToggleSidebar screenActionKind = "toggle_sidebar"
	actionNewSession    screenActionKind = "new_session"
	actionJumpLatest    screenActionKind = "jump_latest"
	actionOpenSession   screenActionKind = "open_session"
)

type screenActionZone struct {
	Kind      screenActionKind
	Rect      rect
	SessionID string
}

type layoutMode int

const (
	layoutWide layoutMode = iota
	layoutStacked
)

type layoutMetrics struct {
	mode             layoutMode
	headerRect       rect
	bodyRect         rect
	conversationRect rect
	runtimeRect      rect
	composerRect     rect
	footerRect       rect
	sidebarRect      rect
}

type uiState struct {
	sessions          []runtime.SessionSummary
	providers         []runtime.ProviderSummary
	activeSessionID   string
	status            runtime.Status
	lastError         string
	lastUpdatedAt     time.Time
	streaming         map[string]string
	activities        []activityEntry
	activeTools       map[string]activeTool
	sidebarOpen       bool
	sidebarSelection  int
	sidebarScroll     int
	selectedMessage   int
	selectedCodeBlock int
	pane              paneMode
	showHelp          bool
	notice            string
	noticeTone        activityTone
	noticeID          int
	headerZones       []screenActionZone
	sidebarZones      []screenActionZone
}

func (s uiState) activeSession(runtimeSvc *runtime.Service) (runtime.Session, bool) {
	return runtimeSvc.Session(s.activeSessionID)
}

func (s uiState) activeSessionSummary() (runtime.SessionSummary, bool) {
	for _, summary := range s.sessions {
		if summary.ID == s.activeSessionID {
			return summary, true
		}
	}
	return runtime.SessionSummary{}, false
}

func (s *uiState) appendActivity(entry activityEntry) {
	if strings.TrimSpace(entry.Title) == "" {
		return
	}
	s.activities = append(s.activities, entry)
	if len(s.activities) > maxActivityEntries {
		s.activities = append([]activityEntry(nil), s.activities[len(s.activities)-maxActivityEntries:]...)
	}
}

func (s *uiState) rememberTool(sessionID string, call provider.ToolCall, startedAt time.Time) {
	if s.activeTools == nil {
		s.activeTools = make(map[string]activeTool)
	}
	s.activeTools[toolStateKey(sessionID, call.ID)] = activeTool{
		SessionID: sessionID,
		Call:      call,
		StartedAt: startedAt,
	}
}

func (s *uiState) forgetTool(sessionID, callID string) {
	if len(s.activeTools) == 0 {
		return
	}
	delete(s.activeTools, toolStateKey(sessionID, callID))
}

func (s *uiState) clearTools(sessionID string) {
	if len(s.activeTools) == 0 {
		return
	}
	for key, toolState := range s.activeTools {
		if toolState.SessionID == sessionID {
			delete(s.activeTools, key)
		}
	}
}

func (s uiState) toolsForSession(sessionID string) []activeTool {
	if len(s.activeTools) == 0 {
		return nil
	}

	toolsForSession := make([]activeTool, 0, len(s.activeTools))
	for _, toolState := range s.activeTools {
		if toolState.SessionID == sessionID {
			toolsForSession = append(toolsForSession, toolState)
		}
	}

	sort.Slice(toolsForSession, func(i, j int) bool {
		return toolsForSession[i].StartedAt.Before(toolsForSession[j].StartedAt)
	})
	return toolsForSession
}

func collectSessionStats(session runtime.Session) sessionStats {
	stats := sessionStats{}
	for _, message := range session.Messages {
		switch message.Role {
		case provider.RoleUser:
			stats.UserMessages++
		case provider.RoleAssistant:
			stats.AssistantMessages++
			stats.ToolRequests += len(message.ToolCalls)
		case provider.RoleTool:
			stats.ToolMessages++
		}
	}
	return stats
}

func recentActivities(entries []activityEntry, sessionID string, limit int) []activityEntry {
	if limit <= 0 || len(entries) == 0 {
		return nil
	}

	filtered := collectRecentActivities(entries, sessionID, limit)
	if len(filtered) == 0 && sessionID != "" {
		filtered = collectRecentActivities(entries, "", limit)
	}
	reverseActivities(filtered)
	return filtered
}

func collectRecentActivities(entries []activityEntry, sessionID string, limit int) []activityEntry {
	result := make([]activityEntry, 0, limit)
	for idx := len(entries) - 1; idx >= 0; idx-- {
		entry := entries[idx]
		if sessionID != "" && entry.SessionID != sessionID {
			continue
		}
		result = append(result, entry)
		if len(result) == limit {
			break
		}
	}
	return result
}

func reverseActivities(entries []activityEntry) {
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
}

func toolNames(calls []provider.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}

	names := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			name = "unknown_tool"
		}
		names = append(names, name)
	}

	return strings.Join(names, ", ")
}

func summarizeToolResult(result tools.Result) string {
	summary := trimMultiline(result.Content)
	if summary == "" {
		if result.IsError {
			return "tool returned an error"
		}
		return "tool completed"
	}

	return truncateVisual(summary, 58)
}

func trimMultiline(value string) string {
	parts := strings.Fields(strings.TrimSpace(value))
	return strings.Join(parts, " ")
}

func formatClock(t time.Time) string {
	if t.IsZero() {
		return "--:--"
	}
	return t.Format("15:04")
}

func formatClockWithSeconds(t time.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.Format("15:04:05")
}

func compactSessionSubtitle(summary runtime.SessionSummary) string {
	return fmt.Sprintf("%d msgs  %s", summary.MessageCount, formatClock(summary.UpdatedAt))
}

func limitLines(value string, limit int) string {
	if limit <= 0 {
		return ""
	}

	lines := strings.Split(value, "\n")
	if len(lines) <= limit {
		return value
	}

	if limit == 1 {
		return "..."
	}
	return strings.Join(append(lines[:limit-1], "..."), "\n")
}

func toolStateKey(sessionID, callID string) string {
	return sessionID + ":" + callID
}

func truncateVisual(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= limit {
		return value
	}
	if limit <= 3 {
		return ansi.Truncate(value, limit, "")
	}
	return ansi.Truncate(value, limit, "...")
}

func computeLayout(width, height int) layoutMetrics {
	if width <= 0 || height <= 0 {
		return layoutMetrics{}
	}

	headerHeight := min(4, max(3, height/12))
	footerHeight := 1
	composerHeight := min(8, max(5, height/5))
	bodyHeight := height - headerHeight - composerHeight - footerHeight
	if bodyHeight < 8 {
		bodyHeight = max(4, height-headerHeight-footerHeight-4)
		composerHeight = max(4, height-headerHeight-footerHeight-bodyHeight)
	}

	layout := layoutMetrics{
		headerRect: rect{X: 0, Y: 0, Width: width, Height: headerHeight},
		bodyRect: rect{
			X:      0,
			Y:      headerHeight,
			Width:  width,
			Height: max(1, bodyHeight),
		},
		composerRect: rect{
			X:      0,
			Y:      headerHeight + bodyHeight,
			Width:  width,
			Height: max(4, composerHeight),
		},
		footerRect: rect{
			X:      0,
			Y:      height - footerHeight,
			Width:  width,
			Height: footerHeight,
		},
	}

	if width >= 118 {
		layout.mode = layoutWide
		runtimeWidth := min(36, max(28, width/4))
		conversationWidth := max(40, width-runtimeWidth-1)
		layout.conversationRect = rect{
			X:      0,
			Y:      layout.bodyRect.Y,
			Width:  conversationWidth,
			Height: layout.bodyRect.Height,
		}
		layout.runtimeRect = rect{
			X:      conversationWidth + 1,
			Y:      layout.bodyRect.Y,
			Width:  width - conversationWidth - 1,
			Height: layout.bodyRect.Height,
		}
	} else {
		layout.mode = layoutStacked
		runtimeHeight := min(12, max(8, layout.bodyRect.Height/3))
		conversationHeight := max(8, layout.bodyRect.Height-runtimeHeight-1)
		runtimeHeight = max(6, layout.bodyRect.Height-conversationHeight-1)
		layout.conversationRect = rect{
			X:      0,
			Y:      layout.bodyRect.Y,
			Width:  width,
			Height: conversationHeight,
		}
		layout.runtimeRect = rect{
			X:      0,
			Y:      layout.bodyRect.Y + conversationHeight + 1,
			Width:  width,
			Height: runtimeHeight,
		}
	}

	sidebarWidth := min(36, max(28, width/3))
	layout.sidebarRect = rect{
		X:      1,
		Y:      headerHeight,
		Width:  min(sidebarWidth, max(18, width-4)),
		Height: max(8, height-headerHeight-footerHeight),
	}
	return layout
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
