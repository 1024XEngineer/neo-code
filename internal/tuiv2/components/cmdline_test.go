package components

import (
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/state"
)

// promptViewState 构造一个带尺寸的 ViewState，供 cmdline 测试使用。
func cmdlineViewState() *state.ViewState {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	vs.Layout.Height = 24
	return vs
}

func TestCmdLineExInputAndSubmit(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlayEx
	c := NewCmdLine(vs)

	// 输入字符
	c.Update(keyMsgRunes('q'))
	c.Update(keyMsgRunes('u'))
	c.Update(keyMsgRunes('i'))
	c.Update(keyMsgRunes('t'))
	if vs.Ex.Input != "quit" {
		t.Fatalf("ex input=%q, want quit", vs.Ex.Input)
	}

	// Backspace 删除末尾
	c.Update(keyType(tea.KeyBackspace))
	if vs.Ex.Input != "qui" {
		t.Fatalf("after backspace input=%q, want qui", vs.Ex.Input)
	}

	// 清空后重新输入 quit 并提交
	vs.Ex.Input = ""
	c.Update(keyMsgRunes('q'))
	c.Update(keyMsgRunes('u'))
	c.Update(keyMsgRunes('i'))
	c.Update(keyMsgRunes('t'))
	_, cmd := c.Update(keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("ex enter should emit ExCommandMsg")
	}
	msg := cmd()
	exMsg, ok := msg.(ExCommandMsg)
	if !ok {
		t.Fatalf("want ExCommandMsg, got %T", msg)
	}
	if exMsg.Command != "quit" {
		t.Fatalf("command=%q, want quit", exMsg.Command)
	}
	if vs.Ex.Input != "" {
		t.Fatalf("ex input should be cleared after submit, got %q", vs.Ex.Input)
	}
}

func TestCmdLineExEscCancels(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlayEx
	vs.Ex.Input = "debug"
	c := NewCmdLine(vs)
	_, cmd := c.Update(keyType(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc should emit CmdLineCancelMsg")
	}
	if _, ok := cmd().(CmdLineCancelMsg); !ok {
		t.Fatal("want CmdLineCancelMsg")
	}
}

func TestCmdLineSearchInputAndSubmit(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlaySearch
	c := NewCmdLine(vs)

	c.Update(keyMsgRunes('e'))
	c.Update(keyMsgRunes('r'))
	c.Update(keyMsgRunes('r'))
	if vs.Search.Query != "err" {
		t.Fatalf("search query=%q, want err", vs.Search.Query)
	}

	_, cmd := c.Update(keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("search enter should emit SearchSubmitMsg")
	}
	msg := cmd()
	sMsg, ok := msg.(SearchSubmitMsg)
	if !ok {
		t.Fatalf("want SearchSubmitMsg, got %T", msg)
	}
	if sMsg.Query != "err" {
		t.Fatalf("query=%q, want err", sMsg.Query)
	}
}

func TestCmdLineViewRendersPrefixAndStale(t *testing.T) {
	// Ex 视图
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlayEx
	vs.Ex.Input = "q"
	c := NewCmdLine(vs)
	if v := c.View(); v == "" {
		t.Fatal("ex view empty")
	}

	// Search 视图 + stale 提示
	vs2 := cmdlineViewState()
	vs2.Overlay.Active = state.OverlaySearch
	vs2.Search.Query = "err"
	vs2.Search.Stale = true
	c2 := NewCmdLine(vs2)
	v := c2.View()
	if v == "" {
		t.Fatal("search view empty")
	}
	// stale 提示应出现在输出中
	if !containsStr(v, "stale") {
		t.Fatalf("stale hint missing in view: %q", v)
	}

	// 无 overlay 时 View 返回空
	vs3 := cmdlineViewState()
	c3 := NewCmdLine(vs3)
	if c3.View() != "" {
		t.Fatal("view should be empty when no overlay active")
	}
}

func TestRunSearchMatching(t *testing.T) {
	stream := []state.StreamEntry{
		{ID: "1", Content: "hello world"},
		{ID: "2", Content: "ERROR: something"},
		{ID: "3", Content: "all good"},
		{ID: "4", Content: "another error here"},
	}
	// 大小写不敏感匹配 "error"
	matches := RunSearch(stream, "error")
	if len(matches) != 2 {
		t.Fatalf("matches=%v, want 2 hits", matches)
	}
	if matches[0] != 1 || matches[1] != 3 {
		t.Fatalf("matches indices=%v, want [1 3]", matches)
	}
	// 空查询返回 nil
	if RunSearch(stream, "   ") != nil {
		t.Fatal("empty query should return nil")
	}
	// 无匹配返回 nil
	if RunSearch(stream, "zzz") != nil {
		t.Fatal("no match should return nil")
	}
}

// keyMsgRunes 构造一个携带 rune 的 KeyMsg。
func keyMsgRunes(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// containsStr 简单子串包含判断。
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

// TestCmdLineInit 覆盖 CmdLine.Init（返回 nil，无启动命令）。
func TestCmdLineInit(t *testing.T) {
	c := NewCmdLine(cmdlineViewState())
	if cmd := c.Init(); cmd != nil {
		t.Fatalf("CmdLine.Init should return nil, got %v", cmd)
	}
}

// TestCmdLineSearchBackspace 覆盖 Search overlay 下的 Backspace（原 60% 盲区）。
func TestCmdLineSearchBackspace(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlaySearch
	vs.Search.Query = "hello"
	c := NewCmdLine(vs)

	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "hell" {
		t.Fatalf("search backspace query=%q, want hell", vs.Search.Query)
	}
	// 连续 Backspace 到空，再按应 no-op 不崩溃
	c.Update(keyType(tea.KeyBackspace))
	c.Update(keyType(tea.KeyBackspace))
	c.Update(keyType(tea.KeyBackspace))
	c.Update(keyType(tea.KeyBackspace))
	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "" {
		t.Fatalf("search query after many backspace=%q, want empty", vs.Search.Query)
	}

	// 非 Ex/Search overlay 下 Backspace 不应改动任何输入
	vs2 := cmdlineViewState()
	vs2.Overlay.Active = state.OverlayPalette
	c2 := NewCmdLine(vs2)
	c2.Update(keyType(tea.KeyBackspace)) // no-op
}

// TestCmdLineNonPrintableKeysIgnored 覆盖导航键等被忽略的分支。
func TestCmdLineNonPrintableKeysIgnored(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlayEx
	c := NewCmdLine(vs)
	// left/right/up/down 等功能键应被忽略，不修改输入
	c.Update(keyType(tea.KeyLeft))
	c.Update(keyType(tea.KeyUp))
	if vs.Ex.Input != "" {
		t.Fatalf("non-printable should be ignored, input=%q", vs.Ex.Input)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCmdLineBackspaceUnicode 验证非 ASCII（中文/emoji）Backspace 不破坏 UTF-8。
// 回归保护：早期字节切片实现会让"错"(3字节)删1字节产生无效 UTF-8。
func TestCmdLineBackspaceUnicode(t *testing.T) {
	// 中文搜索词
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlaySearch
	vs.Search.Query = "错误"
	c := NewCmdLine(vs)
	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "错" {
		t.Fatalf("中文 backspace query=%q, want 错", vs.Search.Query)
	}
	if !utf8.ValidString(vs.Search.Query) {
		t.Fatalf("中文 backspace 产生无效 UTF-8: %q", vs.Search.Query)
	}
	// emoji
	vs2 := cmdlineViewState()
	vs2.Overlay.Active = state.OverlaySearch
	vs2.Search.Query = "😀ab"
	c2 := NewCmdLine(vs2)
	c2.Update(keyType(tea.KeyBackspace))
	if vs2.Search.Query != "😀a" {
		t.Fatalf("emoji backspace query=%q, want 😀a", vs2.Search.Query)
	}
	// Ex 输入中文
	vs3 := cmdlineViewState()
	vs3.Overlay.Active = state.OverlayEx
	vs3.Ex.Input = "测试"
	c3 := NewCmdLine(vs3)
	c3.Update(keyType(tea.KeyBackspace))
	if vs3.Ex.Input != "测" {
		t.Fatalf("ex 中文 backspace=%q, want 测", vs3.Ex.Input)
	}
	// 空输入 backspace 不崩溃
	vs4 := cmdlineViewState()
	vs4.Overlay.Active = state.OverlaySearch
	c4 := NewCmdLine(vs4)
	c4.Update(keyType(tea.KeyBackspace))
	if vs4.Search.Query != "" {
		t.Fatalf("empty backspace query=%q, want empty", vs4.Search.Query)
	}
}

// TestDeleteLastRune 单元测试包级辅助函数。
func TestDeleteLastRune(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a", ""},
		{"ab", "a"},
		{"错", ""},
		{"错误", "错"},
		{"😀", ""},
		{"a错", "a"},
		{"错a", "错"},
	}
	for _, tc := range cases {
		got := deleteLastRune(tc.in)
		if got != tc.want {
			t.Fatalf("deleteLastRune(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCmdLineBackspaceUnicodeConsecutive 验证连续多次 backspace 逐 rune 删除多字节序列。
// 覆盖第 2 轮审核指出的集成层盲区：cmdline.Update 路径下连续删除 "你好" → "你" → ""。
func TestCmdLineBackspaceUnicodeConsecutive(t *testing.T) {
	vs := cmdlineViewState()
	vs.Overlay.Active = state.OverlaySearch
	vs.Search.Query = "你好"
	c := NewCmdLine(vs)
	// 第一次：你好 → 你
	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "你" || !utf8.ValidString(vs.Search.Query) {
		t.Fatalf("1st backspace query=%q valid=%v, want 你", vs.Search.Query, utf8.ValidString(vs.Search.Query))
	}
	// 第二次：你 → 空
	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "" {
		t.Fatalf("2nd backspace query=%q, want empty", vs.Search.Query)
	}
	// 第三次：空 → 空（no-op）
	c.Update(keyType(tea.KeyBackspace))
	if vs.Search.Query != "" {
		t.Fatalf("3rd backspace on empty query=%q, want empty", vs.Search.Query)
	}
}
