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
	SelectedProvider        string                 `yaml:"selected_provider,omitempty"`
	CurrentModel            string                 `yaml:"current_model,omitempty"`
	Shell                   string                 `yaml:"shell"`
	ToolTimeoutSec          int                    `yaml:"tool_timeout_sec,omitempty"`
	GenerateStartTimeoutSec int                    `yaml:"generate_start_timeout_sec,omitempty"`
	Runtime                 RuntimeConfig          `yaml:"runtime,omitempty"`
	Context                 persistedContextConfig `yaml:"context,omitempty"`
	Tools                   ToolsConfig            `yaml:"tools,omitempty"`
	Memo                    persistedMemoConfig    `yaml:"memo,omitempty"`
	Gateway                 GatewayConfig          `yaml:"gateway,omitempty"`
}

type persistedContextConfig struct {
	Compact persistedCompactConfig `yaml:"compact,omitempty"`
	Budget  persistedBudgetConfig  `yaml:"budget,omitempty"`
}

type persistedCompactConfig struct {
	ManualStrategy                string `yaml:"manual_strategy,omitempty"`
	ManualKeepRecentMessages      int    `yaml:"manual_keep_recent_messages,omitempty"`
	MaxSummaryChars               int    `yaml:"max_summary_chars,omitempty"`
	MicroCompactDisabled          bool   `yaml:"micro_compact_disabled,omitempty"`
	MicroCompactRetainedToolSpans int    `yaml:"micro_compact_retained_tool_spans,omitempty"`
	ReadTimeMaxMessageSpans       int    `yaml:"read_time_max_message_spans,omitempty"`
	MaxArchivedPromptChars        int    `yaml:"max_archived_prompt_chars,omitempty"`
}

type persistedBudgetConfig struct {
	PromptBudget         int `yaml:"prompt_budget,omitempty"`
	ReserveTokens        int `yaml:"reserve_tokens,omitempty"`
	FallbackPromptBudget int `yaml:"fallback_prompt_budget,omitempty"`
	MaxReactiveCompacts  int `yaml:"max_reactive_compacts,omitempty"`
}

type persistedMemoConfig struct {
	Enabled               *bool `yaml:"enabled,omitempty"`
	AutoExtract           *bool `yaml:"auto_extract,omitempty"`
	MaxEntries            *int  `yaml:"max_entries,omitempty"`
	MaxIndexBytes         *int  `yaml:"max_index_bytes,omitempty"`
	ExtractTimeoutSec     *int  `yaml:"extract_timeout_sec,omitempty"`
	ExtractRecentMessages *int  `yaml:"extract_recent_messages,omitempty"`
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(l.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("config: read config file: %w", err)
	}

	cfg, err := parseConfigWithContextDefaults(data, l.defaults.Context, l.defaults.Memo)
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

	if err := writeFileAtomically(l.ConfigPath(), data, 0o644); err != nil {
		return fmt.Errorf("config: write config file: %w", err)
	}

	return nil
}

func defaultBaseDir() string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if !filepath.IsAbs(home) {
		var err error
		home, err = os.UserHomeDir()
		if err != nil || !filepath.IsAbs(strings.TrimSpace(home)) {
			return dirName
		}
	}
	return filepath.Join(home, dirName)
}

func parseConfig(data []byte) (*Config, error) {
	defaults := StaticDefaults()
	return parseConfigWithContextDefaults(data, defaults.Context, defaults.Memo)
}

// parseConfigWithContextDefaults 负责在解析配置时注入上下文压缩相关默认值。
func parseConfigWithContextDefaults(
	data []byte,
	contextDefaults ContextConfig,
	memoDefaults ...MemoConfig,
) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Config{}, nil
	}

	resolvedMemo := defaultMemoConfig()
	if len(memoDefaults) > 0 {
		resolvedMemo = memoDefaults[0]
	}
	return parseCurrentConfig(data, contextDefaults, resolvedMemo)
}

func parseCurrentConfig(data []byte, contextDefaults ContextConfig, memoDefaults MemoConfig) (*Config, error) {
	var file persistedConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if strings.Contains(err.Error(), "max_index_lines") {
			return nil, fmt.Errorf("config: memo.max_index_lines has been removed, migrate to memo.max_entries: %w", err)
		}
		return nil, err
	}
	cfg := &Config{
		SelectedProvider:        strings.TrimSpace(file.SelectedProvider),
		CurrentModel:            strings.TrimSpace(file.CurrentModel),
		Shell:                   strings.TrimSpace(file.Shell),
		ToolTimeoutSec:          file.ToolTimeoutSec,
		GenerateStartTimeoutSec: file.GenerateStartTimeoutSec,
		Runtime:                 file.Runtime,
		Context:                 fromPersistedContextConfig(file.Context, contextDefaults),
		Tools:                   file.Tools,
		Memo:                    fromPersistedMemoConfig(file.Memo, memoDefaults),
		Gateway:                 file.Gateway,
	}

	return cfg, nil
}

func marshalPersistedConfig(snapshot Config) ([]byte, error) {
	file := persistedConfig{
		SelectedProvider:        snapshot.SelectedProvider,
		CurrentModel:            snapshot.CurrentModel,
		Shell:                   snapshot.Shell,
		ToolTimeoutSec:          snapshot.ToolTimeoutSec,
		GenerateStartTimeoutSec: snapshot.GenerateStartTimeoutSec,
		Runtime:                 snapshot.Runtime,
		Context:                 newPersistedContextConfig(snapshot.Context),
		Tools:                   snapshot.Tools,
		Memo:                    newPersistedMemoConfig(snapshot.Memo),
		Gateway:                 snapshot.Gateway,
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
			ManualStrategy:                cfg.Compact.ManualStrategy,
			ManualKeepRecentMessages:      cfg.Compact.ManualKeepRecentMessages,
			MaxSummaryChars:               cfg.Compact.MaxSummaryChars,
			MicroCompactDisabled:          cfg.Compact.MicroCompactDisabled,
			MicroCompactRetainedToolSpans: cfg.Compact.MicroCompactRetainedToolSpans,
			ReadTimeMaxMessageSpans:       cfg.Compact.ReadTimeMaxMessageSpans,
			MaxArchivedPromptChars:        cfg.Compact.MaxArchivedPromptChars,
		},
		Budget: persistedBudgetConfig{
			PromptBudget:         cfg.Budget.PromptBudget,
			ReserveTokens:        cfg.Budget.ReserveTokens,
			FallbackPromptBudget: cfg.Budget.FallbackPromptBudget,
			MaxReactiveCompacts:  cfg.Budget.MaxReactiveCompacts,
		},
	}
}

// fromPersistedContextConfig 将持久化配置恢复为运行时上下文配置并补齐默认值。
func fromPersistedContextConfig(file persistedContextConfig, defaults ContextConfig) ContextConfig {
	out := ContextConfig{
		Compact: CompactConfig{
			ManualStrategy:                strings.TrimSpace(file.Compact.ManualStrategy),
			ManualKeepRecentMessages:      file.Compact.ManualKeepRecentMessages,
			MaxSummaryChars:               file.Compact.MaxSummaryChars,
			MicroCompactDisabled:          file.Compact.MicroCompactDisabled,
			MicroCompactRetainedToolSpans: file.Compact.MicroCompactRetainedToolSpans,
			ReadTimeMaxMessageSpans:       file.Compact.ReadTimeMaxMessageSpans,
			MaxArchivedPromptChars:        file.Compact.MaxArchivedPromptChars,
		},
		Budget: BudgetConfig{
			PromptBudget:         file.Budget.PromptBudget,
			ReserveTokens:        file.Budget.ReserveTokens,
			FallbackPromptBudget: file.Budget.FallbackPromptBudget,
			MaxReactiveCompacts:  file.Budget.MaxReactiveCompacts,
		},
	}
	out.Compact.ApplyDefaults(defaults.Compact)
	out.Budget.ApplyDefaults(defaults.Budget)
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

	sections := []struct {
		providers []ProviderConfig
		source    ProviderSource
	}{
		{providers: builtin, source: ProviderSourceBuiltin},
		{providers: custom, source: ProviderSourceCustom},
	}

	for _, section := range sections {
		for _, provider := range section.providers {
			candidate := cloneProviderConfig(provider)
			if candidate.Source == "" {
				candidate.Source = section.source
			}
			if err := appendProvider(candidate); err != nil {
				return nil, err
			}
		}
	}

	return assembled, nil
}

// newPersistedMemoConfig 将运行时 memo 配置收敛为 YAML 持久化结构。
func newPersistedMemoConfig(cfg MemoConfig) persistedMemoConfig {
	enabled := cfg.Enabled
	autoExtract := cfg.AutoExtract
	maxEntries := cfg.MaxEntries
	maxIndexBytes := cfg.MaxIndexBytes
	extractTimeoutSec := cfg.ExtractTimeoutSec
	extractRecentMessages := cfg.ExtractRecentMessages
	return persistedMemoConfig{
		Enabled:               &enabled,
		AutoExtract:           &autoExtract,
		MaxEntries:            &maxEntries,
		MaxIndexBytes:         &maxIndexBytes,
		ExtractTimeoutSec:     &extractTimeoutSec,
		ExtractRecentMessages: &extractRecentMessages,
	}
}

// fromPersistedMemoConfig 将持久化配置恢复为运行时 memo 配置。
func fromPersistedMemoConfig(file persistedMemoConfig, defaults MemoConfig) MemoConfig {
	out := defaults
	if file.Enabled != nil {
		out.Enabled = *file.Enabled
	}
	if file.AutoExtract != nil {
		out.AutoExtract = *file.AutoExtract
	}
	if file.MaxEntries != nil {
		out.MaxEntries = *file.MaxEntries
	}
	if file.MaxIndexBytes != nil {
		out.MaxIndexBytes = *file.MaxIndexBytes
	}
	if file.ExtractTimeoutSec != nil {
		out.ExtractTimeoutSec = *file.ExtractTimeoutSec
	}
	if file.ExtractRecentMessages != nil {
		out.ExtractRecentMessages = *file.ExtractRecentMessages
	}
	return out
}
