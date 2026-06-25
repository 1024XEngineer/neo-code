package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

const (
	streamHeaderRows      = 1
	streamReservedRows    = 7
	streamTimeGap         = 5 * time.Minute
	streamVirtualOverscan = 20
)

// AgentStream 渲染 Agent 行为流，包括消息、工具调用和状态条目。
type AgentStream struct {
	state *state.ViewState
}

var _ tea.Model = (*AgentStream)(nil)

// NewAgentStream 创建 Agent Stream 组件。
func NewAgentStream(viewState *state.ViewState) *AgentStream {
	return &AgentStream{state: viewState}
}

// Init 不启动额外命令，组件只读取共享 ViewState。
func (c *AgentStream) Init() tea.Cmd {
	return nil
}

// Update 处理 Agent Stream 的滚动按键，不维护冗余业务状态。
func (c *AgentStream) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	maxOffset := c.maxScrollOffset()
	switch key.String() {
	case "k", "up":
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset+1, maxOffset)
		c.state.Layout.AutoScroll = false
	case "j", "down":
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset-1, maxOffset)
		c.state.Layout.AutoScroll = c.state.Layout.ScrollOffset == 0
	case "g":
		c.state.Layout.ScrollOffset = maxOffset
		c.state.Layout.AutoScroll = false
	case "G":
		c.state.Layout.ScrollOffset = 0
		c.state.Layout.AutoScroll = true
	case "ctrl+u":
		halfPage := c.halfPageSize()
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset+halfPage, maxOffset)
		c.state.Layout.AutoScroll = false
	case "ctrl+d":
		halfPage := c.halfPageSize()
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset-halfPage, maxOffset)
		c.state.Layout.AutoScroll = c.state.Layout.ScrollOffset == 0
	case "ctrl+b":
		// 整页上翻，步长为可见行数（vim Ctrl+B 语义）。
		fullPage := c.visibleLineCount()
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset+fullPage, maxOffset)
		c.state.Layout.AutoScroll = false
	case "ctrl+f":
		// 整页下翻，步长为可见行数（vim Ctrl+F 语义）。
		fullPage := c.visibleLineCount()
		c.state.Layout.ScrollOffset = clampScroll(c.state.Layout.ScrollOffset-fullPage, maxOffset)
		c.state.Layout.AutoScroll = c.state.Layout.ScrollOffset == 0
	}
	return c, nil
}

// View 渲染 Agent Stream，按滚动窗口选择可见条目并进行宽度安全截断。
//
// 渲染管线（虚拟化坐标转换）：
//  1. renderAllEntriesWithWindow → 得到窗口内渲染行 + 窗口元信息（winEndLine/totalLines）
//  2. computeLocalOffset 把全局 ScrollOffset 转为窗口内局部偏移
//  3. visibleLines 用局部偏移裁剪窗口行为可见行
//
// 该转换消除"同一 ScrollOffset 在全流与窗口两个坐标系混用"导致的跳转错位与帧间振荡。
func (c *AgentStream) View() string {
	width := c.streamWidth()
	lines := []string{theme.MutedStyle().Render(c.headerText())}
	rendered, winEndLine, totalLines := c.renderAllEntriesWithWindow()
	if len(rendered) == 0 {
		rendered = []string{
			theme.AccentStyle().Render("  " + theme.StatusSymbol(theme.PhaseRunning) + " 我可以帮你做什么？"),
			theme.MutedStyle().Render("  " + theme.StatusSymbol(theme.PhaseIdle) + " " + surfaceName),
		}
	}
	localOffset := c.computeLocalOffset(winEndLine, totalLines)
	lines = append(lines, c.visibleLines(rendered, localOffset)...)
	content := strings.Join(lines, "\n")
	if width > 0 {
		return fitBlock(content, width, true)
	}
	return content
}

// computeLocalOffset 将全局 ScrollOffset（全流渲染行偏移）转换为虚拟窗口内的局部偏移。
//
// 公式：localOffset = winEndLine + ScrollOffset - totalLines。
// 非虚拟化（≤1000）或窗口贴底时 winEndLine==totalLines，退化为 localOffset==ScrollOffset。
// AutoScroll 时强制 0（贴底显示最新）。纯函数，无副作用，不修改 ScrollOffset。
func (c *AgentStream) computeLocalOffset(winEndLine, totalLines int) int {
	if c.state.Layout.AutoScroll {
		return 0
	}
	offset := winEndLine + c.state.Layout.ScrollOffset - totalLines
	if offset < 0 {
		return 0
	}
	return offset
}

// headerText 渲染 Stream 标题，并在手动滚动时显示偏移量。
func (c *AgentStream) headerText() string {
	if c.state.Layout.AutoScroll {
		return "Agent Stream"
	}
	return fmt.Sprintf("Agent Stream  scroll:%d", c.state.Layout.ScrollOffset)
}

// streamWidth 根据布局断点计算 Agent Stream 可用宽度。
func (c *AgentStream) streamWidth() int {
	width := c.state.Layout.Width
	if width >= 100 && c.state.Layout.ShowInspector {
		return width - c.state.Layout.InspectorWidth - 3
	}
	return width
}

// visibleLineCount 根据终端高度估算可展示的流行数。
func (c *AgentStream) visibleLineCount() int {
	height := c.state.Layout.Height
	if height <= 0 {
		return 8
	}
	limit := height - streamReservedRows - streamHeaderRows
	if limit < 4 {
		return 4
	}
	return limit
}

// halfPageSize 返回半页滚动所需的行数，至少为 1。
func (c *AgentStream) halfPageSize() int {
	h := c.visibleLineCount() / 2
	if h < 1 {
		return 1
	}
	return h
}

// maxScrollOffset 计算全流渲染内容允许的最大手动滚动偏移（基于全流总行数，非虚拟窗口）。
//
// 必须基于全流（entryLineMap）而非 virtualEntries 的窗口，否则 visibleLines 的
// clamp 会用窗口局部值破坏全局 ScrollOffset，导致搜索跳转后帧间振荡。
func (c *AgentStream) maxScrollOffset() int {
	entries := c.state.Stream
	if len(entries) == 0 {
		return 0
	}
	lineMap := c.entryLineMap()
	totalLines := lineMap[len(lineMap)-1] + len(c.renderEntry(entries[len(entries)-1]))
	visible := c.visibleLineCount()
	if totalLines <= visible {
		return 0
	}
	return totalLines - visible
}

// visibleLines 根据局部偏移截取窗口内的可见行。
//
// 接收 localOffset（窗口内偏移，由 computeLocalOffset 从全局 ScrollOffset 转换），
// 不再直接读或写 ScrollOffset，消除帧间振荡。AutoScroll 由调用方（computeLocalOffset）
// 处理为 localOffset=0。
func (c *AgentStream) visibleLines(lines []string, localOffset int) []string {
	visible := c.visibleLineCount()
	if len(lines) <= visible {
		return lines
	}
	maxOffset := len(lines) - visible
	if localOffset > maxOffset {
		localOffset = maxOffset
	}
	if localOffset < 0 {
		localOffset = 0
	}
	end := len(lines) - localOffset
	start := end - visible
	if start < 0 {
		start = 0
	}
	return lines[start:end]
}

// renderAllEntriesWithWindow 渲染虚拟窗口内的 entry 为行集合，并返回窗口元信息
// （winEndLine=窗口最后一个 entry 之后的全局渲染行号，totalLines=全流总渲染行数），
// 供 View 经 computeLocalOffset 做全局→局部坐标转换。
//
// entryLineMap 仅在此计算一次并下传给 virtualEntriesWithWindow/entryIndexAtLine，
// 避免单次渲染重复 O(n) 遍历。
func (c *AgentStream) renderAllEntriesWithWindow() ([]string, int, int) {
	entries := c.state.Stream
	lineMap := c.entryLineMap()
	totalLines := 0
	if len(entries) > 0 {
		totalLines = lineMap[len(lineMap)-1] + len(c.renderEntry(entries[len(entries)-1]))
	}
	winEntries, winStartIdx := c.virtualEntriesWithWindow(lineMap, totalLines)
	if len(winEntries) == 0 {
		return nil, totalLines, totalLines
	}
	lines := make([]string, 0, len(winEntries)*2)
	var previous *state.StreamEntry
	// 窗口首个 entry 的前导上下文（分隔/时间戳）需与全流渲染一致，故用窗口起始的
	// 前一个 entry（若存在）判定。
	if winStartIdx > 0 {
		prev := c.state.Stream[winStartIdx-1]
		previous = &prev
	}
	for index := range winEntries {
		entry := winEntries[index]
		if shouldSeparate(previous, &entry) && len(lines) > 0 {
			lines = append(lines, "")
		}
		if shouldShowTimestamp(previous, &entry) {
			lines = append(lines, c.renderTimestamp(entry.Timestamp))
		}
		lines = append(lines, c.renderEntry(entry)...)
		previous = &entry
	}
	// winEndLine = 窗口最后一个 entry 的起始行 + 其渲染行数
	lastIdx := winStartIdx + len(winEntries) - 1
	winEndLine := lineMap[lastIdx] + len(c.renderEntry(winEntries[len(winEntries)-1]))
	return lines, winEndLine, totalLines
}

// virtualEntriesWithWindow 返回虚拟化 entry 窗口与窗口起始 entry 的全局索引。
// 接收预计算的 lineMap 与 totalLines，避免重复 O(n)。
func (c *AgentStream) virtualEntriesWithWindow(lineMap []int, totalLines int) ([]state.StreamEntry, int) {
	entries := c.state.Stream
	if len(entries) <= 1000 {
		return entries, 0
	}
	visible := c.visibleLineCount() + streamVirtualOverscan*2
	if visible > len(entries) {
		visible = len(entries)
	}
	end := c.entryIndexAtLine(c.state.Layout.ScrollOffset, entries, lineMap, totalLines)
	if end > len(entries) {
		end = len(entries)
	}
	if end < visible {
		end = visible
	}
	start := end - visible
	if start < 0 {
		start = 0
	}
	return entries[start:end], start
}

// virtualEntries 保留为兼容入口（返回纯 entry 切片，供测试与可能的旧调用）。
func (c *AgentStream) virtualEntries() []state.StreamEntry {
	lineMap := c.entryLineMap()
	entries := c.state.Stream
	totalLines := 0
	if len(entries) > 0 {
		totalLines = lineMap[len(lineMap)-1] + len(c.renderEntry(entries[len(entries)-1]))
	}
	win, _ := c.virtualEntriesWithWindow(lineMap, totalLines)
	return win
}

// entryIndexAtLine 给定渲染行偏移（从底部往上数的行数），返回视口底部对应的 entry 索引。
//
// 利用 entryLineMap 的全局行号映射，找到"最后一个起始行号 <= (totalLines - offset)"
// 的 entry。offset=0（最新）时返回 len(entries)；offset 越大返回越小的索引（越靠前）。
// 仅 virtualEntries（>1000 entry）调用，冷路径。
func (c *AgentStream) entryIndexAtLine(offset int, entries []state.StreamEntry, lineMap []int, totalLines int) int {
	if len(entries) == 0 {
		return 0
	}
	// 视口底部对应的渲染行号
	bottomLine := totalLines - offset
	if bottomLine <= 0 {
		return 0
	}
	// 找最后一个 lineMap[i] <= bottomLine 的 entry 索引（线性扫描，冷路径可接受）
	idx := 0
	for i := 0; i < len(lineMap); i++ {
		if lineMap[i] <= bottomLine {
			idx = i
		} else {
			break
		}
	}
	// 视口底部落在 idx 这个 entry 的渲染区域内，窗口终点取 idx+1（含该 entry）
	return idx + 1
}

// entryLineMap 遍历全量 state.Stream（不走 virtualEntries，保证全局正确），
// 按 renderAllEntries 的同款逻辑（含 shouldSeparate 空行、shouldShowTimestamp
// 时间戳行、renderEntry 多行）累加每个 entry 的起始渲染行号。
//
// 返回的 slice 长度等于 len(state.Stream)，lineMap[i] 为第 i 个 entry 在完整
// 渲染输出中的起始行号。O(n)，在搜索跳转（ScrollToEntry）、maxScrollOffset、
// virtualEntries（>1000）等路径调用；5000 entry <1ms，可接受。
func (c *AgentStream) entryLineMap() []int {
	entries := c.state.Stream
	lineMap := make([]int, len(entries))
	line := 0
	var previous *state.StreamEntry
	for i := range entries {
		entry := entries[i]
		// 与 renderAllEntries 一致：先判定分隔空行与时间戳行
		if shouldSeparate(previous, &entry) && line > 0 {
			line++ // 空行分隔
		}
		if shouldShowTimestamp(previous, &entry) {
			line++ // 时间戳行
		}
		lineMap[i] = line
		line += len(c.renderEntry(entry)) // entry 占用的渲染行数
		previous = &entry
	}
	return lineMap
}

// ScrollToEntry 滚动 stream 使指定全局 entry 索引落到视口顶部附近。
//
// 通过 entryLineMap 计算 entry→渲染行映射，再换算为 ScrollOffset（其语义为
// "视口底部之上隐藏的渲染行数"，见 visibleLines 的 end := len(lines) - ScrollOffset），
// 消除原先直接用 entry 索引差的维度错配。跳转后关闭 AutoScroll。越界时 no-op。
// offset 经 maxScrollOffset clamp 保证全局合法性。
//
// 定位策略：视口顶部（与 less/grep 跳转一致，只读浏览场景用户跳转后向下阅读）。
func (c *AgentStream) ScrollToEntry(entryIndex int) {
	entries := c.state.Stream
	if entryIndex < 0 || entryIndex >= len(entries) {
		return
	}
	lineMap := c.entryLineMap()
	targetLine := lineMap[entryIndex]
	// 令目标行落在视口顶部：start = targetLine，而 start = (totalLines - offset) - visible，
	// 故 offset = totalLines - visible - targetLine。用 maxScrollOffset 做全局上界 clamp。
	maxOffset := c.maxScrollOffset()
	offset := maxOffset - targetLine
	if offset < 0 {
		offset = 0
	}
	c.state.Layout.AutoScroll = false
	c.state.Layout.ScrollOffset = offset
}

// renderEntry 将单条 StreamEntry 渲染为一种或多种 Ghost Console 行。
func (c *AgentStream) renderEntry(entry state.StreamEntry) []string {
	switch entry.Type {
	case "message":
		return c.renderMessage(entry)
	case "tool_start":
		return c.renderToolStart(entry)
	case "tool_end":
		return c.renderToolEnd(entry)
	case "tool_output":
		return c.renderToolOutput(entry)
	case "permission", "permission_requested":
		return c.renderPermission(entry)
	case "question", "ask_user_question", "user_question_requested":
		return c.renderQuestion(entry)
	case "status", "run_started", "run_finished", "run_cancelled", "phase_changed", "session_updated", "model_changed", "health_changed":
		return c.renderStatus(entry)
	case "error", "run_error", "gateway_offline":
		return c.renderError(entry)
	default:
		return c.renderStatus(entry)
	}
}

// renderMessage 渲染角色感知的消息正文，支持换行。
func (c *AgentStream) renderMessage(entry state.StreamEntry) []string {
	role := ""
	if v, ok := entry.Metadata["role"].(string); ok {
		role = v
	}
	var label string
	switch role {
	case "user":
		label = "  " + theme.InfoStyle().Render("you") + " "
	default: // "assistant" or empty
		label = "  " + theme.AccentStyle().Render("neo") + " "
	}
	// 续行缩进与首行标签（"  you "/"  neo "）的显示宽度一致，
	// 使多行消息的正文逐行对齐，不再出现第二行起缩进不足导致的错位。
	indent := strings.Repeat(" ", theme.DisplayWidth(label))
	text := entry.Content
	if text == "" {
		text = "-"
	}
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for index, part := range parts {
		if index == 0 {
			lines = append(lines, label+theme.BaseStyle().Render(part))
			continue
		}
		lines = append(lines, theme.BaseStyle().Render(indent+part))
	}
	return lines
}

// renderToolStart 渲染工具调用开始行。
func (c *AgentStream) renderToolStart(entry state.StreamEntry) []string {
	toolName := stringOrDash(entry.ToolName)
	content := entry.Content
	if content == "" {
		content = entry.ToolInput
	}
	line := theme.AccentStyle().Render("  "+theme.StreamPrefix("tool_start")+" tool."+toolName) +
		theme.MutedStyle().Render(" "+theme.Separator()+" ") +
		renderToolContent(content)
	return []string{line}
}

// renderToolEnd 渲染工具调用完成行。
func (c *AgentStream) renderToolEnd(entry state.StreamEntry) []string {
	line := theme.SuccessStyle().Render("  " + theme.StreamPrefix("tool_end") + " tool." + stringOrDash(entry.ToolName))
	if entry.Content != "" {
		line += theme.MutedStyle().Render(" " + theme.Separator() + " " + entry.Content)
	}
	return []string{line}
}

// renderToolOutput 渲染工具输出内容，使用缩进指示条。
func (c *AgentStream) renderToolOutput(entry state.StreamEntry) []string {
	content := entry.Content
	if content == "" {
		content = "-"
	}
	return renderWrappedLines(content, "  "+theme.AccentBar()+" ", theme.CodeBlockStyle())
}

// renderPermission 渲染权限请求状态行。
func (c *AgentStream) renderPermission(entry state.StreamEntry) []string {
	return []string{theme.WarningStyle().Render("  " + theme.StreamPrefix("permission_requested") + " " + stringOrDash(entry.Content))}
}

// renderQuestion 渲染 ask_user 问题行。
func (c *AgentStream) renderQuestion(entry state.StreamEntry) []string {
	return []string{theme.MutedStyle().Render("  " + theme.Separator() + " " + stringOrDash(entry.Content))}
}

// renderStatus 渲染普通状态变更行。
func (c *AgentStream) renderStatus(entry state.StreamEntry) []string {
	return []string{theme.MutedStyle().Render("  " + theme.StreamPrefix(entry.Type) + " " + stringOrDash(entry.Content))}
}

// renderError 渲染错误状态行。
func (c *AgentStream) renderError(entry state.StreamEntry) []string {
	return []string{theme.ErrorStyle().Render("  " + theme.StreamPrefix("error") + " " + stringOrDash(entry.Content))}
}

// renderTimestamp 渲染长时间间隔分隔时间戳。
func (c *AgentStream) renderTimestamp(timestamp time.Time) string {
	if timestamp.IsZero() {
		return ""
	}
	return theme.TimestampStyle().Render("  " + timestamp.Format("15:04"))
}

// renderWrappedLines 按内容换行拆分并为每行添加前缀和样式。
func renderWrappedLines(content string, prefix string, style interface{ Render(...string) string }) []string {
	parts := strings.Split(content, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, style.Render(prefix+part))
	}
	return lines
}

// renderToolContent 根据内容形态选择文件路径或普通弱文本样式。
func renderToolContent(content string) string {
	if content == "" {
		return theme.MutedStyle().Render("-")
	}
	if strings.Contains(content, "/") || strings.Contains(content, ".") {
		return theme.FilePathStyle().Render(content)
	}
	return theme.MutedStyle().Render(content)
}

// shouldSeparate 判断相邻条目之间是否需要空行分组。
func shouldSeparate(previous *state.StreamEntry, current *state.StreamEntry) bool {
	if previous == nil || current == nil {
		return false
	}
	if previous.Type == "tool_start" && (current.Type == "tool_end" || current.Type == "tool_output") {
		return false
	}
	if previous.Type == "tool_output" && current.Type == "tool_end" {
		return false
	}
	return previous.Type != current.Type
}

// shouldShowTimestamp 判断相邻条目之间是否需要时间戳分隔。
func shouldShowTimestamp(previous *state.StreamEntry, current *state.StreamEntry) bool {
	if previous == nil || current == nil || previous.Timestamp.IsZero() || current.Timestamp.IsZero() {
		return false
	}
	return current.Timestamp.Sub(previous.Timestamp) > streamTimeGap
}

// clampScroll 将滚动偏移限制在可见窗口允许范围内。
func clampScroll(value int, max int) int {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}

// stringOrDash 在占位布局中用短横线表示空值。
func stringOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
