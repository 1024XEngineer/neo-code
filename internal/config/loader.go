package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultPath returns the canonical config path in the user's home directory.
func DefaultPath() string {
	dir := defaultStateDir()
	if dir == "" {
		return filepath.Join("~", defaultConfigDirName, defaultConfigFileName)
	}

	return filepath.Join(dir, defaultConfigFileName)
}

// DefaultSessionsPath returns the canonical on-disk session store location.
func DefaultSessionsPath() string {
	dir := defaultStateDir()
	if dir == "" {
		return filepath.Join("~", defaultConfigDirName, defaultSessionsFileName)
	}

	return filepath.Join(dir, defaultSessionsFileName)
}

func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, defaultConfigDirName)
}

// Load reads, normalizes, and validates a config file.
func Load(path string) (Config, error) {
	resolvedPath, err := expandPath(path)
	if err != nil {
		return Config{}, err
	}

	payload, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := writeDefaultConfig(resolvedPath); err != nil {
				return Config{}, fmt.Errorf("创建默认配置文件失败: %w", err)
			}

			payload, err = os.ReadFile(resolvedPath)
			if err != nil {
				return Config{}, fmt.Errorf("读取自动生成的配置文件失败: %w", err)
			}
		} else {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := yaml.Unmarshal(payload, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if err := normalize(&cfg); err != nil {
		return Config{}, err
	}

	if err := Validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func writeDefaultConfig(path string) error {
	cfg := defaultConfig()

	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化默认配置失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	header := "# NeoCode 首次启动已自动生成此配置文件。\n" +
		"# 你通常只需要确认 model / workdir，并设置对应的 API Key 环境变量。\n\n"

	if err := os.WriteFile(path, append([]byte(header), payload...), 0o644); err != nil {
		return fmt.Errorf("写入默认配置文件失败: %w", err)
	}

	return nil
}

func defaultConfig() Config {
	model := "gpt-4.1-mini"

	return Config{
		Providers: []ProviderConfig{
			{
				Name:      "openai",
				Type:      "openai",
				BaseURL:   "https://api.openai.com/v1",
				Model:     model,
				APIKeyEnv: "OPENAI_API_KEY",
			},
		},
		SelectedProvider: "openai",
		CurrentModel:     model,
		Workdir:          ".",
		Shell:            defaultShell(),
		SessionsPath:     DefaultSessionsPath(),
	}
}

func defaultShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func normalize(cfg *Config) error {
	if len(cfg.Providers) == 0 {
		return nil
	}

	for idx := range cfg.Providers {
		cfg.Providers[idx].Name = strings.TrimSpace(cfg.Providers[idx].Name)
		cfg.Providers[idx].Type = strings.TrimSpace(cfg.Providers[idx].Type)
		cfg.Providers[idx].BaseURL = strings.TrimSpace(cfg.Providers[idx].BaseURL)
		cfg.Providers[idx].Model = strings.TrimSpace(cfg.Providers[idx].Model)
		cfg.Providers[idx].APIKeyEnv = normalizeEnvName(cfg.Providers[idx].APIKeyEnv)
	}

	if strings.TrimSpace(cfg.SelectedProvider) == "" {
		cfg.SelectedProvider = cfg.Providers[0].Name
	}

	if strings.TrimSpace(cfg.CurrentModel) == "" {
		if provider, ok := cfg.ProviderByName(cfg.SelectedProvider); ok {
			cfg.CurrentModel = provider.Model
		}
	}

	if strings.TrimSpace(cfg.Workdir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve current directory: %w", err)
		}
		cfg.Workdir = cwd
	} else {
		workdir, err := filepath.Abs(cfg.Workdir)
		if err != nil {
			return fmt.Errorf("resolve workdir: %w", err)
		}
		cfg.Workdir = workdir
	}

	if strings.TrimSpace(cfg.Shell) == "" {
		cfg.Shell = defaultShell()
	}

	if strings.TrimSpace(cfg.SessionsPath) == "" {
		cfg.SessionsPath = DefaultSessionsPath()
	} else {
		sessionsPath, err := expandPath(cfg.SessionsPath)
		if err != nil {
			return fmt.Errorf("resolve sessions path: %w", err)
		}
		cfg.SessionsPath = sessionsPath
	}

	return nil
}

func normalizeEnvName(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"'`)

	if strings.HasPrefix(trimmed, "${") && strings.HasSuffix(trimmed, "}") {
		trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "${"), "}")
	}

	if strings.HasPrefix(trimmed, "$") {
		trimmed = strings.TrimPrefix(trimmed, "$")
	}

	return strings.TrimSpace(trimmed)
}

func expandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = DefaultPath()
	}

	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}

		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~"))
	}

	return filepath.Abs(trimmed)
}
