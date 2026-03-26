package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/components"
	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"
	"go-llm-demo/internal/tui/todo"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	toolStatusPrefix         = "[TOOL_STATUS]"
	toolContextPrefix        = "[TOOL_CONTEXT]"
	maxToolContextOutputSize = 4000
	maxToolContextMessages   = 3
)

var (
	validateChatAPIKey = services.ValidateChatAPIKey
	writeAppConfig     = configs.WriteAppConfig
	getWorkspaceRoot   = services.GetWorkspaceRoot
	executeToolCall    = services.ExecuteToolCall
)

type bubblePane interface {
	update(tea.Msg) tea.Cmd
	focus() tea.Cmd
	blur()
}

// Update 处理 Bubble Tea 事件并驱动聊天状态更新。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case spinner.TickMsg:
		return m, m.status.update(msg)

	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		m.SetHeight(msg.Height)
		m.syncLayout()
		m.refreshViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if handled := m.handleMouse(msg); handled {
			m.refreshViewport()
			return m, nil
		}
		return m, m.updateBubblePane(m.activePanel(), msg)

	case StreamReadyMsg:
		if msg.Stream == nil {
			m.chat.Generating = false
			return m, m.setDoneStatus("Done")
		}
		m.streamChan = msg.Stream
		return m, m.streamResponseFromChannel()

	case StreamChunkMsg:
		if m.chat.Generating {
			visible := m.consumeThinkingChunk(msg.Content)
			if visible != "" {
				m.AppendLastMessage(visible)
				m.refreshViewport()
			}
		}
		return m, m.streamResponseFromChannel()

	case StreamDoneMsg:
		mu := m.mutex()
		mu.Lock()
		m.chat.Generating = false
		m.streamChan = nil

		var lastContent string
		shouldCheckToolCall := !m.chat.ToolExecuting && len(m.chat.Messages) > 0
		if len(m.chat.Messages) > 0 {
			lastMsg := &m.chat.Messages[len(m.chat.Messages)-1]
			lastMsg.Streaming = false
			if lastMsg.Role == "assistant" {
				lastContent = lastMsg.Content
			} else {
				shouldCheckToolCall = false
			}
		}
		mu.Unlock()
		m.resetThinkingFilter()

		// 当前工具协议约定：模型如果想调用工具，需要把最后一条 assistant 消息完整输出为
		// {"tool":"...","params":{...}} 结构。这里在流结束后统一解析，避免半截 JSON 被误触发。
		if shouldCheckToolCall {
			var jsonData map[string]interface{}
			if err := json.Unmarshal([]byte(lastContent), &jsonData); err == nil {
				if toolName, ok := jsonData["tool"].(string); ok && toolName != "" {
					mu := m.mutex()
					mu.Lock()
					if m.chat.ToolExecuting {
						mu.Unlock()
						return m, nil
					}
					m.chat.ToolExecuting = true
					mu.Unlock()

					paramsMap := map[string]interface{}{}
					if toolParams, ok := jsonData["params"].(map[string]interface{}); ok {
						paramsMap = services.NormalizeToolParams(toolParams)
					}

					// 显示工具执行中提示（仅用于 UI，不参与模型上下文）
					m.AddMessage("system", formatToolStatusMessage(toolName, paramsMap))
					m.setToolRunningStatus(toolName)

					// 在goroutine中执行工具调用
					return m, func() tea.Msg {
						call := services.ToolCall{Tool: toolName, Params: paramsMap}
						result := executeToolCall(call)
						if result == nil {
							mu := m.mutex()
							mu.Lock()
							m.chat.ToolExecuting = false
							mu.Unlock()
							return ToolErrorMsg{Err: fmt.Errorf("tool execution failed: empty result")}
						}
						return ToolResultMsg{Result: result, Call: call}
					}
				}
			}
		}
		m.setDoneStatus("Done")

		return m, nil

	case StreamErrorMsg:
		mu := m.mutex()
		mu.Lock()
		m.chat.Generating = false
		m.streamChan = nil
		replacedPlaceholder := false
		if len(m.chat.Messages) > 0 {
			lastMsg := &m.chat.Messages[len(m.chat.Messages)-1]
			if lastMsg.Role == "assistant" && strings.TrimSpace(lastMsg.Content) == "" {
				lastMsg.Content = fmt.Sprintf("Error: %v", msg.Err)
				lastMsg.Streaming = false
				replacedPlaceholder = true
			}
		}
		mu.Unlock()
		m.resetThinkingFilter()
		m.setErrorStatus("Request failed")
		if !replacedPlaceholder {
			m.AddMessage("assistant", fmt.Sprintf("Error: %v", msg.Err))
		}
		m.TrimHistory(m.chat.HistoryTurns)
		return m, nil

	case RefreshMemoryMsg:
		stats, err := m.client.GetMemoryStats(context.Background())
		if err == nil && stats != nil {
			m.chat.MemoryStats = *stats
		}
		return m, nil

	case ExitMsg:
		return m, tea.Quit

	case ToolResultMsg:
		mu := m.mutex()
		mu.Lock()
		m.chat.ToolExecuting = false
		mu.Unlock()
		// 将结构化工具上下文添加为系统消息，然后重新获取AI响应
		if toolType, target, ok := isSecurityAskResult(msg.Result); ok {
			mu := m.mutex()
			mu.Lock()
			m.chat.PendingApproval = &state.PendingApproval{
				Call:     msg.Call,
				ToolType: toolType,
				Target:   target,
			}
			pending := m.chat.PendingApproval
			mu.Unlock()

			m.AddMessage("assistant", formatPendingApprovalMessage(pending))
			m.setApprovalRequiredStatus()
			m.refreshViewport()
			return m, nil
		}
		m.AddMessage("system", formatToolContextMessage(msg.Result))
		m.AddMessage("assistant", "")

		// 构建包含工具结果的消息并重新请求AI
		return m, m.queueAssistantResponse()

	case ToolErrorMsg:
		mu := m.mutex()
		mu.Lock()
		m.chat.ToolExecuting = false
		mu.Unlock()
		// 将工具执行错误添加为结构化系统上下文
		m.AddMessage("system", formatToolErrorContext(msg.Err))
		m.AddMessage("assistant", "")

		// 构建包含错误信息的消息并重新请求AI
		return m, m.queueAssistantResponse()
	}

	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ui.Mode == state.ModeHelp {
		if msg.Type == tea.KeyEsc || msg.String() == "q" {
			_ = m.switchMode(state.ModeChat, "composer")
			m.refreshViewport()
			return *m, nil
		}
	}

	switch msg.Type {
	case tea.KeyTab:
		cmd := m.cycleFocus(true)
		m.refreshViewport()
		return *m, cmd
	case tea.KeyShiftTab:
		cmd := m.cycleFocus(false)
		m.refreshViewport()
		return *m, cmd
	}

	if key.Matches(msg, m.textarea.KeyMap.InsertNewline) {
		m.chat.CmdHistIndex = -1
		inputCmd := m.updateBubblePane("composer", msg)
		m.refreshViewport()
		if m.viewport.AtBottom() {
			m.ui.AutoScroll = true
		}
		return *m, inputCmd
	}

	switch msg.Type {
	case tea.KeyEnter:
		return m.handleSubmit()

	case tea.KeyF5:
		return m.handleSubmit()

	case tea.KeyF8:
		return m.handleSubmit()
	case tea.KeyPgUp, tea.KeyPgDown:
		if m.activePanel() != "composer" {
			return *m, m.updateBubblePane(m.activePanel(), msg)
		}
	case tea.KeyUp:
		if m.activePanel() != "composer" {
			return *m, m.updateBubblePane(m.activePanel(), msg)
		}
		if strings.TrimSpace(m.textarea.Value()) == "" && len(m.chat.CommandHistory) > 0 {
			if m.chat.CmdHistIndex < len(m.chat.CommandHistory)-1 {
				m.chat.CmdHistIndex++
			}
			if m.chat.CmdHistIndex >= 0 && m.chat.CmdHistIndex < len(m.chat.CommandHistory) {
				m.textarea.SetValue(m.chat.CommandHistory[len(m.chat.CommandHistory)-1-m.chat.CmdHistIndex])
				m.textarea.CursorEnd()
				return *m, nil
			}
		}
	case tea.KeyDown:
		if m.activePanel() != "composer" {
			return *m, m.updateBubblePane(m.activePanel(), msg)
		}
		if m.chat.CmdHistIndex > 0 {
			m.chat.CmdHistIndex--
			m.textarea.SetValue(m.chat.CommandHistory[len(m.chat.CommandHistory)-1-m.chat.CmdHistIndex])
			m.textarea.CursorEnd()
			return *m, nil
		}
		if m.chat.CmdHistIndex == 0 {
			m.chat.CmdHistIndex = -1
			m.textarea.Reset()
			return *m, nil
		}
	}

	if m.activePanel() != "composer" && shouldFocusComposerForKey(msg) {
		focusCmd := m.setFocus("composer")
		inputCmd := m.updateBubblePane("composer", msg)
		m.refreshViewport()
		if inputCmd == nil {
			return *m, focusCmd
		}
		if focusCmd == nil {
			return *m, inputCmd
		}
		return *m, tea.Batch(focusCmd, inputCmd)
	}

	m.chat.CmdHistIndex = -1
	inputCmd := m.updateBubblePane("composer", msg)
	m.refreshViewport()
	if m.viewport.AtBottom() {
		m.ui.AutoScroll = true
	}
	return *m, inputCmd
}

func shouldFocusComposerForKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace:
		return true
	default:
		return strings.TrimSpace(msg.String()) != ""
	}
}

func (m *Model) setFocus(panel string) tea.Cmd {
	panel = strings.TrimSpace(panel)
	switch panel {
	case "conversation", "context", "composer", "help":
	default:
		panel = "composer"
	}

	if m.ui.Mode == state.ModeTodo {
		panel = "todo"
	} else if m.ui.Mode == state.ModeHelp {
		switch panel {
		case "help", "composer":
		default:
			panel = "help"
		}
	}

	if previous := m.bubblePane(m.ui.Focused); previous != nil {
		previous.blur()
	}
	m.ui.Focused = panel
	if pane := m.bubblePane(panel); pane != nil {
		return pane.focus()
	}
	return nil
}

func (m Model) focusOrder() []string {
	if m.ui.Mode == state.ModeTodo {
		return []string{"todo"}
	}
	if m.ui.Mode == state.ModeHelp {
		return []string{"help", "composer"}
	}
	return []string{"conversation", "context", "composer"}
}

func (m *Model) cycleFocus(forward bool) tea.Cmd {
	order := m.focusOrder()
	if len(order) == 0 {
		return nil
	}

	current := m.activePanel()
	index := 0
	for i, panel := range order {
		if panel == current {
			index = i
			break
		}
	}

	if forward {
		index = (index + 1) % len(order)
	} else {
		index = (index - 1 + len(order)) % len(order)
	}

	return m.setFocus(order[index])
}

func (m Model) panelAtPosition(y int, x int) string {
	if y < 0 || x < 0 {
		return ""
	}

	if y < m.layout.mainHeight {
		if m.ui.Mode == state.ModeHelp {
			return "help"
		}
		if m.layout.stacked {
			if y < m.layout.conversationHeight {
				return "conversation"
			}
			if y > m.layout.conversationHeight {
				return "context"
			}
			return ""
		}

		if x < m.layout.conversationWidth {
			return "conversation"
		}
		if x > m.layout.conversationWidth {
			return "context"
		}
		return ""
	}

	if y < m.layout.mainHeight+m.layout.composerHeight {
		return "composer"
	}
	return ""
}

func (m *Model) bubblePane(panel string) bubblePane {
	switch strings.TrimSpace(panel) {
	case "composer":
		return &m.textarea
	case "todo":
		return &m.todo
	case "conversation":
		return &m.viewport
	case "context":
		return &m.context
	default:
		return nil
	}
}

func (m *Model) updateBubblePane(panel string, msg tea.Msg) tea.Cmd {
	pane := m.bubblePane(panel)
	if pane == nil {
		return nil
	}

	cmd := pane.update(msg)
	switch strings.TrimSpace(panel) {
	case "todo":
		action, err := m.todo.consumeAction()
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Todo action failed: %v", err))
			m.refreshViewport()
			return cmd
		}
		switch action {
		case todoPanelExit:
			_ = m.switchMode(state.ModeChat, "composer")
		case todoPanelPromptAdd:
			m.AddMessage("assistant", todo.MsgPromptAdd)
			_ = m.switchMode(state.ModeChat, "composer")
		case todoPanelRefreshed:
		}
	case "conversation":
		m.ui.AutoScroll = m.viewport.AtBottom()
	}
	return cmd
}

func (m *Model) handleMouse(msg tea.MouseMsg) bool {
	panel := m.panelAtPosition(msg.Y, msg.X)

	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if panel == "conversation" || panel == "context" {
			_ = m.setFocus(panel)
			_ = m.updateBubblePane(panel, msg)
			return true
		}
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}

	if panel != "" {
		_ = m.setFocus(panel)
	}
	if panel != "conversation" {
		return panel != ""
	}

	contentRow, contentCol, ok := m.chatContentPosition(msg)
	if !ok {
		return panel != ""
	}
	region, found := m.viewport.clickableRegion(contentRow, contentCol)
	if !found || region.Kind != "copy" {
		return panel != ""
	}
	if err := m.copyCodeBlock(region.CodeBlock); err != nil {
		m.setNoticeStatus(fmt.Sprintf("Copy failed: %v", err))
		return true
	}
	m.setNoticeStatus(components.FormatCopyNotice(region.CodeBlock))
	return true
}

func (m *Model) chatContentPosition(msg tea.MouseMsg) (int, int, bool) {
	return m.viewport.contentPosition(msg.Y, msg.X)
}

func findClickableRegion(regions []components.ClickableRegion, row, col int) (components.ClickableRegion, bool) {
	for _, region := range regions {
		if row < region.StartRow || row > region.EndRow {
			continue
		}
		if col < region.StartCol || col > region.EndCol {
			continue
		}
		return region, true
	}
	return components.ClickableRegion{}, false
}

func (m *Model) copyCodeBlock(ref components.CodeBlockRef) error {
	if m.copyToClipboard == nil {
		return fmt.Errorf("clipboard unavailable")
	}
	return m.copyToClipboard(ref.Code)
}

func (m *Model) switchMode(mode state.Mode, focus string) tea.Cmd {
	m.ui.Mode = mode
	return m.setFocus(focus)
}

func (m *Model) queueAssistantResponse() tea.Cmd {
	m.chat.Generating = true
	m.ui.AutoScroll = true
	statusCmd := m.setThinkingStatus()
	m.refreshViewport()
	return tea.Batch(statusCmd, m.streamResponse(m.buildMessages()))
}

func (m *Model) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	m.textarea.resetForSubmit()
	m.syncLayout()

	if input == "" {
		return *m, nil
	}

	if m.ui.Mode == state.ModeHelp {
		_ = m.switchMode(state.ModeChat, "composer")
		m.refreshViewport()
		if input == "/help" {
			return *m, nil
		}
	}

	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}
	if !m.chat.APIKeyReady {
		m.AddMessage("assistant", "The current API Key could not be validated. Use /apikey <env_name>, /provider <name>, or /switch <model> to update the configuration, or /exit to quit.")
		return *m, nil
	}

	if m.chat.PendingApproval != nil {
		m.AddMessage("assistant", "A security approval is pending. Use /y to allow once or /n to reject before sending a new message.")
		return *m, nil
	}

	m.clearNotices()
	m.resetThinkingFilter()
	m.AddMessage("user", input)
	m.AddMessage("assistant", "")
	// 在请求发出前先裁剪原始消息，避免 UI 历史无限扩张并影响短期上下文质量。
	m.TrimHistory(m.chat.HistoryTurns)
	m.TrimHistory(m.chat.HistoryTurns)
	m.chat.CommandHistory = append(m.chat.CommandHistory, input)
	m.chat.CmdHistIndex = -1

	return *m, m.queueAssistantResponse()
}

func (m *Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return *m, nil
	}

	m.clearNotices()
	cmd := fields[0]
	args := fields[1:]
	if !m.chat.APIKeyReady && !isAPIKeyRecoveryCommand(cmd) {
		m.AddMessage("assistant", "The current API Key could not be validated. Only /apikey <env_name>, /provider <name>, /help, /switch <model>, /pwd (/workspace), and /exit are available.")
		return *m, nil
	}

	switch cmd {
	case "/help":
		_ = m.switchMode(state.ModeHelp, "help")
	case "/y":
		if len(args) > 0 {
			m.AddMessage("assistant", "Usage: /y")
			return *m, nil
		}
		if m.chat.PendingApproval == nil {
			m.AddMessage("assistant", "There is no pending security approval.")
			return *m, nil
		}
		if m.chat.ToolExecuting {
			m.AddMessage("assistant", "Another tool is still running. Please retry /y after it finishes.")
			return *m, nil
		}

		pending := *m.chat.PendingApproval
		m.chat.PendingApproval = nil
		if strings.TrimSpace(pending.Call.Tool) == "" {
			m.AddMessage("assistant", "The pending tool request is incomplete and cannot be executed.")
			return *m, nil
		}

		m.AddMessage("assistant", fmt.Sprintf("Approved. Running tool %s.", pending.Call.Tool))
		m.AddMessage("system", formatToolStatusMessage(pending.Call.Tool, pending.Call.Params))

		mu := m.mutex()
		mu.Lock()
		if m.chat.ToolExecuting {
			m.chat.PendingApproval = &pending
			mu.Unlock()
			return *m, nil
		}
		m.chat.ToolExecuting = true
		mu.Unlock()

		m.setToolRunningStatus(pending.Call.Tool)
		m.refreshViewport()
		return *m, func() tea.Msg {
			services.ApproveSecurityAsk(pending.ToolType, pending.Target)
			result := executeToolCall(pending.Call)
			if result == nil {
				mu := m.mutex()
				mu.Lock()
				m.chat.ToolExecuting = false
				mu.Unlock()
				return ToolErrorMsg{Err: fmt.Errorf("tool execution failed: empty result")}
			}
			return ToolResultMsg{Result: result, Call: pending.Call}
		}
	case "/n":
		if len(args) > 0 {
			m.AddMessage("assistant", "Usage: /n")
			return *m, nil
		}
		if m.chat.PendingApproval == nil {
			m.AddMessage("assistant", "There is no pending security approval.")
			return *m, nil
		}

		pending := *m.chat.PendingApproval
		m.chat.PendingApproval = nil
		toolName := strings.TrimSpace(pending.Call.Tool)
		if toolName == "" {
			toolName = "unknown"
		}
		m.AddMessage("assistant", fmt.Sprintf("Rejected tool %s for target %s.", toolName, pending.Target))
		m.setDoneStatus("Rejected pending tool")
		return *m, nil
	case "/exit", "/quit", "/q":
		return *m, tea.Quit
	case "/apikey":
		if len(args) == 0 {
			m.AddMessage("assistant", "Usage: /apikey <env_name>")
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "The current configuration is not loaded, so the API key environment variable name cannot be changed.")
			return *m, nil
		}
		previousEnvName := cfg.AI.APIKey
		cfg.AI.APIKey = strings.TrimSpace(args[0])
		envName := cfg.APIKeyEnvVarName()
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("Environment variable %s is not set. Use /apikey <env_name> to switch to another one, or /exit to quit.", envName))
			return *m, nil
		}
		err := validateChatAPIKey(context.Background(), cfg)
		if err == nil {
			if writeErr := writeAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
				cfg.AI.APIKey = previousEnvName
				m.chat.APIKeyReady = configs.RuntimeAPIKey() != ""
				m.AddMessage("assistant", fmt.Sprintf("Failed to switch the API key environment variable name: %v", writeErr))
				return *m, nil
			}
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("Switched the API key environment variable name to %s and validated it successfully.", envName))
			return *m, nil
		}
		m.chat.APIKeyReady = false
		if errors.Is(err, services.ErrInvalidAPIKey) {
			m.AddMessage("assistant", fmt.Sprintf("The API key in environment variable %s is invalid: %v. Use /apikey <env_name>, /provider <name>, or /switch <model> to update the configuration, or /exit to quit.", envName, err))
			return *m, nil
		}
		m.AddMessage("assistant", fmt.Sprintf("The API key in environment variable %s could not be validated: %v. Use /apikey <env_name>, /provider <name>, or /switch <model> to update the configuration, or /exit to quit.", envName, err))
		return *m, nil
	case "/provider":
		if len(args) == 0 {
			m.AddMessage("assistant", fmt.Sprintf("Usage: /provider <name>\nSupported providers:\n  - %s", strings.Join(services.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "The current configuration is not loaded, so the provider cannot be changed.")
			return *m, nil
		}
		providerName, ok := services.NormalizeProviderName(strings.Join(args, " "))
		if !ok {
			m.AddMessage("assistant", fmt.Sprintf("Unsupported provider: %s\nSupported providers:\n  - %s", strings.Join(args, " "), strings.Join(services.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg.AI.Provider = providerName
		cfg.AI.Model = services.DefaultModelForProvider(providerName)
		m.chat.ActiveModel = cfg.AI.Model
		if writeErr := writeAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to switch provider: %v", writeErr))
			return *m, nil
		}
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("Switched provider to %s, but environment variable %s is not set. Use /apikey <env_name> or set that environment variable.", providerName, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := validateChatAPIKey(context.Background(), cfg); err == nil {
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("Switched provider to %s. The current model was reset to the default: %s.", providerName, cfg.AI.Model))
			return *m, nil
		} else {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("Switched provider to %s, but the API key could not be validated: %v. You can continue using /apikey <env_name>, /provider <name>, or /switch <model> to adjust the configuration.", providerName, err))
			return *m, nil
		}
	case "/switch":
		if len(args) == 0 {
			m.AddMessage("assistant", "Usage: /switch <model>")
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "The current configuration is not loaded, so the model cannot be changed.")
			return *m, nil
		}
		target := strings.Join(args, " ")
		cfg.AI.Model = target
		if writeErr := writeAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to switch model: %v", writeErr))
			return *m, nil
		}
		m.chat.ActiveModel = target
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("Switched model to %s, but environment variable %s is not set.", target, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := validateChatAPIKey(context.Background(), cfg); err == nil {
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("Switched model to: %s", target))
			return *m, nil
		} else {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("Switched model to %s, but the API key could not be validated: %v.", target, err))
			return *m, nil
		}
	case "/pwd", "/workspace":
		if len(args) > 0 {
			m.AddMessage("assistant", "Usage: /pwd or /workspace")
			return *m, nil
		}
		root := strings.TrimSpace(m.chat.WorkspaceRoot)
		if root == "" {
			root = getWorkspaceRoot()
		}
		if strings.TrimSpace(root) == "" {
			m.AddMessage("assistant", "Current workspace: unknown")
			return *m, nil
		}
		m.AddMessage("assistant", fmt.Sprintf("Current workspace: %s", root))
	case "/memory":
		stats, err := m.client.GetMemoryStats(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to read memory stats: %v", err))
			return *m, nil
		}
		m.chat.MemoryStats = *stats
		m.AddMessage("assistant", fmt.Sprintf(
			"Memory stats:\n  Persistent: %d\n  Session: %d\n  Total: %d\n  TopK: %d\n  Min score: %.2f\n  File: %s\n  Types: %s",
			stats.PersistentItems, stats.SessionItems, stats.TotalItems, stats.TopK, stats.MinScore, stats.Path, formatTypeStats(stats.ByType),
		))
	case "/clear-memory":
		if len(args) == 0 || args[0] != "confirm" {
			m.AddMessage("assistant", "This command will clear persistent memory. Use /clear-memory confirm")
			return *m, nil
		}
		if err := m.client.ClearMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to clear persistent memory: %v", err))
			return *m, nil
		}
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.AddMessage("assistant", "Cleared local persistent memory")
	case "/todo":
		if len(args) == 0 {
			_ = m.switchMode(state.ModeTodo, "todo")
			return m.refreshTodos()
		}
		subCmd := args[0]
		switch subCmd {
		case "add":
			if len(args) < 2 {
				m.AddMessage("assistant", todo.MsgUsageAdd)
				return *m, nil
			}
			content := args[1]
			priority := services.TodoPriorityMedium
			if len(args) > 2 {
				if p, ok := services.ParseTodoPriority(args[2]); ok {
					priority = p
				}
			}
			_, err := m.client.AddTodo(context.Background(), content, priority)
			if err != nil {
				m.AddMessage("assistant", fmt.Sprintf(todo.MsgAddFailed, err))
				return *m, nil
			}
			m.AddMessage("assistant", fmt.Sprintf(todo.MsgAddSuccess, content))
			return m.refreshTodos()
		case "list":
			_ = m.switchMode(state.ModeTodo, "todo")
			return m.refreshTodos()
		default:
			m.AddMessage("assistant", fmt.Sprintf(todo.MsgUnknownSubCmd, subCmd))
		}
	case "/clear", "/clear-context":
		if err := m.client.ClearSessionMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to clear session memory: %v", err))
			return *m, nil
		}
		m.chat.Messages = nil
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.AddMessage("assistant", "Cleared the current session context")
	case "/run":
		if len(args) > 0 {
			code := strings.Join(args, " ")
			return *m, tea.Batch(
				tea.Printf("\n--- Running code ---\n"),
				runCodeCmd(code),
			)
		}
	case "/explain":
		if len(args) > 0 {
			code := strings.Join(args, " ")
			return *m, m.sendCodeToAI(code)
		}
		return *m, nil
	default:
		m.AddMessage("assistant", fmt.Sprintf("Unknown command: %s. Enter /help to view the available commands.", cmd))
	}
	m.refreshViewport()

	return *m, nil
}

func (m *Model) refreshTodos() (tea.Model, tea.Cmd) {
	if err := m.todo.refresh(); err != nil {
		m.refreshViewport()
		return *m, nil
	}
	m.refreshViewport()
	return *m, nil
}

func isAPIKeyRecoveryCommand(cmd string) bool {
	switch cmd {
	case "/apikey", "/provider", "/help", "/switch", "/pwd", "/workspace", "/y", "/n", "/exit", "/quit", "/q":
		return true
	default:
		return false
	}
}

func formatTypeStats(byType map[string]int) string {
	if len(byType) == 0 {
		return "none"
	}
	ordered := []string{
		services.TypeUserPreference,
		services.TypeProjectRule,
		services.TypeCodeFact,
		services.TypeFixRecipe,
		services.TypeSessionMemory,
	}
	parts := make([]string, 0, len(byType))
	for _, key := range ordered {
		if count := byType[key]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, count))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func (m *Model) buildMessages() []services.Message {
	mu := m.mutex()
	mu.Lock()
	defer mu.Unlock()
	result := make([]services.Message, 0, len(m.chat.Messages))
	// 工具结果会被注入成 system 上下文，但只保留最近几条，
	// 否则连续工具链很容易把真正的对话历史挤出上下文窗口。
	keepToolContextIndex := recentToolContextIndexes(m.chat.Messages, maxToolContextMessages)

	// 按照消息的原始时间顺序进行迭代
	for idx, msg := range m.chat.Messages {
		if msg.Role == "system" && isResumeSummaryMessage(msg.Content) {
			continue
		}
		if msg.Role == "system" && isTransientToolStatusMessage(msg.Content) {
			continue
		}
		if msg.Role == "system" && isToolContextMessage(msg.Content) {
			if _, ok := keepToolContextIndex[idx]; !ok {
				continue
			}
		}
		// 跳过空的 assistant 消息
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" {
			continue
		}
		// 将非空消息按其原始角色和内容添加到结果中
		result = append(result, services.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return result
}

func (m *Model) streamResponse(messages []services.Message) tea.Cmd {
	return func() tea.Msg {
		stream, err := m.client.Chat(context.Background(), messages, m.chat.ActiveModel)
		if err != nil {
			return StreamErrorMsg{Err: err}
		}
		return StreamReadyMsg{Stream: stream}
	}
}

func (m *Model) streamResponseFromChannel() tea.Cmd {
	if m.streamChan == nil {
		return nil
	}
	return func() tea.Msg {
		chunk, ok := <-m.streamChan
		if !ok {
			return StreamDoneMsg{}
		}
		return StreamChunkMsg{Content: chunk}
	}
}

func (m *Model) sendCodeToAI(code string) tea.Cmd {
	prompt := fmt.Sprintf("Please explain the following code:\n```\n%s\n```", code)
	m.clearNotices()
	m.resetThinkingFilter()
	m.AddMessage("user", prompt)
	m.AddMessage("assistant", "")
	m.TrimHistory(m.chat.HistoryTurns)
	return m.queueAssistantResponse()
}

func isTransientToolStatusMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), toolStatusPrefix)
}

func isToolContextMessage(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), toolContextPrefix)
}

func recentToolContextIndexes(messages []state.Message, keep int) map[int]struct{} {
	result := map[int]struct{}{}
	if keep <= 0 || len(messages) == 0 {
		return result
	}
	for i := len(messages) - 1; i >= 0 && len(result) < keep; i-- {
		msg := messages[i]
		if msg.Role == "system" && isToolContextMessage(msg.Content) {
			result[i] = struct{}{}
		}
	}
	return result
}

func formatToolStatusMessage(toolName string, params map[string]interface{}) string {
	detail := ""
	if filePath, ok := params["filePath"].(string); ok && strings.TrimSpace(filePath) != "" {
		detail = " file=" + strings.TrimSpace(filePath)
	} else if path, ok := params["path"].(string); ok && strings.TrimSpace(path) != "" {
		detail = " path=" + strings.TrimSpace(path)
	} else if workdir, ok := params["workdir"].(string); ok && strings.TrimSpace(workdir) != "" {
		detail = " workdir=" + strings.TrimSpace(workdir)
	}
	return fmt.Sprintf("%s tool=%s%s", toolStatusPrefix, strings.TrimSpace(toolName), detail)
}

func isSecurityAskResult(result *services.ToolResult) (string, string, bool) {
	if result == nil || result.Success || result.Metadata == nil {
		return "", "", false
	}
	action, _ := result.Metadata["securityAction"].(string)
	if strings.TrimSpace(strings.ToLower(action)) != "ask" {
		return "", "", false
	}
	toolType, _ := result.Metadata["securityToolType"].(string)
	target, _ := result.Metadata["securityTarget"].(string)
	if strings.TrimSpace(toolType) == "" || strings.TrimSpace(target) == "" {
		return "", "", false
	}
	return strings.TrimSpace(toolType), strings.TrimSpace(target), true
}

func formatPendingApprovalMessage(pending *state.PendingApproval) string {
	if pending == nil {
		return "Security approval is required. Use /y to allow once or /n to reject."
	}
	toolName := strings.TrimSpace(pending.Call.Tool)
	if toolName == "" {
		toolName = "unknown"
	}
	return fmt.Sprintf("Security approval required for %s.\nTarget: %s\nUse /y to allow once, or /n to reject.", toolName, pending.Target)
}

func formatToolContextMessage(result *services.ToolResult) string {
	if result == nil {
		return toolContextPrefix + "\n" + "tool=unknown\n" + "success=false\n" + "error:\nTool returned empty result"
	}

	// 这里故意使用稳定的纯文本 key/value 结构，而不是直接把 ToolResult 原样塞回模型：
	// 一方面更容易截断超长输出，另一方面也能减少不同工具返回格式带来的歧义。
	builder := strings.Builder{}
	builder.WriteString(toolContextPrefix)
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("tool=%s\n", strings.TrimSpace(result.ToolName)))
	builder.WriteString(fmt.Sprintf("success=%t\n", result.Success))

	if len(result.Metadata) > 0 {
		if encoded, err := json.Marshal(result.Metadata); err == nil {
			builder.WriteString("metadata=")
			builder.WriteString(string(encoded))
			builder.WriteString("\n")
		}
	}

	if result.Success {
		output := strings.TrimSpace(result.Output)
		if output != "" {
			builder.WriteString("output:\n")
			builder.WriteString(truncateForContext(output, maxToolContextOutputSize))
		}
	} else {
		errText := strings.TrimSpace(result.Error)
		if errText == "" {
			errText = strings.TrimSpace(result.Output)
		}
		if errText != "" {
			builder.WriteString("error:\n")
			builder.WriteString(truncateForContext(errText, maxToolContextOutputSize))
		}
	}

	return builder.String()
}

func formatToolErrorContext(err error) string {
	errText := "Unknown error"
	if err != nil {
		errText = err.Error()
	}
	return toolContextPrefix + "\n" + "tool=unknown\n" + "success=false\n" + "error:\n" + truncateForContext(errText, maxToolContextOutputSize)
}

func truncateForContext(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	if maxLen <= 0 || len(trimmed) <= maxLen {
		return trimmed
	}
	suffix := fmt.Sprintf("\n... (truncated, total=%d chars)", len(trimmed))
	keep := maxLen - len(suffix)
	if keep < 0 {
		keep = 0
	}
	return trimmed[:keep] + suffix
}

func runCodeCmd(code string) tea.Cmd {
	return func() tea.Msg {
		ext, runner := detectLanguage(code)
		if ext == "" {
			return StreamErrorMsg{Err: fmt.Errorf("could not detect the code language")}
		}

		tmpFile, err := os.CreateTemp("", "neocode-*."+ext)
		if err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("failed to create a temporary file: %w", err)}
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(code); err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("failed to write the temporary file: %w", err)}
		}
		tmpFile.Close()

		var cmd *exec.Cmd
		if runner != "" {
			cmd = exec.Command(runner, tmpFile.Name())
		} else {
			cmd = exec.Command("go", "run", tmpFile.Name())
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err != nil {
			return StreamErrorMsg{Err: err}
		}

		return StreamDoneMsg{}
	}
}

func detectLanguage(code string) (string, string) {
	code = strings.TrimSpace(code)

	if strings.HasPrefix(code, "#!/bin/bash") || strings.HasPrefix(code, "#!/bin/sh") {
		return "sh", "bash"
	}
	if strings.HasPrefix(code, "package main") || strings.Contains(code, "func main()") {
		return "go", ""
	}
	if strings.HasPrefix(code, "def ") || strings.HasPrefix(code, "class ") {
		return "py", "python"
	}
	if strings.HasPrefix(code, "fn ") || strings.HasPrefix(code, "impl ") {
		return "rs", "rustc"
	}
	if strings.HasPrefix(code, "console.log") || strings.Contains(code, "=>") {
		return "js", "node"
	}

	return "", ""
}

func (m *Model) syncLayout() {
	m.configureLayout()
}

func (m *Model) refreshViewport() {
	m.syncLayout()
	m.ui.AutoScroll = m.viewport.refresh(m.ui.Mode, m.todo, m.toComponentMessages(), m.ui.AutoScroll)
	m.context.refresh(m.renderContextBody(components.PanelInnerWidth(m.layout.contextWidth)))
}
