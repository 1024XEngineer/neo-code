package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultCompactManualKeepRecentMessages       = 10
	DefaultCompactMaxSummaryChars                = 1200
	DefaultAutoCompactInputTokenThreshold        = 100000
	DefaultMicroCompactRetainedToolSpans         = 2

	CompactManualStrategyKeepRecent  = "keep_recent"
	CompactManualStrategyFullReplace = "full_replace"
)

type ContextConfig struct {
	Compact     CompactConfig     `yaml:"compact,omitempty"`
	AutoCompact AutoCompactConfig `yaml:"auto_compact,omitempty"`
}

type CompactConfig struct {
	ManualStrategy                string `yaml:"manual_strategy,omitempty"`
	ManualKeepRecentMessages      int    `yaml:"manual_keep_recent_messages,omitempty"`
	MaxSummaryChars               int    `yaml:"max_summary_chars,omitempty"`
	MicroCompactDisabled          bool   `yaml:"micro_compact_disabled,omitempty"`
	MicroCompactRetainedToolSpans int    `yaml:"micro_compact_retained_tool_spans,omitempty"`
	MaxArchivedPromptChars        int    `yaml:"max_archived_prompt_chars,omitempty"`
}

// AutoCompactConfig controls automatic context compression triggered by token thresholds.
type AutoCompactConfig struct {
	Enabled             bool `yaml:"enabled"`
	InputTokenThreshold int  `yaml:"input_token_threshold,omitempty"`
}

// defaultContextConfig 返回上下文压缩相关配置的默认值。
func defaultContextConfig() ContextConfig {
	return ContextConfig{
		Compact:     defaultCompactConfig(),
		AutoCompact: defaultAutoCompactConfig(),
	}
}

func defaultAutoCompactConfig() AutoCompactConfig {
	return AutoCompactConfig{
		InputTokenThreshold: DefaultAutoCompactInputTokenThreshold,
	}
}

// defaultCompactConfig 返回手动 compact 策略的默认配置。
func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		ManualStrategy:                CompactManualStrategyKeepRecent,
		ManualKeepRecentMessages:      DefaultCompactManualKeepRecentMessages,
		MaxSummaryChars:               DefaultCompactMaxSummaryChars,
		MicroCompactRetainedToolSpans: DefaultMicroCompactRetainedToolSpans,
	}
}

// Clone 返回上下文配置的独立副本，避免后续修改污染原值。
func (c ContextConfig) Clone() ContextConfig {
	return ContextConfig{
		Compact:     c.Compact.Clone(),
		AutoCompact: c.AutoCompact.Clone(),
	}
}

// Clone 返回 compact 配置的值副本。
func (c CompactConfig) Clone() CompactConfig {
	return c
}

// Clone 返回 auto_compact 配置的值副本。
func (c AutoCompactConfig) Clone() AutoCompactConfig {
	return c
}

// ApplyDefaults 为上下文配置补齐缺省的 compact 参数。
func (c *ContextConfig) ApplyDefaults(defaults ContextConfig) {
	if c == nil {
		return
	}

	c.Compact.ApplyDefaults(defaults.Compact)
	c.AutoCompact.ApplyDefaults(defaults.AutoCompact)
}

// ApplyDefaults 为 compact 配置填充缺省策略和阈值。
func (c *CompactConfig) ApplyDefaults(defaults CompactConfig) {
	if c == nil {
		return
	}

	if strings.TrimSpace(c.ManualStrategy) == "" {
		c.ManualStrategy = defaults.ManualStrategy
	}
	if c.ManualKeepRecentMessages <= 0 {
		c.ManualKeepRecentMessages = defaults.ManualKeepRecentMessages
	}
	if c.MaxSummaryChars <= 0 {
		c.MaxSummaryChars = defaults.MaxSummaryChars
	}
	if c.MicroCompactRetainedToolSpans <= 0 {
		c.MicroCompactRetainedToolSpans = defaults.MicroCompactRetainedToolSpans
	}
}

// ApplyDefaults 为 auto_compact 配置填充缺省阈值。
func (c *AutoCompactConfig) ApplyDefaults(defaults AutoCompactConfig) {
	if c == nil {
		return
	}
	if c.InputTokenThreshold <= 0 {
		c.InputTokenThreshold = defaults.InputTokenThreshold
	}
}

// Validate 校验上下文压缩配置是否合法。
func (c ContextConfig) Validate() error {
	if err := c.Compact.Validate(); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	if err := c.AutoCompact.Validate(); err != nil {
		return fmt.Errorf("auto_compact: %w", err)
	}
	return nil
}

// Validate 校验 compact 配置中的策略和阈值是否可用。
func (c CompactConfig) Validate() error {
	if c.ManualKeepRecentMessages <= 0 {
		return errors.New("manual_keep_recent_messages must be greater than 0")
	}
	if c.MaxSummaryChars <= 0 {
		return errors.New("max_summary_chars must be greater than 0")
	}

	switch strings.ToLower(strings.TrimSpace(c.ManualStrategy)) {
	case CompactManualStrategyKeepRecent, CompactManualStrategyFullReplace:
		return nil
	default:
		return fmt.Errorf("manual_strategy %q is not supported", c.ManualStrategy)
	}
}

// Validate 校验 auto_compact 配置是否合法。
func (c AutoCompactConfig) Validate() error {
	if c.Enabled && c.InputTokenThreshold <= 0 {
		return errors.New("input_token_threshold must be greater than 0 when enabled")
	}
	return nil
}
