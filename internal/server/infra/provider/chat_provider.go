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

	providerName := strings.TrimSpace(configs.GlobalAppConfig.AI.Provider)
	if providerName == "" {
		providerName = "modelscope"
	}
	if model == "" {
		model = strings.TrimSpace(configs.GlobalAppConfig.AI.Model)
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		apiKey := configs.RuntimeAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("missing %s environment variable", configs.APIKeyEnvVar)
		}
		modelName := model
		if modelName == "" {
			modelName = DefaultModel()
		}
		return &ModelScopeProvider{
			APIKey: apiKey,
			Model:  modelName,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported ai.provider: %s", providerName)
	}
}

// ValidateChatAPIKey 按当前提供方配置校验运行时 API Key。
func ValidateChatAPIKey(ctx context.Context, cfg *configs.AppConfiguration) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	providerName := strings.TrimSpace(cfg.AI.Provider)
	if providerName == "" {
		providerName = "modelscope"
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		return validateModelScopeAPIKey(ctx, cfg)
	default:
		return fmt.Errorf("unsupported ai.provider: %s", providerName)
	}
}
