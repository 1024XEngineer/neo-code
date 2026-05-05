package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	// FeishuIngressWebhook 表示飞书回调 HTTP 入站模式。
	FeishuIngressWebhook = "webhook"
	// FeishuIngressSDK 表示飞书 SDK 长连接入站模式。
	FeishuIngressSDK = "sdk"
	// DefaultFeishuAdapterListen 定义飞书适配器默认监听地址。
	DefaultFeishuAdapterListen = "127.0.0.1:18080"
	// DefaultFeishuAdapterEventPath 定义飞书事件回调默认路径。
	DefaultFeishuAdapterEventPath = "/feishu/events"
	// DefaultFeishuAdapterCardPath 定义飞书审批卡片回调默认路径。
	DefaultFeishuAdapterCardPath = "/feishu/cards"
	// DefaultFeishuIdempotencyTTLSec 定义飞书事件去重 TTL 默认秒数。
	DefaultFeishuIdempotencyTTLSec = 600
	// DefaultFeishuGatewayRequestTimeoutSec 定义飞书适配器访问网关默认超时秒数。
	DefaultFeishuGatewayRequestTimeoutSec = 8
	// DefaultFeishuReconnectBackoffMinMs 定义飞书网关重连最小退避时间毫秒。
	DefaultFeishuReconnectBackoffMinMs = 500
	// DefaultFeishuReconnectBackoffMaxMs 定义飞书网关重连最大退避时间毫秒。
	DefaultFeishuReconnectBackoffMaxMs = 10000
	// DefaultFeishuRebindIntervalSec 定义飞书适配器重绑会话默认间隔秒数。
	DefaultFeishuRebindIntervalSec = 15
	// FeishuAppSecretEnvVar 定义飞书应用密钥固定环境变量名。
	FeishuAppSecretEnvVar = "FEISHU_APP_SECRET"
	// FeishuSigningSecretEnvVar 定义飞书签名密钥固定环境变量名。
	FeishuSigningSecretEnvVar = "FEISHU_SIGNING_SECRET"
)

// FeishuConfig 表示飞书适配器配置。
type FeishuConfig struct {
	Enabled                bool                      `yaml:"enabled,omitempty"`
	Ingress                string                    `yaml:"ingress,omitempty"`
	AppID                  string                    `yaml:"app_id,omitempty"`
	AppSecret              string                    `yaml:"-"`
	BotUserID              string                    `yaml:"bot_user_id,omitempty"`
	BotOpenID              string                    `yaml:"bot_open_id,omitempty"`
	VerifyToken            string                    `yaml:"verify_token,omitempty"`
	SigningSecret          string                    `yaml:"-"`
	InsecureSkipSignVerify bool                      `yaml:"insecure_skip_signature_verify,omitempty"`
	Adapter                FeishuAdapterConfig       `yaml:"adapter,omitempty"`
	GatewayClient          FeishuGatewayClientConfig `yaml:"gateway,omitempty"`
	RequestTimeoutSec      int                       `yaml:"request_timeout_sec,omitempty"`
	IdempotencyTTLSec      int                       `yaml:"idempotency_ttl_sec,omitempty"`
	ReconnectBackoffMinM   int                       `yaml:"reconnect_backoff_min_ms,omitempty"`
	ReconnectBackoffMaxM   int                       `yaml:"reconnect_backoff_max_ms,omitempty"`
	RebindIntervalSec      int                       `yaml:"rebind_interval_sec,omitempty"`
}

// FeishuAdapterConfig 表示飞书适配器 HTTP 服务配置。
type FeishuAdapterConfig struct {
	Listen   string `yaml:"listen,omitempty"`
	EventURI string `yaml:"event_path,omitempty"`
	CardURI  string `yaml:"card_path,omitempty"`
}

// FeishuGatewayClientConfig 表示飞书适配器访问网关时的连接配置。
type FeishuGatewayClientConfig struct {
	ListenAddress string `yaml:"listen,omitempty"`
	TokenFile     string `yaml:"token_file,omitempty"`
}

// defaultFeishuConfig 返回飞书配置默认值。
func defaultFeishuConfig() FeishuConfig {
	return FeishuConfig{
		Ingress: FeishuIngressWebhook,
		Adapter: FeishuAdapterConfig{
			Listen:   DefaultFeishuAdapterListen,
			EventURI: DefaultFeishuAdapterEventPath,
			CardURI:  DefaultFeishuAdapterCardPath,
		},
		RequestTimeoutSec:    DefaultFeishuGatewayRequestTimeoutSec,
		IdempotencyTTLSec:    DefaultFeishuIdempotencyTTLSec,
		ReconnectBackoffMinM: DefaultFeishuReconnectBackoffMinMs,
		ReconnectBackoffMaxM: DefaultFeishuReconnectBackoffMaxMs,
		RebindIntervalSec:    DefaultFeishuRebindIntervalSec,
	}
}

// ApplyDefaults 为飞书配置补齐默认值。
func (c *FeishuConfig) ApplyDefaults(defaults FeishuConfig) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Ingress) == "" {
		c.Ingress = defaults.Ingress
	}
	if strings.TrimSpace(c.Adapter.Listen) == "" {
		c.Adapter.Listen = defaults.Adapter.Listen
	}
	if strings.TrimSpace(c.Adapter.EventURI) == "" {
		c.Adapter.EventURI = defaults.Adapter.EventURI
	}
	if strings.TrimSpace(c.Adapter.CardURI) == "" {
		c.Adapter.CardURI = defaults.Adapter.CardURI
	}
	if c.RequestTimeoutSec <= 0 {
		c.RequestTimeoutSec = defaults.RequestTimeoutSec
	}
	if c.IdempotencyTTLSec <= 0 {
		c.IdempotencyTTLSec = defaults.IdempotencyTTLSec
	}
	if c.ReconnectBackoffMinM <= 0 {
		c.ReconnectBackoffMinM = defaults.ReconnectBackoffMinM
	}
	if c.ReconnectBackoffMaxM <= 0 {
		c.ReconnectBackoffMaxM = defaults.ReconnectBackoffMaxM
	}
	if c.RebindIntervalSec <= 0 {
		c.RebindIntervalSec = defaults.RebindIntervalSec
	}
}

// Clone 深拷贝飞书配置。
func (c FeishuConfig) Clone() FeishuConfig {
	return c
}

// Validate 校验飞书配置合法性。
func (c FeishuConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	ingress := strings.TrimSpace(strings.ToLower(c.Ingress))
	if ingress == "" {
		ingress = FeishuIngressWebhook
	}
	if ingress != FeishuIngressWebhook && ingress != FeishuIngressSDK {
		return fmt.Errorf("ingress must be one of %q or %q", FeishuIngressWebhook, FeishuIngressSDK)
	}
	if strings.TrimSpace(c.AppID) == "" {
		return fmt.Errorf("app_id is required when feishu.enabled=true")
	}
	if strings.TrimSpace(resolveFeishuSecret(c.AppSecret, FeishuAppSecretEnvVar)) == "" {
		return fmt.Errorf("%s is required when feishu.enabled=true", FeishuAppSecretEnvVar)
	}
	if ingress == FeishuIngressWebhook {
		if strings.TrimSpace(c.VerifyToken) == "" {
			return fmt.Errorf("verify_token is required when feishu.enabled=true and ingress=webhook")
		}
		if !c.InsecureSkipSignVerify && strings.TrimSpace(resolveFeishuSecret(c.SigningSecret, FeishuSigningSecretEnvVar)) == "" {
			return fmt.Errorf("%s is required when feishu.enabled=true and ingress=webhook unless insecure_skip_signature_verify=true", FeishuSigningSecretEnvVar)
		}
		if strings.TrimSpace(c.Adapter.Listen) == "" {
			return fmt.Errorf("adapter.listen is required when feishu.enabled=true and ingress=webhook")
		}
		if strings.TrimSpace(c.Adapter.EventURI) == "" {
			return fmt.Errorf("adapter.event_path is required when feishu.enabled=true and ingress=webhook")
		}
		if strings.TrimSpace(c.Adapter.CardURI) == "" {
			return fmt.Errorf("adapter.card_path is required when feishu.enabled=true and ingress=webhook")
		}
	}
	if c.RequestTimeoutSec <= 0 {
		return fmt.Errorf("request_timeout_sec must be greater than 0")
	}
	if c.IdempotencyTTLSec <= 0 {
		return fmt.Errorf("idempotency_ttl_sec must be greater than 0")
	}
	if c.ReconnectBackoffMinM <= 0 || c.ReconnectBackoffMaxM <= 0 {
		return fmt.Errorf("reconnect_backoff_min_ms/max_ms must be greater than 0")
	}
	if c.ReconnectBackoffMinM > c.ReconnectBackoffMaxM {
		return fmt.Errorf("reconnect_backoff_min_ms must be less than or equal to reconnect_backoff_max_ms")
	}
	if c.RebindIntervalSec <= 0 {
		return fmt.Errorf("rebind_interval_sec must be greater than 0")
	}
	return nil
}

// resolveFeishuSecret 按运行时优先级解析飞书密钥，允许启动流程先注入内存值，再回退到固定环境变量。
func resolveFeishuSecret(current string, envName string) string {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}
	return strings.TrimSpace(os.Getenv(envName))
}
