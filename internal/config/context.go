package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultCompactManualKeepRecentMessages = 10
	DefaultCompactMaxSummaryChars          = 1200
	DefaultBudgetPromptBudget              = 0
	DefaultBudgetReserveTokens             = 13000
	DefaultBudgetFallbackPromptBudget      = 100000
	DefaultBudgetMaxReactiveCompacts       = 3
	DefaultMicroCompactRetainedToolSpans   = 6
	DefaultCompactReadTimeMaxMessageSpans  = 24
	DefaultAskMaxInputTokens               = 8000
	DefaultAskRetainTurns                  = 5
	DefaultAskSummaryMaxChars              = 1200

	CompactManualStrategyKeepRecent  = "keep_recent"
	CompactManualStrategyFullReplace = "full_replace"
)

type ContextConfig struct {
	Compact CompactConfig `yaml:"compact,omitempty"`
	Budget  BudgetConfig  `yaml:"budget,omitempty"`
	Ask     AskConfig     `yaml:"ask,omitempty"`
}

type CompactConfig struct {
	ManualStrategy                string `yaml:"manual_strategy,omitempty"`
	ManualKeepRecentMessages      int    `yaml:"manual_keep_recent_messages,omitempty"`
	MaxSummaryChars               int    `yaml:"max_summary_chars,omitempty"`
	MicroCompactDisabled          bool   `yaml:"micro_compact_disabled,omitempty"`
	MicroCompactRetainedToolSpans int    `yaml:"micro_compact_retained_tool_spans,omitempty"`
	ReadTimeMaxMessageSpans       int    `yaml:"read_time_max_message_spans,omitempty"`
	MaxArchivedPromptChars        int    `yaml:"max_archived_prompt_chars,omitempty"`
}

// BudgetConfig 定义上下文预算控制面的配置。
type BudgetConfig struct {
	PromptBudget         int `yaml:"prompt_budget,omitempty"`
	ReserveTokens        int `yaml:"reserve_tokens,omitempty"`
	FallbackPromptBudget int `yaml:"fallback_prompt_budget,omitempty"`
	MaxReactiveCompacts  int `yaml:"max_reactive_compacts,omitempty"`
}

// AskConfig 定义 Ask 模式的轻量上下文策略。
type AskConfig struct {
	MaxInputTokens  int `yaml:"max_input_tokens,omitempty"`
	RetainTurns     int `yaml:"retain_turns,omitempty"`
	SummaryMaxChars int `yaml:"summary_max_chars,omitempty"`
}

// defaultContextConfig 返回上下文压缩相关配置的默认值。
func defaultContextConfig() ContextConfig {
	return ContextConfig{
		Compact: defaultCompactConfig(),
		Budget:  defaultBudgetConfig(),
		Ask:     defaultAskConfig(),
	}
}

// defaultBudgetConfig 返回预算控制面的默认配置。
func defaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		PromptBudget:         DefaultBudgetPromptBudget,
		ReserveTokens:        DefaultBudgetReserveTokens,
		FallbackPromptBudget: DefaultBudgetFallbackPromptBudget,
		MaxReactiveCompacts:  DefaultBudgetMaxReactiveCompacts,
	}
}

// defaultCompactConfig 返回手动 compact 策略的默认配置。
func defaultCompactConfig() CompactConfig {
	return CompactConfig{
		ManualStrategy:                CompactManualStrategyKeepRecent,
		ManualKeepRecentMessages:      DefaultCompactManualKeepRecentMessages,
		MaxSummaryChars:               DefaultCompactMaxSummaryChars,
		MicroCompactRetainedToolSpans: DefaultMicroCompactRetainedToolSpans,
		ReadTimeMaxMessageSpans:       DefaultCompactReadTimeMaxMessageSpans,
	}
}

// defaultAskConfig 返回 Ask 模式默认配置。
func defaultAskConfig() AskConfig {
	return AskConfig{
		MaxInputTokens:  DefaultAskMaxInputTokens,
		RetainTurns:     DefaultAskRetainTurns,
		SummaryMaxChars: DefaultAskSummaryMaxChars,
	}
}

// Clone 返回上下文配置的独立副本，避免后续修改污染原值。
func (c ContextConfig) Clone() ContextConfig {
	return ContextConfig{
		Compact: c.Compact.Clone(),
		Budget:  c.Budget.Clone(),
		Ask:     c.Ask.Clone(),
	}
}

// Clone 返回 compact 配置的值副本。
func (c CompactConfig) Clone() CompactConfig {
	return c
}

// Clone 返回 budget 配置的值副本。
func (c BudgetConfig) Clone() BudgetConfig {
	return c
}

// Clone 返回 ask 配置的值副本。
func (c AskConfig) Clone() AskConfig {
	return c
}

// ApplyDefaults 为上下文配置补齐缺省的 compact 参数。
func (c *ContextConfig) ApplyDefaults(defaults ContextConfig) {
	if c == nil {
		return
	}

	c.Compact.ApplyDefaults(defaults.Compact)
	c.Budget.ApplyDefaults(defaults.Budget)
	c.Ask.ApplyDefaults(defaults.Ask)
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
	if c.ReadTimeMaxMessageSpans <= 0 {
		c.ReadTimeMaxMessageSpans = defaults.ReadTimeMaxMessageSpans
	}
}

// ApplyDefaults 为 budget 配置填充缺省值。
func (c *BudgetConfig) ApplyDefaults(defaults BudgetConfig) {
	if c == nil {
		return
	}
	if c.ReserveTokens <= 0 {
		c.ReserveTokens = defaults.ReserveTokens
	}
	if c.FallbackPromptBudget <= 0 {
		c.FallbackPromptBudget = defaults.FallbackPromptBudget
	}
	if c.MaxReactiveCompacts <= 0 {
		c.MaxReactiveCompacts = defaults.MaxReactiveCompacts
	}
}

// ApplyDefaults 为 ask 配置填充缺省值。
func (c *AskConfig) ApplyDefaults(defaults AskConfig) {
	if c == nil {
		return
	}
	if c.MaxInputTokens <= 0 {
		c.MaxInputTokens = defaults.MaxInputTokens
	}
	if c.RetainTurns <= 0 {
		c.RetainTurns = defaults.RetainTurns
	}
	if c.SummaryMaxChars <= 0 {
		c.SummaryMaxChars = defaults.SummaryMaxChars
	}
}

// Validate 校验上下文压缩配置是否合法。
func (c ContextConfig) Validate() error {
	if err := c.Compact.Validate(); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	if err := c.Budget.Validate(); err != nil {
		return fmt.Errorf("budget: %w", err)
	}
	if err := c.Ask.Validate(); err != nil {
		return fmt.Errorf("ask: %w", err)
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
	if c.ReadTimeMaxMessageSpans <= 0 {
		return errors.New("read_time_max_message_spans must be greater than 0")
	}

	switch strings.ToLower(strings.TrimSpace(c.ManualStrategy)) {
	case CompactManualStrategyKeepRecent, CompactManualStrategyFullReplace:
		return nil
	default:
		return fmt.Errorf("manual_strategy %q is not supported", c.ManualStrategy)
	}
}

// Validate 校验 budget 配置是否合法。
func (c BudgetConfig) Validate() error {
	if c.PromptBudget < 0 {
		return errors.New("prompt_budget must be greater than or equal to 0")
	}
	if c.ReserveTokens <= 0 {
		return errors.New("reserve_tokens must be greater than 0")
	}
	if c.FallbackPromptBudget <= 0 {
		return errors.New("fallback_prompt_budget must be greater than 0")
	}
	if c.MaxReactiveCompacts <= 0 {
		return errors.New("max_reactive_compacts must be greater than 0")
	}
	return nil
}

// Validate 校验 ask 配置是否合法。
func (c AskConfig) Validate() error {
	if c.MaxInputTokens <= 0 {
		return errors.New("max_input_tokens must be greater than 0")
	}
	if c.RetainTurns <= 0 {
		return errors.New("retain_turns must be greater than 0")
	}
	if c.SummaryMaxChars <= 0 {
		return errors.New("summary_max_chars must be greater than 0")
	}
	return nil
}
