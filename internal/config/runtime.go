package config

import (
	"errors"

	"neo-code/internal/provider"
	"neo-code/internal/session"
)

const (
	DefaultMaxRepeatCycleStreak = 3
	DefaultMaxTurns             = 90
)

// RuntimeConfig 定义 runtime 层的可调参数。
type RuntimeConfig struct {
	MaxRepeatCycleStreak int                 `yaml:"max_repeat_cycle_streak,omitempty"`
	MaxTurns             int                 `yaml:"max_turns,omitempty"`
	Verification         VerificationConfig  `yaml:"verification,omitempty"`
	Hooks                RuntimeHooksConfig  `yaml:"hooks,omitempty"`
	Assets               RuntimeAssetsConfig `yaml:"assets,omitempty"`
}

// RuntimeAssetsConfig 定义运行时对 session_asset 的大小限制。
type RuntimeAssetsConfig struct {
	MaxSessionAssetBytes       int64 `yaml:"max_session_asset_bytes,omitempty"`
	MaxSessionAssetsTotalBytes int64 `yaml:"max_session_assets_total_bytes,omitempty"`
	// TextAssetEnabled 控制是否把文本类 asset 在提交会话前内联为 text part；关闭时文本 asset
	// 仅作为图像风格的会话附件存在（保持向后兼容，便于回滚）。
	TextAssetEnabled *bool `yaml:"text_asset_enabled,omitempty"`
	// MaxTextAssetBytes 限制单个文本 asset 的字节上限（保存与读取都受此约束）。
	MaxTextAssetBytes int64 `yaml:"max_text_asset_bytes,omitempty"`
	// MaxTextAssetChars 限制单个文本 asset 在 UTF-8 解码后允许保留的最大字符数。
	MaxTextAssetChars int `yaml:"max_text_asset_chars,omitempty"`
}

// defaultRuntimeConfig 返回 runtime 配置的静态默认值。
func defaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		MaxRepeatCycleStreak: DefaultMaxRepeatCycleStreak,
		MaxTurns:             DefaultMaxTurns,
		Verification:         defaultVerificationConfig(),
		Hooks:                defaultRuntimeHooksConfig(),
		Assets:               defaultRuntimeAssetsConfig(),
	}
}

// defaultRuntimeAssetsConfig 返回 runtime 附件限制配置默认值。
func defaultRuntimeAssetsConfig() RuntimeAssetsConfig {
	enabled := true
	return RuntimeAssetsConfig{
		MaxSessionAssetBytes:       session.MaxSessionAssetBytes,
		MaxSessionAssetsTotalBytes: provider.MaxSessionAssetsTotalBytes,
		TextAssetEnabled:           &enabled,
		MaxTextAssetBytes:          session.DefaultMaxTextAssetBytes,
		MaxTextAssetChars:          session.DefaultMaxTextAssetChars,
	}
}

// Clone 复制 runtime 配置，避免调用方共享可变状态。
func (c RuntimeConfig) Clone() RuntimeConfig {
	return RuntimeConfig{
		MaxRepeatCycleStreak: c.MaxRepeatCycleStreak,
		MaxTurns:             c.MaxTurns,
		Verification:         c.Verification.Clone(),
		Hooks:                c.Hooks.Clone(),
		Assets:               c.Assets.Clone(),
	}
}

// ApplyDefaults 在配置缺失、为零或非法时回填默认阈值。
func (c *RuntimeConfig) ApplyDefaults(defaults RuntimeConfig) {
	if c == nil {
		return
	}
	if c.MaxRepeatCycleStreak <= 0 {
		c.MaxRepeatCycleStreak = defaults.MaxRepeatCycleStreak
	}
	if c.MaxTurns <= 0 {
		c.MaxTurns = defaults.MaxTurns
	}
	c.Verification.ApplyDefaults(defaults.Verification)
	c.Hooks.ApplyDefaults(defaults.Hooks)
	c.Assets.ApplyDefaults(defaults.Assets)
}

// Validate 校验 runtime 配置是否满足最小约束。
func (c RuntimeConfig) Validate() error {
	if c.MaxRepeatCycleStreak <= 0 {
		return errors.New("max_repeat_cycle_streak must be greater than 0")
	}
	if c.MaxTurns < 0 {
		return errors.New("max_turns must be greater than or equal to 0")
	}
	verification := c.Verification.Clone()
	verification.ApplyDefaults(defaultVerificationConfig())
	if err := verification.Validate(); err != nil {
		return err
	}
	hooks := c.Hooks.Clone()
	hooks.ApplyDefaults(defaultRuntimeHooksConfig())
	if err := hooks.Validate(); err != nil {
		return err
	}
	if err := c.Assets.Validate(); err != nil {
		return err
	}
	return nil
}

// ResolveSessionAssetPolicy 归一化 runtime 附件存储策略并施加代码硬上限兜底。
func (c RuntimeConfig) ResolveSessionAssetPolicy() session.AssetPolicy {
	return c.Assets.ResolveSessionAssetPolicy()
}

// ResolveRequestAssetBudget 归一化 runtime 附件请求预算并施加代码硬上限兜底。
func (c RuntimeConfig) ResolveRequestAssetBudget() provider.RequestAssetBudget {
	return c.Assets.ResolveRequestAssetBudget()
}

// ResolveTextAssetPolicy 归一化 runtime 文本附件策略并施加代码硬上限兜底。
func (c RuntimeConfig) ResolveTextAssetPolicy() session.TextAssetPolicy {
	return c.Assets.ResolveTextAssetPolicy()
}

// IsTextAssetEnabled 返回当前 runtime 文本附件内联开关。
func (c RuntimeConfig) IsTextAssetEnabled() bool {
	return c.Assets.IsTextAssetEnabled()
}

// Clone 复制附件限制配置，避免调用方共享可变状态。
func (c RuntimeAssetsConfig) Clone() RuntimeAssetsConfig {
	out := c
	if c.TextAssetEnabled != nil {
		enabled := *c.TextAssetEnabled
		out.TextAssetEnabled = &enabled
	}
	return out
}

// ApplyDefaults 在配置缺失、为零或非法时回填附件限制默认值。
func (c *RuntimeAssetsConfig) ApplyDefaults(defaults RuntimeAssetsConfig) {
	if c == nil {
		return
	}
	if c.MaxSessionAssetBytes <= 0 {
		c.MaxSessionAssetBytes = defaults.MaxSessionAssetBytes
	}
	if c.MaxSessionAssetsTotalBytes <= 0 {
		c.MaxSessionAssetsTotalBytes = defaults.MaxSessionAssetsTotalBytes
	}
	if c.TextAssetEnabled == nil {
		// nil 视为 true，与 IsTextAssetEnabled() 的 nil-as-true 语义对齐。
		enabled := true
		if defaults.TextAssetEnabled != nil {
			enabled = *defaults.TextAssetEnabled
		}
		c.TextAssetEnabled = &enabled
	}
	if c.MaxTextAssetBytes <= 0 {
		c.MaxTextAssetBytes = defaults.MaxTextAssetBytes
	}
	if c.MaxTextAssetChars <= 0 {
		c.MaxTextAssetChars = defaults.MaxTextAssetChars
	}
}

// Validate 校验附件限制配置是否满足最小约束；0 表示使用默认值，仅禁止负数。
func (c RuntimeAssetsConfig) Validate() error {
	if c.MaxSessionAssetBytes < 0 {
		return errors.New("runtime.assets.max_session_asset_bytes must be greater than or equal to 0")
	}
	if c.MaxSessionAssetsTotalBytes < 0 {
		return errors.New("runtime.assets.max_session_assets_total_bytes must be greater than or equal to 0")
	}
	if c.MaxTextAssetBytes < 0 {
		return errors.New("runtime.assets.max_text_asset_bytes must be greater than or equal to 0")
	}
	if c.MaxTextAssetChars < 0 {
		return errors.New("runtime.assets.max_text_asset_chars must be greater than or equal to 0")
	}
	return nil
}

// IsTextAssetEnabled 返回文本类 asset 内联开关；nil 视为 true。
func (c RuntimeAssetsConfig) IsTextAssetEnabled() bool {
	if c.TextAssetEnabled == nil {
		return true
	}
	return *c.TextAssetEnabled
}

// ResolveSessionAssetPolicy 归一化附件存储策略并应用代码硬上限。
func (c RuntimeAssetsConfig) ResolveSessionAssetPolicy() session.AssetPolicy {
	return session.NormalizeAssetPolicy(session.AssetPolicy{
		MaxSessionAssetBytes: c.MaxSessionAssetBytes,
	})
}

// ResolveRequestAssetBudget 归一化附件请求预算并应用代码硬上限。
func (c RuntimeAssetsConfig) ResolveRequestAssetBudget() provider.RequestAssetBudget {
	assetPolicy := c.ResolveSessionAssetPolicy()
	return provider.NormalizeRequestAssetBudget(provider.RequestAssetBudget{
		MaxSessionAssetsTotalBytes: c.MaxSessionAssetsTotalBytes,
	}, assetPolicy.MaxSessionAssetBytes)
}

// ResolveTextAssetPolicy 归一化文本附件策略并应用代码硬上限。
func (c RuntimeAssetsConfig) ResolveTextAssetPolicy() session.TextAssetPolicy {
	return session.NormalizeTextAssetPolicy(session.TextAssetPolicy{
		Whitelist:         session.DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: c.MaxTextAssetBytes,
		MaxTextAssetChars: c.MaxTextAssetChars,
	})
}
