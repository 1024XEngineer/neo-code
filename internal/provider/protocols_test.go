package provider

import "testing"

func TestNormalizeProviderProtocolSettingsLegacyMapping(t *testing.T) {
	t.Parallel()

	settings, err := NormalizeProviderProtocolSettings(
		DriverOpenAICompat,
		"",
		"",
		"",
		"",
		"",
		"",
		OpenAICompatibleAPIStyleResponses,
		DiscoveryResponseProfileGeneric,
	)
	if err != nil {
		t.Fatalf("NormalizeProviderProtocolSettings() error = %v", err)
	}

	if settings.ChatProtocol != ChatProtocolOpenAIResponses {
		t.Fatalf("expected chat protocol %q, got %q", ChatProtocolOpenAIResponses, settings.ChatProtocol)
	}
	if settings.ResponseProfile != DiscoveryResponseProfileGeneric {
		t.Fatalf("expected response profile %q, got %q", DiscoveryResponseProfileGeneric, settings.ResponseProfile)
	}
	if settings.DiscoveryEndpointPath != DiscoveryEndpointPathModels {
		t.Fatalf("expected discovery endpoint path %q, got %q", DiscoveryEndpointPathModels, settings.DiscoveryEndpointPath)
	}
}

func TestNormalizeProviderProtocolSettingsRejectsIllegalCombination(t *testing.T) {
	t.Parallel()

	_, err := NormalizeProviderProtocolSettings(
		DriverAnthropic,
		ChatProtocolAnthropicMessages,
		"",
		DiscoveryProtocolAnthropicModels,
		"",
		AuthStrategyBearer,
		DiscoveryResponseProfileGeneric,
		"",
		"",
	)
	if err == nil {
		t.Fatal("expected illegal chat/auth combination to fail")
	}
}

func TestNormalizeProviderProtocolSettingsUnknownDriverKeepsLegacyAPIStyleEmpty(t *testing.T) {
	t.Parallel()

	settings, err := NormalizeProviderProtocolSettings(
		"custom-driver",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("NormalizeProviderProtocolSettings() error = %v", err)
	}
	if settings.LegacyAPIStyle != "" {
		t.Fatalf("expected unknown driver to keep legacy api style empty, got %q", settings.LegacyAPIStyle)
	}
}

func TestNormalizeProviderProtocolSettingsRejectsUnsupportedEnums(t *testing.T) {
	t.Parallel()

	t.Run("unsupported chat protocol", func(t *testing.T) {
		t.Parallel()

		_, err := NormalizeProviderProtocolSettings(
			DriverOpenAICompat,
			"unsupported-chat",
			"",
			DiscoveryProtocolOpenAIModels,
			"",
			AuthStrategyBearer,
			DiscoveryResponseProfileOpenAI,
			"",
			"",
		)
		if err == nil {
			t.Fatal("expected unsupported chat protocol error")
		}
	})

	t.Run("unsupported discovery protocol", func(t *testing.T) {
		t.Parallel()

		_, err := NormalizeProviderProtocolSettings(
			DriverOpenAICompat,
			ChatProtocolOpenAIChatCompletions,
			"",
			"unsupported-discovery",
			"",
			AuthStrategyBearer,
			DiscoveryResponseProfileOpenAI,
			"",
			"",
		)
		if err == nil {
			t.Fatal("expected unsupported discovery protocol error")
		}
	})

	t.Run("unsupported auth strategy", func(t *testing.T) {
		t.Parallel()

		_, err := NormalizeProviderProtocolSettings(
			DriverOpenAICompat,
			ChatProtocolOpenAIChatCompletions,
			"",
			DiscoveryProtocolOpenAIModels,
			"",
			"unsupported-auth",
			DiscoveryResponseProfileOpenAI,
			"",
			"",
		)
		if err == nil {
			t.Fatal("expected unsupported auth strategy error")
		}
	})
}

func TestNormalizeProviderProtocolSettingsDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		driver                 string
		chatProtocol           string
		discoveryProtocol      string
		wantChatEndpoint       string
		wantDiscoveryEndpoint  string
		wantAuthStrategy       string
		wantResponseProfile    string
	}{
		{
			name:                  "openai defaults",
			driver:                DriverOpenAICompat,
			wantChatEndpoint:      "/chat/completions",
			wantDiscoveryEndpoint: "/models",
			wantAuthStrategy:      AuthStrategyBearer,
			wantResponseProfile:   DiscoveryResponseProfileOpenAI,
		},
		{
			name:                  "anthropic defaults",
			driver:                DriverAnthropic,
			wantChatEndpoint:      "/messages",
			wantDiscoveryEndpoint: "/models",
			wantAuthStrategy:      AuthStrategyAnthropic,
			wantResponseProfile:   DiscoveryResponseProfileGeneric,
		},
		{
			name:                  "gemini responses endpoint mapping",
			driver:                DriverGemini,
			chatProtocol:          ChatProtocolOpenAIResponses,
			discoveryProtocol:     DiscoveryProtocolGeminiModels,
			wantChatEndpoint:      "/responses",
			wantDiscoveryEndpoint: "/models",
			wantAuthStrategy:      AuthStrategyBearer,
			wantResponseProfile:   DiscoveryResponseProfileGemini,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings, err := NormalizeProviderProtocolSettings(
				tt.driver,
				tt.chatProtocol,
				"",
				tt.discoveryProtocol,
				"",
				"",
				"",
				"",
				"",
			)
			if err != nil {
				t.Fatalf("NormalizeProviderProtocolSettings() error = %v", err)
			}
			if settings.ChatEndpointPath != tt.wantChatEndpoint {
				t.Fatalf("expected chat endpoint %q, got %q", tt.wantChatEndpoint, settings.ChatEndpointPath)
			}
			if settings.DiscoveryEndpointPath != tt.wantDiscoveryEndpoint {
				t.Fatalf("expected discovery endpoint %q, got %q", tt.wantDiscoveryEndpoint, settings.DiscoveryEndpointPath)
			}
			if settings.AuthStrategy != tt.wantAuthStrategy {
				t.Fatalf("expected auth strategy %q, got %q", tt.wantAuthStrategy, settings.AuthStrategy)
			}
			if settings.ResponseProfile != tt.wantResponseProfile {
				t.Fatalf("expected response profile %q, got %q", tt.wantResponseProfile, settings.ResponseProfile)
			}
		})
	}
}
