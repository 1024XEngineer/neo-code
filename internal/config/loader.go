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
	SelectedProvider string                 `yaml:"selected_provider,omitempty"`
	CurrentModel     string                 `yaml:"current_model,omitempty"`
	Shell            string                 `yaml:"shell"`
	MaxLoops         int                    `yaml:"max_loops,omitempty"`
	ToolTimeoutSec   int                    `yaml:"tool_timeout_sec,omitempty"`
	Context          persistedContextConfig `yaml:"context,omitempty"`
	Tools            ToolsConfig            `yaml:"tools,omitempty"`
}

type persistedContextConfig struct {
	Compact     persistedCompactConfig     `yaml:"compact,omitempty"`
	AutoCompact persistedAutoCompactConfig `yaml:"auto_compact,omitempty"`
}

type persistedCompactConfig struct {
	ManualStrategy           string `yaml:"manual_strategy,omitempty"`
	ManualKeepRecentMessages int    `yaml:"manual_keep_recent_messages,omitempty"`
	MaxSummaryChars          int    `yaml:"max_summary_chars,omitempty"`
	MicroCompactDisabled     bool   `yaml:"micro_compact_disabled,omitempty"`
}

type persistedAutoCompactConfig struct {
	Enabled             bool `yaml:"enabled"`
	InputTokenThreshold int  `yaml:"input_token_threshold,omitempty"`
}

func NewLoader(baseDir string, defaults *Config) *Loader {
	if defaults == nil {
		panic("config: loader defaults are nil")
	}

	if strings.TrimSpace(baseDir) == "" {
		baseDir = defaultBaseDir()
	}

	snapshot := defaults.Clone()
	if len(snapshot.Providers) == 0 {
		snapshot.Providers = cloneProviders(DefaultProviders())
	}
	snapshot.applyStaticDefaults(*StaticDefaults())
	if err := snapshot.ValidateSnapshot(); err != nil {
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
	customProviders, err := loadCustomProviders(l.baseDir)
	if err != nil {
		return nil, err
	}
	cfg.Providers, err = assembleProviders(l.defaults.Providers, customProviders)
	if err != nil {
		return nil, err
	}
	cfg.applyStaticDefaults(l.defaults)
	if err := cfg.ValidateSnapshot(); err != nil {
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
	if len(snapshot.Providers) == 0 {
		snapshot.Providers = cloneProviders(l.defaults.Providers)
	}
	snapshot.applyStaticDefaults(l.defaults)
	if err := snapshot.ValidateSnapshot(); err != nil {
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
	return parseConfigWithContextDefaults(data, StaticDefaults().Context)
}

// parseConfigWithContextDefaults 负责在解析配置时注入上下文压缩相关默认值。
func parseConfigWithContextDefaults(data []byte, contextDefaults ContextConfig) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Config{}, nil
	}

	return parseCurrentConfig(data, contextDefaults)
}

func parseCurrentConfig(data []byte, contextDefaults ContextConfig) (*Config, error) {
	var file persistedConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	cfg := &Config{
		SelectedProvider: strings.TrimSpace(file.SelectedProvider),
		CurrentModel:     strings.TrimSpace(file.CurrentModel),
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

// newPersistedContextConfig 将运行时上下文配置收敛为 YAML 持久化结构。
func newPersistedContextConfig(cfg ContextConfig) persistedContextConfig {
	return persistedContextConfig{
		Compact: persistedCompactConfig{
			ManualStrategy:           cfg.Compact.ManualStrategy,
			ManualKeepRecentMessages: cfg.Compact.ManualKeepRecentMessages,
			MaxSummaryChars:          cfg.Compact.MaxSummaryChars,
			MicroCompactDisabled:     cfg.Compact.MicroCompactDisabled,
		},
		AutoCompact: persistedAutoCompactConfig{
			Enabled:             cfg.AutoCompact.Enabled,
			InputTokenThreshold: cfg.AutoCompact.InputTokenThreshold,
		},
	}
}

// fromPersistedContextConfig 将持久化配置恢复为运行时上下文配置并补齐默认值。
func fromPersistedContextConfig(file persistedContextConfig, defaults ContextConfig) ContextConfig {
	out := ContextConfig{
		Compact: CompactConfig{
			ManualStrategy:           strings.TrimSpace(file.Compact.ManualStrategy),
			ManualKeepRecentMessages: file.Compact.ManualKeepRecentMessages,
			MaxSummaryChars:          file.Compact.MaxSummaryChars,
			MicroCompactDisabled:     file.Compact.MicroCompactDisabled,
		},
		AutoCompact: AutoCompactConfig{
			Enabled:             file.AutoCompact.Enabled,
			InputTokenThreshold: file.AutoCompact.InputTokenThreshold,
		},
	}
	out.Compact.ApplyDefaults(defaults.Compact)
	out.AutoCompact.ApplyDefaults(defaults.AutoCompact)
	return out
}

// assembleProviders 按来源组装运行时 provider 集合，并在发现重名时直接报错。
func assembleProviders(builtin []ProviderConfig, custom []ProviderConfig) ([]ProviderConfig, error) {
	assembled := make([]ProviderConfig, 0, len(builtin)+len(custom))
	seen := make(map[string]string, len(builtin)+len(custom))

	appendProvider := func(provider ProviderConfig) error {
		name := strings.TrimSpace(provider.Name)
		key := normalizeProviderName(name)
		if key == "" {
			assembled = append(assembled, cloneProviderConfig(provider))
			return nil
		}
		if existing, exists := seen[key]; exists {
			return fmt.Errorf("config: duplicate provider name %q for %q and %q", name, existing, name)
		}
		seen[key] = name
		assembled = append(assembled, cloneProviderConfig(provider))
		return nil
	}

	for _, provider := range builtin {
		candidate := cloneProviderConfig(provider)
		if candidate.Source == "" {
			candidate.Source = ProviderSourceBuiltin
		}
		if err := appendProvider(candidate); err != nil {
			return nil, err
		}
	}
	for _, provider := range custom {
		candidate := cloneProviderConfig(provider)
		if candidate.Source == "" {
			candidate.Source = ProviderSourceCustom
		}
		if err := appendProvider(candidate); err != nil {
			return nil, err
		}
	}
	return assembled, nil
}
