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
	DefaultWorkdir                 = "."
	DefaultToolTimeoutSec          = 20
	DefaultGenerateStartTimeoutSec = 90
)

type Config struct {
	Providers               []ProviderConfig `yaml:"-"`
	SelectedProvider        string           `yaml:"selected_provider"`
	CurrentModel            string           `yaml:"current_model"`
	Workdir                 string           `yaml:"-"`
	Shell                   string           `yaml:"shell"`
	ToolTimeoutSec          int              `yaml:"tool_timeout_sec,omitempty"`
	GenerateStartTimeoutSec int              `yaml:"generate_start_timeout_sec,omitempty"`
	Runtime                 RuntimeConfig    `yaml:"runtime,omitempty"`
	Context                 ContextConfig    `yaml:"context,omitempty"`
	Tools                   ToolsConfig      `yaml:"tools,omitempty"`
	Memo                    MemoConfig       `yaml:"memo,omitempty"`
	Gateway                 GatewayConfig    `yaml:"gateway,omitempty"`
}

// StaticDefaults 返回 config 层负责的静态默认值骨架，不包含 provider 装配和选择状态修复。
func StaticDefaults() *Config {
	return &Config{
		Workdir:                 DefaultWorkdir,
		Shell:                   defaultShell(),
		ToolTimeoutSec:          DefaultToolTimeoutSec,
		GenerateStartTimeoutSec: DefaultGenerateStartTimeoutSec,
		Runtime:                 defaultRuntimeConfig(),
		Context:                 defaultContextConfig(),
		Tools: ToolsConfig{
			WebFetch: defaultWebFetchConfig(),
			MCP:      defaultMCPConfig(),
		},
		Memo:    defaultMemoConfig(),
		Gateway: defaultGatewayConfig(),
	}
}

func (c *Config) Clone() Config {
	if c == nil {
		return *StaticDefaults()
	}

	clone := *c
	clone.Providers = cloneProviders(c.Providers)
	clone.Runtime = c.Runtime.Clone()
	clone.Context = c.Context.Clone()
	clone.Tools = c.Tools.Clone()
	clone.Memo = c.Memo.Clone()
	clone.Gateway = c.Gateway.Clone()
	return clone
}

// applyStaticDefaults 仅补齐静态配置默认值
func (c *Config) applyStaticDefaults(defaults Config) {
	if c == nil {
		return
	}

	if strings.TrimSpace(c.Workdir) == "" {
		c.Workdir = defaults.Workdir
	}
	if strings.TrimSpace(c.Shell) == "" {
		c.Shell = defaults.Shell
	}
	if c.ToolTimeoutSec <= 0 {
		c.ToolTimeoutSec = defaults.ToolTimeoutSec
	}
	if c.GenerateStartTimeoutSec <= 0 {
		c.GenerateStartTimeoutSec = defaults.GenerateStartTimeoutSec
	}
	c.Runtime.ApplyDefaults(defaults.Runtime)
	c.Context.ApplyDefaults(defaults.Context)
	c.Tools.ApplyDefaults(defaults.Tools)
	c.Memo.ApplyDefaults(defaults.Memo)
	c.Gateway.ApplyDefaults(defaults.Gateway)

	c.Workdir = normalizeWorkdir(c.Workdir)
}

// ValidateSnapshot 校验配置快照本身是否结构完整
func (c *Config) ValidateSnapshot() error {
	if c == nil {
		return errors.New("config: config is nil")
	}
	if len(c.Providers) == 0 {
		return errors.New("config: providers is empty")
	}
	if err := validateOptionalGenerateDurationSeconds("generate_start_timeout_sec", c.GenerateStartTimeoutSec); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	seen := make(map[string]struct{}, len(c.Providers))
	seenEndpoints := make(map[string]string, len(c.Providers))
	for i, provider := range c.Providers {
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("config: provider[%d]: %w", i, err)
		}

		key := normalizeProviderName(provider.Name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("config: duplicate provider name %q", provider.Name)
		}
		seen[key] = struct{}{}

		identity, err := provider.Identity()
		if err != nil {
			return fmt.Errorf("config: provider[%d]: %w", i, err)
		}
		if existingName, exists := seenEndpoints[identity.Key()]; exists {
			return fmt.Errorf(
				"config: duplicate provider endpoint %q for providers %q and %q",
				identity.String(),
				existingName,
				provider.Name,
			)
		}
		seenEndpoints[identity.Key()] = provider.Name
	}

	if strings.TrimSpace(c.Workdir) == "" {
		return errors.New("config: workdir is empty")
	}
	if !filepath.IsAbs(c.Workdir) {
		return fmt.Errorf("config: workdir must be absolute, got %q", c.Workdir)
	}
	if err := c.Tools.Validate(); err != nil {
		return fmt.Errorf("config: tools: %w", err)
	}
	if err := c.Runtime.Validate(); err != nil {
		return fmt.Errorf("config: runtime: %w", err)
	}
	if err := c.Context.Validate(); err != nil {
		return fmt.Errorf("config: context: %w", err)
	}
	if err := c.Memo.Validate(); err != nil {
		return fmt.Errorf("config: memo: %w", err)
	}
	if err := c.Gateway.Validate(); err != nil {
		return fmt.Errorf("config: gateway: %w", err)
	}

	return nil
}

func (c *Config) ProviderByName(name string) (ProviderConfig, error) {
	if c == nil {
		return ProviderConfig{}, errors.New("config: config is nil")
	}

	target := normalizeProviderName(name)
	for _, provider := range c.Providers {
		if normalizeProviderName(provider.Name) == target {
			return provider, nil
		}
	}

	return ProviderConfig{}, fmt.Errorf("config: provider %q not found", name)
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
