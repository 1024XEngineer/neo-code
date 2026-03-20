package main

import (
	"bufio"
	"context"
	"fmt"
	"neo-code/ai"
	"neo-code/config"
	"neo-code/services"
	"os"
	"strings"
)

func main() {
	fmt.Println("=== NeoCode ===")
	fmt.Println("Use /switch <model> to change models, /models to list available models, /help for commands")
	// 加载应用与模型 YAML
	if err := config.LoadAppConfig("config.yaml"); err != nil {
		fmt.Printf("Failed to load app config: %v\n", err)
		return
	}
	if err := config.LoadModelConfig("config/models.yaml"); err != nil {
		fmt.Printf("Warning: model config not loaded: %v\n", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	activeModel := ai.DefaultModel()
	if activeModel == "" {
		fmt.Println("Default model is not configured, cannot start")
		return
	}

	personaPrompt, err := loadPersonaPrompt(personaFilePath())
	if err != nil {
		fmt.Printf("Failed to load persona file: %v\n", err)
		return
	}

	historyTurns := shortTermHistoryTurns()
	history := initialHistory(personaPrompt, historyTurns)

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
			shouldExit, err := services.HandleCommand(ctx, line, &activeModel, &historyChanged)
			if err != nil {
				fmt.Println(err)
			}
			if historyChanged {
				history = initialHistory(personaPrompt, historyTurns)
				continue
			}
			if shouldExit {
				fmt.Println("Exiting NeoCode")
				break
			}
			continue
		}

		fmt.Println("Thinking...")
		history = append(history, ai.Message{Role: "user", Content: line})
		messages := append([]ai.Message(nil), history...)
		rep, err := services.Chat(ctx, messages, activeModel)
		if err != nil {
			history = history[:len(history)-1]
			fmt.Printf("Generation failed: %v\n", err)
			continue
		}

		var replyBuilder strings.Builder
		for msg := range rep {
			replyBuilder.WriteString(msg)
			fmt.Print(msg)
		}
		if replyBuilder.Len() > 0 {
			history = append(history, ai.Message{Role: "assistant", Content: replyBuilder.String()})
			history = trimHistory(history, historyTurns)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Input error: %v\n", err)
	}
}

func trimHistory(history []ai.Message, maxTurns int) []ai.Message {
	var systemMessages []ai.Message
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

	trimmed := append([]ai.Message(nil), systemMessages...)
	trimmed = append(trimmed, conversation[len(conversation)-maxMessages:]...)
	return trimmed
}

func initialHistory(personaPrompt string, historyTurns int) []ai.Message {
	history := make([]ai.Message, 0, historyTurns*2+1)
	if personaPrompt != "" {
		history = append(history, ai.Message{Role: "system", Content: personaPrompt})
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

func personaFilePath() string {
	if config.GlobalAppConfig != nil && strings.TrimSpace(config.GlobalAppConfig.Persona.FilePath) != "" {
		return strings.TrimSpace(config.GlobalAppConfig.Persona.FilePath)
	}
	return "./persona.txt"
}

func shortTermHistoryTurns() int {
	if config.GlobalAppConfig != nil && config.GlobalAppConfig.History.ShortTermTurns > 0 {
		return config.GlobalAppConfig.History.ShortTermTurns
	}
	return 6
}
