package provider

import "testing"

func TestProviderIdentityKeyIncludesDriverSpecificFields(t *testing.T) {
	t.Parallel()

	identity := ProviderIdentity{
		Driver:                "openaicompat",
		BaseURL:               "https://api.example.com/v1",
		ChatAPIMode:           ChatAPIModeResponses,
		ChatEndpointPath:      "/responses",
		DiscoveryEndpointPath: "/v2/models",
	}

	if got, want := identity.Key(), "openaicompat|https://api.example.com/v1|responses|/responses|/v2/models"; got != want {
		t.Fatalf("expected identity key %q, got %q", want, got)
	}
}

func TestNormalizeProviderIdentityUsesDriverSpecificNormalization(t *testing.T) {
	t.Parallel()

	identity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                " OpenAICompat ",
		BaseURL:               "https://API.EXAMPLE.COM/v1/",
		DiscoveryEndpointPath: " models ",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() error = %v", err)
	}

	if identity.Driver != DriverOpenAICompat {
		t.Fatalf("expected normalized driver %q, got %q", DriverOpenAICompat, identity.Driver)
	}
	if identity.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected normalized base url %q, got %q", "https://api.example.com/v1", identity.BaseURL)
	}
	if identity.ChatEndpointPath != "" {
		t.Fatalf("expected empty chat endpoint path to stay empty in identity, got %q", identity.ChatEndpointPath)
	}
	if identity.DiscoveryEndpointPath != "/models" {
		t.Fatalf("expected normalized discovery endpoint path %q, got %q", "/models", identity.DiscoveryEndpointPath)
	}
}

func TestNormalizeProviderIdentityOpenAICompatPreservesChatEndpointSemanticDifference(t *testing.T) {
	t.Parallel()

	directIdentity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                DriverOpenAICompat,
		BaseURL:               "https://api.example.com/v1",
		ChatEndpointPath:      "",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() direct error = %v", err)
	}
	chatIdentity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                DriverOpenAICompat,
		BaseURL:               "https://api.example.com/v1",
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() chat error = %v", err)
	}

	if directIdentity.Key() == chatIdentity.Key() {
		t.Fatalf("expected distinct identity keys for direct mode and /chat/completions, got %q", directIdentity.Key())
	}
	if chatIdentity.ChatEndpointPath != "/chat/completions" {
		t.Fatalf("expected /chat/completions to be preserved, got %q", chatIdentity.ChatEndpointPath)
	}
}

func TestNormalizeProviderIdentityShrinksSDKDriverFields(t *testing.T) {
	t.Parallel()

	identity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                " Gemini ",
		BaseURL:               "https://API.EXAMPLE.COM/v1/",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() error = %v", err)
	}

	if identity.Driver != DriverGemini {
		t.Fatalf("expected normalized driver %q, got %q", DriverGemini, identity.Driver)
	}
	if identity.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected normalized base url %q, got %q", "https://api.example.com/v1", identity.BaseURL)
	}
	if identity.ChatEndpointPath != "" || identity.DiscoveryEndpointPath != "" {
		t.Fatalf("expected sdk driver identity to keep only driver/base_url, got %+v", identity)
	}
}

func TestProviderIdentityStringMatchesKey(t *testing.T) {
	t.Parallel()

	identity := ProviderIdentity{
		Driver:           "openaicompat",
		BaseURL:          "https://api.example.com/v1",
		ChatEndpointPath: "/responses",
	}
	if identity.String() != identity.Key() {
		t.Fatalf("expected String() to match Key(), got %q vs %q", identity.String(), identity.Key())
	}
}

func TestNewProviderIdentityValidatesInputs(t *testing.T) {
	t.Parallel()

	identity, err := NewProviderIdentity(" OpenAICompat ", "https://API.EXAMPLE.COM/v1/")
	if err != nil {
		t.Fatalf("NewProviderIdentity() error = %v", err)
	}
	if identity.Driver != "openaicompat" || identity.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("unexpected identity: %+v", identity)
	}

	if _, err := NewProviderIdentity("   ", "https://api.example.com/v1"); err == nil {
		t.Fatalf("expected empty driver to fail")
	}
	if _, err := NewProviderIdentity("openaicompat", "not-a-url"); err == nil {
		t.Fatalf("expected invalid base URL to fail")
	}
	if _, err := NewProviderIdentity("openaicompat", "https://token@api.example.com/v1"); err == nil {
		t.Fatalf("expected base URL with userinfo to fail")
	}
}

func TestNormalizeProviderIdentityAnthropicAndUnknownDriver(t *testing.T) {
	t.Parallel()

	anthropicIdentity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:  " Anthropic ",
		BaseURL: "https://API.EXAMPLE.COM/v1/",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() anthropic error = %v", err)
	}
	if anthropicIdentity.Driver != DriverAnthropic {
		t.Fatalf("expected anthropic driver, got %+v", anthropicIdentity)
	}
	if anthropicIdentity.ChatEndpointPath != "" {
		t.Fatalf("expected anthropic identity to drop protocol matrix fields, got %+v", anthropicIdentity)
	}
	if anthropicIdentity.DiscoveryEndpointPath != "" {
		t.Fatalf("expected anthropic identity to omit discovery endpoint, got %+v", anthropicIdentity)
	}

	fallbackIdentity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                " custom ",
		BaseURL:               "https://API.EXAMPLE.COM/v1/",
		DiscoveryEndpointPath: "gateway/models",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() fallback error = %v", err)
	}
	if fallbackIdentity.Driver != "custom" || fallbackIdentity.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected fallback identity to normalize driver and base URL, got %+v", fallbackIdentity)
	}
	if fallbackIdentity.DiscoveryEndpointPath != "/gateway/models" {
		t.Fatalf("expected fallback identity to preserve normalized discovery settings, got %+v", fallbackIdentity)
	}
}

func TestNormalizeProviderIdentityOpenAICompatKeepsOnlyPaths(t *testing.T) {
	t.Parallel()

	identity, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:                DriverOpenAICompat,
		BaseURL:               "https://api.example.com/v1",
		ChatAPIMode:           " responses ",
		ChatEndpointPath:      "/responses",
		DiscoveryEndpointPath: "/models",
	})
	if err != nil {
		t.Fatalf("NormalizeProviderIdentity() error = %v", err)
	}
	if identity.ChatAPIMode != ChatAPIModeResponses {
		t.Fatalf("expected chat api mode %q, got %q", ChatAPIModeResponses, identity.ChatAPIMode)
	}
	if identity.ChatEndpointPath != "/responses" {
		t.Fatalf("expected chat endpoint path %q, got %q", "/responses", identity.ChatEndpointPath)
	}
	if identity.DiscoveryEndpointPath != DiscoveryEndpointPathModels {
		t.Fatalf("expected discovery endpoint %q, got %q", DiscoveryEndpointPathModels, identity.DiscoveryEndpointPath)
	}
}

func TestNormalizeProviderIdentityRejectsInvalidChatAPIMode(t *testing.T) {
	t.Parallel()

	_, err := NormalizeProviderIdentity(ProviderIdentity{
		Driver:      DriverOpenAICompat,
		BaseURL:     "https://api.example.com/v1",
		ChatAPIMode: "unknown",
	})
	if err == nil {
		t.Fatal("expected invalid chat_api_mode to fail")
	}
}

func TestNormalizeProviderDiscoveryEndpointPath(t *testing.T) {
	t.Parallel()

	got, err := NormalizeProviderDiscoveryEndpointPath(" models ")
	if err != nil {
		t.Fatalf("NormalizeProviderDiscoveryEndpointPath() error = %v", err)
	}
	if got != "/models" {
		t.Fatalf("expected /models, got %q", got)
	}

	if _, err := NormalizeProviderDiscoveryEndpointPath("https://api.example.com/models"); err == nil {
		t.Fatalf("expected absolute URL to be rejected")
	}
	if _, err := NormalizeProviderDiscoveryEndpointPath("/models?x=1"); err == nil {
		t.Fatalf("expected query string to be rejected")
	}
}

func TestNormalizeProviderDiscoverySettings(t *testing.T) {
	t.Parallel()

	endpointPath, err := NormalizeProviderDiscoverySettings(DriverOpenAICompat, "")
	if err != nil {
		t.Fatalf("NormalizeProviderDiscoverySettings() openaicompat error = %v", err)
	}
	if endpointPath != DiscoveryEndpointPathModels {
		t.Fatalf("expected openaicompat defaults, got endpoint=%q", endpointPath)
	}

	endpointPath, err = NormalizeProviderDiscoverySettings("custom-driver", "")
	if err != nil {
		t.Fatalf("NormalizeProviderDiscoverySettings() custom driver error = %v", err)
	}
	if endpointPath != DiscoveryEndpointPathModels {
		t.Fatalf("expected custom driver defaults, got endpoint=%q", endpointPath)
	}
}
