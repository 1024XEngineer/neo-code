package gemini

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// DriverName 是 Gemini 协议驱动的唯一标识。
const DriverName = provider.DriverGemini

// Driver 返回 Gemini 协议驱动定义。
func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return New(cfg)
		},
		Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			client, err := newDiscoverySDKClient(ctx, cfg)
			if err != nil {
				return nil, err
			}

			descriptors := make([]providertypes.ModelDescriptor, 0, 64)
			for model, iterErr := range client.Models.All(ctx) {
				if iterErr != nil {
					return nil, fmt.Errorf("%sdiscover models via sdk: %w", errorPrefix, iterErr)
				}
				if model == nil {
					continue
				}

				modelID := strings.TrimSpace(strings.TrimPrefix(model.Name, "models/"))
				if modelID == "" {
					modelID = strings.TrimSpace(model.Name)
				}
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
					Description:     strings.TrimSpace(model.Description),
					ContextWindow:   int(model.InputTokenLimit),
					MaxOutputTokens: int(model.OutputTokenLimit),
				})
			}
			return providertypes.MergeModelDescriptors(descriptors), nil
		},
		ValidateCatalogIdentity: validateCatalogIdentity,
	}
}

// newDiscoverySDKClient 构造模型发现使用的 Gemini SDK 客户端。
func newDiscoverySDKClient(ctx context.Context, cfg provider.RuntimeConfig) (*genai.Client, error) {
	return newSDKClient(ctx, cfg, true)
}

// newGenerateSDKClient 构造生成链路使用的 Gemini SDK 客户端。
func newGenerateSDKClient(ctx context.Context, cfg provider.RuntimeConfig) (*genai.Client, error) {
	return newSDKClient(ctx, cfg, false)
}

// newSDKClient 根据调用场景构造 Gemini SDK 客户端，避免生成链路被底层总超时抢占控制权。
func newSDKClient(ctx context.Context, cfg provider.RuntimeConfig, discovery bool) (*genai.Client, error) {
	apiKey, err := cfg.ResolveAPIKeyValue()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{}
	if discovery {
		httpClient.Timeout = provider.DefaultSDKRequestTimeout
	}
	clientConfig := &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpClient,
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL:    strings.TrimSpace(cfg.BaseURL),
			APIVersion: "/",
		}
	}
	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("%screate sdk client: %w", errorPrefix, err)
	}
	return client, nil
}

// validateCatalogIdentity 在 SDK 模式下不再限制 endpoint 相关字段。
func validateCatalogIdentity(identity provider.ProviderIdentity) error {
	_ = identity
	return nil
}
