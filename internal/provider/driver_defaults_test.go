package provider

import "testing"

func TestResolveDriverProtocolDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		driver           string
		wantChat         string
		wantDiscovery    string
		wantAuthStrategy string
		wantProfile      string
	}{
		{
			name:             "gemini defaults",
			driver:           DriverGemini,
			wantChat:         ChatProtocolOpenAIChatCompletions,
			wantDiscovery:    DiscoveryProtocolGeminiModels,
			wantAuthStrategy: AuthStrategyBearer,
			wantProfile:      DiscoveryResponseProfileGemini,
		},
		{
			name:             "anthropic defaults",
			driver:           DriverAnthropic,
			wantChat:         ChatProtocolAnthropicMessages,
			wantDiscovery:    DiscoveryProtocolAnthropicModels,
			wantAuthStrategy: AuthStrategyAnthropic,
			wantProfile:      DiscoveryResponseProfileGeneric,
		},
		{
			name:             "openaicompat defaults",
			driver:           DriverOpenAICompat,
			wantChat:         ChatProtocolOpenAIChatCompletions,
			wantDiscovery:    DiscoveryProtocolOpenAIModels,
			wantAuthStrategy: AuthStrategyBearer,
			wantProfile:      DiscoveryResponseProfileOpenAI,
		},
		{
			name:             "unknown driver falls back to custom defaults",
			driver:           " custom ",
			wantChat:         ChatProtocolOpenAIChatCompletions,
			wantDiscovery:    DiscoveryProtocolCustomHTTPJSON,
			wantAuthStrategy: AuthStrategyBearer,
			wantProfile:      DiscoveryResponseProfileGeneric,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveDriverProtocolDefaults(tt.driver)
			if got.ChatProtocol != tt.wantChat {
				t.Fatalf("expected chat protocol %q, got %q", tt.wantChat, got.ChatProtocol)
			}
			if got.DiscoveryProtocol != tt.wantDiscovery {
				t.Fatalf("expected discovery protocol %q, got %q", tt.wantDiscovery, got.DiscoveryProtocol)
			}
			if got.AuthStrategy != tt.wantAuthStrategy {
				t.Fatalf("expected auth strategy %q, got %q", tt.wantAuthStrategy, got.AuthStrategy)
			}
			if got.ResponseProfile != tt.wantProfile {
				t.Fatalf("expected response profile %q, got %q", tt.wantProfile, got.ResponseProfile)
			}
		})
	}
}

func TestResolveDriverDiscoveryConfig(t *testing.T) {
	t.Parallel()

	t.Run("uses default endpoint when endpoint is empty", func(t *testing.T) {
		t.Parallel()

		protocol, endpoint, profile, err := ResolveDriverDiscoveryConfig(DriverGemini, "")
		if err != nil {
			t.Fatalf("ResolveDriverDiscoveryConfig() error = %v", err)
		}
		if protocol != DiscoveryProtocolGeminiModels {
			t.Fatalf("expected discovery protocol %q, got %q", DiscoveryProtocolGeminiModels, protocol)
		}
		if endpoint != DiscoveryEndpointPathModels {
			t.Fatalf("expected endpoint %q, got %q", DiscoveryEndpointPathModels, endpoint)
		}
		if profile != DiscoveryResponseProfileGemini {
			t.Fatalf("expected response profile %q, got %q", DiscoveryResponseProfileGemini, profile)
		}
	})

	t.Run("keeps explicit relative endpoint", func(t *testing.T) {
		t.Parallel()

		protocol, endpoint, profile, err := ResolveDriverDiscoveryConfig(DriverOpenAICompat, " custom/models ")
		if err != nil {
			t.Fatalf("ResolveDriverDiscoveryConfig() error = %v", err)
		}
		if protocol != DiscoveryProtocolOpenAIModels {
			t.Fatalf("expected discovery protocol %q, got %q", DiscoveryProtocolOpenAIModels, protocol)
		}
		if endpoint != "/custom/models" {
			t.Fatalf("expected endpoint %q, got %q", "/custom/models", endpoint)
		}
		if profile != DiscoveryResponseProfileOpenAI {
			t.Fatalf("expected response profile %q, got %q", DiscoveryResponseProfileOpenAI, profile)
		}
	})

	t.Run("returns validation error for absolute endpoint", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := ResolveDriverDiscoveryConfig(DriverOpenAICompat, "https://api.example.com/models")
		if err == nil {
			t.Fatal("expected error for absolute endpoint path")
		}
	})
}

func TestResolveDriverAuthConfig(t *testing.T) {
	t.Parallel()

	authStrategy, apiVersion := ResolveDriverAuthConfig(DriverAnthropic)
	if authStrategy != AuthStrategyAnthropic {
		t.Fatalf("expected auth strategy %q, got %q", AuthStrategyAnthropic, authStrategy)
	}
	if apiVersion != "" {
		t.Fatalf("expected empty api version, got %q", apiVersion)
	}

	authStrategy, apiVersion = ResolveDriverAuthConfig("unknown")
	if authStrategy != AuthStrategyBearer {
		t.Fatalf("expected default auth strategy %q, got %q", AuthStrategyBearer, authStrategy)
	}
	if apiVersion != "" {
		t.Fatalf("expected empty api version for default driver, got %q", apiVersion)
	}
}
