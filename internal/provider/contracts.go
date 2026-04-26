package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

// APIKeyResolver 定义 provider 在真正发请求前解析 API Key 的能力。
type APIKeyResolver func(envName string) (string, error)

// RuntimeConfig 表示 provider 构建与模型发现使用的最小运行时输入。
type RuntimeConfig struct {
	Name                  string
	Driver                string
	BaseURL               string
	DefaultModel          string
	APIKeyEnv             string
	APIKeyResolver        APIKeyResolver
	SessionAssetPolicy    session.AssetPolicy
	RequestAssetBudget    RequestAssetBudget
	ChatAPIMode           string
	ChatEndpointPath      string
	DiscoveryEndpointPath string
	GenerateMaxRetries    int
	GenerateStartTimeout  time.Duration
	GenerateIdleTimeout   time.Duration
}

// ResolveAPIKeyValue 在 provider 即将发起请求前解析当前配置引用的 API Key。
func (c RuntimeConfig) ResolveAPIKeyValue() (string, error) {
	envName := strings.TrimSpace(c.APIKeyEnv)
	if envName == "" {
		if strings.TrimSpace(c.Name) == "" {
			return "", errors.New("provider runtime config: api_key_env is empty")
		}
		return "", fmt.Errorf("provider runtime config: provider %q api_key_env is empty", strings.TrimSpace(c.Name))
	}

	if c.APIKeyResolver != nil {
		value, err := c.APIKeyResolver(envName)
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return "", fmt.Errorf("provider runtime config: environment variable %s is empty", envName)
		}
		return trimmed, nil
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return "", fmt.Errorf("provider runtime config: environment variable %s is empty", envName)
	}
	return value, nil
}

// ResolvedGenerateMaxRetries 返回运行时已归一化的生成重试次数。
func (c RuntimeConfig) ResolvedGenerateMaxRetries() int {
	return NormalizeGenerateMaxRetries(c.GenerateMaxRetries)
}

// ResolvedGenerateStartTimeout 返回运行时已归一化的生成首包超时。
func (c RuntimeConfig) ResolvedGenerateStartTimeout() time.Duration {
	return NormalizeGenerateStartTimeout(c.GenerateStartTimeout)
}

// ResolvedGenerateIdleTimeout 返回运行时已归一化的生成空闲超时。
func (c RuntimeConfig) ResolvedGenerateIdleTimeout() time.Duration {
	return NormalizeGenerateIdleTimeout(c.GenerateIdleTimeout)
}

// StaticAPIKeyResolver 返回一个仅供测试和受控注入场景使用的固定密钥解析器。
func StaticAPIKeyResolver(apiKey string) APIKeyResolver {
	trimmed := strings.TrimSpace(apiKey)
	return func(_ string) (string, error) {
		if trimmed == "" {
			return "", errors.New("provider runtime config: static api key is empty")
		}
		return trimmed, nil
	}
}

// Provider 定义模型生成能力，通过 channel 推送流式事件给上层消费。
type Provider interface {
	EstimateInputTokens(ctx context.Context, req providertypes.GenerateRequest) (providertypes.BudgetEstimate, error)
	Generate(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error
}

// CatalogInput 汇总 provider/catalog 查询、发现与缓存所需的最小输入。
type CatalogInput struct {
	Identity               ProviderIdentity
	ConfiguredModels       []providertypes.ModelDescriptor
	DefaultModels          []providertypes.ModelDescriptor
	DisableDiscovery       bool
	ResolveDiscoveryConfig func() (RuntimeConfig, error)
}
