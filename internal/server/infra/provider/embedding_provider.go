package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatProvider interface {
	GetModelName() string
	Chat(ctx context.Context, messages []Message) (<-chan string, error)
}

type EmbeddingProvider interface {
	GetModelName() string
	Embed(ctx context.Context, text string) ([]float64, error)
}

type ProviderConfig struct {
	APIKey   string
	BaseURL  string
	Model    string
	Provider string
}

func NewChatProviderFromEnv(model string) (ChatProvider, error) {
	providerName := envValue("AI_PROVIDER")
	if providerName == "" {
		providerName = "modelscope"
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		cfg := ProviderConfig{
			APIKey:   envValue("AI_API_KEY", "MODELSCOPE_API_KEY"),
			BaseURL:  envValue("AI_BASE_URL", "MODELSCOPE_BASE_URL"),
			Model:    model,
			Provider: providerName,
		}

		if cfg.APIKey == "" {
			return nil, fmt.Errorf("missing AI_API_KEY")
		}
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("missing AI_BASE_URL")
		}

		return &ModelScopeProvider{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AI_PROVIDER: %s", providerName)
	}
}

func NewEmbeddingProviderFromEnv() (EmbeddingProvider, error) {
	providerName := envValue("EMBEDDING_PROVIDER")
	if providerName == "" {
		providerName = "modelscope"
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		cfg := ProviderConfig{
			APIKey:   envValue("EMBEDDING_API_KEY", "AI_API_KEY", "MODELSCOPE_API_KEY"),
			BaseURL:  envValue("EMBEDDING_BASE_URL"),
			Model:    envValue("EMBEDDING_MODEL"),
			Provider: providerName,
		}

		if cfg.APIKey == "" {
			return nil, fmt.Errorf("missing EMBEDDING_API_KEY")
		}
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("missing EMBEDDING_BASE_URL")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("missing EMBEDDING_MODEL")
		}

		return &ModelScopeEmbeddingProvider{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported EMBEDDING_PROVIDER: %s", providerName)
	}
}

func envValue(keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}
