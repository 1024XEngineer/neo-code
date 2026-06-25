package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
)

// ---- ConfirmOverlay ----

func TestConfirmOverlayLifecycle(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 40
	vs.Layout.Height = 12
	vs.Confirm = state.ConfirmState{Title: "Delete", Message: "sure?"}
	c := NewConfirmOverlay(vs)

	if c.Init() != nil {
		t.Fatal("Init should return nil")
	}
	// y / enter -> ConfirmYesMsg
	for _, m := range []tea.Msg{keyMsg("y"), keyType(tea.KeyEnter)} {
		_, cmd := c.Update(m)
		if _, ok := cmd().(ConfirmYesMsg); !ok {
			t.Fatalf("%v should emit ConfirmYesMsg", m)
		}
	}
	// n / esc / ctrl+c -> ConfirmNoMsg
	for _, m := range []tea.Msg{keyMsg("n"), keyType(tea.KeyEsc), keyType(tea.KeyCtrlC)} {
		_, cmd := c.Update(m)
		if _, ok := cmd().(ConfirmNoMsg); !ok {
			t.Fatalf("%v should emit ConfirmNoMsg", m)
		}
	}
	// 其它键与非按键 -> nil
	if _, cmd := c.Update(keyMsg("z")); cmd != nil {
		t.Fatal("unrelated key should return nil")
	}
	if _, cmd := c.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("non-key msg should return nil")
	}
	if view := c.View(); !strings.Contains(view, "Delete") {
		t.Fatalf("view missing title: %q", view)
	}
}

// ---- HelpOverlay ----

func TestHelpOverlayLifecycle(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 60
	vs.Layout.Height = 24
	vs.Overlay.Active = state.OverlayHelp
	h := NewHelpOverlay(vs)

	if h.Init() != nil {
		t.Fatal("Init should return nil")
	}
	// esc/ctrl+c/q/? 关闭浮层
	for _, m := range []tea.Msg{keyType(tea.KeyEsc), keyType(tea.KeyCtrlC), keyMsg("q"), keyMsg("?")} {
		vs.Overlay.Active = state.OverlayHelp
		_, _ = h.Update(m)
		if vs.Overlay.Active != "" {
			t.Fatalf("%v should close help overlay", m)
		}
	}
	// 其它键不关闭
	vs.Overlay.Active = state.OverlayHelp
	_, _ = h.Update(keyMsg("x"))
	if vs.Overlay.Active != "help" {
		t.Fatal("unrelated key should not close help")
	}
	// 非 KeyMsg
	if _, cmd := h.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("non-key should be nil")
	}
	if view := h.View(); !strings.Contains(view, "Keyboard Shortcuts") {
		t.Fatalf("help view missing title: %q", view)
	}
	if padRight("ab", 5) != "ab   " {
		t.Fatal("padRight wrong")
	}
	if padRight("abcdef", 3) != "abcdef" {
		t.Fatal("padRight should not truncate")
	}
}

// ---- AmbientStatus ----

func TestAmbientStatusVariants(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	for _, phase := range []string{
		state.RuntimePhaseRunning,
		state.RuntimePhaseWaitingPermission,
		state.RuntimePhaseWaitingUser,
		state.RuntimePhaseError,
		state.RuntimePhaseCancelled,
		state.RuntimePhaseIdle,
	} {
		vs.Runtime.Phase = phase
		s := NewAmbientStatus(vs)
		if s.Init() != nil {
			t.Fatal("Init should be nil")
		}
		if _, cmd := s.Update(tea.KeyMsg{}); cmd != nil {
			t.Fatal("Update should be nil")
		}
		if v := s.View(); v == "" {
			t.Fatalf("phase %s produced empty view", phase)
		}
	}
	// 模型回退
	vs.Gateway.ActiveModel = ""
	if v := NewAmbientStatus(vs).View(); !strings.Contains(v, "model:-") {
		t.Fatalf("model fallback missing: %q", v)
	}
	// 活动会话标题
	vs.Gateway.ActiveSess = &gateway.SessionSummary{Title: "My Session"}
	if v := NewAmbientStatus(vs).View(); !strings.Contains(v, "My Session") {
		t.Fatalf("session title missing: %q", v)
	}
}

// ---- SoftInspector ----

func TestSoftInspectorVariants(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 60
	vs.Layout.Height = 20
	vs.Layout.InspectorWidth = 30

	// 隐藏 -> 空
	vs.Layout.ShowInspector = false
	if NewSoftInspector(vs).View() != "" {
		t.Fatal("hidden inspector should render empty")
	}

	vs.Layout.ShowInspector = true
	insp := NewSoftInspector(vs)
	if insp.Init() != nil {
		t.Fatal("Init should be nil")
	}
	if _, cmd := insp.Update(tea.KeyMsg{}); cmd != nil {
		t.Fatal("Update should be nil")
	}
	if v := insp.View(); !strings.Contains(v, "Soft Inspector") {
		t.Fatalf("missing title: %q", v)
	}

	// 无会话
	vs.Gateway.Sessions = nil
	insp.View()

	// 超过 3 个会话 -> "+N more"
	vs.Gateway.Sessions = []gateway.SessionSummary{
		{ID: "1", Title: "one"}, {ID: "2", Title: "two"}, {ID: "3", Title: "three"},
		{ID: "4", Title: "four"}, {ID: "5", Title: "five"},
	}
	// 活跃工具（start 未 end）+ 文件（end 带 delete 用 DiffDel）
	vs.Stream = []state.StreamEntry{
		{Type: "tool_start", ToolName: "bash"},
		{Type: "tool_end", ToolName: "write", Content: "wrote internal/main.go"},
		{Type: "tool_end", ToolName: "rm", Content: "deleted a/b/c.yaml"},
	}
	view := insp.View()
	if !strings.Contains(view, "+2 more") {
		t.Fatalf("expected '+2 more' for >3 sessions: %q", view)
	}

	// token total != 0 分支
	vs.Runtime.Tokens = state.TokenUsage{Input: 1, Output: 2, Total: 99}
	insp.View()
}

func TestExtractFilePath(t *testing.T) {
	cases := map[string]string{
		"wrote internal/main.go": "internal/main.go",
		"changed config.yaml":    "config.yaml",
		"deleted a/b/c.txt":      "a/b/c.txt",
		"no path here":           "",
		"plainword":              "",
	}
	for in, want := range cases {
		if got := extractFilePath(in); got != want {
			t.Fatalf("extractFilePath(%q)=%q, want %q", in, got, want)
		}
	}
}

// ---- AgentStream 滚动与辅助 ----

func TestAgentStreamScrollAndHelpers(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 12
	vs.Stream = numberedEntries(20)
	s := NewAgentStream(vs)

	if s.Init() != nil {
		t.Fatal("Init should be nil")
	}
	// k/up 向上滚（offset 增加）
	_, _ = s.Update(keyMsg("k"))
	if vs.Layout.ScrollOffset == 0 {
		t.Fatal("k should increase scroll offset")
	}
	// ctrl+u 半页向上
	before := vs.Layout.ScrollOffset
	_, _ = s.Update(keyType(tea.KeyCtrlU))
	if vs.Layout.ScrollOffset <= before {
		t.Fatal("ctrl+u should scroll further up")
	}
	// g 顶（max offset）
	_, _ = s.Update(keyMsg("g"))
	if vs.Layout.ScrollOffset == 0 {
		t.Fatal("g should jump to top")
	}
	// G 底（offset 0，AutoScroll true）
	_, _ = s.Update(keyMsg("G"))
	if vs.Layout.ScrollOffset != 0 || !vs.Layout.AutoScroll {
		t.Fatal("G should jump to bottom")
	}
	// j/down 与 ctrl+d 向下方向（先上移再下移）
	_, _ = s.Update(keyMsg("k"))
	_, _ = s.Update(keyMsg("j"))
	_, _ = s.Update(keyType(tea.KeyCtrlD))

	// halfPageSize >= 1
	if s.halfPageSize() < 1 {
		t.Fatal("halfPageSize should be >= 1")
	}
	// headerText：手动滚动分支
	vs.Layout.AutoScroll = false
	if !strings.Contains(s.headerText(), "scroll:") {
		t.Fatalf("manual headerText should show scroll: %q", s.headerText())
	}
	// renderToolContent 各分支
	renderToolContent("")
	renderToolContent("path/to/file.go")
	renderToolContent("config.json")
	renderToolContent("plainword")
	// clampScroll 边界
	if clampScroll(-1, 5) != 0 || clampScroll(10, 5) != 5 || clampScroll(3, 5) != 3 {
		t.Fatal("clampScroll bounds wrong")
	}
}
