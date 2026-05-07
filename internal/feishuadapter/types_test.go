package feishuadapter

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidateAcceptsValidConfiguration(t *testing.T) {
	cfg := Config{
		IngressMode:         IngressModeWebhook,
		ListenAddress:       "127.0.0.1:18080",
		EventPath:           "/feishu/events",
		CardPath:            "/feishu/cards",
		AppID:               "app",
		AppSecret:           "secret",
		VerifyToken:         "verify",
		SigningSecret:       "sign",
		RequestTimeout:      time.Second,
		IdempotencyTTL:      time.Minute,
		ReconnectBackoffMin: 100 * time.Millisecond,
		ReconnectBackoffMax: time.Second,
		RebindInterval:      time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate valid config: %v", err)
	}
}

func TestConfigValidateRejectsInvalidInputs(t *testing.T) {
	testCases := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "missing listen", cfg: Config{IngressMode: IngressModeWebhook, AppID: "app", AppSecret: "secret"}, want: "listen address is required"},
		{name: "missing event path", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", AppID: "app", AppSecret: "secret"}, want: "event path is required"},
		{name: "missing card path", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", EventPath: "/events", AppID: "app", AppSecret: "secret"}, want: "card path is required"},
		{name: "missing app id", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", EventPath: "/events", CardPath: "/cards"}, want: "app id is required"},
		{name: "missing app secret", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", EventPath: "/events", CardPath: "/cards", AppID: "app"}, want: "app secret is required"},
		{name: "missing verify token", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", EventPath: "/events", CardPath: "/cards", AppID: "app", AppSecret: "secret"}, want: "verify token is required"},
		{name: "missing signing secret", cfg: Config{IngressMode: IngressModeWebhook, ListenAddress: "127.0.0.1:1", EventPath: "/events", CardPath: "/cards", AppID: "app", AppSecret: "secret", VerifyToken: "verify"}, want: "signing secret is required"},
		{name: "request timeout", cfg: Config{IngressMode: IngressModeSDK, AppID: "app", AppSecret: "secret"}, want: "request timeout must be greater than zero"},
		{name: "idempotency ttl", cfg: Config{IngressMode: IngressModeSDK, AppID: "app", AppSecret: "secret", RequestTimeout: time.Second}, want: "idempotency ttl must be greater than zero"},
		{name: "backoff", cfg: Config{IngressMode: IngressModeSDK, AppID: "app", AppSecret: "secret", RequestTimeout: time.Second, IdempotencyTTL: time.Minute}, want: "reconnect backoff must be greater than zero"},
		{name: "backoff order", cfg: Config{IngressMode: IngressModeSDK, AppID: "app", AppSecret: "secret", RequestTimeout: time.Second, IdempotencyTTL: time.Minute, ReconnectBackoffMin: time.Second, ReconnectBackoffMax: 100 * time.Millisecond}, want: "reconnect backoff min cannot exceed max"},
		{name: "rebind", cfg: Config{IngressMode: IngressModeSDK, AppID: "app", AppSecret: "secret", RequestTimeout: time.Second, IdempotencyTTL: time.Minute, ReconnectBackoffMin: 100 * time.Millisecond, ReconnectBackoffMax: time.Second}, want: "rebind interval must be greater than zero"},
		{name: "invalid ingress", cfg: Config{IngressMode: "other", AppID: "app", AppSecret: "secret", RequestTimeout: time.Second, IdempotencyTTL: time.Minute, ReconnectBackoffMin: time.Second, ReconnectBackoffMax: time.Second, RebindInterval: time.Second}, want: "ingress mode must be"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want substring %q", err, testCase.want)
			}
		})
	}
}

func TestConfigValidateAllowsSkippingSignatureVerification(t *testing.T) {
	cfg := Config{
		IngressMode:            IngressModeWebhook,
		ListenAddress:          "127.0.0.1:18080",
		EventPath:              "/feishu/events",
		CardPath:               "/feishu/cards",
		AppID:                  "app",
		AppSecret:              "secret",
		VerifyToken:            "verify",
		InsecureSkipSignVerify: true,
		RequestTimeout:         time.Second,
		IdempotencyTTL:         time.Minute,
		ReconnectBackoffMin:    100 * time.Millisecond,
		ReconnectBackoffMax:    time.Second,
		RebindInterval:         time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate insecure config: %v", err)
	}
}

func TestConfigValidateSDKDoesNotRequireWebhookFields(t *testing.T) {
	cfg := Config{
		IngressMode:         IngressModeSDK,
		AppID:               "app",
		AppSecret:           "secret",
		RequestTimeout:      3 * time.Second,
		IdempotencyTTL:      time.Minute,
		ReconnectBackoffMin: time.Second,
		ReconnectBackoffMax: 2 * time.Second,
		RebindInterval:      3 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate sdk mode: %v", err)
	}
}
