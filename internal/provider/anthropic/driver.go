package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthroption "github.com/anthropics/anthropic-sdk-go/option"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// DriverName 是 Anthropic 协议驱动的唯一标识。
const DriverName = provider.DriverAnthropic

// Driver 返回 Anthropic 协议驱动定义。
func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return New(cfg)
		},
		Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			client, err := newDiscoverySDKClient(cfg)
			if err != nil {
				return nil, err
			}

			descriptors := make([]providertypes.ModelDescriptor, 0, 64)
			pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
			for pager.Next() {
				model := pager.Current()
				modelID := strings.TrimSpace(model.ID)
				if modelID == "" {
					continue
				}
				displayName := strings.TrimSpace(model.DisplayName)
				if displayName == "" {
					displayName = modelID
				}
				descriptors = append(descriptors, providertypes.ModelDescriptor{
					ID:              modelID,
					Name:            displayName,
					ContextWindow:   int(model.MaxInputTokens),
					MaxOutputTokens: int(model.MaxTokens),
				})
			}
			if err := pager.Err(); err != nil {
				return nil, fmt.Errorf("%sdiscover models via sdk: %w", errorPrefix, err)
			}
			return providertypes.MergeModelDescriptors(descriptors), nil
		},
		ValidateCatalogIdentity: validateCatalogIdentity,
	}
}

// newDiscoverySDKClient 构造模型发现使用的 Anthropic SDK 客户端。
func newDiscoverySDKClient(cfg provider.RuntimeConfig) (anthropic.Client, error) {
	return newSDKClient(cfg, true)
}

// newGenerateSDKClient 构造生成链路使用的 Anthropic SDK 客户端，并关闭 SDK 内建重试。
func newGenerateSDKClient(cfg provider.RuntimeConfig) (anthropic.Client, error) {
	return newSDKClient(cfg, false)
}

// newSDKClient 根据调用场景构造 Anthropic SDK 客户端，避免生成链路被底层超时与重试抢占控制权。
func newSDKClient(cfg provider.RuntimeConfig, discovery bool) (anthropic.Client, error) {
	apiKey, err := cfg.ResolveAPIKeyValue()
	if err != nil {
		return anthropic.Client{}, err
	}

	httpClient := &http.Client{}
	if discovery {
		httpClient.Timeout = provider.DefaultSDKRequestTimeout
	}

	options := []anthroption.RequestOption{
		anthroption.WithHTTPClient(httpClient),
		anthroption.WithAPIKey(apiKey),
	}
	if !discovery {
		options = append(options, anthroption.WithMaxRetries(0))
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		options = append(options, anthroption.WithBaseURL(strings.TrimSpace(cfg.BaseURL)))
	}
	return anthropic.NewClient(options...), nil
}

// validateCatalogIdentity 在 SDK 模式下不再限制 endpoint 相关字段。
func validateCatalogIdentity(identity provider.ProviderIdentity) error {
	_ = identity
	return nil
}
