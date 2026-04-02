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
	SelectedProvider string                 `yaml:"selected_provider"`
	CurrentModel     string                 `yaml:"current_model"`
	Workdir          string                 `yaml:"workdir"`
	Shell            string                 `yaml:"shell"`
	MaxLoops         int                    `yaml:"max_loops,omitempty"`
	ToolTimeoutSec   int                    `yaml:"tool_timeout_sec,omitempty"`
	Context          persistedContextConfig `yaml:"context,omitempty"`
	Tools            ToolsConfig            `yaml:"tools,omitempty"`
}

type persistedContextConfig struct {
	Compact persistedCompactConfig `yaml:"compact,omitempty"`
}

type persistedCompactConfig struct {
	MicroEnabled                  *bool  `yaml:"micro_enabled,omitempty"`
	ToolResultKeepRecent          int    `yaml:"tool_result_keep_recent,omitempty"`
	ToolResultPlaceholderMinChars int    `yaml:"tool_result_placeholder_min_chars,omitempty"`
	ManualStrategy                string `yaml:"manual_strategy,omitempty"`
	ManualKeepRecentSpans         int    `yaml:"manual_keep_recent_spans,omitempty"`
	MaxSummaryChars               int    `yaml:"max_summary_chars,omitempty"`
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

	cfg, err := parseConfigWithContextDefaults(data, l.defaults.Context)
	if err != nil {
		return nil, fmt.Errorf("config: parse config file: %w", err)
	}
	cfg.ApplyDefaultsFrom(l.defaults)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	needsRewrite, err := persistedConfigDiffers(data, *cfg)
	if err != nil {
		return nil, err
	}
	if needsRewrite {
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

	data, err := marshalPersistedConfig(snapshot)
	if err != nil {
		return err
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

func parseConfig(data []byte) (*Config, error) {
	return parseConfigWithContextDefaults(data, Default().Context)
}

func parseConfigWithContextDefaults(data []byte, contextDefaults ContextConfig) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Config{}, nil
	}

	return parseCurrentConfig(data, contextDefaults)
}

func parseCurrentConfig(data []byte, contextDefaults ContextConfig) (*Config, error) {
	var file persistedConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}

	cfg := &Config{
		SelectedProvider: strings.TrimSpace(file.SelectedProvider),
		CurrentModel:     strings.TrimSpace(file.CurrentModel),
		Workdir:          strings.TrimSpace(file.Workdir),
		Shell:            strings.TrimSpace(file.Shell),
		MaxLoops:         file.MaxLoops,
		ToolTimeoutSec:   file.ToolTimeoutSec,
		Context:          fromPersistedContextConfig(file.Context, contextDefaults),
		Tools:            file.Tools,
	}

	return cfg, nil
}

func marshalPersistedConfig(snapshot Config) ([]byte, error) {
	file := persistedConfig{
		SelectedProvider: snapshot.SelectedProvider,
		CurrentModel:     snapshot.CurrentModel,
		Workdir:          snapshot.Workdir,
		Shell:            snapshot.Shell,
		MaxLoops:         snapshot.MaxLoops,
		ToolTimeoutSec:   snapshot.ToolTimeoutSec,
		Context:          newPersistedContextConfig(snapshot.Context),
		Tools:            snapshot.Tools,
	}

	data, err := yaml.Marshal(&file)
	if err != nil {
		return nil, fmt.Errorf("config: marshal config: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return data, nil
}

func newPersistedContextConfig(cfg ContextConfig) persistedContextConfig {
	microEnabled := cfg.Compact.MicroEnabled
	return persistedContextConfig{
		Compact: persistedCompactConfig{
			MicroEnabled:                  &microEnabled,
			ToolResultKeepRecent:          cfg.Compact.ToolResultKeepRecent,
			ToolResultPlaceholderMinChars: cfg.Compact.ToolResultPlaceholderMinChars,
			ManualStrategy:                cfg.Compact.ManualStrategy,
			ManualKeepRecentSpans:         cfg.Compact.ManualKeepRecentSpans,
			MaxSummaryChars:               cfg.Compact.MaxSummaryChars,
		},
	}
}

func fromPersistedContextConfig(file persistedContextConfig, defaults ContextConfig) ContextConfig {
	out := ContextConfig{
		Compact: CompactConfig{
			ToolResultKeepRecent:          file.Compact.ToolResultKeepRecent,
			ToolResultPlaceholderMinChars: file.Compact.ToolResultPlaceholderMinChars,
			ManualStrategy:                strings.TrimSpace(file.Compact.ManualStrategy),
			ManualKeepRecentSpans:         file.Compact.ManualKeepRecentSpans,
			MaxSummaryChars:               file.Compact.MaxSummaryChars,
		},
	}
	if file.Compact.MicroEnabled != nil {
		out.Compact.MicroEnabled = *file.Compact.MicroEnabled
	} else {
		out.Compact.MicroEnabled = defaults.Compact.MicroEnabled
	}
	return out
}

func persistedConfigDiffers(data []byte, cfg Config) (bool, error) {
	canonical, err := marshalPersistedConfig(cfg)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(canonical)), nil
}
