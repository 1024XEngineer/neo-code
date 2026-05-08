package config

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultRunnerGatewayAddress 定义 runner 连接网关的默认地址。
	DefaultRunnerGatewayAddress = "127.0.0.1:8080"
	// DefaultRunnerTokenFile 定义 runner 认证 token 文件默认路径。
	DefaultRunnerTokenFile = ""
	// DefaultRunnerHeartbeatIntervalSec 定义 runner 心跳间隔默认秒数。
	DefaultRunnerHeartbeatIntervalSec = 10
	// DefaultRunnerReconnectBackoffMinMs 定义 runner 重连最小退避毫秒。
	DefaultRunnerReconnectBackoffMinMs = 500
	// DefaultRunnerReconnectBackoffMaxMs 定义 runner 重连最大退避毫秒。
	DefaultRunnerReconnectBackoffMaxMs = 10000
	// DefaultRunnerRequestTimeoutSec 定义 runner 请求超时秒数。
	DefaultRunnerRequestTimeoutSec = 30
)

// RunnerConfig 表示本地 runner 的配置。
type RunnerConfig struct {
	Enabled              bool     `yaml:"enabled,omitempty"`
	GatewayAddress       string   `yaml:"gateway_address,omitempty"`
	TokenFile            string   `yaml:"token_file,omitempty"`
	RunnerID             string   `yaml:"runner_id,omitempty"`
	RunnerName           string   `yaml:"runner_name,omitempty"`
	WorkdirAllowlist     []string `yaml:"workdir_allowlist,omitempty"`
	HeartbeatIntervalSec int      `yaml:"-"`
	ReconnectBackoffMinM int      `yaml:"-"`
	ReconnectBackoffMaxM int      `yaml:"-"`
	RequestTimeoutSec    int      `yaml:"-"`
}

// defaultRunnerConfig 返回 runner 配置默认值。
func defaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		GatewayAddress:       DefaultRunnerGatewayAddress,
		HeartbeatIntervalSec: DefaultRunnerHeartbeatIntervalSec,
		ReconnectBackoffMinM: DefaultRunnerReconnectBackoffMinMs,
		ReconnectBackoffMaxM: DefaultRunnerReconnectBackoffMaxMs,
		RequestTimeoutSec:    DefaultRunnerRequestTimeoutSec,
	}
}

// ApplyDefaults 为 runner 配置补齐默认值。
func (c *RunnerConfig) ApplyDefaults(defaults RunnerConfig) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.GatewayAddress) == "" {
		c.GatewayAddress = defaults.GatewayAddress
	}
	if c.HeartbeatIntervalSec <= 0 {
		c.HeartbeatIntervalSec = defaults.HeartbeatIntervalSec
	}
	if c.ReconnectBackoffMinM <= 0 {
		c.ReconnectBackoffMinM = defaults.ReconnectBackoffMinM
	}
	if c.ReconnectBackoffMaxM <= 0 {
		c.ReconnectBackoffMaxM = defaults.ReconnectBackoffMaxM
	}
	if c.RequestTimeoutSec <= 0 {
		c.RequestTimeoutSec = defaults.RequestTimeoutSec
	}
}

// Clone 深拷贝 runner 配置。
func (c RunnerConfig) Clone() RunnerConfig {
	clone := c
	if c.WorkdirAllowlist != nil {
		clone.WorkdirAllowlist = make([]string, len(c.WorkdirAllowlist))
		copy(clone.WorkdirAllowlist, c.WorkdirAllowlist)
	}
	return clone
}

// Validate 校验 runner 配置合法性。
func (c RunnerConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.GatewayAddress) == "" {
		return fmt.Errorf("gateway_address is required when runner.enabled=true")
	}
	if c.HeartbeatIntervalSec <= 0 {
		return fmt.Errorf("heartbeat_interval_sec must be greater than 0")
	}
	if c.ReconnectBackoffMinM <= 0 || c.ReconnectBackoffMaxM <= 0 {
		return fmt.Errorf("reconnect_backoff_min_ms/max_ms must be greater than 0")
	}
	if c.ReconnectBackoffMinM > c.ReconnectBackoffMaxM {
		return fmt.Errorf("reconnect_backoff_min_ms must be less than or equal to reconnect_backoff_max_ms")
	}
	if c.RequestTimeoutSec <= 0 {
		return fmt.Errorf("request_timeout_sec must be greater than 0")
	}
	return nil
}

// HeartbeatInterval returns the heartbeat interval as time.Duration.
func (c RunnerConfig) HeartbeatInterval() time.Duration {
	if c.HeartbeatIntervalSec <= 0 {
		return time.Duration(DefaultRunnerHeartbeatIntervalSec) * time.Second
	}
	return time.Duration(c.HeartbeatIntervalSec) * time.Second
}

// ReconnectBackoffMin returns the min reconnect backoff as time.Duration.
func (c RunnerConfig) ReconnectBackoffMin() time.Duration {
	if c.ReconnectBackoffMinM <= 0 {
		return time.Duration(DefaultRunnerReconnectBackoffMinMs) * time.Millisecond
	}
	return time.Duration(c.ReconnectBackoffMinM) * time.Millisecond
}

// ReconnectBackoffMax returns the max reconnect backoff as time.Duration.
func (c RunnerConfig) ReconnectBackoffMax() time.Duration {
	if c.ReconnectBackoffMaxM <= 0 {
		return time.Duration(DefaultRunnerReconnectBackoffMaxMs) * time.Millisecond
	}
	return time.Duration(c.ReconnectBackoffMaxM) * time.Millisecond
}

// RequestTimeout returns the request timeout as time.Duration.
func (c RunnerConfig) RequestTimeout() time.Duration {
	if c.RequestTimeoutSec <= 0 {
		return time.Duration(DefaultRunnerRequestTimeoutSec) * time.Second
	}
	return time.Duration(c.RequestTimeoutSec) * time.Second
}
