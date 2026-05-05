package cli

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/feishuadapter"
)

func TestNewFeishuAdapterCommandForwardsFlags(t *testing.T) {
	originalRunner := runFeishuAdapterCommand
	t.Cleanup(func() { runFeishuAdapterCommand = originalRunner })

	var captured feishuAdapterCommandOptions
	runFeishuAdapterCommand = func(ctx context.Context, options feishuAdapterCommandOptions) error {
		captured = options
		return nil
	}

	cmd := newFeishuAdapterCommand()
	cmd.SetArgs([]string{
		"--listen", "127.0.0.1:19090",
		"--event-path", "/event",
		"--card-path", "/card",
		"--app-id", "app",
		"--app-secret", "secret",
		"--insecure-skip-signature-verify",
		"--gateway-listen", "tcp://gateway",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if captured.Listen != "127.0.0.1:19090" || captured.EventPath != "/event" || captured.CardPath != "/card" {
		t.Fatalf("unexpected forwarded options: %#v", captured)
	}
	if captured.AppID != "app" || captured.AppSecret != "secret" || captured.GatewayListen != "tcp://gateway" {
		t.Fatalf("unexpected credential/gateway options: %#v", captured)
	}
	if !captured.InsecureSkipSignVerify {
		t.Fatalf("expected insecure skip flag to be forwarded, got %#v", captured)
	}
}

func TestMergeFeishuOptionsAppliesDefaultsAndOverrides(t *testing.T) {
	merged := mergeFeishuOptions(config.FeishuConfig{}, feishuAdapterCommandOptions{
		Listen:            "127.0.0.1:20000",
		AppID:             "app-x",
		AppSecret:         "secret-x",
		RequestTimeoutSec: 20,
	}, config.GatewayConfig{})

	if merged.Listen != "127.0.0.1:20000" {
		t.Fatalf("listen = %q, want override", merged.Listen)
	}
	if merged.AppID != "app-x" || merged.AppSecret != "secret-x" {
		t.Fatalf("app credentials not applied: %#v", merged)
	}
	if merged.RequestTimeoutSec != 20 {
		t.Fatalf("request timeout = %d, want 20", merged.RequestTimeoutSec)
	}
	if merged.EventPath == "" || merged.CardPath == "" || merged.RebindIntervalSec <= 0 {
		t.Fatalf("expected defaults applied, got %#v", merged)
	}
}

func TestMergeFeishuOptionsAppliesAllCLIOverrides(t *testing.T) {
	merged := mergeFeishuOptions(config.FeishuConfig{
		Enabled: false,
	}, feishuAdapterCommandOptions{
		EventPath:              "/event",
		CardPath:               "/card",
		VerifyToken:            "verify",
		SigningSecret:          "sign",
		InsecureSkipSignVerify: true,
		IdempotencyTTLSec:      120,
		ReconnectBackoffMinM:   30,
		ReconnectBackoffMaxM:   60,
		RebindIntervalSec:      7,
		GatewayListen:          "unix:///tmp/gateway.sock",
		GatewayTokenFile:       "/tmp/gateway.token",
	}, config.GatewayConfig{})

	if merged.EventPath != "/event" || merged.CardPath != "/card" {
		t.Fatalf("paths not overridden: %#v", merged)
	}
	if merged.VerifyToken != "verify" || merged.SigningSecret != "sign" || !merged.InsecureSkipSignVerify {
		t.Fatalf("security settings not overridden: %#v", merged)
	}
	if merged.IdempotencyTTLSec != 120 || merged.ReconnectBackoffMinMs != 30 || merged.ReconnectBackoffMaxMs != 60 || merged.RebindIntervalSec != 7 {
		t.Fatalf("numeric overrides not applied: %#v", merged)
	}
	if merged.GatewayListenAddress != "unix:///tmp/gateway.sock" || merged.GatewayTokenFile != "/tmp/gateway.token" {
		t.Fatalf("gateway overrides not applied: %#v", merged)
	}
}

func TestNewRootCommandContainsFeishuAdapter(t *testing.T) {
	root := NewRootCommand()
	found := false
	for _, command := range root.Commands() {
		if command.Name() == "feishu-adapter" {
			found = true
			if !shouldSkipGlobalPreload(command) {
				t.Fatal("feishu-adapter should skip global preload")
			}
			if !shouldSkipSilentUpdateCheck(command) {
				t.Fatal("feishu-adapter should skip silent update check")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected feishu-adapter command in root")
	}
}

func TestNewFeishuAdapterCommandPropagatesRunnerError(t *testing.T) {
	originalRunner := runFeishuAdapterCommand
	t.Cleanup(func() { runFeishuAdapterCommand = originalRunner })

	expected := errors.New("boom")
	runFeishuAdapterCommand = func(ctx context.Context, options feishuAdapterCommandOptions) error {
		return expected
	}
	cmd := newFeishuAdapterCommand()
	if err := cmd.ExecuteContext(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
}

type stubFeishuGatewayClient struct {
	closed bool
}

func (s *stubFeishuGatewayClient) Authenticate(context.Context) error { return nil }
func (s *stubFeishuGatewayClient) BindStream(context.Context, string, string) error {
	return nil
}
func (s *stubFeishuGatewayClient) Run(context.Context, string, string, string) error { return nil }
func (s *stubFeishuGatewayClient) ResolvePermission(context.Context, string, string) error {
	return nil
}
func (s *stubFeishuGatewayClient) Ping(context.Context) error { return nil }
func (s *stubFeishuGatewayClient) Notifications() <-chan feishuadapter.GatewayNotification {
	ch := make(chan feishuadapter.GatewayNotification)
	close(ch)
	return ch
}
func (s *stubFeishuGatewayClient) Close() error {
	s.closed = true
	return nil
}

type stubFeishuMessenger struct{}

func (stubFeishuMessenger) SendText(context.Context, string, string) error { return nil }
func (stubFeishuMessenger) SendPermissionCard(context.Context, string, feishuadapter.PermissionCardPayload) error {
	return nil
}

func writeFeishuAdapterConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	loader := config.NewLoader(filepath.Join(home, ".neocode"), config.StaticDefaults())
	cfg := loader.DefaultConfig()
	cfg.Feishu = config.FeishuConfig{
		Enabled:       true,
		AppID:         "app",
		AppSecret:     "secret",
		VerifyToken:   "verify",
		SigningSecret: "sign",
		Adapter: config.FeishuAdapterConfig{
			Listen:   "127.0.0.1:18080",
			EventURI: "/feishu/events",
			CardURI:  "/feishu/cards",
		},
		RequestTimeoutSec:    1,
		IdempotencyTTLSec:    60,
		ReconnectBackoffMinM: 10,
		ReconnectBackoffMaxM: 20,
		RebindIntervalSec:    1,
		GatewayClient: config.FeishuGatewayClientConfig{
			ListenAddress: filepath.Join(home, "gateway.sock"),
			TokenFile:     filepath.Join(home, "gateway.token"),
		},
	}
	if err := loader.Save(context.Background(), &cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func TestDefaultFeishuAdapterCommandRunnerInitializesAdapter(t *testing.T) {
	writeFeishuAdapterConfig(t)

	originalGatewayFactory := newGatewayRPCClientForFeishu
	originalMessengerFactory := newFeishuMessenger
	t.Cleanup(func() {
		newGatewayRPCClientForFeishu = originalGatewayFactory
		newFeishuMessenger = originalMessengerFactory
	})

	var capturedGatewayConfig feishuadapter.GatewayClientConfig
	newGatewayRPCClientForFeishu = func(cfg feishuadapter.GatewayClientConfig) (feishuadapter.GatewayClient, error) {
		capturedGatewayConfig = cfg
		return &stubFeishuGatewayClient{}, nil
	}
	newFeishuMessenger = func(appID string, appSecret string, httpClient feishuadapter.HTTPClient) feishuadapter.Messenger {
		if appID != "app" || appSecret != "secret" {
			t.Fatalf("unexpected messenger credentials: %q %q", appID, appSecret)
		}
		return stubFeishuMessenger{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	err := defaultFeishuAdapterCommandRunner(ctx, feishuAdapterCommandOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runner error = %v, want context canceled", err)
	}
	if capturedGatewayConfig.ListenAddress == "" || capturedGatewayConfig.TokenFile == "" {
		t.Fatalf("gateway config not forwarded: %#v", capturedGatewayConfig)
	}
}

func TestDefaultFeishuAdapterCommandRunnerClosesGatewayOnAdapterInitError(t *testing.T) {
	writeFeishuAdapterConfig(t)

	originalGatewayFactory := newGatewayRPCClientForFeishu
	originalMessengerFactory := newFeishuMessenger
	originalAdapterFactory := newFeishuAdapter
	t.Cleanup(func() {
		newGatewayRPCClientForFeishu = originalGatewayFactory
		newFeishuMessenger = originalMessengerFactory
		newFeishuAdapter = originalAdapterFactory
	})

	gateway := &stubFeishuGatewayClient{}
	newGatewayRPCClientForFeishu = func(cfg feishuadapter.GatewayClientConfig) (feishuadapter.GatewayClient, error) {
		return gateway, nil
	}
	newFeishuMessenger = func(string, string, feishuadapter.HTTPClient) feishuadapter.Messenger {
		return stubFeishuMessenger{}
	}
	newFeishuAdapter = func(cfg feishuadapter.Config, gateway feishuadapter.GatewayClient, messenger feishuadapter.Messenger, logger *log.Logger) (*feishuadapter.Adapter, error) {
		return nil, errors.New("adapter init failed")
	}

	err := defaultFeishuAdapterCommandRunner(context.Background(), feishuAdapterCommandOptions{})
	if err == nil || err.Error() != "adapter init failed" {
		t.Fatalf("runner error = %v, want adapter init failed", err)
	}
	if !gateway.closed {
		t.Fatal("expected gateway client to close on adapter init failure")
	}
}

func TestDefaultFeishuAdapterCommandRunnerPropagatesGatewayInitError(t *testing.T) {
	writeFeishuAdapterConfig(t)

	originalGatewayFactory := newGatewayRPCClientForFeishu
	t.Cleanup(func() {
		newGatewayRPCClientForFeishu = originalGatewayFactory
	})
	newGatewayRPCClientForFeishu = func(cfg feishuadapter.GatewayClientConfig) (feishuadapter.GatewayClient, error) {
		return nil, errors.New("dial failed")
	}

	err := defaultFeishuAdapterCommandRunner(context.Background(), feishuAdapterCommandOptions{})
	if err == nil || err.Error() != "init gateway client: dial failed" {
		t.Fatalf("runner error = %v, want wrapped gateway init error", err)
	}
}

func TestDefaultFeishuAdapterCommandRunnerPropagatesLoadAndValidateError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(":\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
	if err := defaultFeishuAdapterCommandRunner(context.Background(), feishuAdapterCommandOptions{}); err == nil {
		t.Fatal("expected config load error")
	}

	home = t.TempDir()
	t.Setenv("HOME", home)
	configDir = filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	invalidFeishu := "selected_provider: openai\ncurrent_model: gpt-4.1\nshell: bash\nfeishu:\n  enabled: true\n  app_id: app\n  app_secret: secret\n  signing_secret: sign\n  adapter:\n    listen: 127.0.0.1:18080\n    event_path: /feishu/events\n    card_path: /feishu/cards\n  request_timeout_sec: 1\n  idempotency_ttl_sec: 60\n  reconnect_backoff_min_ms: 10\n  reconnect_backoff_max_ms: 20\n  rebind_interval_sec: 1\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(invalidFeishu), 0o644); err != nil {
		t.Fatalf("write invalid feishu config: %v", err)
	}
	err := defaultFeishuAdapterCommandRunner(context.Background(), feishuAdapterCommandOptions{})
	if err == nil {
		t.Fatal("expected validation error from config")
	}
}
