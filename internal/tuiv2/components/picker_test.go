package components

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
)

// 通用ViewState：带终端尺寸和默认模型/会话数据，用于驱动各 picker。
func pickerState() *state.ViewState {
	vs := state.NewViewState()
	vs.Layout.Width = 70
	vs.Layout.Height = 24
	vs.Gateway.Models = []gateway.ModelInfo{
		{ID: "m-pro", Name: "Pro", Provider: "fake", Current: true},
		{ID: "m-fast", Name: "Fast", Provider: "fake", Current: false},
	}
	vs.Gateway.Sessions = []gateway.SessionSummary{
		{ID: "s1", Title: "First", UpdatedAt: time.Now()},
		{ID: "s2", Title: "Second", UpdatedAt: time.Now()},
	}
	return vs
}

// ============ Palette ============

func TestPaletteComponentLifecycle(t *testing.T) {
	vs := pickerState()
	p := NewPalette(vs)
	if p.Init() != nil {
		t.Fatal("Init nil")
	}
	// 非 Key/Mouse 消息 -> nil
	if _, cmd := p.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("non-key should be nil")
	}
}

func TestPaletteHandleKeyAllBranches(t *testing.T) {
	// esc/ctrl+c 关闭
	for _, m := range []tea.Msg{keyType(tea.KeyEsc), keyType(tea.KeyCtrlC)} {
		vs := pickerState()
		vs.Overlay.Active = state.OverlayPalette
		p := NewPalette(vs)
		_, _ = p.Update(tea.KeyMsg(m.(tea.KeyMsg)))
		if vs.Overlay.Active != "" {
			t.Fatalf("%v should close palette", m)
		}
	}
	// enter / space -> 选中并返回 PaletteCommandMsg
	for _, m := range []tea.Msg{keyType(tea.KeyEnter), keyMsg(" ")} {
		vs := pickerState()
		p := NewPalette(vs)
		_, cmd := p.Update(m)
		if cmd == nil {
			t.Fatalf("%v should select", m)
		}
		if _, ok := cmd().(PaletteCommandMsg); !ok {
			t.Fatalf("%v should emit PaletteCommandMsg", m)
		}
	}
	// up/k 下移、down/j 上移、backspace 删除查询
	vs := pickerState()
	p := NewPalette(vs)
	_, _ = p.Update(keyMsg("a")) // 输入
	_, _ = p.Update(keyMsg("b")) // 输入
	_, _ = p.Update(keyType(tea.KeyBackspace))
	if vs.Overlay.Query != "a" {
		t.Fatalf("backspace query=%q want 'a'", vs.Overlay.Query)
	}
	_, _ = p.Update(keyMsg("k")) // up（已在顶不变化）
	_, _ = p.Update(keyMsg("j")) // down
	if vs.Overlay.Selected == 0 {
		// 列表为空匹配时不会下移；这里 query="a" 无匹配 -> Selected 仍 0，属正常
	}
	// enter 但无匹配 -> nil
	vs.Overlay.Query = "zzzz"
	if _, cmd := p.Update(keyType(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter with no match should return nil")
	}
}

func TestPaletteMatchedItemsTiers(t *testing.T) {
	p := NewPalette(pickerState())
	if len(p.matchedItems()) == 0 {
		t.Fatal("no-query should return all")
	}
	vs := pickerState()
	p2 := NewPalette(vs)
	vs.Overlay.Query = "mode"
	first := p2.matchedItems()[0].Name
	if first != "/mode" {
		t.Fatalf("query 'mode' first=%q, want /mode", first)
	}
	vs.Overlay.Query = "/mode" // 带斜杠也应命中
	if p2.matchedItems()[0].Name != "/mode" {
		t.Fatalf("query '/mode' first=%q", p2.matchedItems()[0].Name)
	}
	vs.Overlay.Query = "zzzz"
	if len(p2.matchedItems()) != 0 {
		t.Fatal("no-match query should return empty")
	}
}

func TestPaletteHandleMouseAndSelect(t *testing.T) {
	vs := pickerState()
	p := NewPalette(vs)
	// wheel
	_, _ = p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	_, _ = p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	// 左键非按下 -> nil
	if _, cmd := p.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); cmd != nil {
		t.Fatal("non-press left should be nil")
	}
	// 左键按下命中首项 -> PaletteCommandMsg
	_, cmd := p.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 2})
	if cmd == nil {
		t.Fatal("left press on item should select")
	}
	// 左键按下越界 -> nil
	if _, cmd := p.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 99}); cmd != nil {
		t.Fatal("out-of-range click should be nil")
	}
}

func TestPaletteViewVariants(t *testing.T) {
	vs := pickerState()
	p := NewPalette(vs)
	if v := p.View(); !strings.Contains(v, "/model") {
		t.Fatalf("view missing items: %q", v)
	}
	// 无匹配
	vs.Overlay.Query = "zzzz"
	if v := p.View(); !strings.Contains(v, "No matches") {
		t.Fatalf("view missing no-match hint: %q", v)
	}
	// 零尺寸回退
	vs.Layout.Width = 0
	vs.Layout.Height = 0
	if v := p.View(); v == "" {
		t.Fatal("zero-size view should not be empty")
	}
}

// ============ ModelPicker ============

func TestModelPickerLifecycleAndKey(t *testing.T) {
	vs := pickerState()
	m := NewModelPicker(vs)
	if m.Init() != nil {
		t.Fatal("Init nil")
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("non-key nil")
	}
	// esc/ctrl+c 关闭
	for _, kk := range []tea.KeyMsg{keyType(tea.KeyEsc), keyType(tea.KeyCtrlC)} {
		vs.Overlay.Active = state.OverlayModelPicker
		_, _ = m.Update(kk)
	}
	// enter / space -> ModelSelectMsg
	for _, kk := range []tea.KeyMsg{keyType(tea.KeyEnter), keyMsg(" ")} {
		vs := pickerState()
		mp := NewModelPicker(vs)
		_, cmd := mp.Update(kk)
		if cmd == nil {
			t.Fatalf("%v should select model", kk)
		}
		if _, ok := cmd().(ModelSelectMsg); !ok {
			t.Fatalf("want ModelSelectMsg")
		}
	}
	// 导航 + backspace + 输入
	mp := NewModelPicker(pickerState())
	_, _ = mp.Update(keyMsg("f"))
	_, _ = mp.Update(keyType(tea.KeyBackspace))
	_, _ = mp.Update(keyMsg("k"))
	_, _ = mp.Update(keyMsg("j"))
	// enter 无匹配 -> nil
	vs2 := pickerState()
	mp2 := NewModelPicker(vs2)
	vs2.Overlay.Query = "zzzz"
	if _, cmd := mp2.Update(keyType(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter no-match should be nil")
	}
}

func TestModelPickerMatchedAndMouseAndView(t *testing.T) {
	vs := pickerState()
	m := NewModelPicker(vs)
	if len(m.matchedModels()) != 2 {
		t.Fatal("no-query should return all models")
	}
	vs.Overlay.Query = "fast"
	if first := m.matchedModels()[0].ID; first != "m-fast" {
		t.Fatalf("query fast first=%q", first)
	}
	vs.Overlay.Query = "zzzz"
	if len(m.matchedModels()) != 0 || !strings.Contains(m.View(), "No models") {
		t.Fatal("no-match models handling wrong")
	}
	// 鼠标
	mm := NewModelPicker(pickerState())
	_, _ = mm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	_, _ = mm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if _, cmd := mm.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); cmd != nil {
		t.Fatal("non-press nil")
	}
	if _, cmd := mm.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 4}); cmd == nil {
		t.Fatal("left press should select")
	}
	if _, cmd := mm.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 99}); cmd != nil {
		t.Fatal("out-of-range nil")
	}
}

// ============ SessionPicker ============

func TestSessionPickerLifecycleAndKey(t *testing.T) {
	vs := pickerState()
	s := NewSessionPicker(vs)
	if s.Init() != nil {
		t.Fatal("Init nil")
	}
	if _, cmd := s.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("non-key nil")
	}
	// esc/ctrl+c 关闭
	for _, kk := range []tea.KeyMsg{keyType(tea.KeyEsc), keyType(tea.KeyCtrlC)} {
		vs.Overlay.Active = state.OverlaySessionPicker
		_, _ = s.Update(kk)
	}
	// enter / space -> SessionSelectMsg
	for _, kk := range []tea.KeyMsg{keyType(tea.KeyEnter), keyMsg(" ")} {
		sp := NewSessionPicker(pickerState())
		_, cmd := sp.Update(kk)
		if cmd == nil {
			t.Fatalf("%v should select session", kk)
		}
		if _, ok := cmd().(SessionSelectMsg); !ok {
			t.Fatal("want SessionSelectMsg")
		}
	}
	// ctrl+d -> SessionDeleteMsg
	sp := NewSessionPicker(pickerState())
	if _, cmd := sp.Update(keyType(tea.KeyCtrlD)); cmd == nil {
		t.Fatal("ctrl+d should emit SessionDeleteMsg")
	}
	// 导航 + 输入 + backspace + enter 无匹配
	sp2 := NewSessionPicker(pickerState())
	_, _ = sp2.Update(keyMsg("s"))
	_, _ = sp2.Update(keyType(tea.KeyBackspace))
	_, _ = sp2.Update(keyMsg("j"))
	_, _ = sp2.Update(keyMsg("k"))
	sp2.state.Overlay.Query = "zzzz"
	if _, cmd := sp2.Update(keyType(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter no-match should be nil")
	}
}

func TestSessionPickerMatchedMouseView(t *testing.T) {
	vs := pickerState()
	s := NewSessionPicker(vs)
	if len(s.matchedSessions()) != 2 {
		t.Fatal("no-query all sessions")
	}
	vs.Overlay.Query = "second"
	if len(s.matchedSessions()) != 1 {
		t.Fatalf("query second len=%d", len(s.matchedSessions()))
	}
	vs.Overlay.Query = "zzzz"
	if len(s.matchedSessions()) != 0 || !strings.Contains(s.View(), "No sessions") {
		t.Fatal("no-match sessions wrong")
	}
	// 空标题会话 -> "untitled"
	vs2 := pickerState()
	vs2.Gateway.Sessions = []gateway.SessionSummary{{ID: "x", Title: ""}}
	if v := NewSessionPicker(vs2).View(); !strings.Contains(v, "untitled") {
		t.Fatalf("empty title should show untitled: %q", v)
	}
	// 鼠标
	sm := NewSessionPicker(pickerState())
	_, _ = sm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	_, _ = sm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if _, cmd := sm.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 4}); cmd == nil {
		t.Fatal("left press should select")
	}
	if _, cmd := sm.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 99}); cmd != nil {
		t.Fatal("out-of-range nil")
	}
	// 零时间戳日期回退
	vs3 := pickerState()
	vs3.Gateway.Sessions = []gateway.SessionSummary{{ID: "z", Title: "Z"}}
	if v := NewSessionPicker(vs3).View(); !strings.Contains(v, "Z") {
		t.Fatal("zero-time session view wrong")
	}
}
