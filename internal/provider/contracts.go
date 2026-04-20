package provider

import (
	"context"

	providertypes "neo-code/internal/provider/types"
)

// RuntimeConfig 表示 provider 构建与模型发现使用的最小运行时输入。
type RuntimeConfig struct {
	Name                  string
	Driver                string
	BaseURL               string
	DefaultModel          string
	APIKey                string
	SessionAssetLimits    providertypes.SessionAssetLimits
	ChatAPIMode           string
	ChatEndpointPath      string
	DiscoveryEndpointPath string
}

// Provider 定义模型生成能力，通过 channel 推送流式事件给上层消费。
type Provider interface {
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
