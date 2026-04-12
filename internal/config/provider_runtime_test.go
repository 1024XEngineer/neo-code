package config

import (
	"testing"

	providerpkg "neo-code/internal/provider"
)

func TestResolvedProviderConfigToRuntimeConfig(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:           "company-gateway",
			Driver:         "openaicompat",
			BaseURL:        "https://llm.example.com/v1",
			Model:          "server-default",
			APIStyle:       "responses",
			DeploymentMode: "ignored",
			APIVersion:     "ignored",
		},
		APIKey: "secret-key",
	}

	got := resolved.ToRuntimeConfig()
	want := providerpkg.RuntimeConfig{
		Name:           "company-gateway",
		Driver:         "openaicompat",
		BaseURL:        "https://llm.example.com/v1",
		DefaultModel:   "server-default",
		APIKey:         "secret-key",
		APIStyle:       "responses",
		DeploymentMode: "ignored",
		APIVersion:     "ignored",
	}

	if got != want {
		t.Fatalf("ToRuntimeConfig() = %+v, want %+v", got, want)
	}
}
