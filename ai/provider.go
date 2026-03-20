package ai

import (
	"context"
	"fmt"
	"neo-code/config"
	"strings"
)

// ChatProvider定义Chat模型接口
type ChatProvider interface {
	GetModelName() string
	Chat(ctx context.Context, messages []Message) (<-chan string, error)
}

// EmbeddingProvider 定义了嵌入模型的接口
type EmbeddingProvider interface {
	GetModelName() string
	Embed(ctx context.Context, text string) ([]float64, error)
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ProviderConfig struct {
	APIKey   string
	BaseURL  string
	Model    string
	Provider string
}

func NewChatProviderFromEnv(model string) (ChatProvider, error) {
	if config.GlobalAppConfig == nil {
		return nil, fmt.Errorf("config.yaml is not loaded")
	}

	providerName := strings.TrimSpace(config.GlobalAppConfig.AI.Provider)
	if providerName == "" {
		providerName = "modelscope"
	}

	if model == "" {
		model = strings.TrimSpace(config.GlobalAppConfig.AI.Model)
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		apiKey := strings.TrimSpace(config.GlobalAppConfig.AI.APIKey)

		cfg := ProviderConfig{
			APIKey:   apiKey,
			BaseURL:  "",
			Model:    model,
			Provider: providerName,
		}

		if cfg.APIKey == "" {
			return nil, fmt.Errorf("missing ai.api_key in config.yaml")
		}

		modelName := cfg.Model
		if modelName == "" {
			modelName = DefaultModel()
		}

		return &ModelScopeProvider{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   modelName,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported ai.provider: %s", providerName)
	}
}

func NewEmbeddingProviderFromEnv() (EmbeddingProvider, error) {
	if config.GlobalAppConfig == nil {
		return nil, fmt.Errorf("config.yaml is not loaded")
	}

	providerName := strings.TrimSpace(config.GlobalAppConfig.Embedding.Provider)
	if providerName == "" {
		providerName = "modelscope"
	}

	embeddingModel := strings.TrimSpace(config.GlobalAppConfig.Embedding.Model)

	switch strings.ToLower(providerName) {
	case "modelscope":
		apiKey := strings.TrimSpace(config.GlobalAppConfig.Embedding.APIKey)

		cfg := ProviderConfig{
			APIKey:   apiKey,
			BaseURL:  "",
			Model:    embeddingModel,
			Provider: providerName,
		}

		if cfg.APIKey == "" {
			return nil, fmt.Errorf("missing embedding.api_key in config.yaml")
		}

		embeddingModelName := cfg.Model
		if embeddingModelName == "" {
			embeddingModelName = config.GetDefaultEmbeddingModel()
		}

		return &ModelScopeEmbeddingProvider{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   embeddingModelName,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported embedding.provider: %s", providerName)
	}
}
