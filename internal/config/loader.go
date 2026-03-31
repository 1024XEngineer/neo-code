package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	dirName    = ".neocode"
	configName = "config.yaml"
)

type Loader struct {
	baseDir  string
	defaults Config
}

type persistedConfig struct {
	SelectedProvider string      `yaml:"selected_provider"`
	CurrentModel     string      `yaml:"current_model"`
	Workdir          string      `yaml:"workdir"`
	Shell            string      `yaml:"shell"`
	MaxLoops         int         `yaml:"max_loops,omitempty"`
	ToolTimeoutSec   int         `yaml:"tool_timeout_sec,omitempty"`
	Tools            ToolsConfig `yaml:"tools,omitempty"`
}

func NewLoader(baseDir string, defaults *Config) *Loader {
	if defaults == nil {
		panic("config: loader defaults are nil")
	}

	if strings.TrimSpace(baseDir) == "" {
		baseDir = defaultBaseDir()
	}

	snapshot := defaults.Clone()
	snapshot.ApplyDefaultsFrom(*Default())
	if err := snapshot.Validate(); err != nil {
		panic(fmt.Sprintf("config: invalid loader defaults: %v", err))
	}

	return &Loader{
		baseDir:  baseDir,
		defaults: snapshot,
	}
}

func (l *Loader) BaseDir() string {
	return l.baseDir
}

func (l *Loader) ConfigPath() string {
	return filepath.Join(l.baseDir, configName)
}

func (l *Loader) DefaultConfig() Config {
	return l.defaults.Clone()
}

func (l *Loader) Load(ctx context.Context) (*Config, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(l.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("config: create config dir: %w", err)
	}
	if _, err := os.Stat(l.ConfigPath()); os.IsNotExist(err) {
		defaultCfg := l.DefaultConfig()
		if err := l.Save(ctx, &defaultCfg); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(l.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("config: read config file: %w", err)
	}

	cfg, err := parseConfig(data, l.defaults)
	if err != nil {
		return nil, fmt.Errorf("config: parse config file: %w", err)
	}
	cfg.ApplyDefaultsFrom(l.defaults)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if requiresConfigRewrite(data) {
		if err := l.Save(ctx, cfg); err != nil {
			return nil, err
		}
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
	snapshot.ApplyDefaultsFrom(l.defaults)
	if err := snapshot.Validate(); err != nil {
		return err
	}

	file := persistedConfig{
		SelectedProvider: snapshot.SelectedProvider,
		CurrentModel:     snapshot.CurrentModel,
		Workdir:          snapshot.Workdir,
		Shell:            snapshot.Shell,
		MaxLoops:         snapshot.MaxLoops,
		ToolTimeoutSec:   snapshot.ToolTimeoutSec,
		Tools:            snapshot.Tools,
	}

	data, err := yaml.Marshal(&file)
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

func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return dirName
	}
	return filepath.Join(home, dirName)
}

func parseConfig(data []byte, defaults Config) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Config{}, nil
	}

	return parseCurrentConfig(data, defaults)
}

type aliasConfig struct {
	MaxLoop       int    `yaml:"max_loop"`
	WorkspaceRoot string `yaml:"workspace_root"`
}

func parseCurrentConfig(data []byte, _ Config) (*Config, error) {
	var file persistedConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}

	var aliases aliasConfig
	if err := yaml.Unmarshal(data, &aliases); err == nil {
		if file.MaxLoops == 0 && aliases.MaxLoop > 0 {
			file.MaxLoops = aliases.MaxLoop
		}
		if strings.TrimSpace(file.Workdir) == "" && strings.TrimSpace(aliases.WorkspaceRoot) != "" {
			file.Workdir = aliases.WorkspaceRoot
		}
	}

	cfg := &Config{
		SelectedProvider: strings.TrimSpace(file.SelectedProvider),
		CurrentModel:     strings.TrimSpace(file.CurrentModel),
		Workdir:          strings.TrimSpace(file.Workdir),
		Shell:            strings.TrimSpace(file.Shell),
		MaxLoops:         file.MaxLoops,
		ToolTimeoutSec:   file.ToolTimeoutSec,
		Tools:            file.Tools,
	}

	return cfg, nil
}

func requiresConfigRewrite(data []byte) bool {
	text := strings.TrimSpace(string(data))
	switch {
	case text == "":
		return false
	case strings.Contains(text, "\nproviders:") || strings.HasPrefix(text, "providers:"):
		return true
	case strings.Contains(text, "provider_overrides:"):
		return true
	case strings.Contains(text, "workspace_root:") || strings.Contains(text, "max_loop:"):
		return true
	default:
		return false
	}
}
