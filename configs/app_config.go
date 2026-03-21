package configs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const APIKeyEnvVar = "AI_API_KEY"

type ModelDetail struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type ModelGroup struct {
	DefaultModel string        `yaml:"default_model"`
	Models       []ModelDetail `yaml:"models"`
}

type AppConfiguration struct {
	App struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"app"`

	AI struct {
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
	} `yaml:"ai"`

	Memory struct {
		TopK           int      `yaml:"top_k"`
		MinMatchScore  float64  `yaml:"min_match_score"`
		MaxPromptChars int      `yaml:"max_prompt_chars"`
		MaxItems       int      `yaml:"max_items"`
		StoragePath    string   `yaml:"storage_path"`
		PersistTypes   []string `yaml:"persist_types"`
	} `yaml:"memory"`

	History struct {
		ShortTermTurns int `yaml:"short_term_turns"`
	} `yaml:"history"`

	Persona struct {
		FilePath string `yaml:"file_path"`
	} `yaml:"persona"`

	Models struct {
		Chat ModelGroup `yaml:"chat"`
	} `yaml:"models"`
}

var GlobalAppConfig *AppConfiguration

// DefaultAppConfig 返回内置的应用默认配置。
func DefaultAppConfig() *AppConfiguration {
	cfg := &AppConfiguration{}
	cfg.App.Name = "NeoCode"
	cfg.App.Version = "1.0.0"
	cfg.AI.Provider = "modelscope"
	cfg.AI.APIKey = ""
	cfg.AI.Model = "Qwen/Qwen3-Coder-480B-A35B-Instruct"
	cfg.Memory.TopK = 5
	cfg.Memory.MinMatchScore = 2.2
	cfg.Memory.MaxPromptChars = 1800
	cfg.Memory.MaxItems = 1000
	cfg.Memory.StoragePath = "./data/memory_rules.json"
	cfg.Memory.PersistTypes = []string{"user_preference", "project_rule", "code_fact", "fix_recipe"}
	cfg.History.ShortTermTurns = 6
	cfg.Persona.FilePath = "./persona.txt"
	cfg.Models.Chat.DefaultModel = "Qwen/Qwen3-Coder-480B-A35B-Instruct"
	cfg.Models.Chat.Models = []ModelDetail{
		{Name: "Qwen/Qwen3-Coder-480B-A35B-Instruct", URL: "https://api-inference.modelscope.cn/v1/chat/completions"},
		{Name: "ZhipuAI/GLM-5", URL: "https://api-inference.modelscope.cn/v1/chat/completions"},
		{Name: "moonshotai/Kimi-K2.5", URL: "https://api-inference.modelscope.cn/v1/chat/completions"},
		{Name: "deepseek-ai/DeepSeek-R1-0528", URL: "https://api-inference.modelscope.cn/v1/chat/completions"},
	}
	return cfg
}

// LoadAppConfig 加载运行时配置并保存到全局变量。
func LoadAppConfig(filePath string) error {
	cfg, err := LoadBootstrapConfig(filePath)
	if err != nil {
		return err
	}
	if err := cfg.ValidateRuntime(); err != nil {
		return err
	}
	GlobalAppConfig = cfg
	return nil
}

// LoadBootstrapConfig 加载不依赖运行时密钥的基础配置。
func LoadBootstrapConfig(filePath string) (*AppConfiguration, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read app config file: %w", err)
	}

	cfg := DefaultAppConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse app config YAML: %w", err)
	}
	cfg.AI.APIKey = ""
	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EnsureConfigFile 加载已有配置文件，或在缺失时写入默认配置。
func EnsureConfigFile(filePath string) (*AppConfiguration, bool, error) {
	if _, err := os.Stat(filePath); err == nil {
		cfg, loadErr := LoadBootstrapConfig(filePath)
		return cfg, false, loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("failed to stat app config file: %w", err)
	}

	cfg := DefaultAppConfig()
	if err := WriteAppConfig(filePath, cfg); err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// WriteAppConfig 将清空 API Key 后的配置写入磁盘。
func WriteAppConfig(filePath string, cfg *AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("app config is nil")
	}
	cfgCopy := *cfg
	cfgCopy.AI.APIKey = ""
	data, err := yaml.Marshal(&cfgCopy)
	if err != nil {
		return fmt.Errorf("failed to marshal app config YAML: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write app config file: %w", err)
	}
	return nil
}

// Validate 检查配置是否满足运行时要求。
func (c *AppConfiguration) Validate() error {
	return c.ValidateRuntime()
}

// ValidateBase 检查不包含密钥的基础配置是否合法。
func (c *AppConfiguration) ValidateBase() error {
	if c == nil {
		return fmt.Errorf("app config is nil")
	}
	if strings.TrimSpace(c.AI.Provider) == "" {
		return fmt.Errorf("invalid config: ai.provider is required")
	}
	if strings.TrimSpace(c.AI.Model) == "" {
		return fmt.Errorf("invalid config: ai.model is required")
	}
	if strings.TrimSpace(c.Models.Chat.DefaultModel) == "" {
		return fmt.Errorf("invalid config: models.chat.default_model is required")
	}
	if len(c.Models.Chat.Models) == 0 {
		return fmt.Errorf("invalid config: models.chat.models must not be empty")
	}
	for i, model := range c.Models.Chat.Models {
		if strings.TrimSpace(model.Name) == "" {
			return fmt.Errorf("invalid config: models.chat.models[%d].name is required", i)
		}
		if strings.TrimSpace(model.URL) == "" {
			return fmt.Errorf("invalid config: models.chat.models[%d].url is required", i)
		}
	}
	if c.Memory.TopK <= 0 {
		return fmt.Errorf("invalid config: memory.top_k must be greater than 0")
	}
	if c.Memory.MinMatchScore < 0 {
		return fmt.Errorf("invalid config: memory.min_match_score must not be negative")
	}
	if c.Memory.MaxPromptChars <= 0 {
		return fmt.Errorf("invalid config: memory.max_prompt_chars must be greater than 0")
	}
	if c.Memory.MaxItems <= 0 {
		return fmt.Errorf("invalid config: memory.max_items must be greater than 0")
	}
	if strings.TrimSpace(c.Memory.StoragePath) == "" {
		return fmt.Errorf("invalid config: memory.storage_path is required")
	}
	if c.History.ShortTermTurns <= 0 {
		return fmt.Errorf("invalid config: history.short_term_turns must be greater than 0")
	}
	return nil
}

// ValidateRuntime 检查配置字段和运行时必需的环境变量。
func (c *AppConfiguration) ValidateRuntime() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if RuntimeAPIKey() == "" {
		return fmt.Errorf("invalid runtime: %s environment variable is required", APIKeyEnvVar)
	}
	return nil
}

// RuntimeAPIKey 返回环境变量中的 API Key，并去掉首尾空白。
func RuntimeAPIKey() string {
	return strings.TrimSpace(os.Getenv(APIKeyEnvVar))
}

// GetChatModelURL 从全局配置中查找聊天模型对应的 URL。
func GetChatModelURL(modelName string) (string, bool) {
	if GlobalAppConfig == nil {
		return "", false
	}
	return GetChatModelURLFromConfig(GlobalAppConfig, modelName)
}

// GetChatModelURLFromConfig 从指定配置中查找聊天模型对应的 URL。
func GetChatModelURLFromConfig(cfg *AppConfiguration, modelName string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	for _, model := range cfg.Models.Chat.Models {
		if model.Name == modelName {
			return model.URL, true
		}
	}
	return "", false
}

// GetDefaultChatModel 返回全局配置中的默认聊天模型。
func GetDefaultChatModel() string {
	if GlobalAppConfig == nil {
		return ""
	}
	return strings.TrimSpace(GlobalAppConfig.Models.Chat.DefaultModel)
}
