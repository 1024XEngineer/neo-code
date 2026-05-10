package config

import "errors"

const (
	DefaultMemoMaxEntries        = 200
	DefaultMemoMaxIndexBytes     = 16 * 1024
	DefaultMemoExtractTimeoutSec = 15
)

// MemoConfig 控制跨会话持久记忆的行为配置。
type MemoConfig struct {
	Enabled           bool `yaml:"enabled,omitempty"`
	AutoExtract       bool `yaml:"auto_extract,omitempty"`
	MaxEntries        int  `yaml:"max_entries,omitempty"`
	MaxIndexBytes     int  `yaml:"max_index_bytes,omitempty"`
	ExtractTimeoutSec int  `yaml:"extract_timeout_sec,omitempty"`
}

// defaultMemoConfig 返回跨会话记忆的默认配置。
func defaultMemoConfig() MemoConfig {
	return MemoConfig{
		Enabled:           true,
		AutoExtract:       true,
		MaxEntries:        DefaultMemoMaxEntries,
		MaxIndexBytes:     DefaultMemoMaxIndexBytes,
		ExtractTimeoutSec: DefaultMemoExtractTimeoutSec,
	}
}

// Clone 返回 memo 配置的值副本。
func (c MemoConfig) Clone() MemoConfig {
	return c
}

// ApplyDefaults 为 memo 配置补齐缺省参数。
func (c *MemoConfig) ApplyDefaults(defaults MemoConfig) {
	if c == nil {
		return
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = defaults.MaxEntries
	}
	if c.MaxIndexBytes == 0 {
		c.MaxIndexBytes = defaults.MaxIndexBytes
	}
	if c.ExtractTimeoutSec == 0 {
		c.ExtractTimeoutSec = defaults.ExtractTimeoutSec
	}
}

// Validate 校验 memo 配置是否合法。
func (c MemoConfig) Validate() error {
	if c.MaxEntries <= 0 {
		return errors.New("max_entries must be greater than 0")
	}
	if c.MaxIndexBytes <= 0 {
		return errors.New("max_index_bytes must be greater than 0")
	}
	if c.ExtractTimeoutSec <= 0 {
		return errors.New("extract_timeout_sec must be greater than 0")
	}
	return nil
}
