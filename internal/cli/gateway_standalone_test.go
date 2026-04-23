package cli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNewGatewayStandaloneCommandPassesFlagsToRunner(t *testing.T) {
	originalRunner := runGatewayCommand
	t.Cleanup(func() { runGatewayCommand = originalRunner })

	var captured gatewayCommandOptions
	runGatewayCommand = func(_ context.Context, options gatewayCommandOptions) error {
		captured = options
		return nil
	}

	command := NewGatewayStandaloneCommand()
	command.SetArgs([]string{
		"--listen", "  /tmp/gateway.sock  ",
		"--http-listen", "  127.0.0.1:19080  ",
		"--log-level", " WARN ",
		"--workdir", "  /workspace/project  ",
		"--metrics-enabled",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.ListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "/tmp/gateway.sock")
	}
	if captured.HTTPAddress != "127.0.0.1:19080" {
		t.Fatalf("http address = %q, want %q", captured.HTTPAddress, "127.0.0.1:19080")
	}
	if captured.LogLevel != "warn" {
		t.Fatalf("log level = %q, want %q", captured.LogLevel, "warn")
	}
	if captured.Workdir != "/workspace/project" {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, "/workspace/project")
	}
	if !captured.MetricsEnabledOverridden || !captured.MetricsEnabled {
		t.Fatalf("metrics flags = %#v, want overridden + true", captured)
	}
}

func TestNewGatewayStandaloneCommandRejectsInvalidLogLevel(t *testing.T) {
	command := NewGatewayStandaloneCommand()
	command.SetArgs([]string{"--log-level", "trace"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected invalid log level error")
	}
	if !strings.Contains(err.Error(), "invalid --log-level") {
		t.Fatalf("error = %v, want contains %q", err, "invalid --log-level")
	}
}

func TestGatewaySubcommandAndStandaloneCommandAreOptionEquivalent(t *testing.T) {
	originalRunner := runGatewayCommand
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGatewayCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	captured := make([]gatewayCommandOptions, 0, 2)
	runGatewayCommand = func(_ context.Context, options gatewayCommandOptions) error {
		captured = append(captured, options)
		return nil
	}

	rootCommand := NewRootCommand()
	rootCommand.SetArgs([]string{
		"--workdir", "/workspace/project",
		"gateway",
		"--listen", "/tmp/gateway.sock",
		"--http-listen", "127.0.0.1:19080",
		"--log-level", "warn",
		"--max-frame-bytes", "1024",
		"--ipc-max-connections", "32",
		"--http-max-request-bytes", "2048",
		"--http-max-stream-connections", "64",
		"--ipc-read-sec", "10",
		"--ipc-write-sec", "11",
		"--http-read-sec", "12",
		"--http-write-sec", "13",
		"--http-shutdown-sec", "14",
		"--metrics-enabled",
	})
	if err := rootCommand.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("root command execute error = %v", err)
	}

	standaloneCommand := NewGatewayStandaloneCommand()
	standaloneCommand.SetArgs([]string{
		"--workdir", "/workspace/project",
		"--listen", "/tmp/gateway.sock",
		"--http-listen", "127.0.0.1:19080",
		"--log-level", "warn",
		"--max-frame-bytes", "1024",
		"--ipc-max-connections", "32",
		"--http-max-request-bytes", "2048",
		"--http-max-stream-connections", "64",
		"--ipc-read-sec", "10",
		"--ipc-write-sec", "11",
		"--http-read-sec", "12",
		"--http-write-sec", "13",
		"--http-shutdown-sec", "14",
		"--metrics-enabled",
	})
	if err := standaloneCommand.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("standalone command execute error = %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("captured options count = %d, want %d", len(captured), 2)
	}
	if !reflect.DeepEqual(captured[0], captured[1]) {
		t.Fatalf("options mismatch:\nsubcommand=%#v\nstandalone=%#v", captured[0], captured[1])
	}
}

func TestExecuteGatewayServerUsesStandaloneCommand(t *testing.T) {
	originalRunner := runGatewayCommand
	t.Cleanup(func() { runGatewayCommand = originalRunner })

	var captured gatewayCommandOptions
	runGatewayCommand = func(_ context.Context, options gatewayCommandOptions) error {
		captured = options
		return nil
	}

	err := ExecuteGatewayServer(context.Background(), []string{
		"--workdir", "/workspace/project",
		"--listen", "/tmp/gateway.sock",
		"--http-listen", "127.0.0.1:19080",
		"--log-level", "info",
	})
	if err != nil {
		t.Fatalf("ExecuteGatewayServer() error = %v", err)
	}
	if captured.Workdir != "/workspace/project" {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, "/workspace/project")
	}
}

func TestGatewaySubcommandAndStandaloneCommandPropagateSameRunnerError(t *testing.T) {
	originalRunner := runGatewayCommand
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGatewayCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	expectedErr := errors.New("gateway runner failed")
	runGatewayCommand = func(_ context.Context, _ gatewayCommandOptions) error {
		return expectedErr
	}

	rootCommand := NewRootCommand()
	rootCommand.SetArgs([]string{"gateway"})
	rootErr := rootCommand.ExecuteContext(context.Background())
	if !errors.Is(rootErr, expectedErr) {
		t.Fatalf("root command error = %v, want %v", rootErr, expectedErr)
	}

	standaloneCommand := NewGatewayStandaloneCommand()
	standaloneErr := standaloneCommand.ExecuteContext(context.Background())
	if !errors.Is(standaloneErr, expectedErr) {
		t.Fatalf("standalone command error = %v, want %v", standaloneErr, expectedErr)
	}
}
