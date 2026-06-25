package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

func TestAgentStreamRendersEntryTypes(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	viewState := state.NewViewState()
	viewState.Layout.Width = 120
	viewState.Layout.Height = 40
	viewState.Stream = []state.StreamEntry{
		{ID: "m", Type: "message", Content: "hello", Timestamp: now},
		{ID: "ts", Type: "tool_start", ToolName: "read_file", Content: "main.go", Timestamp: now},
		{ID: "to", Type: "tool_output", Content: "12: package main", Timestamp: now},
		{ID: "te", Type: "tool_end", ToolName: "read_file", Timestamp: now},
		{ID: "p", Type: "permission", Content: "allow tool.write_file? [y/n]", Timestamp: now},
		{ID: "q", Type: "question", Content: "choose: 1. A  2. B", Timestamp: now},
		{ID: "s", Type: "status", Content: "connected to ghost-console", Timestamp: now},
		{ID: "e", Type: "error", Content: "connection refused", Timestamp: now},
	}

	view := NewAgentStream(viewState).View()
	for _, want := range []string{
		"hello",
		theme.StreamPrefix("tool_start") + " tool.read_file",
		"main.go",
		theme.AccentBar() + " 12: package main",
		theme.StreamPrefix("tool_end") + " tool.read_file",
		theme.StreamPrefix("permission_requested") + " allow tool.write_file? [y/n]",
		theme.Separator() + " choose: 1. A  2. B",
		theme.StreamPrefix("status") + " connected to ghost-console",
		theme.StreamPrefix("error") + " connection refused",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "┌") || strings.Contains(view, "┐") || strings.Contains(view, "└") || strings.Contains(view, "┘") {
		t.Fatalf("View() contains border rune:\n%s", view)
	}
}

func TestAgentStreamManualAndAutoScroll(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 80
	viewState.Layout.Height = 12
	viewState.Stream = numberedEntries(20)
	stream := NewAgentStream(viewState)

	if !viewState.Layout.AutoScroll {
		t.Fatal("AutoScroll default = false, want true")
	}
	_, _ = stream.Update(keyMsg("k"))
	if viewState.Layout.AutoScroll {
		t.Fatal("AutoScroll after k = true, want false")
	}
	if viewState.Layout.ScrollOffset == 0 {
		t.Fatal("ScrollOffset after k = 0, want > 0")
	}
	_, _ = stream.Update(keyMsg("j"))
	if !viewState.Layout.AutoScroll || viewState.Layout.ScrollOffset != 0 {
		t.Fatalf("after j at bottom AutoScroll=%t offset=%d, want true/0", viewState.Layout.AutoScroll, viewState.Layout.ScrollOffset)
	}
	_, _ = stream.Update(keyMsg("g"))
	if viewState.Layout.AutoScroll || viewState.Layout.ScrollOffset == 0 {
		t.Fatalf("after g AutoScroll=%t offset=%d, want false/>0", viewState.Layout.AutoScroll, viewState.Layout.ScrollOffset)
	}
	_, _ = stream.Update(keyMsg("G"))
	if !viewState.Layout.AutoScroll || viewState.Layout.ScrollOffset != 0 {
		t.Fatalf("after G AutoScroll=%t offset=%d, want true/0", viewState.Layout.AutoScroll, viewState.Layout.ScrollOffset)
	}
}

// TestAgentStreamFullPageScroll 覆盖 Ctrl+F/Ctrl+B 整页翻页。
func TestAgentStreamFullPageScroll(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 80
	viewState.Layout.Height = 12
	viewState.Stream = numberedEntries(40)
	stream := NewAgentStream(viewState)

	// Ctrl+B 在底部(自动滚动, offset=0)上翻一页 → offset 增加
	_, _ = stream.Update(keyMsg("ctrl+b"))
	if viewState.Layout.AutoScroll {
		t.Fatal("ctrl+b should disable AutoScroll")
	}
	offsetAfterB := viewState.Layout.ScrollOffset
	if offsetAfterB == 0 {
		t.Fatal("ctrl+b should increase ScrollOffset")
	}

	// Ctrl+F 下翻一页 → offset 减少
	_, _ = stream.Update(keyMsg("ctrl+f"))
	if viewState.Layout.ScrollOffset >= offsetAfterB {
		t.Fatalf("ctrl+f should decrease offset: before=%d after=%d", offsetAfterB, viewState.Layout.ScrollOffset)
	}

	// 空流下 ctrl+f/b 不崩溃
	emptyVS := state.NewViewState()
	emptyVS.Layout.Width = 80
	emptyVS.Layout.Height = 10
	emptyStream := NewAgentStream(emptyVS)
	_, _ = emptyStream.Update(keyMsg("ctrl+f"))
	_, _ = emptyStream.Update(keyMsg("ctrl+b"))
}

func TestAgentStreamWidthIsSafe(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 40
	viewState.Layout.Height = 10
	viewState.Stream = []state.StreamEntry{{ID: "long", Type: "message", Content: "这是一段很长很长的中英文 mixed content that must not wrap"}}

	for index, line := range strings.Split(NewAgentStream(viewState).View(), "\n") {
		if width := theme.DisplayWidth(line); width > 39 {
			t.Fatalf("line %d width = %d, want <= 39: %q", index, width, line)
		}
	}
}

func TestAgentStreamLargeStreamRenderBudget(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 100
	viewState.Layout.Height = 30
	viewState.Stream = numberedEntries(1200)
	stream := NewAgentStream(viewState)

	start := time.Now()
	view := stream.View()
	if elapsed := time.Since(start); elapsed > 16*time.Millisecond {
		t.Fatalf("View() elapsed = %v, want <= 16ms", elapsed)
	}
	if !strings.Contains(view, "line 1199") {
		t.Fatalf("View() does not include tail entry:\n%s", view)
	}
}

func TestAgentStreamTimestampGap(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 100
	viewState.Layout.Height = 20
	first := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	viewState.Stream = []state.StreamEntry{
		{ID: "one", Type: "message", Content: "one", Timestamp: first},
		{ID: "two", Type: "message", Content: "two", Timestamp: first.Add(6 * time.Minute)},
	}
	view := NewAgentStream(viewState).View()
	if !strings.Contains(view, "12:06") {
		t.Fatalf("View() missing timestamp gap:\n%s", view)
	}
}

func TestAgentStreamMultilineMessageAlignsUnderLabel(t *testing.T) {
	viewState := state.NewViewState()
	viewState.Layout.Width = 80
	viewState.Layout.Height = 20
	viewState.Stream = []state.StreamEntry{
		{ID: "m", Type: "message", Content: "第一行\n第二行内容\n第三行", Metadata: map[string]any{"role": "user"}},
	}
	stream := NewAgentStream(viewState)

	lines := stream.renderMessage(viewState.Stream[0])
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	if len(plain) != 3 {
		t.Fatalf("rendered lines = %d, want 3: %v", len(plain), plain)
	}
	// 首行 = "  you " + 内容（you 占 3 列，前后各 2/1 空格，共 6 列）。
	wantIndent := theme.DisplayWidth("  you ")
	if !strings.HasPrefix(plain[0], "  you ") {
		t.Fatalf("line 0 = %q, want prefix %q", plain[0], "  you ")
	}
	// 续行必须是 wantIndent 个前导空格，使正文与首行正文逐列对齐。
	for i, line := range plain[1:] {
		leading := len(line) - len(strings.TrimLeft(line, " "))
		if leading != wantIndent {
			t.Fatalf("continuation line %d leading = %d, want %d: %q", i+1, leading, wantIndent, line)
		}
	}
}

func numberedEntries(count int) []state.StreamEntry {
	entries := make([]state.StreamEntry, 0, count)
	for i := 0; i < count; i++ {
		entries = append(entries, state.StreamEntry{
			ID:      fmt.Sprintf("entry-%d", i),
			Type:    "message",
			Content: fmt.Sprintf("line %d", i),
		})
	}
	return entries
}

func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// TestEntryLineMapCorrectness 验证 entryLineMap 在含分隔/时间戳/多行场景下的行号正确性。
func TestEntryLineMapCorrectness(t *testing.T) {
	// 场景：3 个 entry。
	// entry0: message "a"（1 行）
	// entry1: message "b\nb2"（多行，2 行）—— 同类型不触发 shouldSeparate
	// entry2: tool_start（类型变化触发 shouldSeparate 空行 + 1 行）
	t1 := time.Now()
	t2 := t1.Add(10 * time.Minute) // >5min 触发 shouldShowTimestamp
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 24
	vs.Stream = []state.StreamEntry{
		{ID: "0", Type: "message", Content: "a", Timestamp: t1},
		{ID: "1", Type: "message", Content: "b\nb2", Timestamp: t1},
		{ID: "2", Type: "tool_start", Content: "tool", Timestamp: t2},
	}
	stream := NewAgentStream(vs)
	got := stream.entryLineMap()
	// 推算：entry0 起始行0 占1行→line=1；entry1 同类型无分隔无时间戳 起始行1 占2行→line=3；
	//       entry2 类型变化 shouldSeparate(空行)+shouldShowTimestamp(时间戳行) 起始行=3+1+1=5
	want := []int{0, 1, 5}
	if len(got) != len(want) {
		t.Fatalf("lineMap len=%d, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("lineMap[%d]=%d, want %d (full=%v)", i, got[i], w, got)
		}
	}
}

// TestEntryLineMapEmptyAndSingle 边界：空 stream 与单 entry。
func TestEntryLineMapEmptyAndSingle(t *testing.T) {
	vs := state.NewViewState()
	stream := NewAgentStream(vs)
	if got := stream.entryLineMap(); len(got) != 0 {
		t.Fatalf("empty stream lineMap len=%d, want 0", len(got))
	}
	vs2 := state.NewViewState()
	vs2.Stream = []state.StreamEntry{{ID: "0", Type: "message", Content: "x"}}
	stream2 := NewAgentStream(vs2)
	got := stream2.entryLineMap()
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("single entry lineMap=%v, want [0]", got)
	}
}

// TestScrollToEntrySetsLineOffset 验证 ScrollToEntry 在内容超出一屏时计算的具体渲染行偏移。
// 用 numberedEntries(20) 单行同类型 entry：entry 索引 == 渲染行号，便于精确断言。
func TestScrollToEntrySetsLineOffset(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 10 // visibleLineCount 较小，确保 totalLines(20) > visible
	vs.Stream = numberedEntries(20)
	stream := NewAgentStream(vs)
	visible := stream.visibleLineCount()
	totalLines := 20 // 单行同类型 entry，渲染行数 = entry 数
	// 跳到 entry 5（起始渲染行 5），offset = (totalLines - visible) - targetLine
	stream.ScrollToEntry(5)
	expectedOffset := (totalLines - visible) - 5
	if expectedOffset < 0 {
		expectedOffset = 0
	}
	if vs.Layout.ScrollOffset != expectedOffset {
		t.Fatalf("ScrollToEntry(5) offset=%d, want %d (totalLines=%d visible=%d)",
			vs.Layout.ScrollOffset, expectedOffset, totalLines, visible)
	}
	if vs.Layout.AutoScroll {
		t.Fatal("ScrollToEntry should disable AutoScroll")
	}
}

// TestScrollToEntryHeterogeneousVisible 端到端：异构 entry（多类型/时间戳/多行）下
// 跳转后目标可见。回归保护：旧代码用 entry 索引差，在异构场景会定位错误。
func TestScrollToEntryHeterogeneousVisible(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(10 * time.Minute)
	// 构造异构 stream：message + 多行 message + tool_start（触发分隔/时间戳/多行）
	entries := make([]state.StreamEntry, 0, 30)
	for i := 0; i < 28; i++ {
		entries = append(entries, state.StreamEntry{
			ID:        fmt.Sprintf("m-%d", i),
			Type:      "message",
			Content:   fmt.Sprintf("msg-%d", i),
			Timestamp: t1,
		})
	}
	// 末尾两个异构 entry，内容唯一便于断言
	entries = append(entries, state.StreamEntry{
		ID: "multi", Type: "message", Content: "multi\nline\ncontent", Timestamp: t2,
	})
	entries = append(entries, state.StreamEntry{
		ID: "tool", Type: "tool_start", Content: "tool.x", Timestamp: t2,
	})
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 10
	vs.Stream = entries
	stream := NewAgentStream(vs)
	// 跳到 "multi"（索引 28）——异构场景，旧代码会定位错误
	stream.ScrollToEntry(28)
	view := stream.View()
	if !strings.Contains(view, "multi") {
		t.Fatalf("heterogeneous: after ScrollToEntry(28), 'multi' not visible.\noffset=%d\nview:\n%s", vs.Layout.ScrollOffset, view)
	}
}

// TestScrollToEntryBoundaries 边界：负数/越界 no-op。
func TestScrollToEntryBoundaries(t *testing.T) {
	vs := state.NewViewState()
	vs.Stream = []state.StreamEntry{{ID: "0", Type: "message", Content: "a"}}
	stream := NewAgentStream(vs)
	prev := vs.Layout.ScrollOffset
	stream.ScrollToEntry(-1)
	if vs.Layout.ScrollOffset != prev {
		t.Fatal("ScrollToEntry(-1) should be no-op")
	}
	stream.ScrollToEntry(100)
	if vs.Layout.ScrollOffset != prev {
		t.Fatal("ScrollToEntry(overflow) should be no-op")
	}
	vs2 := state.NewViewState()
	stream2 := NewAgentStream(vs2)
	stream2.ScrollToEntry(0) // 空 stream 不崩溃
}

// TestScrollToEntryTargetVisible 端到端：跳转后目标 entry 内容出现在 View() 可见行中。
func TestScrollToEntryTargetVisible(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 10 // 视口小
	vs.Stream = numberedEntries(20)
	stream := NewAgentStream(vs)
	stream.ScrollToEntry(15)
	view := stream.View()
	if !strings.Contains(view, "line 15") {
		t.Fatalf("after ScrollToEntry(15), target not visible.\noffset=%d\nview:\n%s", vs.Layout.ScrollOffset, view)
	}
}

// TestVirtualEntriesLargeStreamAfterScroll 回归保护：>1000 条异构 stream 下，ScrollToEntry 后
// virtualEntries 返回的窗口包含目标 entry，且连续两帧渲染不振荡（ScrollOffset 不被
// 局部 clamp 破坏）。用异构 entry（message/tool_start 交替触发 shouldSeparate）确保
// 旧 bug 代码（entry 索引差 + 局部 clamp）无法通过。
func TestVirtualEntriesLargeStreamAfterScroll(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 24
	// 异构 entry：message/tool_start 交替，触发 shouldSeparate 分隔空行，
	// 使 entry 索引 ≠ 渲染行号，旧代码会定位错误。
	entries := make([]state.StreamEntry, 0, 1200)
	for i := 0; i < 1200; i++ {
		tp := "message"
		if i%2 == 0 {
			tp = "tool_start"
		}
		entries = append(entries, state.StreamEntry{
			ID:      fmt.Sprintf("entry-%d", i),
			Type:    tp,
			Content: fmt.Sprintf("content-%d", i),
		})
	}
	vs.Stream = entries
	stream := NewAgentStream(vs)
	// 跳到 entry 1000（远端，异构区域）
	stream.ScrollToEntry(1000)
	// 1) 窗口必须包含 entry 1000
	virtual := stream.virtualEntries()
	found := false
	for _, e := range virtual {
		if e.ID == "entry-1000" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("virtualEntries after ScrollToEntry(1000) missing target: window=[%s..%s] size=%d",
			virtual[0].ID, virtual[len(virtual)-1].ID, len(virtual))
	}
	// 2) 连续两帧 View 不振荡（ScrollOffset 不被局部 clamp 破坏）
	v1 := stream.View()
	v2 := stream.View()
	if v1 != v2 {
		t.Fatal("OSCILLATION: consecutive View() frames differ after ScrollToEntry (ScrollOffset corrupted by local clamp)")
	}
	// 3) 目标内容在视口可见
	if !strings.Contains(v1, "content-1000") {
		t.Fatal("target content-1000 not visible after ScrollToEntry")
	}
}

// TestEntryIndexAtLineCorrectness 验证渲染行偏移到 entry 索引的映射。
func TestEntryIndexAtLineCorrectness(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 24
	vs.Stream = numberedEntries(5) // lineMap=[0,1,2,3,4] totalLines=5
	stream := NewAgentStream(vs)
	lineMap := stream.entryLineMap()
	totalLines := lineMap[len(lineMap)-1] + 1 // 单行 entry，最后起始行4 + 1行 = 5
	if got := stream.entryIndexAtLine(0, vs.Stream, lineMap, totalLines); got != 5 {
		t.Fatalf("entryIndexAtLine(0)=%d want 5", got)
	}
	if got := stream.entryIndexAtLine(5, vs.Stream, lineMap, totalLines); got != 0 {
		t.Fatalf("entryIndexAtLine(5)=%d want 0", got)
	}
	if got := stream.entryIndexAtLine(2, vs.Stream, lineMap, totalLines); got != 4 {
		t.Fatalf("entryIndexAtLine(2)=%d want 4", got)
	}
	vs2 := state.NewViewState()
	stream2 := NewAgentStream(vs2)
	if got := stream2.entryIndexAtLine(0, vs2.Stream, stream2.entryLineMap(), 0); got != 0 {
		t.Fatalf("empty stream entryIndexAtLine=%d want 0", got)
	}
}
