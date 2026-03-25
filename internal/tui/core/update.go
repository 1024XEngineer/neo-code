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

// Update 处理 Bubble Tea 事件并驱动聊天状态更新。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.SetWidth(msg.Width)
		m.SetHeight(msg.Height)
		m.syncLayout()
		m.refreshViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		if handled := m.handleMouseClick(msg); handled {
			m.refreshViewport()
			return m, nil
		}
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		m.ui.AutoScroll = m.viewport.AtBottom()
		return m, vpCmd

	case StreamChunkMsg:
		if m.chat.Generating {
			m.AppendLastMessage(msg.Content)
			m.refreshViewport()
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
		m.refreshViewport()

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
		if !replacedPlaceholder {
			m.AddMessage("assistant", fmt.Sprintf("Error: %v", msg.Err))
		}
		m.TrimHistory(m.chat.HistoryTurns)
		m.refreshViewport()
		return m, nil

	case ShowHelpMsg:
		m.ui.Mode = state.ModeHelp
		m.refreshViewport()
		return m, nil

	case HideHelpMsg:
		m.ui.Mode = state.ModeChat
		m.refreshViewport()
		return m, nil

	case RefreshMemoryMsg:
		stats, err := m.client.GetMemoryStats(context.Background())
		if err == nil && stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.refreshViewport()
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
			m.refreshViewport()
			return m, nil
		}
		m.AddMessage("system", formatToolContextMessage(msg.Result))
		m.AddMessage("assistant", "")
		m.chat.Generating = true
		m.refreshViewport()

		// 构建包含工具结果的消息并重新请求AI
		messages := m.buildMessages()
		return m, m.streamResponse(messages)

	case ToolErrorMsg:
		mu := m.mutex()
		mu.Lock()
		m.chat.ToolExecuting = false
		mu.Unlock()
		// 将工具执行错误添加为结构化系统上下文
		m.AddMessage("system", formatToolErrorContext(msg.Err))
		m.AddMessage("assistant", "")
		m.chat.Generating = true
		m.refreshViewport()

		// 构建包含错误信息的消息并重新请求AI
		messages := m.buildMessages()
		return m, m.streamResponse(messages)
	}

	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ui.Mode == state.ModeHelp {
		if msg.Type == tea.KeyEsc || msg.String() == "q" {
			m.ui.Mode = state.ModeChat
			m.refreshViewport()
			return *m, nil
		}
	}

	if m.ui.Mode == state.ModeTodo {
		return m.handleTodoKey(msg)
	}

	switch msg.Type {
	case tea.KeyF5:
		return m.handleSubmit()

	case tea.KeyF8:
		return m.handleSubmit()

	case tea.KeyPgUp:
		m.ui.AutoScroll = false
		m.viewport.HalfViewUp()
		return *m, nil

	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		m.ui.AutoScroll = m.viewport.AtBottom()
		return *m, nil

	case tea.KeyUp:
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

	m.chat.CmdHistIndex = -1
	var inputCmd tea.Cmd
	m.textarea, inputCmd = m.textarea.Update(msg)
	m.refreshViewport()
	if m.viewport.AtBottom() {
		m.ui.AutoScroll = true
	}
	return *m, inputCmd
}

func (m *Model) handleMouseClick(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	contentRow, contentCol, ok := m.chatContentPosition(msg)
	if !ok {
		return false
	}
	region, found := findClickableRegion(m.chatLayout.Regions, contentRow, contentCol)
	if !found || region.Kind != "copy" {
		return false
	}
	if err := m.copyCodeBlock(region.CodeBlock); err != nil {
		m.ui.CopyStatus = fmt.Sprintf("Copy failed: %v", err)
		return true
	}
	m.ui.CopyStatus = components.FormatCopyNotice(region.CodeBlock)
	return true
}

func (m *Model) chatContentPosition(msg tea.MouseMsg) (int, int, bool) {
	statusHeight := 1
	chatTop := statusHeight
	chatBottom := chatTop + m.viewport.Height
	if msg.Y < chatTop || msg.Y >= chatBottom {
		return 0, 0, false
	}
	return m.viewport.YOffset + (msg.Y - chatTop), msg.X, true
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

func (m *Model) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	m.textarea.Reset()
	m.textarea.SetHeight(m.calculateInputHeight())
	m.syncLayout()

	if input == "" {
		return *m, nil
	}

	switch m.ui.Mode {
	case state.ModeHelp:
		m.ui.Mode = state.ModeChat
		return *m, nil
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

	m.AddMessage("user", input)
	m.AddMessage("assistant", "")
	// 在请求发出前先裁剪原始消息，避免 UI 历史无限扩张并影响短期上下文质量。
	m.TrimHistory(m.chat.HistoryTurns)
	m.chat.Generating = true
	m.ui.AutoScroll = true
	m.refreshViewport()

	m.chat.CommandHistory = append(m.chat.CommandHistory, input)
	m.chat.CmdHistIndex = -1

	messages := m.buildMessages()
	return *m, m.streamResponse(messages)
}

func (m *Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return *m, nil
	}

	cmd := fields[0]
	args := fields[1:]
	if !m.chat.APIKeyReady && !isAPIKeyRecoveryCommand(cmd) {
		m.AddMessage("assistant", "The current API Key could not be validated. Only /apikey <env_name>, /provider <name>, /help, /switch <model>, /pwd (/workspace), and /exit are available.")
		return *m, nil
	}

	switch cmd {
	case "/help":
		m.ui.Mode = state.ModeHelp
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
		projectFiles := "none"
		if len(stats.ProjectSources) > 0 {
			projectFiles = strings.Join(stats.ProjectSources, ", ")
		}
		m.AddMessage("assistant", fmt.Sprintf(
			"Memory stats:\n  Persistent: %d\n  Session: %d\n  Total: %d\n  TopK: %d\n  Min score: %.2f\n  File: %s\n  Project memory files: %s\n  Types: %s",
			stats.PersistentItems, stats.SessionItems, stats.TotalItems, stats.TopK, stats.MinScore, stats.Path, projectFiles, formatTypeStats(stats.ByType),
		))
	case "/memory-list":
		items, err := m.client.ListMemoryItems(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to list memory items: %v", err))
			return *m, nil
		}
		if len(items) == 0 {
			m.AddMessage("assistant", "No memory items stored.")
			return *m, nil
		}
		lines := make([]string, 0, len(items)+1)
		lines = append(lines, "Memory items:")
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("- [%s/%s] %s", item.Type, item.Scope, item.Summary))
		}
		m.AddMessage("assistant", strings.Join(lines, "\n"))
	case "/project-memory":
		sources, err := m.client.GetProjectMemorySources(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to load project memory files: %v", err))
			return *m, nil
		}
		if len(sources) == 0 {
			m.AddMessage("assistant", "No explicit project memory files are loaded for this workspace.")
			return *m, nil
		}
		lines := make([]string, 0, len(sources)+1)
		lines = append(lines, "Explicit project memory files:")
		for _, source := range sources {
			lines = append(lines, fmt.Sprintf("- %s", source.Path))
		}
		m.AddMessage("assistant", strings.Join(lines, "\n"))
	case "/resume":
		if provider, ok := m.client.(services.WorkingSessionSummaryProvider); ok {
			summary, err := provider.GetWorkingSessionSummary(context.Background())
			if err != nil {
				m.AddMessage("assistant", fmt.Sprintf("Failed to load working session summary: %v", err))
				return *m, nil
			}
			if strings.TrimSpace(summary) == "" {
				m.AddMessage("assistant", "No saved working session summary for this workspace.")
				return *m, nil
			}
			m.AddMessage("assistant", summary)
			return *m, nil
		}
		m.AddMessage("assistant", "Working session summary is not available in the current client.")
	case "/remember":
		if len(args) == 0 {
			m.AddMessage("assistant", "Usage: /remember <text>")
			return *m, nil
		}
		text := strings.TrimSpace(strings.TrimPrefix(input, cmd))
		if err := m.client.Remember(context.Background(), text); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("Failed to remember this note: %v", err))
			return *m, nil
		}
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.AddMessage("assistant", "Stored a manual memory note for future coding assistance.")
	case "/memory-mode":
		mode := m.client.MemoryMode(context.Background())
		if strings.TrimSpace(mode) == "" {
			mode = "rule"
		}
		m.AddMessage("assistant", fmt.Sprintf("Current memory extractor mode: %s", mode))
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
			m.ui.Mode = state.ModeTodo
			return m.refreshTodos()
		}
		subCmd := args[0]
		switch subCmd {
		case "add":
			if len(args) < 2 {
				m.AddMessage("assistant", todo.MsgUsageAdd)
				return *m, nil
			}
			priority := services.TodoPriorityMedium
			contentParts := append([]string(nil), args[1:]...)
			if len(contentParts) > 1 {
				if p, ok := services.ParseTodoPriority(contentParts[len(contentParts)-1]); ok {
					priority = p
					contentParts = contentParts[:len(contentParts)-1]
				}
			}
			content := strings.TrimSpace(strings.Join(contentParts, " "))
			if content == "" {
				m.AddMessage("assistant", todo.MsgUsageAdd)
				return *m, nil
			}
			_, err := m.client.AddTodo(context.Background(), content, priority)
			if err != nil {
				m.AddMessage("assistant", fmt.Sprintf(todo.MsgAddFailed, err))
				return *m, nil
			}
			m.AddMessage("assistant", fmt.Sprintf(todo.MsgAddSuccess, content))
			return m.refreshTodos()
		case "list":
			m.ui.Mode = state.ModeTodo
			return m.refreshTodos()
		default:
			m.AddMessage("assistant", fmt.Sprintf(todo.MsgUnknownSubCmd, subCmd))
		}
	case "/clear-context":
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
	todos, err := m.client.GetTodoList(context.Background())
	if err == nil {
		m.todos = todos
	}
	m.refreshViewport()
	return *m, nil
}

func (m *Model) handleTodoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, todo.Keys.Back):
		m.ui.Mode = state.ModeChat
		m.refreshViewport()
		return *m, nil

	case key.Matches(msg, todo.Keys.Up):
		if m.todoCursor > 0 {
			m.todoCursor--
		}
		m.refreshViewport()
		return *m, nil

	case key.Matches(msg, todo.Keys.Down):
		if m.todoCursor < len(m.todos)-1 {
			m.todoCursor++
		}
		m.refreshViewport()
		return *m, nil

	case key.Matches(msg, todo.Keys.Done):
		if len(m.todos) > 0 {
			t := m.todos[m.todoCursor]
			nextStatus := services.TodoInProgress
			switch t.Status {
			case services.TodoPending:
				nextStatus = services.TodoInProgress
			case services.TodoInProgress:
				nextStatus = services.TodoCompleted
			case services.TodoCompleted:
				nextStatus = services.TodoPending
			}
			_ = m.client.UpdateTodoStatus(context.Background(), t.ID, nextStatus)
			return m.refreshTodos()
		}

	case key.Matches(msg, todo.Keys.Delete):
		if len(m.todos) > 0 {
			t := m.todos[m.todoCursor]
			_ = m.client.RemoveTodo(context.Background(), t.ID)
			if m.todoCursor >= len(m.todos)-1 && m.todoCursor > 0 {
				m.todoCursor--
			}
			return m.refreshTodos()
		}

	case key.Matches(msg, todo.Keys.Add):
		// 切换到聊天模式，让用户通过 /todo add 命令行新增，或者这里可以简单处理
		m.AddMessage("assistant", todo.MsgPromptAdd)
		m.ui.Mode = state.ModeChat
		m.refreshViewport()
		return *m, nil
	}

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

func containsModel(models []string, target string) bool {
	for _, model := range models {
		if model == target {
			return true
		}
	}
	return false
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
	stream, err := m.client.Chat(context.Background(), messages, m.chat.ActiveModel)
	if err != nil {
		return func() tea.Msg { return StreamErrorMsg{Err: err} }
	}

	m.streamChan = stream
	return func() tea.Msg {
		chunk, ok := <-stream
		if !ok {
			return StreamDoneMsg{}
		}
		return StreamChunkMsg{Content: chunk}
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
	m.AddMessage("user", prompt)
	m.AddMessage("assistant", "")
	m.TrimHistory(m.chat.HistoryTurns)
	m.chat.Generating = true
	m.ui.AutoScroll = true
	m.refreshViewport()

	messages := m.buildMessages()
	return m.streamResponse(messages)
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

func (m *Model) calculateInputHeight() int {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < 3 {
		return 3
	}
	if lines > 8 {
		return 8
	}
	return lines
}

func (m *Model) syncLayout() {
	if m.ui.Width <= 0 || m.ui.Height <= 0 {
		return
	}
	inputWidth := m.ui.Width
	if inputWidth < 20 {
		inputWidth = 20
	}
	m.textarea.SetWidth(inputWidth)
	m.textarea.SetHeight(m.calculateInputHeight())
	m.textarea.Prompt = "> "

	statusHeight := 1
	inputHeight := m.textarea.Height() + 2
	helpHeight := 0
	if m.ui.Mode == state.ModeHelp {
		helpHeight = minInt(20, m.ui.Height-statusHeight-3)
	}
	contentHeight := m.ui.Height - statusHeight - inputHeight - helpHeight
	if contentHeight < 3 {
		contentHeight = 3
	}
	m.viewport.Width = m.ui.Width
	m.viewport.Height = contentHeight
}

func (m *Model) refreshViewport() {
	m.syncLayout()
	content := m.renderChatContent()
	m.viewport.SetContent(content)
	if m.ui.AutoScroll || m.viewport.AtBottom() {
		m.viewport.GotoBottom()
		m.ui.AutoScroll = true
	}
}
