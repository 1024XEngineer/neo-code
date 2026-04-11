package openaicompat

import (
	"context"
	"net/http"

	"neo-code/internal/provider"
	"neo-code/internal/provider/transport"
	providertypes "neo-code/internal/provider/types"
)

// DriverName 是当前 OpenAI-compatible 协议驱动的唯一标识。
const DriverName = provider.DriverOpenAICompat

// defaultRetryTransport 返回内置的带重试 HTTP Transport。
func defaultRetryTransport() http.RoundTripper {
	return transport.NewRetryTransport(http.DefaultTransport, transport.DefaultRetryConfig())
}

// Driver 返回 OpenAI-compatible 协议驱动定义。
func Driver() provider.DriverDefinition {
	return driverDefinition(DriverName)
}

// validateCatalogIdentity 复用 api_style 分流规则，在 catalog 快照与缓存路径上提前拒绝当前尚不支持的静态配置。
func validateCatalogIdentity(identity provider.ProviderIdentity) error {
	_, err := supportedAPIStyle(identity.APIStyle)
	return err
}

// driverDefinition 根据驱动名构造共享的 OpenAI-compatible 协议驱动定义。
func driverDefinition(name string) provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: name,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return New(cfg, withTransport(defaultRetryTransport()))
		},
		Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			p, err := New(cfg, withTransport(defaultRetryTransport()))
			if err != nil {
				return nil, err
			}
			return p.DiscoverModels(ctx)
		},
		ValidateCatalogIdentity: validateCatalogIdentity,
	}
}
