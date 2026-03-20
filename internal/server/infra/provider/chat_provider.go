package provider

import (
	"fmt"
	"os"
	"strings"

	"go-llm-demo/internal/server/domain"
)

func NewChatProviderFromEnv(model string) (domain.ChatProvider, error) {
	providerName := envValue("AI_PROVIDER")
	if providerName == "" {
		providerName = "modelscope"
	}

	apiKey := envValue("AI_API_KEY", "MODELSCOPE_API_KEY")
	baseURL := envValue("AI_BASE_URL", "MODELSCOPE_BASE_URL")

	if apiKey == "" {
		return nil, fmt.Errorf("missing AI_API_KEY")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("missing AI_BASE_URL")
	}
	if model == "" {
		return nil, fmt.Errorf("missing AI_MODEL")
	}

	switch strings.ToLower(providerName) {
	case "modelscope":
		return &ModelScopeProvider{
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   model,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported AI_PROVIDER: %s", providerName)
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
