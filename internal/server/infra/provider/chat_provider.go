package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
)

var (
	ErrInvalidAPIKey        = errors.New("invalid api key")
	ErrAPIKeyValidationSoft = errors.New("api key validation uncertain")
)

// NewChatProvider 为指定模型创建已配置的聊天提供方。
func NewChatProvider(model string) (domain.ChatProvider, error) {
	if configs.GlobalAppConfig == nil {
		return nil, fmt.Errorf("config.yaml is not loaded")
	}

	providerName := CurrentProvider()
	if model == "" {
		model = DefaultModel()
	}
	if model == "" {
		return nil, fmt.Errorf("ai.model is required for provider %s", providerName)
	}
	baseURL, err := ResolveChatEndpoint(configs.GlobalAppConfig, model)
	if err != nil {
		return nil, err
	}
	apiKey := configs.RuntimeAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("missing %s environment variable", configs.RuntimeAPIKeyEnvVarName())
	}

	return &ChatCompletionProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

// ValidateChatAPIKey 按当前提供方配置校验运行时 API Key。
func ValidateChatAPIKey(ctx context.Context, cfg *configs.AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	providerName := providerNameFromConfig(cfg)
	if providerName == "" {
		return fmt.Errorf("unsupported ai.provider: %s", cfg.AI.Provider)
	}
	if strings.TrimSpace(cfg.AI.Model) == "" {
		return fmt.Errorf("ai.model is required for provider %s", providerName)
	}

	return validateModelScopeAPIKey(ctx, cfg)
}
