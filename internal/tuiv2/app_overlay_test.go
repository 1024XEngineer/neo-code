package tuiv2

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/fakegateway"
	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
)

// pump 执行一条 cmd，把产出的消息送回 Update，返回最新的 App 指针。
func pump(t *testing.T, app *App, cmd tea.Cmd) *App {
	t.Helper()
	if cmd == nil {
		return app
	}
	msg := cmd()
	if msg == nil {
		return app
	}
	updated, next := app.Update(msg)
	app = updated.(*App)
	return pump(t, app, next)
}

// pumpAll 顺序执行多条 cmd。
func pumpAll(t *testing.T, app *App, cmds ...tea.Cmd) *App {
	t.Helper()
	for _, c := range cmds {
		app = pump(t, app, c)
	}
	return app
}

func newReadyApp(t *testing.T) *App {
	t.Helper()
	client, err := fakegateway.NewFakeClient(fakegateway.ScenarioDefault)
	if err != nil {
		t.Fatalf("NewFakeClient: %v", err)
	}
	app := NewApp(StartupConfig{Backend: "fake", Scenario: "default", Client: client}).(*App)
	updated, winCmd := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app = updated.(*App)
	app = pumpAll(t, app, app.Init(), winCmd)
	return app
}

// send 把一条消息送进 App.Update 并抽干其产生的命令，返回最新 App 指针。
func send(t *testing.T, app *App, msg tea.Msg) *App {
	t.Helper()
	updated, cmd := app.Update(msg)
	app = updated.(*App)
	return pump(t, app, cmd)
}

func runesKey(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestLeaderSessionPickerFlow 模拟用户真实操作：Esc -> Normal, Space -> Leader,
// s -> 会话选择器, 下移, 回车。注意：Leader 后缀键之间不能抽干 leaderTimeoutCmd，
// 否则 1s 超时会先把 Mode 回退到 Normal（真实使用时用户会在超时前按下后缀键）。
func TestLeaderSessionPickerFlow(t *testing.T) {
	app := newReadyApp(t)
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEsc}) // NormalMode
	updated, _ := app.Update(runesKey(" "))          // Space -> Leader（不抽干超时）
	app = updated.(*App)
	updated, _ = app.Update(runesKey("s")) // s -> session_picker
	app = updated.(*App)
	if app.state.Overlay.Active != "session_picker" {
		t.Fatalf("session picker did not open via leader, active=%q", app.state.Overlay.Active)
	}
	app = send(t, app, tea.KeyMsg{Type: tea.KeyDown})
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.state.Overlay.Active != "" {
		t.Fatalf("session picker did NOT close on Enter via leader flow (still %q)", app.state.Overlay.Active)
	}
}

// TestModelPickerViaPaletteCloses 验证：面板里输入 "model" 回车打开模型选择器后，
// 再回车能正常选中并关闭（用户反馈的"选择器回车不关闭"）。
func TestModelPickerViaPaletteCloses(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlayPalette)
	for _, r := range "model" {
		app = send(t, app, runesKey(string(r)))
	}
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter}) // /model -> model_picker
	if app.state.Overlay.Active != "model_picker" {
		t.Fatalf("expected model_picker after /model, got %q", app.state.Overlay.Active)
	}
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter}) // 选中第一个模型
	if app.state.Overlay.Active != "" {
		t.Fatalf("model picker did NOT close on Enter (still %q)", app.state.Overlay.Active)
	}
}

// TestPaletteTypeModeEnter 模拟在命令面板里输入 "mode" 过滤后回车。
func TestPaletteTypeModeEnter(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlayPalette)
	for _, r := range "mode" {
		app = send(t, app, runesKey(string(r)))
	}
	t.Logf("palette query=%q selected=%d", app.state.Overlay.Query, app.state.Overlay.Selected)
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	t.Logf("after Enter: overlay=%q", app.state.Overlay.Active)
	if app.state.Overlay.Active != "" {
		t.Fatalf("palette did NOT close after typing 'mode' + Enter (still %q)", app.state.Overlay.Active)
	}
}

// TestPaletteCtrlPOpenNavigateEnter 模拟真实路径：InputMode 下 Ctrl+P 打开面板，
// 下移选中 /mode，回车。验证面板关闭且回到对话界面。
func TestPaletteCtrlPOpenNavigateEnter(t *testing.T) {
	app := newReadyApp(t)
	app = send(t, app, tea.KeyMsg{Type: tea.KeyCtrlP}) // 打开面板
	if app.state.Overlay.Active != "palette" {
		t.Fatalf("palette did not open via Ctrl+P, active=%q", app.state.Overlay.Active)
	}
	app = send(t, app, tea.KeyMsg{Type: tea.KeyDown}) // /model -> /mode
	if app.state.Overlay.Selected != 1 {
		t.Fatalf("selected=%d, want 1 after one down", app.state.Overlay.Selected)
	}
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	last := ""
	if n := len(app.state.Stream); n > 0 {
		last = app.state.Stream[n-1].Content
	}
	t.Logf("after Enter: overlay=%q lastStream=%q", app.state.Overlay.Active, last)
	if app.state.Overlay.Active != "" {
		t.Fatalf("palette did NOT close on Enter via Ctrl+P flow (still %q)", app.state.Overlay.Active)
	}
}

// TestPaletteStaleStateAfterEvents 复现真实 bug：事件流会通过 state.Reduce
// 把 a.state 替换成新指针，而 bindComponents 没有重新绑定浮层组件，导致
// palette/model/session 选择器持有旧的 state 指针——于是下移/回车改的是旧 state，
// App 的当前 state 里 Overlay.Active 始终不变，面板"回车不关闭、跳回第一项"。
func TestPaletteStaleStateAfterEvents(t *testing.T) {
	app := newReadyApp(t)
	// 走真实事件处理路径：a.state = state.Reduce(...) 后调用 bindComponents。
	updated, _ := app.Update(gatewayEventMsg{event: gateway.GatewayEvent{
		Type:    gateway.EventPhaseChanged,
		Payload: map[string]any{"phase": state.RuntimePhaseIdle},
	}})
	app = updated.(*App)
	app.openOverlay(state.OverlayPalette)
	app = send(t, app, tea.KeyMsg{Type: tea.KeyDown}) // 高亮到 /mode
	app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.state.Overlay.Active != "" {
		t.Fatalf("palette did NOT close after Enter (overlay components hold stale state pointer): active=%q", app.state.Overlay.Active)
	}
}

// 空格应像回车一样执行当前高亮项并关闭面板，而不是被当成查询字符重置到 /model。
func TestPaletteSpaceSelects(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlayPalette)
	app = send(t, app, tea.KeyMsg{Type: tea.KeyDown}) // 选中 /mode
	app = send(t, app, runesKey(" "))                 // 空格确认
	last := ""
	if n := len(app.state.Stream); n > 0 {
		last = app.state.Stream[n-1].Content
	}
	t.Logf("after space: overlay=%q lastStream=%q", app.state.Overlay.Active, last)
	if app.state.Overlay.Active != "" {
		t.Fatalf("palette did NOT close on Space (still %q)", app.state.Overlay.Active)
	}
	if !strings.Contains(last, "Agent mode") {
		t.Fatalf("space did not select /mode (it ran something else): %q", last)
	}
}

// TestPaletteNavigateSelectsTarget 模拟用户在面板里用下移键选中
// /mode、/compact、/checkpoint 后回车，验证面板会关闭且回到对话界面。
func TestPaletteNavigateSelectsTarget(t *testing.T) {
	cases := []struct {
		name  string
		downs int
	}{
		{"mode", 1},       // /model(0) -> /mode(1)
		{"compact", 3},    // -> /compact(3)
		{"checkpoint", 4}, // -> /checkpoint(4)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newReadyApp(t)
			app.openOverlay(state.OverlayPalette)
			for i := 0; i < tc.downs; i++ {
				app = send(t, app, tea.KeyMsg{Type: tea.KeyDown})
			}
			t.Logf("selected index before Enter=%d", app.state.Overlay.Selected)
			app = send(t, app, tea.KeyMsg{Type: tea.KeyEnter})
			last := ""
			if n := len(app.state.Stream); n > 0 {
				last = app.state.Stream[n-1].Content
			}
			t.Logf("after Enter: overlay=%q lastStream=%q", app.state.Overlay.Active, last)
			if app.state.Overlay.Active != "" {
				t.Fatalf("/%s: palette did NOT close on Enter (still %q)", tc.name, app.state.Overlay.Active)
			}
		})
	}
}

func TestModelPickerEnterCloses(t *testing.T) {
	app := newReadyApp(t)
	t.Logf("models=%d sessions=%d", len(app.state.Gateway.Models), len(app.state.Gateway.Sessions))
	app.openOverlay(state.OverlayModelPicker)

	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = updated.(*App)
	app = pump(t, app, cmd) // 处理 ModelSelectMsg
	t.Logf("model picker active after Enter=%q", app.state.Overlay.Active)
	if app.state.Overlay.Active != "" {
		t.Fatalf("model picker did NOT close on Enter (still %q)", app.state.Overlay.Active)
	}
}

func TestSessionPickerEnterCloses(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlaySessionPicker)

	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = updated.(*App)
	app = pump(t, app, cmd) // 处理 SessionSelectMsg -> loadSessionCmd
	t.Logf("session picker active after Enter=%q", app.state.Overlay.Active)
	if app.state.Overlay.Active != "" {
		t.Fatalf("session picker did NOT close on Enter (still %q)", app.state.Overlay.Active)
	}
}

func TestConfirmEnterConfirms(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlaySessionPicker)
	// Ctrl+D 在 session picker 中触发删除请求 -> 应打开 confirm
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	app = updated.(*App)
	app = pump(t, app, cmd) // 处理 SessionDeleteMsg -> openConfirm
	if app.state.Overlay.Active != "confirm" {
		t.Fatalf("confirm overlay not open after Ctrl+D, active=%q", app.state.Overlay.Active)
	}

	updated, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = updated.(*App)
	app = pump(t, app, cmd) // 处理 ConfirmYesMsg -> deleteSessionCmd
	t.Logf("confirm active after Enter=%q", app.state.Overlay.Active)
	if app.state.Overlay.Active != "" {
		t.Fatalf("confirm overlay did NOT close on Enter (still %q)", app.state.Overlay.Active)
	}
}

func TestSessionSwitchShowsConfirmation(t *testing.T) {
	app := newReadyApp(t)
	app.openOverlay(state.OverlaySessionPicker)
	// 下移到第二个会话后回车切换
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = updated.(*App)
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = updated.(*App)
	app = pump(t, app, cmd) // 处理 SessionSelectMsg -> loadSessionCmd

	// 会话历史被重载，后续还会有异步事件追加，因此只校验状态条目存在。
	var contents []string
	var hasSwitch, hasReloaded bool
	for _, entry := range app.state.Stream {
		contents = append(contents, entry.Content)
		if strings.Contains(entry.Content, "Switched to session") {
			hasSwitch = true
		}
		if strings.Contains(entry.Content, "Session fixture loaded") {
			hasReloaded = true
		}
	}
	if !hasReloaded {
		t.Fatalf("session stream was not reloaded from target session: %v", contents)
	}
	if !hasSwitch {
		t.Fatalf("no switch confirmation in stream: %v", contents)
	}
}
