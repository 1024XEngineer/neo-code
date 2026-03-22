package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/tui/core"
	"go-llm-demo/internal/tui/infra"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	setUTF8Mode()
	loadDotEnv(".env")

	scanner := bufio.NewScanner(os.Stdin)
	ready, err := ensureAPIKeyInteractive(context.Background(), scanner, "config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
		os.Exit(1)
	}
	if !ready {
		fmt.Println("已退出 NeoCode")
		return
	}

	if err := configs.LoadAppConfig("config.yaml"); err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	persona := loadPersonaPrompt(configs.GlobalAppConfig.Persona.FilePath)
	historyTurns := configs.GlobalAppConfig.History.ShortTermTurns

	client, err := infra.NewLocalChatClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	model := core.NewModel(client, persona, historyTurns, "config.yaml")
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}

func ensureAPIKeyInteractive(ctx context.Context, scanner *bufio.Scanner, configPath string) (bool, error) {
	cfg, created, err := configs.EnsureConfigFile(configPath)
	if err != nil {
		return false, err
	}
	if created {
		fmt.Printf("已创建 %s\n", configPath)
	}

	for {
		if cfg.RuntimeAPIKey() == "" {
			envName := cfg.APIKeyEnvVarName()
			fmt.Printf("未检测到环境变量 %s。可使用 /apikey <env_name>、/provider <name> 切换配置，或先设置该环境变量后再 /retry。\n", envName)
			fmt.Printf("Windows 示例: setx %s \"your-api-key\"\n", envName)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			continue
		}

		if err := provider.ValidateChatAPIKey(ctx, cfg); err == nil {
			if saveErr := configs.WriteAppConfig(configPath, cfg); saveErr != nil {
				return false, saveErr
			}
			configs.GlobalAppConfig = cfg
			fmt.Println("API key 验证通过。")
			return true, nil
		} else if errors.Is(err, provider.ErrInvalidAPIKey) {
			fmt.Printf("环境变量 %s 中的 API key 无效: %v\n", cfg.APIKeyEnvVarName(), err)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			continue
		} else if errors.Is(err, provider.ErrAPIKeyValidationSoft) {
			fmt.Printf("无法确认环境变量 %s 中的 API key 有效性: %v\n", cfg.APIKeyEnvVarName(), err)
			result, handleErr := handleSetupDecision(scanner, cfg, true, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			if result == setupContinue {
				configs.GlobalAppConfig = cfg
				return true, nil
			}
			continue
		} else {
			fmt.Printf("模型验证失败: %v\n", err)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			if result == setupContinue {
				configs.GlobalAppConfig = cfg
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

func handleSetupDecision(scanner *bufio.Scanner, cfg *configs.AppConfiguration, allowContinue bool, configPath string) (setupDecision, error) {
	for {
		prompt := "选择 /retry, /apikey <env_name>, /provider <name>, /models, /switch <model>, 或 /exit > "
		if allowContinue {
			prompt = "选择 /retry, /continue, /apikey <env_name>, /provider <name>, /models, /switch <model>, 或 /exit > "
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
		case "/apikey":
			if len(fields) < 2 {
				fmt.Println("用法: /apikey <env_name>")
				continue
			}
			applyAPIKeyEnvName(cfg, fields[1])
			fmt.Printf("已切换 API Key 环境变量名为: %s\n", cfg.APIKeyEnvVarName())
			return setupRetry, nil
		case "/continue":
			if !allowContinue {
				fmt.Println("/continue 仅在网络或服务问题导致无法确认时可用。")
				continue
			}
			if saveErr := configs.WriteAppConfig(configPath, cfg); saveErr != nil {
				return setupExit, saveErr
			}
			fmt.Println("继续启动，使用当前 API key 和模型。")
			return setupContinue, nil
		case "/models":
			printAvailableModels(cfg)
		case "/provider":
			if len(fields) < 2 {
				fmt.Println("用法: /provider <name>")
				printSupportedProviders()
				continue
			}
			providerName, ok := provider.NormalizeProviderName(fields[1])
			if !ok {
				fmt.Printf("不支持的提供商 %q\n", fields[1])
				printSupportedProviders()
				continue
			}
			cfg.AI.Provider = providerName
			if provider.ProviderSupportsModelCatalog(providerName) && !provider.IsSupportedModelForConfig(cfg, cfg.AI.Model) {
				cfg.AI.Model = provider.DefaultModelForConfig(cfg)
			}
			fmt.Printf("已切换到提供商: %s\n", providerName)
			if provider.ProviderSupportsModelCatalog(providerName) {
				fmt.Printf("当前模型: %s\n", cfg.AI.Model)
			} else {
				fmt.Printf("当前提供商不提供内置模型列表，请确认 ai.model，或使用 /switch <model> 设置。当前模型: %s\n", cfg.AI.Model)
			}
			return setupRetry, nil
		case "/switch":
			if len(fields) < 2 {
				fmt.Println("用法: /switch <model>")
				printAvailableModels(cfg)
				continue
			}
			target := strings.Join(fields[1:], " ")
			if provider.ProviderSupportsModelCatalog(cfg.AI.Provider) && !provider.IsSupportedModelForConfig(cfg, target) {
				fmt.Printf("模型 %q 不受支持\n", target)
				printAvailableModels(cfg)
				continue
			}
			cfg.AI.Model = target
			fmt.Printf("已切换到模型: %s\n", target)
			return setupRetry, nil
		case "/exit":
			return setupExit, nil
		default:
			if allowContinue {
				fmt.Println("请输入 /retry, /continue, /apikey <env_name>, /provider <name>, /models, /switch <model>, 或 /exit。")
			} else {
				fmt.Println("请输入 /retry, /apikey <env_name>, /provider <name>, /models, /switch <model>, 或 /exit。")
			}
		}
	}
}

func applyAPIKeyEnvName(cfg *configs.AppConfiguration, envName string) {
	if cfg == nil {
		return
	}
	cfg.AI.APIKey = strings.TrimSpace(envName)
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
			fmt.Println("输入不能为空。")
			continue
		}
		if input == "/exit" {
			return "", false, nil
		}
		return input, true, nil
	}
}

func printAvailableModels(cfg *configs.AppConfiguration) {
	providerName := provider.CurrentProvider()
	if cfg != nil {
		providerName = cfg.AI.Provider
	}
	if !provider.ProviderSupportsModelCatalog(providerName) {
		fmt.Printf("当前提供商 %s 不提供内置模型列表，请使用 /switch <model> 手动设置模型。\n", providerName)
		return
	}
	fmt.Println("可用模型:")
	for _, model := range provider.SupportedModelsForConfig(cfg) {
		fmt.Printf("  %s\n", model)
	}
}

func printSupportedProviders() {
	fmt.Println("可用提供商:")
	for _, name := range provider.SupportedProviders() {
		fmt.Printf("  %s\n", name)
	}
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
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
		os.Setenv(key, value)
	}

	return nil
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

func setUTF8Mode() {
	if runtime.GOOS == "windows" {
		setWindowsUTF8()
	}
}

func setWindowsUTF8() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setConsoleCP := kernel32.NewProc("SetConsoleCP")

	setConsoleOutputCP.Call(uintptr(65001))
	setConsoleCP.Call(uintptr(65001))
}
