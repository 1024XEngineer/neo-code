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
	Feishu                  FeishuConfig           `yaml:"feishu,omitempty"`
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
	data, _, err = normalizeVerificationSchemaContent(data)
	if err != nil {
		return nil, fmt.Errorf("config: normalize verification schema: %w", err)
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

	data, err := preprocessLegacyVerificationSchema(data)
	if err != nil {
		return nil, fmt.Errorf("config: preprocess legacy verification schema: %w", err)
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
		Feishu:                  file.Feishu,
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
		Feishu:                  snapshot.Feishu,
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

// normalizeVerificationSchemaContent 在内存中预处理 verification schema，避免旧字段先于 strict decode 触发硬失败。
func normalizeVerificationSchemaContent(raw []byte) ([]byte, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, false, nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false, err
	}
	if doc == nil {
		return raw, false, nil
	}

	changed, err := migrateVerificationConfig(doc)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return raw, false, nil
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// preprocessLegacyVerificationSchema 在 strict decode 前对 verification schema 做内存态预处理：
// 删除废弃字段（enabled / final_intercept / default_task_policy），并将简单的字符串 command 安全迁移为 argv。
// 若 command 包含 shell 元字符（引号、管道、重定向等），则显式报错要求手工改写为 argv。
func preprocessLegacyVerificationSchema(data []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return data, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return data, nil
	}

	for i := 0; i < len(doc.Content); i += 2 {
		if strings.TrimSpace(doc.Content[i].Value) != "runtime" {
			continue
		}
		runtimeNode := doc.Content[i+1]
		if runtimeNode.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j < len(runtimeNode.Content); j += 2 {
			if strings.TrimSpace(runtimeNode.Content[j].Value) != "verification" {
				continue
			}
			verificationNode := runtimeNode.Content[j+1]
			if verificationNode.Kind != yaml.MappingNode {
				continue
			}
			if err := cleanupLegacyVerificationNode(verificationNode); err != nil {
				return nil, err
			}
			break
		}
		break
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	_ = enc.Close()
	return buf.Bytes(), nil
}

// cleanupLegacyVerificationNode 清理 verification 节点下的废弃字段，并处理 legacy command string。
func cleanupLegacyVerificationNode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var toRemove []int
	for i := 0; i < len(node.Content); i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		switch key {
		case "enabled", "final_intercept", "default_task_policy":
			toRemove = append(toRemove, i, i+1)
		case "verifiers":
			if err := cleanupLegacyVerifiersNode(node.Content[i+1]); err != nil {
				return err
			}
		}
	}
	// 从后往前删除，避免索引偏移
	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		if idx < len(node.Content) {
			node.Content = append(node.Content[:idx], node.Content[idx+1:]...)
		}
	}
	return nil
}

// cleanupLegacyVerifiersNode 遍历 verifiers 映射，删除废弃字段并将简单的字符串 command 迁移为 argv sequence。
func cleanupLegacyVerifiersNode(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		verifierNode := node.Content[i+1]
		if verifierNode.Kind != yaml.MappingNode {
			continue
		}
		var toRemove []int
		for j := 0; j < len(verifierNode.Content); j += 2 {
			key := strings.TrimSpace(verifierNode.Content[j].Value)
			if key == "enabled" || key == "required" {
				toRemove = append(toRemove, j, j+1)
				continue
			}
			if key != "command" {
				continue
			}
			commandNode := verifierNode.Content[j+1]
			if commandNode.Kind == yaml.ScalarNode {
				val := strings.TrimSpace(commandNode.Value)
				if val == "" {
					continue
				}
				if containsShellMetacharacters(val) {
					return fmt.Errorf("unsupported shell syntax in verifier command %q: rewrite it as argv", val)
				}
				tokens := strings.Fields(val)
				if len(tokens) == 0 {
					continue
				}
				// 将标量替换为序列
				commandNode.Kind = yaml.SequenceNode
				commandNode.Tag = "!!seq"
				commandNode.Value = ""
				commandNode.Content = make([]*yaml.Node, 0, len(tokens))
				for _, t := range tokens {
					commandNode.Content = append(commandNode.Content, &yaml.Node{
						Kind:  yaml.ScalarNode,
						Tag:   "!!str",
						Value: t,
					})
				}
			}
		}
		// 从后往前删除废弃字段
		for k := len(toRemove) - 1; k >= 0; k-- {
			idx := toRemove[k]
			if idx < len(verifierNode.Content) {
				verifierNode.Content = append(verifierNode.Content[:idx], verifierNode.Content[idx+1:]...)
			}
		}
	}
	return nil
}

// containsShellMetacharacters 判断字符串是否包含需要 shell 解释的元字符。
func containsShellMetacharacters(s string) bool {
	metachars := "'\"`$|&;<>(){}[]*?~\\"
	for _, r := range s {
		if strings.ContainsRune(metachars, r) {
			return true
		}
	}
	return false
}
