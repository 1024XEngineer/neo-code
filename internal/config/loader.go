package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const (
	dirName    = ".neocode"
	configName = "config.yaml"
	envName    = ".env"
)

type Loader struct {
	baseDir string
}

func NewLoader(baseDir string) *Loader {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = defaultBaseDir()
	}
	return &Loader{baseDir: baseDir}
}

func (l *Loader) BaseDir() string {
	return l.baseDir
}

func (l *Loader) ConfigPath() string {
	return filepath.Join(l.baseDir, configName)
}

func (l *Loader) EnvPath() string {
	return filepath.Join(l.baseDir, envName)
}

func (l *Loader) Load(ctx context.Context) (*Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	l.LoadEnvironment()

	if err := os.MkdirAll(l.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("config: create config dir: %w", err)
	}
	if _, err := os.Stat(l.ConfigPath()); os.IsNotExist(err) {
		if err := l.Save(ctx, Default()); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(l.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("config: read config file: %w", err)
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return nil, fmt.Errorf("config: parse config file: %w", err)
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (l *Loader) Save(ctx context.Context, cfg *Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(l.baseDir, 0o755); err != nil {
		return fmt.Errorf("config: create config dir: %w", err)
	}

	snapshot := cfg.Clone()
	snapshot.ApplyDefaults()
	if err := snapshot.Validate(); err != nil {
		return err
	}

	data, err := yaml.Marshal(&snapshot)
	if err != nil {
		return fmt.Errorf("config: marshal config: %w", err)
	}

	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}

	if err := os.WriteFile(l.ConfigPath(), data, 0o644); err != nil {
		return fmt.Errorf("config: write config file: %w", err)
	}

	return nil
}

func (l *Loader) LoadEnvironment() {
	_ = godotenv.Load()
	_ = godotenv.Load(l.EnvPath())
}

func (l *Loader) OverloadManagedEnvironment() error {
	return godotenv.Overload(l.EnvPath())
}

func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return dirName
	}
	return filepath.Join(home, dirName)
}

func parseConfig(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Default(), nil
	}

	cfg, currentErr := parseCurrentConfig(data)
	if currentErr == nil {
		return cfg, nil
	}

	legacy, legacyErr := parseLegacyConfig(data)
	if legacyErr == nil {
		return legacy, nil
	}

	return nil, currentErr
}

type aliasConfig struct {
	MaxLoop       int    `yaml:"max_loop"`
	WorkspaceRoot string `yaml:"workspace_root"`
}

type legacyConfig struct {
	SelectedProvider string                          `yaml:"selected_provider"`
	CurrentModel     string                          `yaml:"current_model"`
	MaxLoop          int                             `yaml:"max_loop"`
	ToolTimeoutSec   int                             `yaml:"tool_timeout_sec"`
	WorkspaceRoot    string                          `yaml:"workspace_root"`
	Shell            string                          `yaml:"shell"`
	Providers        map[string]legacyProviderConfig `yaml:"providers"`
}

type legacyProviderConfig struct {
	Type      string   `yaml:"type"`
	BaseURL   string   `yaml:"base_url"`
	APIKeyEnv string   `yaml:"api_key_env"`
	Models    []string `yaml:"models"`
}

func parseCurrentConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	var aliases aliasConfig
	if err := yaml.Unmarshal(data, &aliases); err == nil {
		if cfg.MaxLoops == 0 && aliases.MaxLoop > 0 {
			cfg.MaxLoops = aliases.MaxLoop
		}
		if strings.TrimSpace(cfg.Workdir) == "" && strings.TrimSpace(aliases.WorkspaceRoot) != "" {
			cfg.Workdir = aliases.WorkspaceRoot
		}
	}

	return cfg, nil
}

func parseLegacyConfig(data []byte) (*Config, error) {
	var legacy legacyConfig
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	return convertLegacyConfig(legacy), nil
}

func convertLegacyConfig(in legacyConfig) *Config {
	out := &Config{
		SelectedProvider: strings.TrimSpace(in.SelectedProvider),
		CurrentModel:     strings.TrimSpace(in.CurrentModel),
		Workdir:          strings.TrimSpace(in.WorkspaceRoot),
		Shell:            strings.TrimSpace(in.Shell),
		MaxLoops:         in.MaxLoop,
		ToolTimeoutSec:   in.ToolTimeoutSec,
	}

	for name, provider := range in.Providers {
		model := firstNonEmpty(provider.Models...)
		if strings.EqualFold(name, in.SelectedProvider) && strings.TrimSpace(in.CurrentModel) != "" {
			model = strings.TrimSpace(in.CurrentModel)
		}

		out.Providers = append(out.Providers, ProviderConfig{
			Name:      strings.TrimSpace(name),
			Type:      strings.TrimSpace(provider.Type),
			BaseURL:   strings.TrimSpace(provider.BaseURL),
			Model:     strings.TrimSpace(model),
			APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
		})
	}

	return out
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}
