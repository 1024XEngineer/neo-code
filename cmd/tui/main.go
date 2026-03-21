package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"go-llm-demo/config"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/tui/core"
	"go-llm-demo/internal/tui/infra"
)

func main() {
	const configPath = "config.yaml"

	scanner := bufio.NewScanner(os.Stdin)
	ready, err := ensureAPIKeyInteractive(context.Background(), scanner, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
		os.Exit(1)
	}
	if !ready {
		fmt.Println("已退出 NeoCode")
		return
	}

	if err := config.LoadAppConfig(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	persona := loadPersonaPrompt(personaFilePath())
	client, err := infra.NewLocalChatClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	model := core.NewModel(client, persona)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}

func ensureAPIKeyInteractive(ctx context.Context, scanner *bufio.Scanner, configPath string) (bool, error) {
	cfg, created, err := config.EnsureConfigFile(configPath)
	if err != nil {
		return false, err
	}
	if created {
		fmt.Printf("Created %s with default settings.\n", configPath)
	}

	for {
		apiKey := strings.TrimSpace(cfg.AI.APIKey)
		if apiKey == "" {
			fmt.Println("No API key configured. Enter your API key, or type /exit to quit.")
			input, ok, inputErr := readInteractiveLine(scanner, "api_key> ")
			if inputErr != nil {
				return false, inputErr
			}
			if !ok {
				return false, nil
			}
			cfg.AI.APIKey = input
		}

		if err := provider.ValidateChatAPIKey(ctx, cfg); err == nil {
			if saveErr := config.WriteAppConfig(configPath, cfg); saveErr != nil {
				return false, saveErr
			}
			fmt.Println("API key validated and saved.")
			return true, nil
		} else if errors.Is(err, provider.ErrInvalidAPIKey) {
			fmt.Printf("API key is invalid: %v\n", err)
			cfg.AI.APIKey = ""
			continue
		} else if errors.Is(err, provider.ErrAPIKeyValidationSoft) {
			fmt.Printf("Unable to confirm API key validity: %v\n", err)
			result, handleErr := handleSetupDecision(scanner, cfg, true, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			if result == setupContinue {
				config.GlobalAppConfig = cfg
				return true, nil
			}
			continue
		} else {
			fmt.Printf("Model validation failed: %v\n", err)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			if result == setupContinue {
				config.GlobalAppConfig = cfg
				return true, nil
			}
		}
	}
}

type setupDecision int

const (
	setupRetry setupDecision = iota
	setupContinue
	setupExit
)

func handleSetupDecision(scanner *bufio.Scanner, cfg *config.AppConfiguration, allowContinue bool, configPath string) (setupDecision, error) {
	for {
		prompt := "Choose /retry, /models, /switch <model>, or /exit > "
		if allowContinue {
			prompt = "Choose /retry, /continue, /models, /switch <model>, or /exit > "
		}
		decision, ok, inputErr := readInteractiveLine(scanner, prompt)
		if inputErr != nil {
			return setupExit, inputErr
		}
		if !ok {
			return setupExit, nil
		}

		fields := strings.Fields(strings.TrimSpace(decision))
		if len(fields) == 0 {
			continue
		}

		switch strings.ToLower(fields[0]) {
		case "/retry":
			return setupRetry, nil
		case "/continue":
			if !allowContinue {
				fmt.Println("/continue is only available when validation cannot be confirmed because of network or service issues.")
				continue
			}
			if saveErr := config.WriteAppConfig(configPath, cfg); saveErr != nil {
				return setupExit, saveErr
			}
			fmt.Println("Continuing startup with the current API key and model.")
			return setupContinue, nil
		case "/models":
			printAvailableModels()
		case "/switch":
			if len(fields) < 2 {
				fmt.Println("Usage: /switch <model>")
				printAvailableModels()
				continue
			}
			target := fields[1]
			if !provider.IsSupportedModel(target) {
				fmt.Printf("Model %q is not supported\n", target)
				printAvailableModels()
				continue
			}
			cfg.AI.Model = target
			fmt.Printf("Switched startup validation model to %s\n", target)
			return setupRetry, nil
		case "/exit":
			return setupExit, nil
		default:
			if allowContinue {
				fmt.Println("Please enter /retry, /continue, /models, /switch <model>, or /exit.")
			} else {
				fmt.Println("Please enter /retry, /models, /switch <model>, or /exit.")
			}
		}
	}
}

func readInteractiveLine(scanner *bufio.Scanner, prompt string) (string, bool, error) {
	for {
		fmt.Print(prompt)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", false, err
			}
			return "", false, nil
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Println("Input cannot be empty.")
			continue
		}
		if input == "/exit" {
			return "", false, nil
		}
		return input, true, nil
	}
}

func printAvailableModels() {
	fmt.Println("Available models:")
	for _, model := range provider.SupportedModels() {
		fmt.Printf("  %s\n", model)
	}
}

func loadPersonaPrompt(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func personaFilePath() string {
	if config.GlobalAppConfig != nil && strings.TrimSpace(config.GlobalAppConfig.Persona.FilePath) != "" {
		return strings.TrimSpace(config.GlobalAppConfig.Persona.FilePath)
	}
	return "./persona.txt"
}
