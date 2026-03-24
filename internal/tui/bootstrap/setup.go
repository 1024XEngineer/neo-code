package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/services"
)

type setupDecision int

const (
	setupRetry setupDecision = iota
	setupContinue
	setupExit
)

func PrepareWorkspace(workspaceFlag string) (string, error) {
	workspaceRoot, err := services.ResolveWorkspaceRoot(workspaceFlag)
	if err != nil {
		return "", err
	}
	if err := services.SetWorkspaceRoot(workspaceRoot); err != nil {
		return "", err
	}
	return workspaceRoot, nil
}

func EnsureAPIKeyInteractive(ctx context.Context, scanner *bufio.Scanner, configPath string) (bool, error) {
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
			fmt.Printf("未检测到环境变量 %s。可使用 /apikey <env_name>、/provider <name>、/switch <model> 切换配置，或先设置该环境变量后再 /retry。\n", envName)
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

		if err := services.ValidateChatAPIKey(ctx, cfg); err == nil {
			if saveErr := configs.WriteAppConfig(configPath, cfg); saveErr != nil {
				return false, saveErr
			}
			configs.GlobalAppConfig = cfg
			fmt.Println("API key 验证通过。")
			return true, nil
		} else if errors.Is(err, services.ErrInvalidAPIKey) {
			fmt.Printf("环境变量 %s 中的 API key 无效: %v\n", cfg.APIKeyEnvVarName(), err)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			continue
		} else if errors.Is(err, services.ErrAPIKeyValidationSoft) {
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

func handleSetupDecision(scanner *bufio.Scanner, cfg *configs.AppConfiguration, allowContinue bool, configPath string) (setupDecision, error) {
	for {
		prompt := "选择 /retry, /apikey <env_name>, /provider <name>, /switch <model>, 或 /exit > "
		if allowContinue {
			prompt = "选择 /retry, /continue, /apikey <env_name>, /provider <name>, /switch <model>, 或 /exit > "
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
		case "/provider":
			if len(fields) < 2 {
				fmt.Println("用法: /provider <name>")
				printSupportedProviders()
				continue
			}
			providerName, ok := services.NormalizeProviderName(fields[1])
			if !ok {
				fmt.Printf("不支持的提供商 %q\n", fields[1])
				printSupportedProviders()
				continue
			}
			cfg.AI.Provider = providerName
			cfg.AI.Model = services.DefaultModelForProvider(providerName)
			fmt.Printf("已切换到提供商: %s\n", providerName)
			fmt.Printf("当前模型已重置为默认值: %s\n", cfg.AI.Model)
			return setupRetry, nil
		case "/switch":
			if len(fields) < 2 {
				fmt.Println("用法: /switch <model>")
				continue
			}
			target := strings.Join(fields[1:], " ")
			cfg.AI.Model = target
			fmt.Printf("已切换到模型: %s\n", target)
			return setupRetry, nil
		case "/exit":
			return setupExit, nil
		default:
			if allowContinue {
				fmt.Println("请输入 /retry, /continue, /apikey <env_name>, /provider <name>, /switch <model>, 或 /exit。")
			} else {
				fmt.Println("请输入 /retry, /apikey <env_name>, /provider <name>, /switch <model>, 或 /exit。")
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

func printSupportedProviders() {
	fmt.Println("可用提供商:")
	for _, name := range services.SupportedProviders() {
		fmt.Printf("  %s\n", name)
	}
}
