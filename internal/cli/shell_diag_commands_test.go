package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"neo-code/internal/ptyproxy"
)

func TestShellCommandUsesFlags(t *testing.T) {
	originalRunner := runShellCommand
	t.Cleanup(func() { runShellCommand = originalRunner })

	var captured shellCommandOptions
	runShellCommand = func(_ context.Context, options shellCommandOptions, _ io.Reader, _ io.Writer, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"--workdir", " /repo ",
		"shell",
		"--shell", " /bin/zsh ",
		"--socket", " /tmp/diag.sock ",
		"--gateway-listen", " /tmp/gateway.sock ",
		"--gateway-token-file", " /tmp/auth.json ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.Workdir != "/repo" {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, "/repo")
	}
	if captured.Shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want %q", captured.Shell, "/bin/zsh")
	}
	if captured.SocketPath != "/tmp/diag.sock" {
		t.Fatalf("socket = %q, want %q", captured.SocketPath, "/tmp/diag.sock")
	}
	if captured.GatewayListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("gateway-listen = %q, want %q", captured.GatewayListenAddress, "/tmp/gateway.sock")
	}
	if captured.GatewayTokenFile != "/tmp/auth.json" {
		t.Fatalf("gateway-token-file = %q, want %q", captured.GatewayTokenFile, "/tmp/auth.json")
	}
}

func TestShellCommandInitPrintsScript(t *testing.T) {
	originalInitRunner := runShellInitCommand
	t.Cleanup(func() { runShellInitCommand = originalInitRunner })

	var called bool
	runShellInitCommand = func(_ context.Context, options shellCommandOptions, stdout io.Writer) error {
		called = true
		if options.Shell != "/bin/bash" {
			t.Fatalf("shell = %q, want /bin/bash", options.Shell)
		}
		_, _ = io.WriteString(stdout, "script-body")
		return nil
	}

	command := NewRootCommand()
	stdout := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetArgs([]string{"shell", "--init", "--shell", "/bin/bash"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !called {
		t.Fatal("expected runShellInitCommand called")
	}
	if !strings.Contains(stdout.String(), "script-body") {
		t.Fatalf("stdout = %q, want contains script-body", stdout.String())
	}
}

func TestDiagCommandSocketPriority(t *testing.T) {
	originalRunner := runDiagCommand
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { runDiagCommand = originalRunner })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	var captured diagCommandOptions
	runDiagCommand = func(_ context.Context, options diagCommandOptions) error {
		captured = options
		return nil
	}
	readDiagEnvValue = func(key string) string {
		if key == ptyproxy.DiagSocketEnv {
			return "/tmp/from-env.sock"
		}
		return ""
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "--socket", " /tmp/from-flag.sock "})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.SocketPath != "/tmp/from-flag.sock" {
		t.Fatalf("socket = %q, want %q", captured.SocketPath, "/tmp/from-flag.sock")
	}
}

func TestDiagCommandUsesLatestPathFallback(t *testing.T) {
	originalRunner := runDiagCommand
	originalReadEnv := readDiagEnvValue
	originalLatest := resolveLatestDiagPath
	t.Cleanup(func() { runDiagCommand = originalRunner })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
	t.Cleanup(func() { resolveLatestDiagPath = originalLatest })

	readDiagEnvValue = func(string) string { return "" }
	resolveLatestDiagPath = func() (string, error) { return "/tmp/discovered.sock", nil }

	var captured diagCommandOptions
	runDiagCommand = func(_ context.Context, options diagCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.SocketPath != "/tmp/discovered.sock" {
		t.Fatalf("socket = %q, want /tmp/discovered.sock", captured.SocketPath)
	}
}

func TestDiagCommandSocketMissing(t *testing.T) {
	originalReadEnv := readDiagEnvValue
	originalLatest := resolveLatestDiagPath
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
	t.Cleanup(func() { resolveLatestDiagPath = originalLatest })

	readDiagEnvValue = func(string) string { return "" }
	resolveLatestDiagPath = func() (string, error) { return "", errors.New("no socket") }

	command := NewRootCommand()
	command.SetArgs([]string{"diag"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected missing socket error")
	}
	if !strings.Contains(err.Error(), "--socket") {
		t.Fatalf("error = %v, want contains --socket", err)
	}
	if !strings.Contains(err.Error(), ptyproxy.DiagSocketEnv) {
		t.Fatalf("error = %v, want contains %s", err, ptyproxy.DiagSocketEnv)
	}
}

func TestDiagAutoCommandOn(t *testing.T) {
	originalRunner := runDiagAutoCommand
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { runDiagAutoCommand = originalRunner })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(string) string { return "/tmp/diag.sock" }
	var captured diagAutoCommandOptions
	runDiagAutoCommand = func(_ context.Context, options diagAutoCommandOptions, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "auto", "on"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !captured.Enabled {
		t.Fatal("expected auto on")
	}
	if captured.SocketPath != "/tmp/diag.sock" {
		t.Fatalf("socket = %q, want /tmp/diag.sock", captured.SocketPath)
	}
}

func TestDiagAutoCommandOff(t *testing.T) {
	originalRunner := runDiagAutoCommand
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { runDiagAutoCommand = originalRunner })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(string) string { return "/tmp/diag.sock" }
	var captured diagAutoCommandOptions
	runDiagAutoCommand = func(_ context.Context, options diagAutoCommandOptions, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "auto", "off"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Enabled {
		t.Fatal("expected auto off")
	}
}

func TestDiagAutoCommandInvalidMode(t *testing.T) {
	command := NewRootCommand()
	command.SetArgs([]string{"diag", "auto", "maybe"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected invalid mode error")
	}
	if !strings.Contains(err.Error(), "on/off") {
		t.Fatalf("error = %v", err)
	}
}

func TestDefaultDiagAutoCommandRunnerPrintsResult(t *testing.T) {
	originalSend := sendAutoModeSignalFn
	t.Cleanup(func() { sendAutoModeSignalFn = originalSend })

	sendAutoModeSignalFn = func(_ context.Context, socketPath string, enabled bool) error {
		if socketPath != "/tmp/diag.sock" {
			t.Fatalf("socketPath = %q", socketPath)
		}
		if !enabled {
			t.Fatal("expected enabled=true")
		}
		return nil
	}

	stdout := &bytes.Buffer{}
	err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
		SocketPath: "/tmp/diag.sock",
		Enabled:    true,
	}, stdout)
	if err != nil {
		t.Fatalf("defaultDiagAutoCommandRunner() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "enabled") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestShellAndDiagCommandsSkipGlobalPreload(t *testing.T) {
	originalPreload := runGlobalPreload
	originalShellRunner := runShellCommand
	originalDiagRunner := runDiagCommand
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runShellCommand = originalShellRunner })
	t.Cleanup(func() { runDiagCommand = originalDiagRunner })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	preloadCalled := 0
	runGlobalPreload = func(context.Context) error {
		preloadCalled++
		return errors.New("should be skipped")
	}
	runShellCommand = func(context.Context, shellCommandOptions, io.Reader, io.Writer, io.Writer) error { return nil }
	runDiagCommand = func(context.Context, diagCommandOptions) error { return nil }
	readDiagEnvValue = func(string) string { return "/tmp/diag.sock" }

	command := NewRootCommand()
	command.SetArgs([]string{"shell"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("shell ExecuteContext() error = %v", err)
	}

	command = NewRootCommand()
	command.SetArgs([]string{"diag"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("diag ExecuteContext() error = %v", err)
	}

	if preloadCalled != 0 {
		t.Fatalf("expected preload skipped, called %d", preloadCalled)
	}
}
