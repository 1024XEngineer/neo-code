package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// DefaultGatewayACLMode 定义网关 ACL 默认模式。
	DefaultGatewayACLMode = "strict"
	// DefaultGatewayMetricsEnabled 定义网关指标是否默认开启。
	DefaultGatewayMetricsEnabled = true
	// DefaultGatewayMaxFrameBytes 定义控制面单帧最大字节数默认值。
	DefaultGatewayMaxFrameBytes = 1 << 20
	// DefaultGatewayIPCMaxConnections 定义 IPC 最大连接数默认值。
	DefaultGatewayIPCMaxConnections = 128
	// DefaultGatewayHTTPMaxRequestBytes 定义 HTTP 最大请求体默认值。
	DefaultGatewayHTTPMaxRequestBytes = 1 << 20
	// DefaultGatewayHTTPMaxStreamConnections 定义 HTTP 流式连接默认上限。
	DefaultGatewayHTTPMaxStreamConnections = 128
	// DefaultGatewayIPCReadSec 定义 IPC 读超时默认秒数。
	DefaultGatewayIPCReadSec = 30
	// DefaultGatewayIPCWriteSec 定义 IPC 写超时默认秒数。
	DefaultGatewayIPCWriteSec = 30
	// DefaultGatewayHTTPReadSec 定义 HTTP 读超时默认秒数。
	DefaultGatewayHTTPReadSec = 15
	// DefaultGatewayHTTPWriteSec 定义 HTTP 写超时默认秒数。
	DefaultGatewayHTTPWriteSec = 15
	// DefaultGatewayHTTPShutdownSec 定义 HTTP 关闭超时默认秒数。
	DefaultGatewayHTTPShutdownSec = 2
)

// GatewayConfig 表示网关治理与安全配置。
type GatewayConfig struct {
	Security      GatewaySecurityConfig      `yaml:"security,omitempty"`
	Limits        GatewayLimitsConfig        `yaml:"limits,omitempty"`
	Timeouts      GatewayTimeoutsConfig      `yaml:"timeouts,omitempty"`
	Observability GatewayObservabilityConfig `yaml:"observability,omitempty"`
}

// GatewaySecurityConfig 表示网关鉴权与 ACL 安全策略配置。
type GatewaySecurityConfig struct {
	ACLMode      string   `yaml:"acl_mode,omitempty"`
	TokenFile    string   `yaml:"token_file,omitempty"`
	AllowOrigins []string `yaml:"allow_origins,omitempty"`
}

// GatewayLimitsConfig 表示网关限流与配额配置。
type GatewayLimitsConfig struct {
	MaxFrameBytes            int `yaml:"max_frame_bytes,omitempty"`
	IPCMaxConnections        int `yaml:"ipc_max_connections,omitempty"`
	HTTPMaxRequestBytes      int `yaml:"http_max_request_bytes,omitempty"`
	HTTPMaxStreamConnections int `yaml:"http_max_stream_connections,omitempty"`
}

// GatewayTimeoutsConfig 表示网关默认超时配置。
type GatewayTimeoutsConfig struct {
	IPCReadSec      int `yaml:"ipc_read_sec,omitempty"`
	IPCWriteSec     int `yaml:"ipc_write_sec,omitempty"`
	HTTPReadSec     int `yaml:"http_read_sec,omitempty"`
	HTTPWriteSec    int `yaml:"http_write_sec,omitempty"`
	HTTPShutdownSec int `yaml:"http_shutdown_sec,omitempty"`
}

// GatewayObservabilityConfig 表示网关可观测性配置。
type GatewayObservabilityConfig struct {
	MetricsEnabled *bool `yaml:"metrics_enabled,omitempty"`
}

// defaultGatewayConfig 返回网关配置默认值。
func defaultGatewayConfig() GatewayConfig {
	return GatewayConfig{
		Security: GatewaySecurityConfig{
			ACLMode:      DefaultGatewayACLMode,
			AllowOrigins: defaultGatewayAllowOrigins(),
		},
		Limits: GatewayLimitsConfig{
			MaxFrameBytes:            DefaultGatewayMaxFrameBytes,
			IPCMaxConnections:        DefaultGatewayIPCMaxConnections,
			HTTPMaxRequestBytes:      DefaultGatewayHTTPMaxRequestBytes,
			HTTPMaxStreamConnections: DefaultGatewayHTTPMaxStreamConnections,
		},
		Timeouts: GatewayTimeoutsConfig{
			IPCReadSec:      DefaultGatewayIPCReadSec,
			IPCWriteSec:     DefaultGatewayIPCWriteSec,
			HTTPReadSec:     DefaultGatewayHTTPReadSec,
			HTTPWriteSec:    DefaultGatewayHTTPWriteSec,
			HTTPShutdownSec: DefaultGatewayHTTPShutdownSec,
		},
		Observability: GatewayObservabilityConfig{
			MetricsEnabled: boolPtr(DefaultGatewayMetricsEnabled),
		},
	}
}

// ApplyDefaults 为网关配置补齐默认值。
func (c *GatewayConfig) ApplyDefaults(defaults GatewayConfig) {
	if c == nil {
		return
	}

	c.Security.ApplyDefaults(defaults.Security)
	c.Limits.ApplyDefaults(defaults.Limits)
	c.Timeouts.ApplyDefaults(defaults.Timeouts)
	c.Observability.ApplyDefaults(defaults.Observability)
}

// Validate 校验网关配置合法性。
func (c GatewayConfig) Validate() error {
	if err := c.Security.Validate(); err != nil {
		return fmt.Errorf("security: %w", err)
	}
	if err := c.Limits.Validate(); err != nil {
		return fmt.Errorf("limits: %w", err)
	}
	if err := c.Timeouts.Validate(); err != nil {
		return fmt.Errorf("timeouts: %w", err)
	}
	if err := c.Observability.Validate(); err != nil {
		return fmt.Errorf("observability: %w", err)
	}
	return nil
}

// Clone 深拷贝网关配置。
func (c GatewayConfig) Clone() GatewayConfig {
	cloned := c
	cloned.Security = c.Security.Clone()
	cloned.Limits = c.Limits.Clone()
	cloned.Timeouts = c.Timeouts.Clone()
	cloned.Observability = c.Observability.Clone()
	return cloned
}

// ApplyDefaults 为安全配置补齐默认值。
func (c *GatewaySecurityConfig) ApplyDefaults(defaults GatewaySecurityConfig) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.ACLMode) == "" {
		c.ACLMode = defaults.ACLMode
	}
	if strings.TrimSpace(c.TokenFile) == "" {
		c.TokenFile = defaults.TokenFile
	}
	if len(c.AllowOrigins) == 0 {
		c.AllowOrigins = append([]string(nil), defaults.AllowOrigins...)
	} else {
		c.AllowOrigins = normalizeGatewayAllowOrigins(c.AllowOrigins)
	}
}

// Validate 校验安全配置。
func (c GatewaySecurityConfig) Validate() error {
	aclMode := strings.ToLower(strings.TrimSpace(c.ACLMode))
	if aclMode != "" && aclMode != DefaultGatewayACLMode {
		return fmt.Errorf("acl_mode must be %q", DefaultGatewayACLMode)
	}
	if strings.TrimSpace(c.TokenFile) != "" {
		cleaned := filepath.Clean(strings.TrimSpace(c.TokenFile))
		if cleaned == "." {
			return fmt.Errorf("token_file is invalid")
		}
	}
	for index, origin := range c.AllowOrigins {
		if strings.TrimSpace(origin) == "" {
			return fmt.Errorf("allow_origins[%d] is empty", index)
		}
	}
	return nil
}

// Clone 深拷贝安全配置。
func (c GatewaySecurityConfig) Clone() GatewaySecurityConfig {
	cloned := c
	cloned.AllowOrigins = append([]string(nil), c.AllowOrigins...)
	return cloned
}

// ApplyDefaults 为限流配置补齐默认值。
func (c *GatewayLimitsConfig) ApplyDefaults(defaults GatewayLimitsConfig) {
	if c == nil {
		return
	}
	if c.MaxFrameBytes <= 0 {
		c.MaxFrameBytes = defaults.MaxFrameBytes
	}
	if c.IPCMaxConnections <= 0 {
		c.IPCMaxConnections = defaults.IPCMaxConnections
	}
	if c.HTTPMaxRequestBytes <= 0 {
		c.HTTPMaxRequestBytes = defaults.HTTPMaxRequestBytes
	}
	if c.HTTPMaxStreamConnections <= 0 {
		c.HTTPMaxStreamConnections = defaults.HTTPMaxStreamConnections
	}
}

// Validate 校验限流配置。
func (c GatewayLimitsConfig) Validate() error {
	if c.MaxFrameBytes <= 0 {
		return fmt.Errorf("max_frame_bytes must be greater than 0")
	}
	if c.IPCMaxConnections <= 0 {
		return fmt.Errorf("ipc_max_connections must be greater than 0")
	}
	if c.HTTPMaxRequestBytes <= 0 {
		return fmt.Errorf("http_max_request_bytes must be greater than 0")
	}
	if c.HTTPMaxStreamConnections <= 0 {
		return fmt.Errorf("http_max_stream_connections must be greater than 0")
	}
	return nil
}

// Clone 复制限流配置。
func (c GatewayLimitsConfig) Clone() GatewayLimitsConfig {
	return c
}

// ApplyDefaults 为超时配置补齐默认值。
func (c *GatewayTimeoutsConfig) ApplyDefaults(defaults GatewayTimeoutsConfig) {
	if c == nil {
		return
	}
	if c.IPCReadSec <= 0 {
		c.IPCReadSec = defaults.IPCReadSec
	}
	if c.IPCWriteSec <= 0 {
		c.IPCWriteSec = defaults.IPCWriteSec
	}
	if c.HTTPReadSec <= 0 {
		c.HTTPReadSec = defaults.HTTPReadSec
	}
	if c.HTTPWriteSec <= 0 {
		c.HTTPWriteSec = defaults.HTTPWriteSec
	}
	if c.HTTPShutdownSec <= 0 {
		c.HTTPShutdownSec = defaults.HTTPShutdownSec
	}
}

// Validate 校验超时配置。
func (c GatewayTimeoutsConfig) Validate() error {
	if c.IPCReadSec <= 0 {
		return fmt.Errorf("ipc_read_sec must be greater than 0")
	}
	if c.IPCWriteSec <= 0 {
		return fmt.Errorf("ipc_write_sec must be greater than 0")
	}
	if c.HTTPReadSec <= 0 {
		return fmt.Errorf("http_read_sec must be greater than 0")
	}
	if c.HTTPWriteSec <= 0 {
		return fmt.Errorf("http_write_sec must be greater than 0")
	}
	if c.HTTPShutdownSec <= 0 {
		return fmt.Errorf("http_shutdown_sec must be greater than 0")
	}
	return nil
}

// Clone 复制超时配置。
func (c GatewayTimeoutsConfig) Clone() GatewayTimeoutsConfig {
	return c
}

// ApplyDefaults 为可观测性配置补齐默认值。
func (c *GatewayObservabilityConfig) ApplyDefaults(defaults GatewayObservabilityConfig) {
	if c == nil {
		return
	}
	if c.MetricsEnabled == nil {
		if defaults.MetricsEnabled != nil {
			c.MetricsEnabled = boolPtr(*defaults.MetricsEnabled)
			return
		}
		c.MetricsEnabled = boolPtr(DefaultGatewayMetricsEnabled)
	}
}

// Validate 校验可观测性配置。
func (c GatewayObservabilityConfig) Validate() error {
	return nil
}

// Clone 复制可观测性配置。
func (c GatewayObservabilityConfig) Clone() GatewayObservabilityConfig {
	cloned := c
	if c.MetricsEnabled != nil {
		cloned.MetricsEnabled = boolPtr(*c.MetricsEnabled)
	}
	return cloned
}

// Enabled 返回 metrics_enabled 的生效值，空值按默认开启处理。
func (c GatewayObservabilityConfig) Enabled() bool {
	if c.MetricsEnabled == nil {
		return DefaultGatewayMetricsEnabled
	}
	return *c.MetricsEnabled
}

// defaultGatewayAllowOrigins 返回网关默认允许的本地来源。
func defaultGatewayAllowOrigins() []string {
	return []string{"http://localhost", "http://127.0.0.1", "http://[::1]", "app://", "file://", "null"}
}

// normalizeGatewayAllowOrigins 归一化 allow_origins，去除空项与空白。
func normalizeGatewayAllowOrigins(origins []string) []string {
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func boolPtr(value bool) *bool {
	result := value
	return &result
}
