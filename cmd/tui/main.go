package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/tui/infra"
)

const defaultHistoryTurns = 6

func main() {
	if err := loadDotEnv(".env"); err != nil {
		fmt.Printf("加载 .env 失败：%v\n", err)
		return
	}

	activeModel := strings.TrimSpace(os.Getenv("AI_MODEL"))
	if activeModel == "" {
		activeModel = provider.DefaultModel()
	}

	personaPrompt, err := loadPersonaPrompt(os.Getenv("PERSONA_FILE_PATH"))
	if err != nil {
		fmt.Printf("加载人设文件失败：%v\n", err)
		return
	}

	if activeModel == "" {
		fmt.Println("未配置可用模型")
		return
	}

	fmt.Println("=== NeoCode ===")
	fmt.Println("Use /switch <model> to change models, /models to list available models, /help for commands")

	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	historyTurns := envInt("SHORT_TERM_HISTORY_TURNS", defaultHistoryTurns)
	history := initialHistory(personaPrompt, historyTurns)

	apiClient, err := infra.NewLocalChatClient()
	if err != nil {
		fmt.Printf("初始化失败：%v\n", err)
		return
	}

	for {
		fmt.Printf("[%s] > ", activeModel)
		if !scanner.Scan() {
			fmt.Println("\nExiting NeoCode")
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			historyChanged := false
			shouldExit, err := handleCommand(ctx, line, &activeModel, &history, &historyChanged, personaPrompt, historyTurns, apiClient)
			if err != nil {
				fmt.Println(err)
			}
			if historyChanged {
				continue
			}
			if shouldExit {
				fmt.Println("Exiting NeoCode")
				break
			}
			continue
		}

		fmt.Println("Thinking...")
		messages := append([]infra.Message(nil), history...)
		messages = append(messages, infra.Message{Role: "user", Content: line})

		rep, err := apiClient.Chat(ctx, messages, activeModel)
		if err != nil {
			fmt.Printf("生成失败：%v\n", err)
			continue
		}

		var replyBuilder strings.Builder
		for msg := range rep {
			replyBuilder.WriteString(msg)
			fmt.Print(msg)
		}
		if replyBuilder.Len() > 0 {
			history = append(history, infra.Message{Role: "assistant", Content: replyBuilder.String()})
			history = trimHistory(history, historyTurns)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("输入错误：%v\n", err)
	}
}

func handleCommand(ctx context.Context, input string, activeModel *string, history *[]infra.Message, historyChanged *bool, personaPrompt string, historyTurns int, client infra.ChatClient) (bool, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false, nil
	}

	switch fields[0] {
	case "/switch":
		if len(fields) < 2 {
			printAvailableModels()
			return false, fmt.Errorf("用法：/switch <model>")
		}

		target := fields[1]
		if !provider.IsSupportedModel(target) {
			printAvailableModels()
			return false, fmt.Errorf("模型不受支持：%s", target)
		}

		*activeModel = target
		fmt.Printf("已切换到模型：%s\n", target)
	case "/models":
		printAvailableModels()
	case "/memory":
		stats, err := client.GetMemoryStats(ctx)
		if err != nil {
			return false, err
		}
		fmt.Printf("memory items: %d, topK: %d, minScore: %.2f, file: %s\n",
			stats.Items, stats.TopK, stats.MinScore, stats.Path)
	case "/clear-memory":
		if err := client.ClearMemory(ctx); err != nil {
			return false, err
		}
		fmt.Println("已清空本地长期记忆")
	case "/clear-context":
		*history = initialHistory(personaPrompt, historyTurns)
		*historyChanged = true
		fmt.Println("已清空当前会话上下文")
	case "/help":
		printHelp()
	case "/exit":
		return true, nil
	default:
		fmt.Printf("无法识别的命令：%s，输入 /help 查看帮助\n", fields[0])
	}

	return false, nil
}

func printAvailableModels() {
	fmt.Println("Available models:")
	for _, model := range provider.SupportedModels {
		fmt.Printf("  %s\n", model)
	}
}

func printHelp() {
	fmt.Println("Commands:")
	fmt.Println("  /switch <model>  Switch the active model")
	fmt.Println("  /models          List supported models")
	fmt.Println("  /memory          Show local memory stats")
	fmt.Println("  /clear-memory    Clear local long-term memory")
	fmt.Println("  /clear-context   Clear current short-term context")
	fmt.Println("  /exit            Exit the program")
	fmt.Println("  /help            Show this help text")
}

func trimHistory(history []infra.Message, maxTurns int) []infra.Message {
	var systemMessages []infra.Message
	start := 0
	for start < len(history) && history[start].Role == "system" {
		systemMessages = append(systemMessages, history[start])
		start++
	}

	conversation := history[start:]
	maxMessages := maxTurns * 2
	if maxTurns <= 0 || len(conversation) <= maxMessages {
		return history
	}

	trimmed := append([]infra.Message(nil), systemMessages...)
	trimmed = append(trimmed, conversation[len(conversation)-maxMessages:]...)
	return trimmed
}

func initialHistory(personaPrompt string, historyTurns int) []infra.Message {
	history := make([]infra.Message, 0, historyTurns*2+1)
	if personaPrompt != "" {
		history = append(history, infra.Message{Role: "system", Content: personaPrompt})
	}
	return history
}

func loadPersonaPrompt(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed := 0
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return fallback
		}
		parsed = parsed*10 + int(ch-'0')
	}
	if parsed <= 0 {
		return fallback
	}
	return parsed
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return scanner.Err()
}
