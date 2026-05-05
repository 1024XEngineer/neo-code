package config

import "testing"

func TestFeishuConfigValidateDisabledAllowsEmpty(t *testing.T) {
	var cfg FeishuConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate disabled feishu config: %v", err)
	}
}

func TestFeishuConfigValidateEnabledRequiresFields(t *testing.T) {
	cfg := FeishuConfig{Enabled: true}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for incomplete enabled config")
	}
}

func TestFeishuConfigValidateRequiresVerifyAndSigningSecretByDefault(t *testing.T) {
	cfg := FeishuConfig{
		Enabled:   true,
		AppID:     "app",
		AppSecret: "secret",
		Adapter: FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/feishu/events",
			CardURI:  "/feishu/cards",
		},
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 10000,
		RebindIntervalSec:    15,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error when verify/signing secret are missing")
	}
}

func TestFeishuConfigValidateAllowsInsecureSkipSignatureVerify(t *testing.T) {
	cfg := FeishuConfig{
		Enabled:                true,
		AppID:                  "app",
		AppSecret:              "secret",
		VerifyToken:            "verify",
		InsecureSkipSignVerify: true,
		Adapter: FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/feishu/events",
			CardURI:  "/feishu/cards",
		},
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 10000,
		RebindIntervalSec:    15,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to pass with insecure skip, got %v", err)
	}
}

func TestFeishuConfigApplyDefaults(t *testing.T) {
	var cfg FeishuConfig
	cfg.ApplyDefaults(FeishuConfig{
		Adapter: FeishuAdapterConfig{
			Listen:   DefaultFeishuAdapterListen,
			EventURI: DefaultFeishuAdapterEventPath,
			CardURI:  DefaultFeishuAdapterCardPath,
		},
		RequestTimeoutSec:    DefaultFeishuGatewayRequestTimeoutSec,
		IdempotencyTTLSec:    DefaultFeishuIdempotencyTTLSec,
		ReconnectBackoffMinM: DefaultFeishuReconnectBackoffMinMs,
		ReconnectBackoffMaxM: DefaultFeishuReconnectBackoffMaxMs,
		RebindIntervalSec:    DefaultFeishuRebindIntervalSec,
	})
	if cfg.Adapter.Listen == "" || cfg.Adapter.EventURI == "" || cfg.Adapter.CardURI == "" {
		t.Fatalf("adapter defaults not applied: %#v", cfg.Adapter)
	}
	if cfg.RequestTimeoutSec <= 0 || cfg.IdempotencyTTLSec <= 0 || cfg.RebindIntervalSec <= 0 {
		t.Fatalf("scalar defaults not applied: %#v", cfg)
	}
}
