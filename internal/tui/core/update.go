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
	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/tools"
	"go-llm-demo/internal/tui/infra"

	tea "github.com/charmbracelet/bubbletea"
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
		m.autoScroll = m.viewport.AtBottom()
		return m, vpCmd

	case StreamChunkMsg:
		if m.generating {
			m.AppendLastMessage(msg.Content)
			m.refreshViewport()
		}
		return m, m.streamResponseFromChannel()

	case StreamDoneMsg:
		m.mu.Lock()
		m.generating = false
		m.streamChan = nil

		var lastContent string
		shouldCheckToolCall := !m.toolExecuting && len(m.messages) > 0
		if len(m.messages) > 0 {
			lastMsg := &m.messages[len(m.messages)-1]
			lastMsg.Streaming = false
			if lastMsg.Role == "assistant" {
				lastContent = lastMsg.Content
			} else {
				shouldCheckToolCall = false
			}
		}
		m.mu.Unlock()

		// 检查最后一条AI消息是否为JSON格式的工具调用
		if shouldCheckToolCall {
			var jsonData map[string]interface{}
			if err := json.Unmarshal([]byte(lastContent), &jsonData); err == nil {
				if toolName, ok := jsonData["tool"].(string); ok && toolName != "" {
					m.mu.Lock()
					if m.toolExecuting {
						m.mu.Unlock()
						return m, nil
					}
					m.toolExecuting = true
					m.mu.Unlock()

					// 显示工具执行中提示
					if toolParams, ok := jsonData["params"].(map[string]interface{}); ok {
						if filePath, ok := toolParams["filePath"].(string); ok && toolName == "read" {
							m.AddMessage("system", fmt.Sprintf("read:正在读取%s文件...", filePath))
						} else if filePath, ok := toolParams["filePath"].(string); ok && (toolName == "edit" || toolName == "write") {
							m.AddMessage("system", fmt.Sprintf("%s:正在处理%s文件...", toolName, filePath))
						} else {
							m.AddMessage("system", fmt.Sprintf("%s:正在执行工具...", toolName))
						}
					}

					// 在goroutine中执行工具调用
					return m, func() tea.Msg {
						var tool tools.Tool
						switch toolName {
						case "read":
							tool = &tools.ReadTool{}
						case "write":
							tool = &tools.WriteTool{}
						case "edit":
							tool = &tools.EditTool{}
						case "bash":
							tool = &tools.BashTool{}
						case "list":
							tool = &tools.ListTool{}
						case "grep":
							tool = &tools.GrepTool{}
						default:
							m.mu.Lock()
							m.toolExecuting = false
							m.mu.Unlock()
							return ToolErrorMsg{Err: fmt.Errorf("不支持的工具: %s", toolName)}
						}

						var paramsMap map[string]interface{}
						if paramsRaw, ok := jsonData["params"]; ok {
							if paramsCasted, ok := paramsRaw.(map[string]interface{}); ok {
								paramsMap = convertSnakeCaseToCamelCase(paramsCasted)
							} else {
								m.mu.Lock()
								m.toolExecuting = false
								m.mu.Unlock()
								return ToolErrorMsg{Err: fmt.Errorf("工具参数格式错误")}
							}
						} else {
							paramsMap = make(map[string]interface{})
						}

						result := tool.Run(paramsMap)

						if result.Success {
							return ToolResultMsg{Result: result}
						}
						return ToolErrorMsg{Err: fmt.Errorf("%s", result.Error)}
					}
				}
			}
		}
		m.refreshViewport()

		return m, nil

	case StreamErrorMsg:
		m.mu.Lock()
		m.generating = false
		m.streamChan = nil
		replacedPlaceholder := false
		if len(m.messages) > 0 {
			lastMsg := &m.messages[len(m.messages)-1]
			if lastMsg.Role == "assistant" && strings.TrimSpace(lastMsg.Content) == "" {
				lastMsg.Content = fmt.Sprintf("错误: %v", msg.Err)
				lastMsg.Streaming = false
				replacedPlaceholder = true
			}
		}
		m.mu.Unlock()
		if !replacedPlaceholder {
			m.AddMessage("assistant", fmt.Sprintf("错误: %v", msg.Err))
		}
		m.TrimHistory(m.historyTurns)
		m.refreshViewport()
		return m, nil

	case ShowHelpMsg:
		m.mode = ModeHelp
		m.refreshViewport()
		return m, nil

	case HideHelpMsg:
		m.mode = ModeChat
		m.refreshViewport()
		return m, nil

	case RefreshMemoryMsg:
		stats, err := m.client.GetMemoryStats(context.Background())
		if err == nil && stats != nil {
			m.memoryStats = *stats
		}
		m.refreshViewport()
		return m, nil

	case ExitMsg:
		return m, tea.Quit

	case ToolResultMsg:
		m.mu.Lock()
		m.toolExecuting = false
		m.mu.Unlock()
		// 将工具执行结果添加为系统消息，然后重新获取AI响应
		m.AddMessage("system", fmt.Sprintf("工具执行结果: %s", msg.Result.Output))
		m.AddMessage("assistant", "")
		m.generating = true
		m.refreshViewport()

		// 构建包含工具结果的消息并重新请求AI
		messages := m.buildMessages()
		return m, m.streamResponse(messages)

	case ToolErrorMsg:
		m.mu.Lock()
		m.toolExecuting = false
		m.mu.Unlock()
		// 将工具执行错误添加为系统消息
		m.AddMessage("system", fmt.Sprintf("工具执行错误: %v", msg.Err))
		m.AddMessage("assistant", "")
		m.generating = true
		m.refreshViewport()

		// 构建包含错误信息的消息并重新请求AI
		messages := m.buildMessages()
		return m, m.streamResponse(messages)
	}

	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc && m.mode == ModeHelp {
		m.mode = ModeChat
		m.refreshViewport()
		return *m, nil
	}

	switch msg.Type {
	case tea.KeyF5:
		return m.handleSubmit()

	case tea.KeyF8:
		return m.handleSubmit()

	case tea.KeyPgUp:
		m.autoScroll = false
		m.viewport.HalfViewUp()
		return *m, nil

	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		m.autoScroll = m.viewport.AtBottom()
		return *m, nil

	case tea.KeyUp:
		if strings.TrimSpace(m.textarea.Value()) == "" && len(m.commandHistory) > 0 {
			if m.cmdHistIndex < len(m.commandHistory)-1 {
				m.cmdHistIndex++
			}
			if m.cmdHistIndex >= 0 && m.cmdHistIndex < len(m.commandHistory) {
				m.textarea.SetValue(m.commandHistory[len(m.commandHistory)-1-m.cmdHistIndex])
				m.textarea.CursorEnd()
				return *m, nil
			}
		}
	case tea.KeyDown:
		if m.cmdHistIndex > 0 {
			m.cmdHistIndex--
			m.textarea.SetValue(m.commandHistory[len(m.commandHistory)-1-m.cmdHistIndex])
			m.textarea.CursorEnd()
			return *m, nil
		}
		if m.cmdHistIndex == 0 {
			m.cmdHistIndex = -1
			m.textarea.Reset()
			return *m, nil
		}
	}

	m.cmdHistIndex = -1
	var inputCmd tea.Cmd
	m.textarea, inputCmd = m.textarea.Update(msg)
	m.refreshViewport()
	if m.viewport.AtBottom() {
		m.autoScroll = true
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

	switch m.mode {
	case ModeHelp:
		m.mode = ModeChat
		return *m, nil
	}

	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}
	if !m.apiKeyReady {
		m.AddMessage("assistant", "当前 API Key 未通过校验，请使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。")
		return *m, nil
	}

	m.AddMessage("user", input)
	m.AddMessage("assistant", "")
	// 在请求发出前先裁剪原始消息，避免 UI 历史无限扩张并影响短期上下文质量。
	m.TrimHistory(m.historyTurns)
	m.generating = true
	m.autoScroll = true
	m.refreshViewport()

	m.commandHistory = append(m.commandHistory, input)
	m.cmdHistIndex = -1

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
	if !m.apiKeyReady && !isAPIKeyRecoveryCommand(cmd) {
		m.AddMessage("assistant", "当前 API Key 未通过校验，仅支持 /apikey <env_name>、/provider <name>、/help、/models、/switch <model> 或 /exit。")
		return *m, nil
	}

	switch cmd {
	case "/help":
		m.mode = ModeHelp
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
			m.apiKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 未设置。请继续使用 /apikey <env_name> 切换，或 /exit 退出。", envName))
			return *m, nil
		}
		err := provider.ValidateChatAPIKey(context.Background(), cfg)
		if err == nil {
			if writeErr := configs.WriteAppConfig(m.configPath, cfg); writeErr != nil {
				cfg.AI.APIKey = previousEnvName
				m.apiKeyReady = configs.RuntimeAPIKey() != ""
				m.AddMessage("assistant", fmt.Sprintf("切换 API Key 环境变量名失败: %v", writeErr))
				return *m, nil
			}
			m.apiKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("已切换 API Key 环境变量名为 %s，并通过校验。", envName))
			return *m, nil
		}
		m.apiKeyReady = false
		if errors.Is(err, provider.ErrInvalidAPIKey) {
			m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 中的 API Key 无效：%v。请继续使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。", envName, err))
			return *m, nil
		}
		m.AddMessage("assistant", fmt.Sprintf("环境变量 %s 的 API Key 未通过校验：%v。请继续使用 /apikey <env_name>、/provider <name>、/switch <model> 调整配置，或 /exit 退出。", envName, err))
		return *m, nil
	case "/provider":
		if len(args) == 0 {
			m.AddMessage("assistant", fmt.Sprintf("用法: /provider <name>\n可用提供商:\n  - %s", strings.Join(provider.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg := configs.GlobalAppConfig
		if cfg == nil {
			m.AddMessage("assistant", "当前配置未加载，无法切换提供商")
			return *m, nil
		}
		providerName, ok := provider.NormalizeProviderName(strings.Join(args, " "))
		if !ok {
			m.AddMessage("assistant", fmt.Sprintf("不支持的提供商: %s\n可用提供商:\n  - %s", strings.Join(args, " "), strings.Join(provider.SupportedProviders(), "\n  - ")))
			return *m, nil
		}
		cfg.AI.Provider = providerName
		if provider.ProviderSupportsModelCatalog(providerName) && !provider.IsSupportedModelForConfig(cfg, cfg.AI.Model) {
			cfg.AI.Model = provider.DefaultModelForConfig(cfg)
		}
		m.activeModel = cfg.AI.Model
		if writeErr := configs.WriteAppConfig(m.configPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("切换提供商失败: %v", writeErr))
			return *m, nil
		}
		if cfg.RuntimeAPIKey() == "" {
			m.apiKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s，但当前环境变量 %s 未设置。请使用 /apikey <env_name> 或设置该环境变量。", providerName, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := provider.ValidateChatAPIKey(context.Background(), cfg); err == nil {
			m.apiKeyReady = true
			if provider.ProviderSupportsModelCatalog(providerName) {
				m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s，当前模型: %s。", providerName, cfg.AI.Model))
			} else {
				m.AddMessage("assistant", fmt.Sprintf("已切换到提供商 %s。该提供商不提供内置模型列表，请确认当前模型 %s，或使用 /switch <model> 修改。", providerName, cfg.AI.Model))
			}
			return *m, nil
		} else {
			m.apiKeyReady = false
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
		if provider.ProviderSupportsModelCatalog(cfg.AI.Provider) && !provider.IsSupportedModelForConfig(cfg, target) {
			m.AddMessage("assistant", fmt.Sprintf("模型不可用: %s", target))
			return *m, nil
		}
		cfg.AI.Model = target
		if writeErr := configs.WriteAppConfig(m.configPath, cfg); writeErr != nil {
			m.AddMessage("assistant", fmt.Sprintf("切换模型失败: %v", writeErr))
			return *m, nil
		}
		m.activeModel = target
		if cfg.RuntimeAPIKey() == "" {
			m.apiKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型: %s，但当前环境变量 %s 未设置。", target, cfg.APIKeyEnvVarName()))
			return *m, nil
		}
		if err := provider.ValidateChatAPIKey(context.Background(), cfg); err == nil {
			m.apiKeyReady = true
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型: %s", target))
			return *m, nil
		} else {
			m.apiKeyReady = false
			m.AddMessage("assistant", fmt.Sprintf("已切换到模型 %s，但 API Key 未通过校验：%v。", target, err))
			return *m, nil
		}
	case "/models":
		models := m.client.ListModels()
		if len(models) == 0 {
			m.AddMessage("assistant", fmt.Sprintf("当前提供商 %s 不提供内置模型列表，请使用 /switch <model> 手动设置模型。", provider.CurrentProvider()))
			return *m, nil
		}
		list := strings.Join(models, "\n  - ")
		m.AddMessage("assistant", fmt.Sprintf("可用模型:\n  - %s", list))
	case "/memory":
		stats, err := m.client.GetMemoryStats(context.Background())
		if err != nil {
			m.AddMessage("assistant", fmt.Sprintf("读取记忆统计失败: %v", err))
			return *m, nil
		}
		m.memoryStats = *stats
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
			m.memoryStats = *stats
		}
		m.AddMessage("assistant", "已清空本地长期记忆")
	case "/clear-context":
		if err := m.client.ClearSessionMemory(context.Background()); err != nil {
			m.AddMessage("assistant", fmt.Sprintf("清空会话记忆失败: %v", err))
			return *m, nil
		}
		m.messages = nil
		stats, _ := m.client.GetMemoryStats(context.Background())
		if stats != nil {
			m.memoryStats = *stats
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
	case "/apikey", "/provider", "/help", "/models", "/switch", "/exit", "/quit", "/q":
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
		domain.TypeUserPreference,
		domain.TypeProjectRule,
		domain.TypeCodeFact,
		domain.TypeFixRecipe,
		domain.TypeSessionMemory,
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

func (m *Model) buildMessages() []infra.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]infra.Message, 0, len(m.messages))

	for _, msg := range m.messages {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Role == "system" {
			result = append(result, infra.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	for _, msg := range m.messages {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Role != "system" {
			result = append(result, infra.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return result
}

func (m *Model) streamResponse(messages []infra.Message) tea.Cmd {
	stream, err := m.client.Chat(context.Background(), messages, m.activeModel)
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
	m.TrimHistory(m.historyTurns)
	m.generating = true
	m.autoScroll = true
	m.refreshViewport()

	messages := m.buildMessages()
	return m.streamResponse(messages)
}

// convertSnakeCaseToCamelCase 将snake_case键转换为camelCase
func convertSnakeCaseToCamelCase(params map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range params {
		// 将snake_case转换为camelCase
		parts := strings.Split(key, "_")
		if len(parts) > 1 {
			camelKey := parts[0]
			for i := 1; i < len(parts); i++ {
				if len(parts[i]) > 0 {
					camelKey += strings.ToUpper(parts[i][:1]) + parts[i][1:]
				}
			}
			result[camelKey] = value
		} else {
			result[key] = value
		}
	}
	return result
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
	if m.width <= 0 || m.height <= 0 {
		return
	}
	inputWidth := m.width
	if inputWidth < 20 {
		inputWidth = 20
	}
	m.textarea.SetWidth(inputWidth)
	m.textarea.SetHeight(m.calculateInputHeight())
	m.textarea.Prompt = "> "

	statusHeight := 1
	inputHeight := m.textarea.Height() + 2
	helpHeight := 0
	if m.mode == ModeHelp {
		helpHeight = minInt(20, m.height-statusHeight-3)
	}
	contentHeight := m.height - statusHeight - inputHeight - helpHeight
	if contentHeight < 3 {
		contentHeight = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = contentHeight
}

func (m *Model) refreshViewport() {
	m.syncLayout()
	content := m.renderChatContent()
	m.viewport.SetContent(content)
	if m.autoScroll || m.viewport.AtBottom() {
		m.viewport.GotoBottom()
		m.autoScroll = true
	}
}
