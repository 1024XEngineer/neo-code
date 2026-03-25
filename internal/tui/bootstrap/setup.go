package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

var (
	resolveWorkspaceRoot = services.ResolveWorkspaceRoot
	setWorkspaceRoot     = services.SetWorkspaceRoot
	initializeSecurity   = services.InitializeSecurity
	ensureConfigFile     = configs.EnsureConfigFile
	validateChatAPIKey   = services.ValidateChatAPIKey
	writeAppConfig       = configs.WriteAppConfig
)

func PrepareWorkspace(workspaceFlag string) (string, error) {
	workspaceRoot, err := resolveWorkspaceRoot(workspaceFlag)
	if err != nil {
		return "", fmt.Errorf("解析工作区失败: %w", err)
	}
	if err := setWorkspaceRoot(workspaceRoot); err != nil {
		return "", fmt.Errorf("设置工作区失败: %w", err)
	}
	if err := initializeSecurity(filepath.Join(workspaceRoot, "configs", "security")); err != nil {
		return "", fmt.Errorf("初始化安全策略失败: %w", err)
	}
	return workspaceRoot, nil
}

func EnsureAPIKeyInteractive(ctx context.Context, scanner *bufio.Scanner, configPath string) (bool, error) {
	cfg, created, err := ensureConfigFile(configPath)
	if err != nil {
		return false, err
	}
	if created {
		fmt.Printf("Created %s\n", configPath)
	}

	for {
		if cfg.RuntimeAPIKey() == "" {
			envName := cfg.APIKeyEnvVarName()
			fmt.Printf("Environment variable %s is not set. Use /apikey <env_name>, /provider <name>, or /switch <model> to change the configuration, or set the variable and then run /retry.\n", envName)
			fmt.Printf("Windows example: setx %s \"your-api-key\"\n", envName)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			continue
		}

		if err := validateChatAPIKey(ctx, cfg); err == nil {
			if saveErr := writeAppConfig(configPath, cfg); saveErr != nil {
				return false, saveErr
			}
			configs.GlobalAppConfig = cfg
			fmt.Println("API key validation passed.")
			return true, nil
		} else if errors.Is(err, services.ErrInvalidAPIKey) {
			fmt.Printf("The API key in environment variable %s is invalid: %v\n", cfg.APIKeyEnvVarName(), err)
			result, handleErr := handleSetupDecision(scanner, cfg, false, configPath)
			if handleErr != nil {
				return false, handleErr
			}
			if result == setupExit {
				return false, nil
			}
			continue
		} else if errors.Is(err, services.ErrAPIKeyValidationSoft) {
			fmt.Printf("Could not verify the API key in environment variable %s: %v\n", cfg.APIKeyEnvVarName(), err)
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
			fmt.Printf("Model validation failed: %v\n", err)
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
		prompt := "Choose /retry, /apikey <env_name>, /provider <name>, /switch <model>, or /exit > "
		if allowContinue {
			prompt = "Choose /retry, /continue, /apikey <env_name>, /provider <name>, /switch <model>, or /exit > "
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
				fmt.Println("Usage: /apikey <env_name>")
				continue
			}
			applyAPIKeyEnvName(cfg, fields[1])
			fmt.Printf("Switched the API key environment variable name to: %s\n", cfg.APIKeyEnvVarName())
			return setupRetry, nil
		case "/continue":
			if !allowContinue {
				fmt.Println("/continue is only available when the API key cannot be verified due to a network or service issue.")
				continue
			}
			if saveErr := writeAppConfig(configPath, cfg); saveErr != nil {
				return setupExit, saveErr
			}
			fmt.Println("Continuing with the current API key and model.")
			return setupContinue, nil
		case "/provider":
			if len(fields) < 2 {
				fmt.Println("Usage: /provider <name>")
				printSupportedProviders()
				continue
			}
			providerName, ok := services.NormalizeProviderName(fields[1])
			if !ok {
				fmt.Printf("Unsupported provider %q\n", fields[1])
				printSupportedProviders()
				continue
			}
			cfg.AI.Provider = providerName
			cfg.AI.Model = services.DefaultModelForProvider(providerName)
			fmt.Printf("Switched provider to: %s\n", providerName)
			fmt.Printf("Reset the current model to the default: %s\n", cfg.AI.Model)
			return setupRetry, nil
		case "/switch":
			if len(fields) < 2 {
				fmt.Println("Usage: /switch <model>")
				continue
			}
			target := strings.Join(fields[1:], " ")
			cfg.AI.Model = target
			fmt.Printf("Switched model to: %s\n", target)
			return setupRetry, nil
		case "/exit":
			return setupExit, nil
		default:
			if allowContinue {
				fmt.Println("Enter /retry, /continue, /apikey <env_name>, /provider <name>, /switch <model>, or /exit.")
			} else {
				fmt.Println("Enter /retry, /apikey <env_name>, /provider <name>, /switch <model>, or /exit.")
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
			fmt.Println("Input cannot be empty.")
			continue
		}
		if input == "/exit" {
			return "", false, nil
		}
		return input, true, nil
	}
}

func printSupportedProviders() {
	fmt.Println("Supported providers:")
	for _, name := range services.SupportedProviders() {
		fmt.Printf("  %s\n", name)
	}
}
