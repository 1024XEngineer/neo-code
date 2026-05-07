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
	t.Setenv(FeishuAppSecretEnvVar, "secret")
	cfg := FeishuConfig{
		Enabled: true,
		Ingress: FeishuIngressWebhook,
		AppID:   "app",
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
	t.Setenv(FeishuAppSecretEnvVar, "secret")
	cfg := FeishuConfig{
		Enabled:                true,
		Ingress:                FeishuIngressWebhook,
		AppID:                  "app",
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

func TestFeishuConfigValidateSDKModeDoesNotRequireWebhookFields(t *testing.T) {
	t.Setenv(FeishuAppSecretEnvVar, "secret")
	cfg := FeishuConfig{
		Enabled:              true,
		Ingress:              FeishuIngressSDK,
		AppID:                "app",
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 10000,
		RebindIntervalSec:    15,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected sdk ingress config to pass without webhook fields, got %v", err)
	}
}

func TestFeishuConfigValidateRequiresAppSecretEnv(t *testing.T) {
	t.Setenv(FeishuAppSecretEnvVar, "")
	cfg := FeishuConfig{
		Enabled:              true,
		Ingress:              FeishuIngressSDK,
		AppID:                "app",
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 10000,
		RebindIntervalSec:    15,
	}
	err := cfg.Validate()
	if err == nil || err.Error() != FeishuAppSecretEnvVar+" is required when feishu.enabled=true" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFeishuConfigValidateRejectsInvalidIngress(t *testing.T) {
	t.Setenv(FeishuAppSecretEnvVar, "secret")
	cfg := FeishuConfig{
		Enabled:              true,
		Ingress:              "invalid",
		AppID:                "app",
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 10000,
		RebindIntervalSec:    15,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid ingress error")
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

func TestFeishuConfigApplyDefaultsAllowsNilReceiver(t *testing.T) {
	var cfg *FeishuConfig
	cfg.ApplyDefaults(defaultFeishuConfig())
}

func TestDefaultFeishuConfigProvidesRuntimeDefaults(t *testing.T) {
	defaults := defaultFeishuConfig()
	if defaults.Ingress != FeishuIngressWebhook {
		t.Fatalf("default ingress = %q, want %q", defaults.Ingress, FeishuIngressWebhook)
	}
	if defaults.Adapter.Listen != DefaultFeishuAdapterListen {
		t.Fatalf("default adapter listen = %q, want %q", defaults.Adapter.Listen, DefaultFeishuAdapterListen)
	}
	if defaults.Adapter.EventURI != DefaultFeishuAdapterEventPath {
		t.Fatalf("default adapter event path = %q, want %q", defaults.Adapter.EventURI, DefaultFeishuAdapterEventPath)
	}
	if defaults.Adapter.CardURI != DefaultFeishuAdapterCardPath {
		t.Fatalf("default adapter card path = %q, want %q", defaults.Adapter.CardURI, DefaultFeishuAdapterCardPath)
	}
	if defaults.RequestTimeoutSec != DefaultFeishuGatewayRequestTimeoutSec {
		t.Fatalf("default request timeout = %d, want %d", defaults.RequestTimeoutSec, DefaultFeishuGatewayRequestTimeoutSec)
	}
	if defaults.IdempotencyTTLSec != DefaultFeishuIdempotencyTTLSec {
		t.Fatalf("default idempotency ttl = %d, want %d", defaults.IdempotencyTTLSec, DefaultFeishuIdempotencyTTLSec)
	}
	if defaults.ReconnectBackoffMinM != DefaultFeishuReconnectBackoffMinMs {
		t.Fatalf("default reconnect min = %d, want %d", defaults.ReconnectBackoffMinM, DefaultFeishuReconnectBackoffMinMs)
	}
	if defaults.ReconnectBackoffMaxM != DefaultFeishuReconnectBackoffMaxMs {
		t.Fatalf("default reconnect max = %d, want %d", defaults.ReconnectBackoffMaxM, DefaultFeishuReconnectBackoffMaxMs)
	}
	if defaults.RebindIntervalSec != DefaultFeishuRebindIntervalSec {
		t.Fatalf("default rebind interval = %d, want %d", defaults.RebindIntervalSec, DefaultFeishuRebindIntervalSec)
	}
}

func TestFeishuConfigClonePreservesValues(t *testing.T) {
	original := FeishuConfig{
		Enabled:       true,
		AppID:         "app",
		AppSecret:     "secret",
		VerifyToken:   "verify",
		SigningSecret: "sign",
		Adapter: FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/events",
			CardURI:  "/cards",
		},
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 1000,
		RebindIntervalSec:    15,
	}
	cloned := original.Clone()
	if cloned != original {
		t.Fatalf("clone = %#v, want %#v", cloned, original)
	}
}

func TestFeishuConfigValidateRejectsInvalidNumericRanges(t *testing.T) {
	base := FeishuConfig{
		Enabled:       true,
		AppID:         "app",
		AppSecret:     "secret",
		VerifyToken:   "verify",
		SigningSecret: "sign",
		Adapter: FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/events",
			CardURI:  "/cards",
		},
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 1000,
		RebindIntervalSec:    15,
	}
	testCases := []struct {
		name   string
		mutate func(*FeishuConfig)
	}{
		{name: "request timeout", mutate: func(cfg *FeishuConfig) { cfg.RequestTimeoutSec = 0 }},
		{name: "idempotency ttl", mutate: func(cfg *FeishuConfig) { cfg.IdempotencyTTLSec = 0 }},
		{name: "reconnect min", mutate: func(cfg *FeishuConfig) { cfg.ReconnectBackoffMinM = 0 }},
		{name: "reconnect order", mutate: func(cfg *FeishuConfig) { cfg.ReconnectBackoffMinM = 2000 }},
		{name: "rebind interval", mutate: func(cfg *FeishuConfig) { cfg.RebindIntervalSec = 0 }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := base.Clone()
			testCase.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestFeishuConfigValidateRejectsMissingRequiredFieldsIndividually(t *testing.T) {
	base := FeishuConfig{
		Enabled:       true,
		AppID:         "app",
		AppSecret:     "secret",
		VerifyToken:   "verify",
		SigningSecret: "sign",
		Adapter: FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/events",
			CardURI:  "/cards",
		},
		RequestTimeoutSec:    8,
		IdempotencyTTLSec:    600,
		ReconnectBackoffMinM: 500,
		ReconnectBackoffMaxM: 1000,
		RebindIntervalSec:    15,
	}
	testCases := []func(*FeishuConfig){
		func(cfg *FeishuConfig) { cfg.AppSecret = "" },
		func(cfg *FeishuConfig) { cfg.SigningSecret = "" },
		func(cfg *FeishuConfig) { cfg.Adapter.Listen = "" },
		func(cfg *FeishuConfig) { cfg.Adapter.EventURI = "" },
		func(cfg *FeishuConfig) { cfg.Adapter.CardURI = "" },
	}
	for index, mutate := range testCases {
		cfg := base.Clone()
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", index)
		}
	}
}
