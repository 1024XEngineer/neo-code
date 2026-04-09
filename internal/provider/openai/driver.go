package openai

import (
	"context"
	"net/http"

	"neo-code/internal/provider"
	"neo-code/internal/provider/transport"
	providertypes "neo-code/internal/provider/types"
)

// DriverName 是 OpenAI 驱动的注册标识。
const DriverName = "openai"

// defaultRetryTransport 返回内置的带重试的 HTTP Transport。
func defaultRetryTransport() http.RoundTripper {
	return transport.NewRetryTransport(http.DefaultTransport, transport.DefaultRetryConfig())
}

// Driver 返回 OpenAI 协议驱动的定义，供 Registry 注册使用。
func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
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
		Capabilities: provider.DriverCapabilities{
			Streaming:           true,
			ToolTransport:       true,
			ModelDiscovery:      true,
			ImageInputTransport: false,
		},
	}
}
