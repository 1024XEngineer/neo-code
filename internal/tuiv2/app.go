// Package tuiv2 实现 Ghost Console TUI v2 的应用骨架。
package tuiv2

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/tuiv2/components"
	"neo-code/internal/tuiv2/gateway"
	"neo-code/internal/tuiv2/keymap"
	"neo-code/internal/tuiv2/state"
	"neo-code/internal/tuiv2/theme"
)

const (
	defaultTerminal      = "0x0"
	inspectorWideWidth   = 30
	inspectorHiddenWidth = 80
	inspectorWideMin     = 100
)

// StartupConfig 承载 TUI v2 独立入口解析出的启动参数和 Gateway 客户端。
type StartupConfig struct {
	Backend  string
	Scenario string
	Debug    bool
	Client   gateway.Client
}

// App 是 TUI v2 的根组件，负责持有 Gateway 客户端、集中式 ViewState 和顶层消息路由。
type App struct {
	client gateway.Client
	state  *state.ViewState
	debug  bool

	backend  string
	scenario string
	eventCh  <-chan gateway.GatewayEvent
	lastErr  string

	// Ctrl+C 双退保护
	lastCtrlC time.Time

	// Leader 重试与切换上一会话所需的私有运行时状态（不入 ViewState，非渲染状态）
	lastUserText  string // 最近一次发送的用户消息文本，供 Space r 重试
	prevSessionID string // 上一个会话 ID，供 Space Space 切换

	ambientStatus  *components.AmbientStatus
	agentStream    *components.AgentStream
	commandPrompt  *components.CommandPrompt
	cmdLine        *components.CmdLine
	softInspector  *components.SoftInspector
	palette        *components.Palette
	helpOverlay    *components.HelpOverlay
	sessionPicker  *components.SessionPicker
	modelPicker    *components.ModelPicker
	confirmOverlay *components.ConfirmOverlay
}

var _ tea.Model = (*App)(nil)

// NewApp 创建 TUI v2 根组件，并初始化集中式 ViewState。
func NewApp(cfg StartupConfig) tea.Model {
	viewState := state.NewViewState()
	return &App{
		client:         cfg.Client,
		state:          viewState,
		debug:          cfg.Debug,
		backend:        cfg.Backend,
		scenario:       cfg.Scenario,
		ambientStatus:  components.NewAmbientStatus(viewState),
		agentStream:    components.NewAgentStream(viewState),
		commandPrompt:  components.NewCommandPrompt(viewState),
		cmdLine:        components.NewCmdLine(viewState),
		softInspector:  components.NewSoftInspector(viewState),
		palette:        components.NewPalette(viewState),
		helpOverlay:    components.NewHelpOverlay(viewState),
		sessionPicker:  components.NewSessionPicker(viewState),
		modelPicker:    components.NewModelPicker(viewState),
		confirmOverlay: components.NewConfirmOverlay(viewState),
	}
}

// Init 通过 Gateway 客户端检查连接并加载初始 ViewState。
func (a *App) Init() tea.Cmd {
	if a.client == nil {
		return a.commandPrompt.Init()
	}
	return loadInitialCmd(a.client)
}

// Update 处理全局消息，并把 Gateway 结果映射到集中式 ViewState。
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.applyWindowSize(msg.Width, msg.Height)
		return a, tea.ClearScreen
	case tea.KeyMsg:
		return a.handleKeyMsg(msg)
	case tea.MouseMsg:
		return a.handleMouseMsg(msg)
	case initialLoadedMsg:
		a.applyInitialLoaded(msg)
		if msg.eventCh != nil {
			return a, tea.Batch(waitEventCmd(msg.eventCh), a.commandPrompt.Init())
		}
		return a, a.commandPrompt.Init()
	case gatewayEventMsg:
		if msg.closed {
			return a, nil
		}
		beforeStreamLen := len(a.state.Stream)
		a.state = state.Reduce(a.state, msg.event)
		if len(a.state.Stream) > beforeStreamLen {
			a.state.Layout.AutoScroll = true
			a.state.Layout.ScrollOffset = 0
			// stream 增长后，已有搜索结果不再包含新内容，标记 stale 提示用户。
			if len(a.state.Search.Matches) > 0 {
				a.state.Search.Stale = true
			}
		}
		// 新 run 开始或会话相关事件时清理 Normal 子状态（搜索/Ex），避免跨 run/会话残留。
		switch msg.event.Type {
		case gateway.EventRunStarted:
			a.clearSearchAndEx()
		}
		a.bindComponents()
		if a.state.Runtime.Phase == state.RuntimePhaseError && len(a.state.Stream) > 0 {
			a.lastErr = a.state.Stream[len(a.state.Stream)-1].Content
		}
		return a, waitEventCmd(a.eventCh)
	case components.SubmitMessageMsg:
		return a, a.handleSubmitMessage(msg)
	case components.PermissionActionMsg:
		return a, a.handlePermissionAction(msg)
	case components.QuestionAnswerMsg:
		return a, a.handleQuestionAnswer(msg)
	case components.PromptCancelMsg:
		a.cancelPrompt(msg.Mode)
		return a, nil
	case components.ExCommandMsg:
		// : 命令行提交：执行命令并关闭 Ex overlay（命令打开新 overlay 者除外）。
		cmd := a.executeExCommand(msg.Command)
		if a.state.Overlay.Active == state.OverlayEx {
			a.closeOverlay()
			a.state.Ex = state.ExState{}
		}
		return a, cmd
	case components.SearchSubmitMsg:
		// / 搜索提交：执行扫描并关闭搜索 overlay 回 Normal。
		cmd := a.executeSearch(msg.Query)
		a.closeOverlay()
		a.state.Search.Active = false
		return a, cmd
	case components.CmdLineCancelMsg:
		// Esc/Ctrl+C 取消 Ex/Search 输入：关闭 overlay 回 Normal。
		a.closeOverlay()
		a.clearSearchAndEx()
		return a, nil
	case components.SlashCommandMsg:
		return a, a.handleSlashCommand(msg)
	case components.PaletteCommandMsg:
		return a, a.handlePaletteCommand(msg)
	case components.SessionSelectMsg:
		return a, a.handleSessionSelect(msg)
	case components.SessionDeleteMsg:
		return a, a.handleSessionDelete(msg)
	case components.ModelSelectMsg:
		return a, a.handleModelSelect(msg)
	case components.ConfirmYesMsg:
		return a, a.handleConfirmYes(msg)
	case components.ConfirmNoMsg:
		a.state.Confirm = state.ConfirmState{}
		a.closeOverlay()
		return a, nil
	case leaderTimeoutMsg:
		if a.state.Mode == state.LeaderMode {
			a.state.Mode = state.NormalMode
		}
		return a, nil
	case sessionSwitchedMsg:
		a.eventCh = msg.eventCh
		if msg.detail != nil {
			a.state.Stream = nil
			a.state.Runtime.Tokens = state.TokenUsage{
				Input:  msg.detail.Usage.Input,
				Output: msg.detail.Usage.Output,
				Total:  msg.detail.Usage.Total,
			}
			for _, item := range msg.detail.Stream {
				a.appendStream(streamEntryFromItem(item))
			}
		}
		// 会话历史重载会清空 Stream，因此切换提示必须在重载之后追加，
		// 否则用户无法确认是否切换成功。
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("session-switch-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   fmt.Sprintf("Switched to session: %s", a.activeSessionTitle()),
			Metadata:  map[string]any{"done": true},
		})
		a.bindComponents()
		if a.eventCh != nil {
			return a, waitEventCmd(a.eventCh)
		}
		return a, nil
	case sessionCreatedMsg:
		return a, a.handleSessionCreated(msg)
	}
	return a, a.routeComponents(msg)
}

// View 自上而下拼接 Focus-Only 静态布局，宽屏时将 Soft Inspector 放到右侧。
func (a *App) View() string {
	// 浮层模式下覆盖主视图
	switch a.state.Overlay.Active {
	case state.OverlayPalette:
		return a.fitViewToTerminal(a.palette.View())
	case state.OverlayHelp:
		return a.fitViewToTerminal(a.helpOverlay.View())
	case state.OverlaySessionPicker:
		return a.fitViewToTerminal(a.sessionPicker.View())
	case state.OverlayModelPicker:
		return a.fitViewToTerminal(a.modelPicker.View())
	case state.OverlayConfirm:
		return a.fitViewToTerminal(a.confirmOverlay.View())
	}
	lines := []string{
		a.ambientStatus.View(),
		a.separatorLine(),
	}
	if a.lastErr != "" {
		lines = append(lines, theme.ErrorStyle().Render("  "+theme.StatusSymbol(theme.PhaseError)+" "+a.lastErr))
	}
	// Ex/Search 输入 overlay：渲染 cmdline 输入行（替代普通 prompt 输入区），
	// 不覆盖主视图，仍保留 ambient/stream 可见。
	promptView := a.commandPrompt.View()
	if a.state.Overlay.Active == state.OverlayEx || a.state.Overlay.Active == state.OverlaySearch {
		promptView = a.cmdLine.View() + "\n" + a.commandPrompt.ModeLine()
	}
	lines = append(lines, a.mainArea(), a.separatorLine(), promptView)
	if a.debug {
		lines = append(lines, "", theme.WarningStyle().Render(a.debugLine()))
	}
	return a.fitViewToTerminal(strings.Join(lines, "\n"))
}

func (a *App) routeComponents(msg tea.Msg) tea.Cmd {
	_, statusCmd := a.ambientStatus.Update(msg)
	_, streamCmd := a.agentStream.Update(msg)
	_, inspectorCmd := a.softInspector.Update(msg)
	_, promptCmd := a.commandPrompt.Update(msg)
	return tea.Batch(statusCmd, streamCmd, inspectorCmd, promptCmd)
}

// routeStreamKey 将滚动按键优先交给 Agent Stream，避免与全局快捷键混淆。
func (a *App) routeStreamKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "j", "k", "g", "G":
		_, cmd := a.agentStream.Update(msg)
		return true, cmd
	default:
		return false, nil
	}
}

// handleMouseMsg 将鼠标事件分发到当前活跃的组件或浮层。
func (a *App) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 浮层激活时，鼠标交给浮层组件
	switch a.state.Overlay.Active {
	case state.OverlayPalette:
		_, cmd := a.palette.Update(msg)
		return a, cmd
	case state.OverlaySessionPicker:
		_, cmd := a.sessionPicker.Update(msg)
		return a, cmd
	case state.OverlayModelPicker:
		_, cmd := a.modelPicker.Update(msg)
		return a, cmd
	}
	// 主视图下，滚轮事件交给 Agent Stream
	switch msg.Type {
	case tea.MouseWheelUp:
		a.state.Layout.ScrollOffset++
		a.state.Layout.AutoScroll = false
	case tea.MouseWheelDown:
		if a.state.Layout.ScrollOffset > 0 {
			a.state.Layout.ScrollOffset--
		}
		a.state.Layout.AutoScroll = a.state.Layout.ScrollOffset == 0
	}
	return a, nil
}

// handleKeyMsg 根据当前模式分发键盘消息。
func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Esc always closes any active overlay first (global escape hatch)
	if a.state.Overlay.Active != state.OverlayNone {
		if msg.String() == "esc" {
			// 关闭 overlay 并清理 Normal 子状态（搜索/Ex 输入）。
			a.closeOverlay()
			a.clearSearchAndEx()
			return a, nil
		}
	}
	// 浮层激活时，键盘消息交给对应浮层组件处理
	switch a.state.Overlay.Active {
	case state.OverlayPalette:
		_, cmd := a.palette.Update(msg)
		return a, cmd
	case state.OverlayHelp:
		_, cmd := a.helpOverlay.Update(msg)
		return a, cmd
	case state.OverlaySessionPicker:
		_, cmd := a.sessionPicker.Update(msg)
		return a, cmd
	case state.OverlayModelPicker:
		_, cmd := a.modelPicker.Update(msg)
		return a, cmd
	case state.OverlayConfirm:
		_, cmd := a.confirmOverlay.Update(msg)
		return a, cmd
	case state.OverlayEx, state.OverlaySearch:
		// Ex/Search 输入 overlay：所有按键路由给 cmdline 组件，
		// 由它处理字符/Backspace/Enter/Esc（Esc/Ctrl+C 已在上方被全局拦截
		// 关闭 overlay，此处主要处理字符输入与提交）。
		_, cmd := a.cmdLine.Update(msg)
		return a, cmd
	}
	switch a.state.Mode {
	case state.LeaderMode:
		return a.handleLeaderKey(msg)
	case state.NormalMode:
		return a.handleNormalModeKey(msg)
	default: // InputModeInput
		return a.handleInputModeKey(msg)
	}
}

// handleInputModeKey 处理 Input Mode 下的键盘输入。
//
// 分层约定（plan-v4）：模式切换键在此拦截，不传给 prompt 编辑器。
// Ctrl+D 不进 MatchInputKey，由本函数按输入框是否为空决定：
//   - 输入为空 → EOF 退出程序
//   - 输入非空 → 删除光标后字符（等同 delete），委派 prompt 处理
func (a *App) handleInputModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+D 上下文分发
	if msg.String() == "ctrl+d" {
		if strings.TrimSpace(a.state.Input.Text) == "" {
			return a, tea.Quit
		}
		_, cmd := a.commandPrompt.Update(tea.KeyMsg{Type: tea.KeyDelete})
		return a, cmd
	}
	action := keymap.MatchInputKey(msg.String())
	switch action {
	case keymap.ActionCtrlC:
		return a.handleCtrlC()
	case keymap.ActionEscape:
		a.state.Mode = state.NormalMode
		return a, nil
	case keymap.ActionOpenPalette:
		a.openOverlay(state.OverlayPalette)
		return a, nil
	case keymap.ActionLogViewer:
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("log-hint-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   "Log viewer not yet available",
			Metadata:  map[string]any{"done": true},
		})
		return a, nil
	default:
		_, promptCmd := a.commandPrompt.Update(msg)
		return a, promptCmd
	}
}

func (a *App) handleCtrlC() (tea.Model, tea.Cmd) {
	phase := a.state.Runtime.Phase
	if phase == state.RuntimePhaseRunning || phase == state.RuntimePhaseWaitingPermission || phase == state.RuntimePhaseWaitingUser {
		// Agent 运行中 → 取消运行
		if a.client != nil {
			return a, cancelRunCmd(a.client, a.activeSessionID(), a.state.Runtime.RunID)
		}
		a.state.Runtime.Phase = state.RuntimePhaseCancelled
		return a, nil
	}
	// Agent 空闲 → 双退保护
	now := time.Now()
	if !a.lastCtrlC.IsZero() && now.Sub(a.lastCtrlC) < 2*time.Second {
		return a, tea.Quit
	}
	a.lastCtrlC = now
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("ctrlc-hint-%d", now.UnixNano()),
		Type:      "status",
		Timestamp: now,
		Content:   "Press Ctrl+C again to quit",
		Metadata:  map[string]any{"done": true},
	})
	return a, nil
}

// leaderTimeoutMsg 用于 Leader Key 1 秒超时回退。
type leaderTimeoutMsg struct{}

// leaderTimeoutCmd 在 1 秒后发送超时消息，将 Leader 模式回退到 Normal。
func leaderTimeoutCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(_ time.Time) tea.Msg {
		return leaderTimeoutMsg{}
	})
}

// handleSubmitMessage 将用户输入交给 GatewayClient，并让后续 ACK 以事件形式回到 reducer。
func (a *App) handleSubmitMessage(msg components.SubmitMessageMsg) tea.Cmd {
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}
	// 记录最近一次用户输入，供 Leader Space r 重试使用。
	a.lastUserText = msg.Text
	// 立即将用户消息追加到 Stream 中以便渲染
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("user-msg-%d", time.Now().UnixNano()),
		Type:      "message",
		Timestamp: time.Now(),
		Content:   msg.Text,
		Metadata:  map[string]any{"role": "user", "done": true},
	})
	if a.client == nil {
		return nil
	}
	sessionID := a.activeSessionID()
	return submitMessageCmd(a.client, sessionID, msg.Text)
}

// handlePermissionAction 将权限快捷键转换成 GatewayClient 权限决策 RPC。
func (a *App) handlePermissionAction(msg components.PermissionActionMsg) tea.Cmd {
	if a.client == nil {
		return nil
	}
	decision := gateway.PermissionDecision{
		SessionID: a.activeSessionID(),
		RunID:     a.state.Runtime.RunID,
		Allow:     msg.Decision == "y" || msg.Decision == "a",
		Reason:    msg.Decision,
	}
	return resolvePermissionCmd(a.client, decision)
}

// handleQuestionAnswer 将 ask_user 回答交给 GatewayClient，并把完成事件交给 reducer。
func (a *App) handleQuestionAnswer(msg components.QuestionAnswerMsg) tea.Cmd {
	if a.client == nil {
		return nil
	}
	answer := gateway.UserQuestionAnswer{
		SessionID: a.activeSessionID(),
		RunID:     a.state.Runtime.RunID,
		Text:      msg.Text,
	}
	return answerQuestionCmd(a.client, answer)
}

// cancelPrompt 取消当前内联交互，只重置输入 UI 状态，不触碰后端运行状态。
func (a *App) cancelPrompt(mode string) {
	a.state.Input.Mode = state.InputStateModeMessage
	a.state.Input.Text = ""
	a.state.Input.Cursor = 0
	a.state.Input.Prompt = ""
	a.state.Input.Options = nil
	a.state.Mode = state.InputModeInput
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("prompt-cancel-%d", time.Now().UnixNano()),
		Type:      "status",
		Timestamp: time.Now(),
		Content:   fmt.Sprintf("%s cancelled", emptyDash(mode)),
		Metadata:  map[string]any{"done": true},
	})
}

// openOverlay 打开指定类型的浮层，重置搜索状态。
func (a *App) openOverlay(overlayType state.OverlayType) {
	a.state.Overlay.Active = overlayType
	a.state.Overlay.Query = ""
	a.state.Overlay.Selected = 0
}

// closeOverlay 关闭当前浮层，重置搜索与选中状态。
func (a *App) closeOverlay() {
	a.state.Overlay.Active = state.OverlayNone
	a.state.Overlay.Query = ""
	a.state.Overlay.Selected = 0
}

func (a *App) handlePaletteCommand(msg components.PaletteCommandMsg) tea.Cmd {
	switch msg.Name {
	case "/exit":
		return tea.Quit
	case "/help":
		a.openOverlay(state.OverlayHelp)
		return nil
	case "/session":
		a.openOverlay(state.OverlaySessionPicker)
		return nil
	case "/model":
		a.openOverlay(state.OverlayModelPicker)
		return nil
	case "/mode":
		return a.toggleAgentMode()
	case "/compact":
		return a.triggerCompact()
	case "/clear":
		a.state.Stream = nil
		a.bindComponents()
		return nil
	default:
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("cmd-%s-%d", msg.Name, time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   fmt.Sprintf("command %s not yet implemented", msg.Name),
			Metadata:  map[string]any{"done": true},
		})
	}
	return nil
}

// handleSlashCommand 处理 Slash 命令输入。
func (a *App) handleSlashCommand(msg components.SlashCommandMsg) tea.Cmd {
	switch msg.Command {
	case "/exit", "/quit":
		return tea.Quit
	case "/help":
		a.openOverlay(state.OverlayHelp)
		return nil
	case "/session":
		a.openOverlay(state.OverlaySessionPicker)
		return nil
	case "/model":
		a.openOverlay(state.OverlayModelPicker)
		return nil
	case "/mode":
		return a.toggleAgentMode()
	case "/compact":
		return a.triggerCompact()
	case "/clear":
		a.state.Stream = nil
		a.bindComponents()
		return nil
	default:
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("slash-%s-%d", msg.Command, time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   fmt.Sprintf("unknown command: %s", msg.Command),
			Metadata:  map[string]any{"done": true},
		})
	}
	return nil
}

// handleSessionSelect 处理会话切换操作。
//
// 切换前先把当前活动会话 ID 存入 prevSessionID，供 Leader Space Space 回切。
func (a *App) handleSessionSelect(msg components.SessionSelectMsg) tea.Cmd {
	if a.client == nil {
		return nil
	}
	if current := a.activeSessionID(); current != "" && current != msg.Session.ID {
		a.prevSessionID = current
	}
	a.state.Gateway.ActiveSess = &msg.Session
	return loadSessionCmd(a.client, msg.Session.ID)
}

// handleSessionDelete 通过确认弹窗处理会话删除操作。
func (a *App) handleSessionDelete(msg components.SessionDeleteMsg) tea.Cmd {
	if a.client == nil {
		return nil
	}
	a.openConfirm(
		"Delete Session",
		fmt.Sprintf("Are you sure you want to delete this session?"),
		"delete_session",
		map[string]any{"session_id": msg.SessionID},
	)
	return nil
}

// handleSessionCreated 处理新会话创建完成。
func (a *App) handleSessionCreated(msg sessionCreatedMsg) tea.Cmd {
	if msg.err != nil {
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("session-err-%d", time.Now().UnixNano()),
			Type:      "error",
			Timestamp: time.Now(),
			Content:   fmt.Sprintf("Failed to create session: %s", msg.err),
			Metadata:  map[string]any{"done": true},
		})
		return nil
	}
	if msg.Session != nil {
		a.state.Gateway.ActiveSess = msg.Session
		a.appendStream(state.StreamEntry{
			ID:        fmt.Sprintf("session-created-%d", time.Now().UnixNano()),
			Type:      "status",
			Timestamp: time.Now(),
			Content:   fmt.Sprintf("New session created: %s", msg.Session.Title),
			Metadata:  map[string]any{"done": true},
		})
		if a.client != nil {
			return loadSessionCmd(a.client, msg.Session.ID)
		}
	}
	return nil
}

// handleModelSelect 处理模型切换操作。
func (a *App) handleModelSelect(msg components.ModelSelectMsg) tea.Cmd {
	if a.client != nil {
		sessionID := a.activeSessionID()
		if err := a.client.SetModel(context.Background(), sessionID, msg.ModelID); err != nil {
			a.appendStream(state.StreamEntry{
				ID:        fmt.Sprintf("model-err-%d", time.Now().UnixNano()),
				Type:      "error",
				Timestamp: time.Now(),
				Content:   fmt.Sprintf("Failed to switch model: %s", err),
				Metadata:  map[string]any{"done": true},
			})
			return nil
		}
	}
	if a.state.Gateway.ActiveSess != nil {
		a.state.Gateway.ActiveSess.Model = msg.ModelID
	}
	a.state.Gateway.ActiveModel = msg.ModelID
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("model-switch-%d", time.Now().UnixNano()),
		Type:      "status",
		Timestamp: time.Now(),
		Content:   fmt.Sprintf("Model switched to %s", msg.ModelName),
		Metadata:  map[string]any{"done": true},
	})
	return nil
}

// handleConfirmYes 处理确认弹窗的确认操作。
func (a *App) handleConfirmYes(msg components.ConfirmYesMsg) tea.Cmd {
	confirm := a.state.Confirm
	a.state.Confirm = state.ConfirmState{}
	a.closeOverlay()
	switch confirm.Action {
	case "delete_session":
		sessionID, _ := confirm.Data["session_id"].(string)
		if sessionID != "" && a.client != nil {
			return deleteSessionCmd(a.client, sessionID)
		}
	}
	return nil
}

// openConfirm 打开确认弹窗。
func (a *App) openConfirm(title, message, action string, data map[string]any) {
	a.state.Confirm = state.ConfirmState{
		Title:   title,
		Message: message,
		Action:  action,
		Data:    data,
	}
	a.state.Overlay.Active = state.OverlayConfirm
	a.state.Overlay.Query = ""
	a.state.Overlay.Selected = 0
}

// activeSessionID 返回当前会话 ID，缺失时使用空字符串让 GatewayClient 自行决定错误语义。
func (a *App) activeSessionID() string {
	if a.state.Gateway.ActiveSess != nil {
		return a.state.Gateway.ActiveSess.ID
	}
	return ""
}

// activeSessionTitle 返回当前会话标题，缺失时回退到会话 ID 或占位文本。
func (a *App) activeSessionTitle() string {
	if a.state.Gateway.ActiveSess != nil {
		if a.state.Gateway.ActiveSess.Title != "" {
			return a.state.Gateway.ActiveSess.Title
		}
		return a.state.Gateway.ActiveSess.ID
	}
	return "untitled"
}

func (a *App) appendStream(entry state.StreamEntry) {
	a.state.Stream = append(a.state.Stream, entry)
}

// bindComponents 将子组件重新绑定到当前 ViewState 指针。
// 注意：state.Reduce 每次返回新的 *ViewState，a.state 会被替换，因此所有
// 子组件（含浮层：palette / help / sessionPicker / modelPicker / confirmOverlay）
// 都必须在这里重新绑定，否则会持有旧指针，导致浮层交互改到废弃状态上、
// 出现"回车不关闭面板、跳回第一项"等问题。
func (a *App) bindComponents() {
	a.ambientStatus = components.NewAmbientStatus(a.state)
	a.agentStream = components.NewAgentStream(a.state)
	a.commandPrompt = components.NewCommandPrompt(a.state)
	a.cmdLine = components.NewCmdLine(a.state)
	a.softInspector = components.NewSoftInspector(a.state)
	a.palette = components.NewPalette(a.state)
	a.helpOverlay = components.NewHelpOverlay(a.state)
	a.sessionPicker = components.NewSessionPicker(a.state)
	a.modelPicker = components.NewModelPicker(a.state)
	a.confirmOverlay = components.NewConfirmOverlay(a.state)
}

// toggleAgentMode 切换 Agent 模式 (build/plan) 并追加状态提示。
func (a *App) toggleAgentMode() tea.Cmd {
	mode := "plan"
	if a.state.Runtime.AgentMode == "plan" || a.state.Runtime.AgentMode == "" {
		mode = "build"
	}
	a.state.Runtime.AgentMode = mode
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("mode-toggle-%d", time.Now().UnixNano()),
		Type:      "status",
		Timestamp: time.Now(),
		Content:   fmt.Sprintf("Agent mode: %s", mode),
		Metadata:  map[string]any{"done": true},
	})
	return nil
}

// toggleFullAccess 切换 Full Access 模式并追加状态提示。
func (a *App) toggleFullAccess() tea.Cmd {
	a.state.Runtime.FullAccess = !a.state.Runtime.FullAccess
	label := "off"
	if a.state.Runtime.FullAccess {
		label = "on"
	}
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("access-toggle-%d", time.Now().UnixNano()),
		Type:      "status",
		Timestamp: time.Now(),
		Content:   fmt.Sprintf("Full access: %s", label),
		Metadata:  map[string]any{"done": true},
	})
	return nil
}

// triggerCompact 触发手动 compact 并追加状态提示。
func (a *App) triggerCompact() tea.Cmd {
	a.appendStream(state.StreamEntry{
		ID:        fmt.Sprintf("compact-%d", time.Now().UnixNano()),
		Type:      "status",
		Timestamp: time.Now(),
		Content:   "Compact triggered",
		Metadata:  map[string]any{"done": true},
	})
	return nil
}

func inputModeName(mode state.InputMode) string {
	switch mode {
	case state.NormalMode:
		return "normal"
	case state.LeaderMode:
		return "leader"
	default:
		return "input"
	}
}

// emptyDash 在占位视图中用短横线表示空值。
func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
