package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"

	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
	DefaultGeminiBaseURL    = "https://generativelanguage.googleapis.com"

	DefaultOpenAIModel    = "gpt-4.1"
	DefaultAnthropicModel = "claude-3-7-sonnet-latest"
	DefaultGeminiModel    = "gemini-2.5-pro"

	DefaultOpenAIAPIKeyEnv    = "OPENAI_API_KEY"
	DefaultAnthropicAPIKeyEnv = "ANTHROPIC_API_KEY"
	DefaultGeminiAPIKeyEnv    = "GEMINI_API_KEY"

	DefaultSelectedProvider = ProviderOpenAI
	DefaultWorkdir          = "."
	DefaultMaxLoops         = 8
	DefaultToolTimeoutSec   = 20
)

type Config struct {
	Providers        []ProviderConfig `yaml:"providers"`
	SelectedProvider string           `yaml:"selected_provider"`
	CurrentModel     string           `yaml:"current_model"`
	Workdir          string           `yaml:"workdir"`
	Shell            string           `yaml:"shell"`
	MaxLoops         int              `yaml:"max_loops,omitempty"`
	ToolTimeoutSec   int              `yaml:"tool_timeout_sec,omitempty"`
}

type ProviderConfig struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	APIKey string `yaml:"-"`
}

type ModelOption struct {
	Name        string
	Description string
}

func Default() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{
				Name:      ProviderOpenAI,
				Type:      ProviderOpenAI,
				BaseURL:   DefaultOpenAIBaseURL,
				Model:     DefaultOpenAIModel,
				APIKeyEnv: DefaultOpenAIAPIKeyEnv,
			},
			{
				Name:      ProviderAnthropic,
				Type:      ProviderAnthropic,
				BaseURL:   DefaultAnthropicBaseURL,
				Model:     DefaultAnthropicModel,
				APIKeyEnv: DefaultAnthropicAPIKeyEnv,
			},
			{
				Name:      ProviderGemini,
				Type:      ProviderGemini,
				BaseURL:   DefaultGeminiBaseURL,
				Model:     DefaultGeminiModel,
				APIKeyEnv: DefaultGeminiAPIKeyEnv,
			},
		},
		SelectedProvider: DefaultSelectedProvider,
		CurrentModel:     DefaultOpenAIModel,
		Workdir:          DefaultWorkdir,
		Shell:            defaultShell(),
		MaxLoops:         DefaultMaxLoops,
		ToolTimeoutSec:   DefaultToolTimeoutSec,
	}
}

func BuiltinModelCatalog() []ModelOption {
	return append([]ModelOption(nil),
		ModelOption{Name: DefaultOpenAIModel, Description: "Stable OpenAI default model"},
		ModelOption{Name: "gpt-4o", Description: "Fast general-purpose OpenAI model"},
		ModelOption{Name: "gpt-5.4", Description: "Frontier reasoning and coding model"},
		ModelOption{Name: "gpt-5.3-codex", Description: "Code-focused GPT-5.3 variant"},
		ModelOption{Name: DefaultAnthropicModel, Description: "Balanced Anthropic coding model"},
		ModelOption{Name: DefaultGeminiModel, Description: "Default Gemini reasoning model"},
	)
}

func (c *Config) Clone() Config {
	if c == nil {
		return *Default()
	}

	clone := *c
	clone.Providers = append([]ProviderConfig(nil), c.Providers...)
	return clone
}

func (c *Config) ApplyDefaults() {
	if c == nil {
		return
	}

	def := Default()

	if len(c.Providers) == 0 {
		c.Providers = append([]ProviderConfig(nil), def.Providers...)
	} else {
		c.Providers = applyProviderDefaults(c.Providers, def.Providers)
	}

	if strings.TrimSpace(c.SelectedProvider) == "" {
		c.SelectedProvider = def.SelectedProvider
	}
	if strings.TrimSpace(c.CurrentModel) == "" {
		if selected, err := c.SelectedProviderConfig(); err == nil {
			c.CurrentModel = selected.Model
		}
	}
	if strings.TrimSpace(c.Workdir) == "" {
		c.Workdir = def.Workdir
	}
	if strings.TrimSpace(c.Shell) == "" {
		c.Shell = def.Shell
	}
	if c.MaxLoops <= 0 {
		c.MaxLoops = def.MaxLoops
	}
	if c.ToolTimeoutSec <= 0 {
		c.ToolTimeoutSec = def.ToolTimeoutSec
	}

	c.Workdir = normalizeWorkdir(c.Workdir)
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config: config is nil")
	}
	if len(c.Providers) == 0 {
		return errors.New("config: providers is empty")
	}

	seen := make(map[string]struct{}, len(c.Providers))
	for i, provider := range c.Providers {
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("config: provider[%d]: %w", i, err)
		}

		key := strings.ToLower(strings.TrimSpace(provider.Name))
		if _, exists := seen[key]; exists {
			return fmt.Errorf("config: duplicate provider name %q", provider.Name)
		}
		seen[key] = struct{}{}
	}

	if strings.TrimSpace(c.SelectedProvider) == "" {
		return errors.New("config: selected_provider is empty")
	}
	selected, err := c.SelectedProviderConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.CurrentModel) == "" {
		return errors.New("config: current_model is empty")
	}
	if strings.TrimSpace(c.Workdir) == "" {
		return errors.New("config: workdir is empty")
	}
	if !filepath.IsAbs(c.Workdir) {
		return fmt.Errorf("config: workdir must be absolute, got %q", c.Workdir)
	}
	if strings.TrimSpace(selected.Model) == "" {
		return fmt.Errorf("config: selected provider %q has empty model", selected.Name)
	}

	return nil
}

func (c *Config) SelectedProviderConfig() (ProviderConfig, error) {
	if c == nil {
		return ProviderConfig{}, errors.New("config: config is nil")
	}
	return c.ProviderByName(c.SelectedProvider)
}

func (c *Config) ProviderByName(name string) (ProviderConfig, error) {
	if c == nil {
		return ProviderConfig{}, errors.New("config: config is nil")
	}

	target := strings.ToLower(strings.TrimSpace(name))
	for _, provider := range c.Providers {
		if strings.ToLower(strings.TrimSpace(provider.Name)) == target {
			return provider, nil
		}
	}

	return ProviderConfig{}, fmt.Errorf("config: provider %q not found", name)
}

func (p ProviderConfig) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider name is empty")
	}
	if strings.TrimSpace(p.Type) == "" {
		return fmt.Errorf("provider %q type is empty", p.Name)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q base_url is empty", p.Name)
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("provider %q model is empty", p.Name)
	}
	if strings.TrimSpace(p.APIKeyEnv) == "" {
		return fmt.Errorf("provider %q api_key_env is empty", p.Name)
	}
	return nil
}

func (p ProviderConfig) ResolveAPIKey() (string, error) {
	envName := strings.TrimSpace(p.APIKeyEnv)
	if envName == "" {
		return "", fmt.Errorf("config: provider %q api_key_env is empty", p.Name)
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return "", fmt.Errorf("config: environment variable %s is empty", envName)
	}

	return value, nil
}

func (p ProviderConfig) Resolve() (ResolvedProviderConfig, error) {
	apiKey, err := p.ResolveAPIKey()
	if err != nil {
		return ResolvedProviderConfig{}, err
	}

	return ResolvedProviderConfig{
		ProviderConfig: p,
		APIKey:         apiKey,
	}, nil
}

func applyProviderDefaults(providers []ProviderConfig, defaults []ProviderConfig) []ProviderConfig {
	out := make([]ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		out = append(out, mergeProviderDefaults(provider, defaults))
	}
	return out
}

func mergeProviderDefaults(provider ProviderConfig, defaults []ProviderConfig) ProviderConfig {
	base, ok := matchDefaultProvider(provider, defaults)
	if !ok {
		return provider
	}

	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = base.Name
	}
	if strings.TrimSpace(provider.Type) == "" {
		provider.Type = base.Type
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		provider.BaseURL = base.BaseURL
	}
	if strings.TrimSpace(provider.Model) == "" {
		provider.Model = base.Model
	}
	if strings.TrimSpace(provider.APIKeyEnv) == "" {
		provider.APIKeyEnv = base.APIKeyEnv
	}

	return provider
}

func matchDefaultProvider(provider ProviderConfig, defaults []ProviderConfig) (ProviderConfig, bool) {
	name := strings.ToLower(strings.TrimSpace(provider.Name))
	kind := strings.ToLower(strings.TrimSpace(provider.Type))

	for _, candidate := range defaults {
		if name != "" && strings.ToLower(candidate.Name) == name {
			return candidate, true
		}
	}
	for _, candidate := range defaults {
		if kind != "" && strings.ToLower(candidate.Type) == kind {
			return candidate, true
		}
	}

	return ProviderConfig{}, false
}

func normalizeWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}

	if workdir == "." {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return workdir
	}

	if filepath.IsAbs(workdir) {
		return filepath.Clean(workdir)
	}

	if wd, err := os.Getwd(); err == nil {
		return filepath.Clean(filepath.Join(wd, workdir))
	}

	return filepath.Clean(workdir)
}

func defaultShell() string {
	if goruntime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}
