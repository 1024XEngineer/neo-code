package tuiv2

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/components"
	"neo-code/internal/tuiv2/fakegateway"
	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/state"
)

// ---- 消息处理器 ----

func TestHandleSubmitMessageAppendsAndReturnsCmd(t *testing.T) {
	app := newReadyApp(t)
	updated, cmd := app.Update(components.SubmitMessageMsg{Text: "  hello  "})
	app = updated.(*App)
	if cmd == nil {
		t.Fatal("submit with client returned nil cmd")
	}
	last := app.state.Stream[len(app.state.Stream)-1]
	if last.Content != "  hello  " || last.Metadata["role"] != "user" {
		t.Fatalf("user message not appended: %+v", last)
	}

	// 空文本 -> nil
	if _, cmd := app.Update(components.SubmitMessageMsg{Text: "   "}); cmd != nil {
		t.Fatal("empty submit should return nil cmd")
	}
}

func TestHandleSubmitMessageNoClient(t *testing.T) {
	app := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	updated, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app = updated.(*App)
	_, cmd := app.Update(components.SubmitMessageMsg{Text: "hi"})
	if cmd != nil {
		t.Fatal("submit without client should return nil cmd")
	}
}

func TestPermissionAndQuestionHandlers(t *testing.T) {
	app := newReadyApp(t)
	app.state.Runtime.RunID = "run-1"

	if _, cmd := app.Update(components.PermissionActionMsg{Decision: "y"}); cmd == nil {
		t.Fatal("permission action with client returned nil")
	}
	if _, cmd := app.Update(components.QuestionAnswerMsg{Text: "answer"}); cmd == nil {
		t.Fatal("question answer with client returned nil")
	}

	// 无 client -> nil
	noc := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	if _, cmd := noc.Update(components.PermissionActionMsg{Decision: "y"}); cmd != nil {
		t.Fatal("permission without client should be nil")
	}
	if _, cmd := noc.Update(components.QuestionAnswerMsg{Text: "x"}); cmd != nil {
		t.Fatal("question without client should be nil")
	}
}

func TestCancelPromptResetsInputAndLogs(t *testing.T) {
	app := newReadyApp(t)
	app.state.Input.Mode = state.InputStateModeQuestionAnswer
	app.state.Input.Text = "abc"
	app.state.Input.Options = []string{"x"}
	app.Update(components.PromptCancelMsg{Mode: state.InputStateModeQuestionAnswer})

	if app.state.Input.Mode != state.InputStateModeMessage || app.state.Input.Text != "" {
		t.Fatalf("input not reset: %+v", app.state.Input)
	}
	if app.state.Input.Options != nil {
		t.Fatalf("options not cleared: %+v", app.state.Input.Options)
	}
}

// ---- Slash / Palette 命令分发 ----

func TestSlashCommandDispatch(t *testing.T) {
	cases := map[string]func(*App) bool{
		"/session": func(a *App) bool { return a.state.Overlay.Active == state.OverlaySessionPicker },
		"/model":   func(a *App) bool { return a.state.Overlay.Active == state.OverlayModelPicker },
		"/help":    func(a *App) bool { return a.state.Overlay.Active == state.OverlayHelp },
		"/mode":    func(a *App) bool { return a.state.Runtime.AgentMode == "build" },
		"/compact": func(a *App) bool { return lastContains(a, "Compact triggered") },
		"/clear":   func(a *App) bool { return len(a.state.Stream) == 0 },
		"/bogus":   func(a *App) bool { return lastContains(a, "unknown command") },
	}
	for cmd, check := range cases {
		t.Run(cmd, func(t *testing.T) {
			app := newReadyApp(t)
			app.Update(components.SlashCommandMsg{Command: cmd})
			if !check(app) {
				t.Fatalf("%s did not produce expected effect: overlay=%q stream=%v", cmd, app.state.Overlay.Active, streamContents(app))
			}
		})
	}
	// /exit / /quit -> tea.Quit
	app := newReadyApp(t)
	if _, cmd := app.Update(components.SlashCommandMsg{Command: "/exit"}); cmd == nil {
		t.Fatal("/exit should return quit cmd")
	}
}

func TestPaletteCommandDispatch(t *testing.T) {
	cases := map[string]func(*App) bool{
		"/session":    func(a *App) bool { return a.state.Overlay.Active == state.OverlaySessionPicker },
		"/model":      func(a *App) bool { return a.state.Overlay.Active == state.OverlayModelPicker },
		"/help":       func(a *App) bool { return a.state.Overlay.Active == state.OverlayHelp },
		"/mode":       func(a *App) bool { return a.state.Runtime.AgentMode == "build" },
		"/compact":    func(a *App) bool { return lastContains(a, "Compact triggered") },
		"/checkpoint": func(a *App) bool { return lastContains(a, "not yet implemented") },
	}
	for name, check := range cases {
		t.Run(name, func(t *testing.T) {
			app := newReadyApp(t)
			app.Update(components.PaletteCommandMsg{Name: name})
			if !check(app) {
				t.Fatalf("%s did not produce expected effect: overlay=%q stream=%v", name, app.state.Overlay.Active, streamContents(app))
			}
		})
	}
	app := newReadyApp(t)
	if _, cmd := app.Update(components.PaletteCommandMsg{Name: "/exit"}); cmd == nil {
		t.Fatal("/exit should return quit cmd")
	}
}

// ---- Ctrl+C 双退保护 ----

func TestHandleCtrlCIdleHintAndDoubleQuit(t *testing.T) {
	app := newReadyApp(t)
	// 空闲单次 -> 提示
	_, cmd := app.handleCtrlC()
	if cmd != nil || !lastContains(app, "Press Ctrl+C again to quit") {
		t.Fatalf("idle single Ctrl+C should hint, cmd=%v stream=%v", cmd, streamContents(app))
	}
	// 运行中 -> 取消
	app.state.Runtime.Phase = state.RuntimePhaseRunning
	_, cmd = app.handleCtrlC()
	if cmd == nil {
		t.Fatal("running Ctrl+C should return cancel cmd")
	}
}

func TestHandleCtrlCCancelWithoutClient(t *testing.T) {
	app := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	app.state.Runtime.Phase = state.RuntimePhaseWaitingPermission
	_, _ = app.handleCtrlC()
	if app.state.Runtime.Phase != state.RuntimePhaseCancelled {
		t.Fatalf("phase=%s, want cancelled", app.state.Runtime.Phase)
	}
}

// ---- 会话/模型/确认 ----

func TestHandleSessionCreatedErrorAndSuccess(t *testing.T) {
	// 错误路径
	app := newReadyApp(t)
	app.Update(sessionCreatedMsg{err: errSentinel("boom")})
	if !lastContains(app, "Failed to create session") {
		t.Fatalf("error path not logged: %v", streamContents(app))
	}
	// 成功路径
	app = newReadyApp(t)
	_, cmd := app.Update(sessionCreatedMsg{Session: &gateway.SessionSummary{ID: "new-1", Title: "New"}})
	if app.state.Gateway.ActiveSess == nil || app.state.Gateway.ActiveSess.ID != "new-1" {
		t.Fatal("active session not set on create")
	}
	if !lastContains(app, "New session created") {
		t.Fatalf("create not logged: %v", streamContents(app))
	}
	if cmd == nil {
		t.Fatal("create with client should return load cmd")
	}
}

func TestHandleSessionDeleteOpensConfirm(t *testing.T) {
	app := newReadyApp(t)
	app.Update(components.SessionDeleteMsg{SessionID: "s-x"})
	if app.state.Overlay.Active != "confirm" || app.state.Confirm.Action != "delete_session" {
		t.Fatalf("confirm not opened: overlay=%q confirm=%+v", app.state.Overlay.Active, app.state.Confirm)
	}
	// 无 client -> 忽略
	noc := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	if _, cmd := noc.Update(components.SessionDeleteMsg{SessionID: "s-x"}); cmd != nil {
		t.Fatal("delete without client should be nil")
	}
}

func TestHandleModelSelectSwitches(t *testing.T) {
	app := newReadyApp(t)
	app.Update(components.ModelSelectMsg{ModelID: "neo-fake-fast", ModelName: "Neo Fake Fast"})
	if app.state.Gateway.ActiveModel != "neo-fake-fast" || !lastContains(app, "Model switched") {
		t.Fatalf("model not switched: model=%q stream=%v", app.state.Gateway.ActiveModel, streamContents(app))
	}
}

func TestHandleConfirmYesDeletesSession(t *testing.T) {
	app := newReadyApp(t)
	app.openConfirm("Delete Session", "msg", "delete_session", map[string]any{"session_id": "sess-9"})
	_, cmd := app.Update(components.ConfirmYesMsg{})
	if app.state.Overlay.Active != "" {
		t.Fatalf("confirm should close, active=%q", app.state.Overlay.Active)
	}
	if cmd == nil {
		t.Fatal("confirm yes on delete_session should return delete cmd")
	}
}

// ---- toggle / compact ----

func TestToggleAgentModeToggleFullAccessTriggerCompact(t *testing.T) {
	app := newReadyApp(t)
	app.toggleAgentMode()
	if app.state.Runtime.AgentMode != "build" {
		t.Fatalf("agent mode=%s, want build", app.state.Runtime.AgentMode)
	}
	app.toggleAgentMode()
	if app.state.Runtime.AgentMode != "plan" {
		t.Fatalf("agent mode=%s, want plan", app.state.Runtime.AgentMode)
	}
	app.toggleFullAccess()
	if !app.state.Runtime.FullAccess {
		t.Fatal("full access should be on")
	}
	app.triggerCompact()
	if !lastContains(app, "Compact triggered") {
		t.Fatal("compact not triggered")
	}
}

// ---- tea.Cmd 工厂 ----

func TestCmdFactories(t *testing.T) {
	client, err := fakegateway.NewFakeClient(fakegateway.ScenarioDefault)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "session-ghost-console"

	if msg := submitMessageCmd(client, sessionID, "hi")(); msg == nil {
		t.Fatal("submitMessageCmd returned nil")
	}
	if msg := resolvePermissionCmd(client, gateway.PermissionDecision{Allow: true, Reason: "y"})(); msg == nil {
		t.Fatal("resolvePermissionCmd returned nil")
	}
	if msg := answerQuestionCmd(client, gateway.UserQuestionAnswer{Text: "a"})(); msg == nil {
		t.Fatal("answerQuestionCmd returned nil")
	}
	if msg := cancelRunCmd(client, sessionID, "run-1")(); msg == nil {
		t.Fatal("cancelRunCmd returned nil")
	}
	if msg := createSessionCmd(client)(); msg == nil {
		t.Fatal("createSessionCmd returned nil")
	}
	if msg := deleteSessionCmd(client, sessionID)(); msg == nil {
		t.Fatal("deleteSessionCmd returned nil")
	}
	if msg := loadSessionCmd(client, sessionID)(); msg == nil {
		t.Fatal("loadSessionCmd returned nil")
	}
	// errorEvent 包装
	if ge := errorEvent(errSentinel("x")); ge.Type != gateway.EventError {
		t.Fatalf("errorEvent wrong: %+v", ge)
	}
}

func TestCmdFactoriesErrorPath(t *testing.T) {
	// 关闭客户端 -> 各 RPC 返回 errorEvent
	client, _ := fakegateway.NewFakeClient(fakegateway.ScenarioDefault)
	_ = client.Close()

	for _, c := range []tea.Cmd{
		submitMessageCmd(client, "s", "x"),
		resolvePermissionCmd(client, gateway.PermissionDecision{}),
		answerQuestionCmd(client, gateway.UserQuestionAnswer{}),
		cancelRunCmd(client, "s", "r"),
		loadSessionCmd(client, "s"),
	} {
		msg := c()
		ge, ok := msg.(gatewayEventMsg)
		if !ok {
			t.Fatalf("expected gatewayEventMsg on closed client, got %T", msg)
		}
		if ge.event.Type != gateway.EventError {
			t.Fatalf("expected EventError, got %s", ge.event.Type)
		}
	}
}

func TestLoadInitialCmdOffline(t *testing.T) {
	client, _ := fakegateway.NewFakeClient(fakegateway.ScenarioGatewayOffline)
	msg := loadInitialCmd(client)()
	loaded, ok := msg.(initialLoadedMsg)
	if !ok {
		t.Fatalf("expected initialLoadedMsg, got %T", msg)
	}
	if loaded.errText == "" {
		t.Fatal("offline scenario should set errText")
	}
}

// ---- 模式键分发 ----

func TestNormalModeKeyDispatch(t *testing.T) {
	app := newReadyApp(t)
	app.state.Mode = state.NormalMode

	// i -> 进入 InputMode
	updated, _ := app.Update(keyRunes("i"))
	app = updated.(*App)
	if app.state.Mode != state.InputModeInput {
		t.Fatalf("after i: mode=%v, want input", app.state.Mode)
	}

	// q -> 退出
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	if _, cmd := app.Update(keyRunes("q")); cmd == nil {
		t.Fatal("q should return quit cmd")
	}

	// Space -> Leader
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	updated, _ = app.Update(keyRunes(" "))
	app = updated.(*App)
	if app.state.Mode != state.LeaderMode {
		t.Fatalf("after space: mode=%v, want leader", app.state.Mode)
	}

	// ? / : 在 NormalMode 下是预留动作（空操作），不应崩溃也不应开浮层
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	app.Update(keyRunes("?"))
	app.Update(keyRunes(":"))

	// 滚动键交给 stream（不 panic 即可）
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	app.Update(keyRunes("j"))
	app.Update(keyRunes("k"))
	app.Update(keyRunes("g"))
	app.Update(keyRunes("G"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
}

func TestLeaderKeyDispatch(t *testing.T) {
	cases := map[string]state.OverlayType{
		"p": state.OverlayPalette,
		"s": state.OverlaySessionPicker,
		"h": state.OverlayHelp,
	}
	for key, wantOverlay := range cases {
		t.Run(key, func(t *testing.T) {
			app := newReadyApp(t)
			app.state.Mode = state.LeaderMode
			updated, _ := app.Update(keyRunes(key))
			app = updated.(*App)
			if app.state.Overlay.Active != wantOverlay {
				t.Fatalf("leader %s: overlay=%q, want %q", key, app.state.Overlay.Active, wantOverlay)
			}
			if app.state.Mode != state.NormalMode {
				t.Fatalf("leader suffix should reset to normal, mode=%v", app.state.Mode)
			}
		})
	}
	// m -> model picker(openOverlay nil), f -> full access(nil),
	// c -> cancel run(空闲 nil), l -> log(nil)（均返回 nil cmd）
	for _, key := range []string{"m", "f", "c", "l"} {
		app := newReadyApp(t)
		app.state.Mode = state.LeaderMode
		if _, cmd := app.Update(keyRunes(key)); cmd != nil {
			t.Fatalf("leader %s should return nil cmd", key)
		}
	}
	// n -> 创建会话（有 client 时返回 create cmd）
	app := newReadyApp(t)
	app.state.Mode = state.LeaderMode
	if _, cmd := app.Update(keyRunes("n")); cmd == nil {
		t.Fatal("leader n should return create cmd")
	}
	// esc -> 回到 normal
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = updated.(*App)
	if app.state.Mode != state.NormalMode {
		t.Fatalf("leader esc should reset to normal, mode=%v", app.state.Mode)
	}
}

// TestLeaderNewActions 覆盖 Phase 10 新增的 Leader 动作：m/c/r/space 与边界。
func TestLeaderNewActions(t *testing.T) {
	// m -> model_picker overlay
	app := newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.Update(keyRunes("m"))
	if app.state.Overlay.Active != state.OverlayModelPicker {
		t.Fatalf("leader m: overlay=%q, want model_picker", app.state.Overlay.Active)
	}

	// c 运行中 -> cancel cmd；空闲 -> nil
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.state.Runtime.Phase = state.RuntimePhaseRunning
	if _, cmd := app.Update(keyRunes("c")); cmd == nil {
		t.Fatal("leader c running should return cancel cmd")
	}
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	if _, cmd := app.Update(keyRunes("c")); cmd != nil {
		t.Fatal("leader c idle should return nil")
	}

	// r 无历史 -> 提示 nil；有历史 -> submit cmd
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	if _, cmd := app.Update(keyRunes("r")); cmd != nil {
		t.Fatal("leader r with no history should return nil")
	}
	if !lastContains(app, "No previous run to retry") {
		t.Fatalf("leader r hint missing: %v", streamContents(app))
	}
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.lastUserText = "hello"
	if _, cmd := app.Update(keyRunes("r")); cmd == nil {
		t.Fatal("leader r with history should return submit cmd")
	}

	// space 无上一会话 -> 提示 nil；有上一会话 -> load cmd
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	if _, cmd := app.Update(keyRunes(" ")); cmd != nil {
		t.Fatal("leader space with no prev session should return nil")
	}
	if !lastContains(app, "No previous session to switch") {
		t.Fatalf("leader space hint missing: %v", streamContents(app))
	}
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.prevSessionID = "sess-prev"
	if _, cmd := app.Update(keyRunes(" ")); cmd == nil {
		t.Fatal("leader space with prev session should return load cmd")
	}

	// 非后缀键 -> 静默回 Normal，不执行动作
	app = newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.Update(keyRunes("x"))
	if app.state.Mode != state.NormalMode {
		t.Fatalf("leader non-suffix should reset to normal, mode=%v", app.state.Mode)
	}
}

func TestInputModeKeyDispatch(t *testing.T) {
	app := newReadyApp(t)
	// esc -> normal
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = updated.(*App)
	if app.state.Mode != state.NormalMode {
		t.Fatalf("esc: mode=%v", app.state.Mode)
	}
	// ctrl+p -> palette
	app = newReadyApp(t)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	app = updated.(*App)
	if app.state.Overlay.Active != "palette" {
		t.Fatalf("ctrl+p: overlay=%q", app.state.Overlay.Active)
	}
	// ctrl+l -> 日志提示
	app = newReadyApp(t)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if !lastContains(app, "Log viewer not yet available") {
		t.Fatalf("ctrl+l not logged: %v", streamContents(app))
	}
}

// TestCtrlDContextual 覆盖 Ctrl+D 三态分发。
func TestCtrlDContextual(t *testing.T) {
	// Input 空输入 → EOF 退出（tea.Quit）
	app := newReadyApp(t)
	app.state.Mode = state.InputModeInput
	app.state.Input.Text = ""
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("ctrl+d empty input should return quit cmd")
	}

	// Input 非空 → 删字符（不退出）
	app = newReadyApp(t)
	app.state.Mode = state.InputModeInput
	app.state.Input.Text = "abc"
	app.state.Input.Cursor = 3
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	// 删除后文本应变少（delete 删光标后字符，光标在末尾时 no-op，移到中间验证）
	app.state.Input.Cursor = 1
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if app.state.Input.Text != "ac" {
		t.Fatalf("ctrl+d non-empty should delete char after cursor, text=%q", app.state.Input.Text)
	}

	// Normal → 半页下翻（路由 stream，不退出）
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd != nil {
		t.Fatal("normal ctrl+d should route to stream, nil cmd")
	}
}

// TestExCommand 覆盖 : 命令行各分支。
func TestExCommand(t *testing.T) {
	cases := map[string]func(*App) bool{
		"":        func(a *App) bool { return lastContains(a, "Unknown ex command") },
		"debug":   func(a *App) bool { return a.debug },
		"help":    func(a *App) bool { return a.state.Overlay.Active == state.OverlayHelp },
		"compact": func(a *App) bool { return lastContains(a, "Compact triggered") },
		"mode":    func(a *App) bool { return a.state.Runtime.AgentMode == "build" },
		"bogus":   func(a *App) bool { return lastContains(a, "Unknown ex command") },
	}
	for cmd, check := range cases {
		t.Run(cmd, func(t *testing.T) {
			app := newReadyApp(t)
			app.Update(components.ExCommandMsg{Command: cmd})
			if !check(app) {
				t.Fatalf("ex %q failed: overlay=%q stream=%v", cmd, app.state.Overlay.Active, streamContents(app))
			}
		})
	}
	// q/quit/exit → tea.Quit
	app := newReadyApp(t)
	if _, cmd := app.Update(components.ExCommandMsg{Command: "q"}); cmd == nil {
		t.Fatal("ex q should return quit cmd")
	}
}

// TestExAndSearchOverlayFlow 覆盖 Normal 下 : 与 / 进入输入、提交、取消。
func TestExAndSearchOverlayFlow(t *testing.T) {
	// Normal 下 : → 打开 Ex overlay
	app := newReadyApp(t)
	app.state.Mode = state.NormalMode
	app.Update(keyRunes(":"))
	if app.state.Overlay.Active != state.OverlayEx {
		t.Fatalf("normal : should open ex overlay, got %q", app.state.Overlay.Active)
	}
	// 字符路由给 cmdLine
	app.Update(keyRunes("q"))
	if app.state.Ex.Input != "q" {
		t.Fatalf("ex input=%q, want q", app.state.Ex.Input)
	}
	// Backspace 删除
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if app.state.Ex.Input != "" {
		t.Fatalf("ex backspace input=%q, want empty", app.state.Ex.Input)
	}
	// ExCommandMsg 提交后关闭 Ex overlay（直接驱动，模拟 runtime 投递）
	app.openEx()
	app.Update(components.ExCommandMsg{Command: "debug"})
	if app.state.Overlay.Active != state.OverlayNone {
		t.Fatalf("ex command submit should close overlay, got %q", app.state.Overlay.Active)
	}

	// Normal 下 / → 打开 Search overlay
	app = newReadyApp(t)
	app.state.Mode = state.NormalMode
	app.Update(keyRunes("/"))
	if app.state.Overlay.Active != state.OverlaySearch {
		t.Fatalf("normal / should open search overlay, got %q", app.state.Overlay.Active)
	}
	app.Update(keyRunes("e"))
	app.Update(keyRunes("r"))
	app.Update(keyRunes("r"))
	if app.state.Search.Query != "err" {
		t.Fatalf("search query=%q, want err", app.state.Search.Query)
	}

	// Esc 关闭 overlay 并清理
	app = newReadyApp(t)
	app.openSearch()
	app.state.Search.Query = "x"
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.state.Overlay.Active != state.OverlayNone {
		t.Fatal("esc should close search overlay")
	}
	if app.state.Search.Query != "" {
		t.Fatalf("esc should clear search query, got %q", app.state.Search.Query)
	}
}

// TestSearchSubmitAndJump 覆盖搜索提交、n/N 跳转、stale 标记。
func TestSearchSubmitAndJump(t *testing.T) {
	app := newReadyApp(t)
	// 注入含匹配的 stream 内容
	app.appendStream(state.StreamEntry{ID: "1", Type: "message", Content: "hello error world"})
	app.appendStream(state.StreamEntry{ID: "2", Type: "message", Content: "all good"})
	app.appendStream(state.StreamEntry{ID: "3", Type: "message", Content: "another ERROR"})

	// 提交搜索 error
	app.Update(components.SearchSubmitMsg{Query: "error"})
	if len(app.state.Search.Matches) != 2 {
		t.Fatalf("matches=%v, want 2", app.state.Search.Matches)
	}
	if app.state.Search.Stale {
		t.Fatal("fresh search should not be stale")
	}

	// n → 跳到下一个
	firstIdx := app.state.Search.MatchIndex
	app.state.Mode = state.NormalMode
	app.Update(keyRunes("n"))
	if app.state.Search.MatchIndex == firstIdx {
		t.Fatal("n should advance match index")
	}
	// N → 回到上一个
	app.Update(keyRunes("N"))

	// 无匹配搜索
	app.Update(components.SearchSubmitMsg{Query: "zzz"})
	if app.state.Search.Matches != nil {
		t.Fatalf("no match should set Matches nil, got %v", app.state.Search.Matches)
	}
	// n 在无匹配时 no-op 不崩溃
	app.Update(keyRunes("n"))

	// 空查询 no-op
	app.Update(components.SearchSubmitMsg{Query: "   "})
}

// TestSearchStaleOnStreamGrowth 覆盖 stream 增长后 stale 标记。
func TestSearchStaleOnStreamGrowth(t *testing.T) {
	app := newReadyApp(t)
	app.appendStream(state.StreamEntry{ID: "1", Type: "message", Content: "error one"})
	app.Update(components.SearchSubmitMsg{Query: "error"})
	if app.state.Search.Stale {
		t.Fatal("search should not be stale initially")
	}
	// 模拟 gateway 事件追加 stream
	app.Update(gatewayEventMsg{event: gateway.GatewayEvent{
		Type:    gateway.EventAgentMessageStart,
		At:      time.Now(),
		Payload: map[string]any{"text": "new error"},
	}})
	if !app.state.Search.Stale {
		t.Fatal("search should be stale after stream growth")
	}
}

func TestRouteStreamKey(t *testing.T) {
	app := newReadyApp(t)
	if ok, _ := app.routeStreamKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); !ok {
		t.Fatal("j should route to stream")
	}
	if ok, _ := app.routeStreamKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); ok {
		t.Fatal("x should not route to stream")
	}
}

// ---- 鼠标 ----

func TestHandleMouseMsgMainViewAndOverlays(t *testing.T) {
	// 主视图滚轮：MouseWheelUp 增加 offset，MouseWheelDown 减少（内容不足也允许推进）
	app := newReadyApp(t)
	app.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	if app.state.Layout.ScrollOffset == 0 {
		t.Fatal("MouseWheelUp should increase scroll offset")
	}
	app.Update(tea.MouseMsg{Type: tea.MouseWheelDown})

	// 浮层鼠标分发不 panic：组件按 Button 判定，故设置 Button
	for _, active := range []state.OverlayType{state.OverlayPalette, state.OverlaySessionPicker, state.OverlayModelPicker} {
		app := newReadyApp(t)
		app.openOverlay(active)
		app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		app.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: 4})
	}
}

// ---- View 各路径 ----

func TestViewOverlayAndMainPaths(t *testing.T) {
	overlays := []state.OverlayType{
		state.OverlayPalette, state.OverlayHelp,
		state.OverlaySessionPicker, state.OverlayModelPicker, state.OverlayConfirm,
	}
	for _, ov := range overlays {
		t.Run(string(ov), func(t *testing.T) {
			app := newReadyApp(t)
			app.openOverlay(ov)
			if app.state.Overlay.Active != ov {
				app.openOverlay(ov)
			}
			view := app.View()
			if view == "" {
				t.Fatalf("%s view empty", ov)
			}
		})
	}

	// 主视图：error 行 + debug 行
	app := newReadyApp(t)
	app.lastErr = "boom"
	app.debug = true
	view := app.View()
	if !strings.Contains(view, "boom") {
		t.Fatal("error line not rendered")
	}
	if !strings.Contains(view, "[debug]") {
		t.Fatal("debug line not rendered")
	}
}

func TestViewInspectorLayoutBreakpoints(t *testing.T) {
	for _, w := range []int{79, 90, 120} {
		app := newReadyApp(t)
		app.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		if view := app.View(); view == "" {
			t.Fatalf("width %d view empty", w)
		}
	}
}

func TestViewZeroSizeFallback(t *testing.T) {
	app := newReadyApp(t)
	app.state.Layout.Width = 0
	app.state.Layout.Height = 0
	if view := app.View(); view == "" {
		t.Fatal("zero-size view empty")
	}
}

// ---- 辅助 ----

func TestUtilityHelpers(t *testing.T) {
	if emptyDash("") != "-" || emptyDash("x") != "x" {
		t.Fatal("emptyDash wrong")
	}
	if inputModeName(state.NormalMode) != "normal" || inputModeName(state.LeaderMode) != "leader" || inputModeName(state.InputModeInput) != "input" {
		t.Fatal("inputModeName wrong")
	}
	app := newReadyApp(t)
	if app.activeSessionID() == "" {
		t.Fatal("activeSessionID should be non-empty after load")
	}
	if app.activeSessionTitle() == "untitled" {
		// 默认场景首个会话有标题
		t.Fatalf("activeSessionTitle=%q, want real title", app.activeSessionTitle())
	}
	// 无活动会话
	app.state.Gateway.ActiveSess = nil
	if app.activeSessionID() != "" || app.activeSessionTitle() != "untitled" {
		t.Fatal("nil session fallback wrong")
	}
	// 无标题会话 -> 回退到 ID
	app.state.Gateway.ActiveSess = &gateway.SessionSummary{ID: "id-only"}
	if app.activeSessionTitle() != "id-only" {
		t.Fatalf("title fallback wrong: %q", app.activeSessionTitle())
	}
}

func TestSeparatorAndFitLine(t *testing.T) {
	if s := separatorLineHelper(10); !strings.Contains(s, "─") {
		t.Fatal("separatorLine missing dash")
	}
	if fitLine("abc", 0) != "abc" {
		t.Fatal("fitLine width<=0 should return as-is")
	}
	if fitLine("abc", 1) != "" {
		t.Fatal("fitLine target<=0 should return empty")
	}
}

// ---- 测试辅助函数 ----

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func keyRunes(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func lastContains(app *App, sub string) bool {
	if len(app.state.Stream) == 0 {
		return false
	}
	return strings.Contains(app.state.Stream[len(app.state.Stream)-1].Content, sub)
}

func streamContents(app *App) []string {
	out := make([]string, len(app.state.Stream))
	for i, e := range app.state.Stream {
		out[i] = e.Content
	}
	return out
}

// separatorLineHelper 以给定宽度渲染分隔线（覆盖 width<=0 分支）。
func separatorLineHelper(width int) string {
	app := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	app.state.Layout.Width = width
	return app.separatorLine()
}

// ---- 第 1 轮审查盲区补测 ----

// TestLeaderTimeoutCmdAndRecovery 覆盖 leaderTimeoutCmd 与超时回退分支（原 50%）。
func TestLeaderTimeoutCmdAndRecovery(t *testing.T) {
	cmd := leaderTimeoutCmd()
	if cmd == nil {
		t.Fatal("leaderTimeoutCmd should return non-nil cmd")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("leader timeout cmd produced nil msg")
	}
	app := newReadyApp(t)
	app.state.Mode = state.LeaderMode
	app.Update(msg)
	if app.state.Mode != state.NormalMode {
		t.Fatalf("leader timeout should reset to normal, mode=%v", app.state.Mode)
	}
	app = newReadyApp(t)
	app.state.Mode = state.InputModeInput
	beforeMode := app.state.Mode
	app.Update(msg)
	if app.state.Mode != beforeMode {
		t.Fatalf("leader timeout in non-leader mode should be no-op, mode=%v", app.state.Mode)
	}
}

// TestScrollToStreamIndexBoundaries 直接覆盖 scrollToStreamIndex 边界（原 71.4%）。
func TestScrollToStreamIndexBoundaries(t *testing.T) {
	app := newReadyApp(t)
	app.appendStream(state.StreamEntry{ID: "1", Type: "message", Content: "a"})
	app.appendStream(state.StreamEntry{ID: "2", Type: "message", Content: "b"})
	app.scrollToStreamIndex(0)
	if app.state.Layout.AutoScroll {
		t.Fatal("scrollToStreamIndex should disable AutoScroll")
	}
	prevOffset := app.state.Layout.ScrollOffset
	app.scrollToStreamIndex(-1)
	if app.state.Layout.ScrollOffset != prevOffset {
		t.Fatal("scrollToStreamIndex(-1) should be no-op")
	}
	app.scrollToStreamIndex(100)
	if app.state.Layout.ScrollOffset != prevOffset {
		t.Fatal("scrollToStreamIndex(overflow) should be no-op")
	}
	emptyApp := newReadyApp(t)
	emptyApp.state.Stream = nil
	emptyApp.scrollToStreamIndex(0)
}

// TestCancelCurrentRunNoClient 覆盖 cancelCurrentRun 在 client==nil 的分支（原 71.4%）。
func TestCancelCurrentRunNoClient(t *testing.T) {
	app := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	app.state.Runtime.Phase = state.RuntimePhaseRunning
	cmd := app.cancelCurrentRun()
	if cmd != nil {
		t.Fatal("cancelCurrentRun without client should return nil cmd")
	}
	if app.state.Runtime.Phase != state.RuntimePhaseCancelled {
		t.Fatalf("phase=%s, want cancelled", app.state.Runtime.Phase)
	}
	app2 := NewApp(StartupConfig{Backend: "fake", Scenario: "default"}).(*App)
	app2.state.Runtime.Phase = state.RuntimePhaseIdle
	cmd = app2.cancelCurrentRun()
	if cmd != nil {
		t.Fatal("cancelCurrentRun idle without client should return nil")
	}
	if app2.state.Runtime.Phase != state.RuntimePhaseIdle {
		t.Fatal("idle phase should be unchanged")
	}
}

// TestPromptModeLineExported 覆盖导出的 ModeLine 方法（原 0%）。
func TestPromptModeLineExported(t *testing.T) {
	vs := state.NewViewState()
	vs.Layout.Width = 80
	p := components.NewCommandPrompt(vs)
	for _, mode := range []state.InputMode{state.InputModeInput, state.NormalMode, state.LeaderMode} {
		vs.Mode = mode
		if v := p.ModeLine(); v == "" {
			t.Fatalf("ModeLine() empty for mode %v", mode)
		}
	}
}

// TestLoadInitialCmdErrorPaths 覆盖 loadInitialCmd 的失败分支（原 71.4%）。
func TestLoadInitialCmdErrorPaths(t *testing.T) {
	client, _ := fakegateway.NewFakeClient(fakegateway.ScenarioGatewayOffline)
	msg := loadInitialCmd(client)()
	loaded, ok := msg.(initialLoadedMsg)
	if !ok {
		t.Fatalf("expected initialLoadedMsg, got %T", msg)
	}
	if loaded.errText == "" {
		t.Fatal("offline scenario should set errText")
	}
	client2, _ := fakegateway.NewFakeClient(fakegateway.ScenarioDefault)
	_ = client2.Close()
	msg2 := loadInitialCmd(client2)()
	loaded2, ok := msg2.(initialLoadedMsg)
	if !ok {
		t.Fatalf("expected initialLoadedMsg on closed client, got %T", msg2)
	}
	if loaded2.errText == "" {
		t.Fatal("closed client should set errText")
	}
}

// TestExecuteSearchEmptyStream 覆盖空 stream 下的搜索（边界）。
func TestExecuteSearchEmptyStream(t *testing.T) {
	app := newReadyApp(t)
	app.state.Stream = nil
	app.executeSearch("anything")
	if app.state.Search.Matches != nil {
		t.Fatal("empty stream search should yield nil matches")
	}
}

// TestExecuteExCommandEmpty 覆盖空 ex 命令的提示分支。
func TestExecuteExCommandEmpty(t *testing.T) {
	app := newReadyApp(t)
	app.executeExCommand("")
	if !lastContains(app, "Unknown ex command") {
		t.Fatalf("empty ex command should hint unknown, stream=%v", streamContents(app))
	}
}

// TestSearchJumpTargetVisibleInAppView 端到端：搜索跳转后目标内容在 App.View 可见。
// 这是 PR 审核反馈的核心修复点——旧代码跳转后匹配项落在视口外。
func TestSearchJumpTargetVisibleInAppView(t *testing.T) {
	app := newReadyApp(t)
	// 注入足够多的 entry 使视口装不下
	for i := 0; i < 30; i++ {
		app.appendStream(state.StreamEntry{
			ID:      fmt.Sprintf("e-%d", i),
			Type:    "message",
			Content: fmt.Sprintf("content-%d", i),
		})
	}
	// 搜索靠后的唯一内容
	app.Update(components.SearchSubmitMsg{Query: "content-28"})
	if len(app.state.Search.Matches) == 0 {
		t.Fatal("search should match content-28")
	}
	// n/N 跳转后 View 应含目标
	view := app.View()
	if !strings.Contains(view, "content-28") {
		t.Fatalf("search jump: target content-28 not visible after jump, offset=%d", app.state.Layout.ScrollOffset)
	}
}
