package cli

import (
	"context"
	"errors"
	"testing"

	"neo-code/internal/config"
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
		"--ingress", "sdk",
		"--listen", "127.0.0.1:19090",
		"--event-path", "/event",
		"--card-path", "/card",
		"--app-id", "app",
		"--bot-user-id", "ou_bot",
		"--bot-open-id", "ou_open_bot",
		"--insecure-skip-signature-verify",
		"--gateway-listen", "tcp://gateway",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if captured.Listen != "127.0.0.1:19090" || captured.EventPath != "/event" || captured.CardPath != "/card" {
		t.Fatalf("unexpected forwarded options: %#v", captured)
	}
	if captured.Ingress != "sdk" {
		t.Fatalf("ingress = %q, want sdk", captured.Ingress)
	}
	if captured.AppID != "app" || captured.GatewayListen != "tcp://gateway" {
		t.Fatalf("unexpected credential/gateway options: %#v", captured)
	}
	if captured.BotUserID != "ou_bot" || captured.BotOpenID != "ou_open_bot" {
		t.Fatalf("unexpected bot id options: %#v", captured)
	}
	if !captured.InsecureSkipSignVerify {
		t.Fatalf("expected insecure skip flag to be forwarded, got %#v", captured)
	}
}

func TestMergeFeishuOptionsAppliesDefaultsAndOverrides(t *testing.T) {
	merged := mergeFeishuOptions(config.FeishuConfig{}, feishuAdapterCommandOptions{
		Listen:            "127.0.0.1:20000",
		Ingress:           "sdk",
		AppID:             "app-x",
		RequestTimeoutSec: 20,
	}, config.GatewayConfig{})

	if merged.Listen != "127.0.0.1:20000" {
		t.Fatalf("listen = %q, want override", merged.Listen)
	}
	if merged.Ingress != "sdk" {
		t.Fatalf("ingress = %q, want sdk", merged.Ingress)
	}
	if merged.AppID != "app-x" {
		t.Fatalf("app id not applied: %#v", merged)
	}
	if merged.RequestTimeoutSec != 20 {
		t.Fatalf("request timeout = %d, want 20", merged.RequestTimeoutSec)
	}
	if merged.EventPath == "" || merged.CardPath == "" || merged.RebindIntervalSec <= 0 {
		t.Fatalf("expected defaults applied, got %#v", merged)
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

func TestInjectFeishuSecretsFromEnvRequiresAppSecret(t *testing.T) {
	t.Setenv(config.FeishuAppSecretEnvVar, "")
	options := mergedFeishuOptions{Ingress: config.FeishuIngressSDK}
	err := injectFeishuSecretsFromEnv(&options)
	if err == nil || err.Error() != "请先设置环境变量 "+config.FeishuAppSecretEnvVar {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectFeishuSecretsFromEnvRequiresSigningSecretForWebhook(t *testing.T) {
	t.Setenv(config.FeishuAppSecretEnvVar, "app-secret")
	t.Setenv(config.FeishuSigningSecretEnvVar, "")
	options := mergedFeishuOptions{Ingress: config.FeishuIngressWebhook}
	err := injectFeishuSecretsFromEnv(&options)
	if err == nil || err.Error() != "请先设置环境变量 "+config.FeishuSigningSecretEnvVar {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectFeishuSecretsFromEnvLoadsSDKSecret(t *testing.T) {
	t.Setenv(config.FeishuAppSecretEnvVar, "app-secret")
	options := mergedFeishuOptions{Ingress: config.FeishuIngressSDK}
	if err := injectFeishuSecretsFromEnv(&options); err != nil {
		t.Fatalf("inject sdk secret: %v", err)
	}
	if options.AppSecret != "app-secret" {
		t.Fatalf("app secret = %q, want app-secret", options.AppSecret)
	}
}
