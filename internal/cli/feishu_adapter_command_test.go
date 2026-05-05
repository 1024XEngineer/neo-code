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
