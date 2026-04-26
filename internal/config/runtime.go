package config

import (
	"errors"

	"neo-code/internal/provider"
	"neo-code/internal/session"
)

const (
	DefaultMaxNoProgressStreak  = 5
	DefaultMaxRepeatCycleStreak = 3
	DefaultMaxTurns             = 90
)

// RuntimeConfig 定义 runtime 层的可调参数。
type RuntimeConfig struct {
	MaxNoProgressStreak  int                 `yaml:"max_no_progress_streak,omitempty"`
	MaxRepeatCycleStreak int                 `yaml:"max_repeat_cycle_streak,omitempty"`
	MaxTurns             int                 `yaml:"max_turns,omitempty"`
	Verification         VerificationConfig  `yaml:"verification,omitempty"`
	Assets               RuntimeAssetsConfig `yaml:"assets,omitempty"`
}

// RuntimeAssetsConfig 定义运行时对 session_asset 的大小限制。
type RuntimeAssetsConfig struct {
	MaxSessionAssetBytes       int64 `yaml:"max_session_asset_bytes,omitempty"`
	MaxSessionAssetsTotalBytes int64 `yaml:"max_session_assets_total_bytes,omitempty"`
}

// defaultRuntimeConfig 返回 runtime 配置的静态默认值。
func defaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		MaxNoProgressStreak:  DefaultMaxNoProgressStreak,
		MaxRepeatCycleStreak: DefaultMaxRepeatCycleStreak,
		MaxTurns:             DefaultMaxTurns,
		Verification:         defaultVerificationConfig(),
		Assets:               defaultRuntimeAssetsConfig(),
	}
}

// defaultRuntimeAssetsConfig 返回 runtime 附件限制配置默认值。
func defaultRuntimeAssetsConfig() RuntimeAssetsConfig {
	return RuntimeAssetsConfig{
		MaxSessionAssetBytes:       session.MaxSessionAssetBytes,
		MaxSessionAssetsTotalBytes: provider.MaxSessionAssetsTotalBytes,
	}
}

// Clone 复制 runtime 配置，避免调用方共享可变状态。
func (c RuntimeConfig) Clone() RuntimeConfig {
	return RuntimeConfig{
		MaxNoProgressStreak:  c.MaxNoProgressStreak,
		MaxRepeatCycleStreak: c.MaxRepeatCycleStreak,
		MaxTurns:             c.MaxTurns,
		Verification:         c.Verification.Clone(),
		Assets:               c.Assets.Clone(),
	}
}

// ApplyDefaults 在配置缺失、为零或非法时回填默认阈值。
func (c *RuntimeConfig) ApplyDefaults(defaults RuntimeConfig) {
	if c == nil {
		return
	}
	if c.MaxNoProgressStreak <= 0 {
		c.MaxNoProgressStreak = defaults.MaxNoProgressStreak
	}
	if c.MaxRepeatCycleStreak <= 0 {
		c.MaxRepeatCycleStreak = defaults.MaxRepeatCycleStreak
	}
	if c.MaxTurns <= 0 {
		c.MaxTurns = defaults.MaxTurns
	}
	c.Verification.ApplyDefaults(defaults.Verification)
	c.Assets.ApplyDefaults(defaults.Assets)
}

// Validate 校验 runtime 配置是否满足最小约束。
func (c RuntimeConfig) Validate() error {
	if c.MaxNoProgressStreak <= 0 {
		return errors.New("max_no_progress_streak must be greater than 0")
	}
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

// Clone 复制附件限制配置，避免调用方共享可变状态。
func (c RuntimeAssetsConfig) Clone() RuntimeAssetsConfig {
	return c
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
}

// Validate 校验附件限制配置是否满足最小约束；0 表示使用默认值，仅禁止负数。
func (c RuntimeAssetsConfig) Validate() error {
	if c.MaxSessionAssetBytes < 0 {
		return errors.New("runtime.assets.max_session_asset_bytes must be greater than or equal to 0")
	}
	if c.MaxSessionAssetsTotalBytes < 0 {
		return errors.New("runtime.assets.max_session_assets_total_bytes must be greater than or equal to 0")
	}
	return nil
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
