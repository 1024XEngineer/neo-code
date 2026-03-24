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
	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	toolStatusPrefix         = "[TOOL_STATUS]"
	toolContextPrefix        = "[TOOL_CONTEXT]"
	maxToolContextOutputSize = 4000
	maxToolContextMessages   = 3
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
						result := services.ExecuteToolCall(call)
						if result == nil {
							mu := m.mutex()
							mu.Lock()
							m.chat.ToolExecuting = false
							mu.Unlock()
							return ToolErrorMsg{Err: fmt.Errorf("工具执行失败: 空返回")}
						}
						return ToolResultMsg{Result: result}
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
				lastMsg.Content = fmt.Sprintf("错误: %v", msg.Err)
				lastMsg.Streaming = false
				replacedPlaceholder = true
			}
		}
		mu.Unlock()
		if !replacedPlaceholder {
			m.AddMessage("assistant", fmt.Sprintf("错误: %v", msg.Err))
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
	if msg.Type == tea.KeyEsc && m.ui.Mode == state.ModeHelp {
		m.ui.Mode = state.ModeChat
		m.refreshViewport()
		return *m, nil
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
		m.AddMessage("assistant", "当前 API Key 未通过校验，请使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。")
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
		m.AddMessage("assistant", "当前 API Key 未通过校验，仅支持 /apikey <env_name>、/provider <name>、/help、/switch <model>、/pwd（/workspace）或 /exit。")
		return *m, nil
	}

	switch cmd {
	case "/help":
		m.ui.Mode = state.ModeHelp
	case "/exit", "/quit", "/q":
		return *m, tea.Quit
	case "/apikey":
		if len(args) == 0 {
			m.AddMessage("assistant", "用法: /apikey <env_name>")
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "当前配置未加载，无法切换 API Key 环境变量名")
			return *m, nil
		}
		previousEnvName := cfg.AI.APIKey
		cfg.AI.APIKey = strings.TrimSpace(args[0])
		envName := cfg.APIKeyEnvVarName()
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 未设置。请继续使用 /apikey <env_name> 切换，或 /exit 退出。", envName))
			return *m, nil
		}
		err := services.ValidateChatAPIKey(context.Background(), cfg)
		if err == nil {
			if writeErr := configs.WriteAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
				cfg.AI.APIKey = previousEnvName
				m.chat.APIKeyReady = configs.RuntimeAPIKey() != ""
				m.AddMessage("assistant", fmt.Sprintf("切换 API Key 环境变量名失败: %v", writeErr))
				return *m, nil
			}
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("已切换 API Key 环境变量名为 %s，并通过校验。", envName))
			return *m, nil
		}
		m.chat.APIKeyReady = false
		if errors.Is(err, services.ErrInvalidAPIKey) {
			m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 中的 API Key 无效：%v。请继续使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。", envName, err))
			return *m, nil
		}
		m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 的 API Key 未通过校验：%v。请继续使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。", envName, err))
		return *m, nil
	case "/provider":
		if len(args) == 0 {
			m.AddMessage("assistant", fmt.Sprintf("用法: /provider <name>\n可用提供商:\n  - %s", strings.Join(services.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "当前配置未加载，无法切换提供商")
			return *m, nil
		}
		providerName, ok := services.NormalizeProviderName(strings.Join(args, " "))
		if !ok {
			m.AddMessage("assistant", fmt.Sprintf("不支持的提供商: %s\n可用提供商:\n  - %s", strings.Join(args, " "), strings.Join(services.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg.AI.Provider = providerName
		cfg.AI.Model = services.DefaultModelForProvider(providerName)
		m.chat.ActiveModel = cfg.AI.Model
		if writeErr := configs.WriteAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("切换提供商失败: %v", writeErr))
			return *m, nil
		}
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s，但当前环境变量 %s 未设置。请使用 /apikey <env_name> 或设置该环境变量。", providerName, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := services.ValidateChatAPIKey(context.Background(), cfg); err == nil {
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s，当前模型已重置为默认值: %s。", providerName, cfg.AI.Model))
			return *m, nil
		} else {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s，但 API Key 未通过校验：%v。可继续使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置。", providerName, err))
			return *m, nil
		}
	case "/switch":
		if len(args) == 0 {
			m.AddMessage("assistant", "用法: /switch <model>")
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "当前配置未加载，无法切换模型")
			return *m, nil
		}
		target := strings.Join(args, " ")
		cfg.AI.Model = target
		if writeErr := configs.WriteAppConfig(m.chat.ConfigPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("切换模型失败: %v", writeErr))
			return *m, nil
		}
		m.chat.ActiveModel = target
		if cfg.RuntimeAPIKey() == "" {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型: %s，但当前环境变量 %s 未设置。", target, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := services.ValidateChatAPIKey(context.Background(), cfg); err == nil {
			m.chat.APIKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型: %s", target))
			return *m, nil
		} else {
			m.chat.APIKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型 %s，但 API Key 未通过校验：%v。", target, err))
			return *m, nil
		}
	case "/pwd", "/workspace":
		if len(args) > 0 {
			m.AddMessage("assistant", "用法: /pwd 或 /workspace")
			return *m, nil
		}
		root := strings.TrimSpace(m.chat.WorkspaceRoot)
		if root == "" {
			root = services.GetWorkspaceRoot()
		}
		if strings.TrimSpace(root) == "" {
			m.AddMessage("assistant", "当前工作区: 未知")
			return *m, nil
		}
		m.AddMessage("assistant", fmt.Sprintf("当前工作区: %s", root))
	case "/memory":
		stats, err := m.client.GetMemoryStats(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("读取记忆统计失败: %v", err))
			return *m, nil
		}
		m.chat.MemoryStats = *stats
		m.AddMessage("assistant", fmt.Sprintf(
			"记忆统计:\n  长期: %d\n  会话: %d\n  总计: %d\n  TopK: %d\n  最小分数: %.2f\n  文件: %s\n  类型: %s",
			stats.PersistentItems, stats.SessionItems, stats.TotalItems, stats.TopK, stats.MinScore, stats.Path, formatTypeStats(stats.ByType),
		))
	case "/clear-memory":
		if len(args) == 0 || args[0] != "confirm" {
			m.AddMessage("assistant", "此命令会清空长期记忆。请使用 /clear-memory confirm")
			return *m, nil
		}
		if err := m.client.ClearMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("清空长期记忆失败: %v", err))
			return *m, nil
		}
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.AddMessage("assistant", "已清空本地长期记忆")
	case "/clear-context":
		if err := m.client.ClearSessionMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("清空会话记忆失败: %v", err))
			return *m, nil
		}
		m.chat.Messages = nil
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.chat.MemoryStats = *stats
		}
		m.AddMessage("assistant", "已清空当前会话上下文")
	case "/run":
		if len(args) > 0 {
			code := strings.Join(args, " ")
			return *m, tea.Batch(
				tea.Printf("\n--- 运行代码 ---\n"),
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
		m.AddMessage("assistant", fmt.Sprintf("未知命令: %s，输入 /help 查看帮助", cmd))
	}
	m.refreshViewport()

	return *m, nil
}

func isAPIKeyRecoveryCommand(cmd string) bool {
	switch cmd {
	case "/apikey", "/provider", "/help", "/switch", "/pwd", "/workspace", "/exit", "/quit", "/q":
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
		return "无"
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
		return "无"
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
	prompt := fmt.Sprintf("请解释以下代码：\n```\n%s\n```", code)
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

func formatToolContextMessage(result *services.ToolResult) string {
	if result == nil {
		return toolContextPrefix + "\n" + "tool=unknown\n" + "success=false\n" + "error:\n工具返回为空"
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
	errText := "未知错误"
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
			return StreamErrorMsg{Err: fmt.Errorf("无法识别代码语言")}
		}

		tmpFile, err := os.CreateTemp("", "neocode-*."+ext)
		if err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("创建临时文件失败: %w", err)}
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(code); err != nil {
			return StreamErrorMsg{Err: fmt.Errorf("写入临时文件失败: %w", err)}
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
