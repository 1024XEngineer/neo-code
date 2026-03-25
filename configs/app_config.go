package configs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultAPIKeyEnvVar = "AI_API_KEY"

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
		TopK                   int      `yaml:"top_k"`
		MinMatchScore          float64  `yaml:"min_match_score"`
		MaxPromptChars         int      `yaml:"max_prompt_chars"`
		MaxItems               int      `yaml:"max_items"`
		StoragePath            string   `yaml:"storage_path"`
		PersistTypes           []string `yaml:"persist_types"`
		ProjectFiles           []string `yaml:"project_files"`
		ProjectPromptChars     int      `yaml:"project_prompt_chars"`
		Extractor              string   `yaml:"extractor"`
		ExtractorModel         string   `yaml:"extractor_model"`
		ExtractorTimeoutSecond int      `yaml:"extractor_timeout_seconds"`
	} `yaml:"memory"`

	History struct {
		ShortTermTurns           int    `yaml:"short_term_turns"`
		MaxToolContextMessages   int    `yaml:"max_tool_context_messages"`
		MaxToolContextOutputSize int    `yaml:"max_tool_context_output_size"`
		PersistSessionState      bool   `yaml:"persist_session_state"`
		WorkspaceStateDir        string `yaml:"workspace_state_dir"`
		ResumeLastSession        bool   `yaml:"resume_last_session"`
	} `yaml:"history"`

	Persona struct {
		FilePath string `yaml:"file_path"`
	} `yaml:"persona"`
}

var GlobalAppConfig *AppConfiguration

// DefaultAppConfig returns the built-in application defaults.
func DefaultAppConfig() *AppConfiguration {
	cfg := &AppConfiguration{}
	cfg.App.Name = "NeoCode"
	cfg.App.Version = "1.0.0"
	cfg.AI.Provider = "openll"
	cfg.AI.APIKey = DefaultAPIKeyEnvVar
	cfg.AI.Model = "gpt-5.4"
	cfg.Memory.TopK = 5
	cfg.Memory.MinMatchScore = 2.2
	cfg.Memory.MaxPromptChars = 1800
	cfg.Memory.MaxItems = 1000
	cfg.Memory.StoragePath = "./data/memory_rules.json"
	cfg.Memory.PersistTypes = []string{"user_preference", "project_rule", "code_fact", "fix_recipe"}
	cfg.Memory.ProjectFiles = []string{"AGENTS.md", "CLAUDE.md", ".neocode/memory.md", "NEOCODE.md"}
	cfg.Memory.ProjectPromptChars = 2400
	cfg.Memory.Extractor = "rule"
	cfg.Memory.ExtractorModel = ""
	cfg.Memory.ExtractorTimeoutSecond = 20
	cfg.History.ShortTermTurns = 6
	cfg.History.MaxToolContextMessages = 3
	cfg.History.MaxToolContextOutputSize = 4000
	cfg.History.PersistSessionState = true
	cfg.History.WorkspaceStateDir = "./data/workspaces"
	cfg.History.ResumeLastSession = true
	cfg.Persona.FilePath = DefaultPersonaFilePath
	return cfg
}

// LoadAppConfig loads runtime config and stores it globally.
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

// LoadBootstrapConfig loads config without requiring runtime secrets.
func LoadBootstrapConfig(filePath string) (*AppConfiguration, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := DefaultAppConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml config: %w", err)
	}
	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EnsureConfigFile loads an existing config or writes defaults when missing.
func EnsureConfigFile(filePath string) (*AppConfiguration, bool, error) {
	if _, err := os.Stat(filePath); err == nil {
		cfg, loadErr := LoadBootstrapConfig(filePath)
		return cfg, false, loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("stat config file: %w", err)
	}

	cfg := DefaultAppConfig()
	if err := WriteAppConfig(filePath, cfg); err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

// WriteAppConfig persists application config to disk.
func WriteAppConfig(filePath string, cfg *AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("app configuration cannot be nil")
	}
	cfgCopy := *cfg
	cfgCopy.AI.APIKey = strings.TrimSpace(cfgCopy.AI.APIKey)
	data, err := yaml.Marshal(&cfgCopy)
	if err != nil {
		return fmt.Errorf("marshal yaml config: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("write yaml config: %w", err)
	}
	return nil
}

// Validate checks runtime config requirements.
func (c *AppConfiguration) Validate() error {
	return c.ValidateRuntime()
}

// ValidateBase checks config values that do not require secrets.
func (c *AppConfiguration) ValidateBase() error {
	if c == nil {
		return fmt.Errorf("app configuration cannot be nil")
	}

	providerName := normalizeProviderName(c.AI.Provider)
	if providerName == "" {
		return fmt.Errorf("invalid config: ai.provider is required")
	}
	if !isSupportedProvider(providerName) {
		return fmt.Errorf("invalid config: unsupported ai.provider %q", strings.TrimSpace(c.AI.Provider))
	}
	c.AI.Provider = providerName

	if strings.TrimSpace(c.AI.Model) == "" {
		return fmt.Errorf("invalid config: ai.model is required")
	}
	if c.Memory.TopK <= 0 {
		return fmt.Errorf("invalid config: memory.top_k must be greater than 0")
	}
	if c.Memory.MinMatchScore < 0 {
		return fmt.Errorf("invalid config: memory.min_match_score cannot be negative")
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
	if c.Memory.ProjectPromptChars <= 0 {
		return fmt.Errorf("invalid config: memory.project_prompt_chars must be greater than 0")
	}
	c.Memory.Extractor = normalizeMemoryExtractorMode(c.Memory.Extractor)
	if c.Memory.Extractor == "" {
		return fmt.Errorf("invalid config: memory.extractor must be one of rule, llm, auto")
	}
	if c.Memory.ExtractorTimeoutSecond <= 0 {
		return fmt.Errorf("invalid config: memory.extractor_timeout_seconds must be greater than 0")
	}
	if c.History.ShortTermTurns <= 0 {
		return fmt.Errorf("invalid config: history.short_term_turns must be greater than 0")
	}
	if c.History.MaxToolContextMessages < 0 {
		return fmt.Errorf("invalid config: history.max_tool_context_messages cannot be negative")
	}
	if c.History.MaxToolContextOutputSize <= 0 {
		return fmt.Errorf("invalid config: history.max_tool_context_output_size must be greater than 0")
	}
	if c.History.PersistSessionState && strings.TrimSpace(c.History.WorkspaceStateDir) == "" {
		return fmt.Errorf("invalid config: history.workspace_state_dir cannot be empty")
	}
	return nil
}

// ValidateRuntime checks config and required environment variables.
func (c *AppConfiguration) ValidateRuntime() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	envVarName := c.APIKeyEnvVarName()
	if c.RuntimeAPIKey() == "" {
		return fmt.Errorf("invalid runtime config: missing %s environment variable", envVarName)
	}
	return nil
}

// APIKeyEnvVarName returns the configured environment variable name for the API key.
func (c *AppConfiguration) APIKeyEnvVarName() string {
	if c == nil {
		return DefaultAPIKeyEnvVar
	}
	if name := strings.TrimSpace(c.AI.APIKey); name != "" {
		return name
	}
	return DefaultAPIKeyEnvVar
}

// RuntimeAPIKey returns the actual API key from environment variables.
func (c *AppConfiguration) RuntimeAPIKey() string {
	return strings.TrimSpace(os.Getenv(c.APIKeyEnvVarName()))
}

// RuntimeAPIKeyEnvVarName returns the active API key environment variable name.
func RuntimeAPIKeyEnvVarName() string {
	if GlobalAppConfig != nil {
		return GlobalAppConfig.APIKeyEnvVarName()
	}
	return DefaultAPIKeyEnvVar
}

// RuntimeAPIKey returns the active API key from the global config.
func RuntimeAPIKey() string {
	if GlobalAppConfig != nil {
		return GlobalAppConfig.RuntimeAPIKey()
	}
	return strings.TrimSpace(os.Getenv(DefaultAPIKeyEnvVar))
}

func normalizeProviderName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.EqualFold(trimmed, "modelscope"):
		return "modelscope"
	case strings.EqualFold(trimmed, "deepseek"):
		return "deepseek"
	case strings.EqualFold(trimmed, "openll"):
		return "openll"
	case strings.EqualFold(trimmed, "siliconflow"):
		return "siliconflow"
	case strings.EqualFold(trimmed, "openai"):
		return "openai"
	case strings.EqualFold(trimmed, "doubao"):
		return "doubao"
	default:
		return trimmed
	}
}

func isSupportedProvider(name string) bool {
	switch normalizeProviderName(name) {
	case "modelscope", "deepseek", "openll", "siliconflow", "openai", "doubao":
		return true
	default:
		return false
	}
}

func normalizeMemoryExtractorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "rule":
		return "rule"
	case "llm":
		return "llm"
	case "auto":
		return "auto"
	default:
		return ""
	}
}
