package provider_test

import (
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	"github.com/dust/neo-code/internal/provider/anthropic"
	"github.com/dust/neo-code/internal/provider/openai"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	t.Parallel()

	openAIProvider, err := openai.New(config.ProviderConfig{
		Name:      config.ProviderOpenAI,
		Type:      config.ProviderOpenAI,
		BaseURL:   config.DefaultOpenAIBaseURL,
		Model:     config.DefaultOpenAIModel,
		APIKeyEnv: config.DefaultOpenAIAPIKeyEnv,
	})
	if err != nil {
		t.Fatalf("openai.New() error = %v", err)
	}
	anthropicProvider := anthropic.New(config.ProviderConfig{
		Name:      config.ProviderAnthropic,
		Type:      config.ProviderAnthropic,
		BaseURL:   config.DefaultAnthropicBaseURL,
		Model:     config.DefaultAnthropicModel,
		APIKeyEnv: config.DefaultAnthropicAPIKeyEnv,
	})

	registry := provider.NewRegistry()
	registry.Register(nil)
	registry.Register(openAIProvider)
	registry.Register(anthropicProvider)

	tests := []struct {
		name       string
		lookup     string
		expectName string
	}{
		{
			name:       "gets openai provider case insensitively",
			lookup:     "OPENAI",
			expectName: config.ProviderOpenAI,
		},
		{
			name:       "gets anthropic provider case insensitively",
			lookup:     "Anthropic",
			expectName: config.ProviderAnthropic,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := registry.Get(tt.lookup)
			if err != nil {
				t.Fatalf("Get(%q) error = %v", tt.lookup, err)
			}
			if got == nil || !strings.EqualFold(got.Name(), tt.expectName) {
				t.Fatalf("expected provider %q, got %+v", tt.expectName, got)
			}
		})
	}
}

func TestRegistryGetMissingProvider(t *testing.T) {
	t.Parallel()

	registry := provider.NewRegistry()
	_, err := registry.Get("missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}
