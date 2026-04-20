package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

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
			client := newSDKClient(cfg)

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

// newSDKClient 构造 Anthropic SDK 客户端，供生成与模型发现链路共享连接配置。
func newSDKClient(cfg provider.RuntimeConfig) anthropic.Client {
	apiKey := strings.TrimSpace(cfg.APIKey)

	httpClient := &http.Client{
		Timeout: 90 * time.Second,
	}
	options := []anthroption.RequestOption{
		anthroption.WithHTTPClient(httpClient),
		anthroption.WithAPIKey(apiKey),
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		options = append(options, anthroption.WithBaseURL(strings.TrimSpace(cfg.BaseURL)))
	}
	return anthropic.NewClient(options...)
}

// validateCatalogIdentity 在 SDK 模式下不再限制 endpoint 相关字段。
func validateCatalogIdentity(identity provider.ProviderIdentity) error {
	_ = identity
	return nil
}
