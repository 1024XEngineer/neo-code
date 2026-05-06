package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/ptyproxy"
)

func TestShellCommandInitAcceptsPositionalShellArgument(t *testing.T) {
	originalInitRunner := runShellInitCommand
	t.Cleanup(func() { runShellInitCommand = originalInitRunner })

	var captured shellCommandOptions
	runShellInitCommand = func(_ context.Context, options shellCommandOptions, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"shell", "--init", "/bin/zsh"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want /bin/zsh", captured.Shell)
	}
}

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

func TestDefaultRunners(t *testing.T) {
	t.Run("defaultShellCommandRunner", func(t *testing.T) {
		originalRun := runManualShellProxy
		t.Cleanup(func() { runManualShellProxy = originalRun })

		var captured ptyproxy.ManualShellOptions
		runManualShellProxy = func(_ context.Context, options ptyproxy.ManualShellOptions) error {
			captured = options
			return nil
		}

		stdin := strings.NewReader("input")
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		err := defaultShellCommandRunner(context.Background(), shellCommandOptions{
			Workdir:              " /repo ",
			Shell:                " /bin/bash ",
			SocketPath:           " /tmp/diag.sock ",
			GatewayListenAddress: " /tmp/gateway.sock ",
			GatewayTokenFile:     " /tmp/token.json ",
		}, stdin, stdout, stderr)
		if err != nil {
			t.Fatalf("defaultShellCommandRunner() error = %v", err)
		}
		if captured.Workdir != "/repo" || captured.Shell != "/bin/bash" {
			t.Fatalf("captured options = %+v", captured)
		}
		if captured.SocketPath != "/tmp/diag.sock" {
			t.Fatalf("captured.SocketPath = %q, want /tmp/diag.sock", captured.SocketPath)
		}
	})

	t.Run("defaultShellInitCommandRunner", func(t *testing.T) {
		originalScriptBuilder := buildShellInitScript
		t.Cleanup(func() { buildShellInitScript = originalScriptBuilder })
		buildShellInitScript = func(shell string) string {
			if shell != "/bin/bash" {
				t.Fatalf("shell = %q, want /bin/bash", shell)
			}
			return "mock-init-script"
		}

		if err := defaultShellInitCommandRunner(context.Background(), shellCommandOptions{Shell: "/bin/bash"}, nil); err != nil {
			t.Fatalf("defaultShellInitCommandRunner(nil stdout) error = %v", err)
		}

		stdout := &bytes.Buffer{}
		if err := defaultShellInitCommandRunner(context.Background(), shellCommandOptions{Shell: "/bin/bash"}, stdout); err != nil {
			t.Fatalf("defaultShellInitCommandRunner() error = %v", err)
		}
		if stdout.String() != "mock-init-script\n" {
			t.Fatalf("stdout = %q, want mock-init-script", stdout.String())
		}
	})

	t.Run("defaultDiagCommandRunner", func(t *testing.T) {
		originalSend := sendDiagnoseSignalFn
		t.Cleanup(func() { sendDiagnoseSignalFn = originalSend })

		var socket string
		sendDiagnoseSignalFn = func(_ context.Context, socketPath string) error {
			socket = socketPath
			return nil
		}
		err := defaultDiagCommandRunner(context.Background(), diagCommandOptions{SocketPath: " /tmp/diag.sock "})
		if err != nil {
			t.Fatalf("defaultDiagCommandRunner() error = %v", err)
		}
		if socket != "/tmp/diag.sock" {
			t.Fatalf("socket = %q, want /tmp/diag.sock", socket)
		}
	})

	t.Run("defaultDiagAutoCommandRunnerQuery", func(t *testing.T) {
		originalQuery := queryAutoModeFn
		t.Cleanup(func() { queryAutoModeFn = originalQuery })
		queryAutoModeFn = func(_ context.Context, socketPath string) (bool, error) {
			if socketPath != "/tmp/diag.sock" {
				t.Fatalf("socketPath = %q", socketPath)
			}
			return false, nil
		}

		stdout := &bytes.Buffer{}
		err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
			SocketPath: "/tmp/diag.sock",
			QueryOnly:  true,
		}, stdout)
		if err != nil {
			t.Fatalf("defaultDiagAutoCommandRunner(query) error = %v", err)
		}
		if !strings.Contains(stdout.String(), "disabled") {
			t.Fatalf("stdout = %q, want disabled", stdout.String())
		}
	})
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

func TestDiagSubcommandsAdditionalPaths(t *testing.T) {
	t.Run("diag diagnose subcommand", func(t *testing.T) {
		originalRunner := runDiagDiagnoseCommand
		originalReadEnv := readDiagEnvValue
		t.Cleanup(func() { runDiagDiagnoseCommand = originalRunner })
		t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

		readDiagEnvValue = func(string) string { return "/tmp/diag.sock" }
		called := false
		runDiagDiagnoseCommand = func(_ context.Context, options diagCommandOptions) error {
			called = true
			if options.SocketPath != "/tmp/diag.sock" {
				t.Fatalf("socket = %q, want /tmp/diag.sock", options.SocketPath)
			}
			return nil
		}

		command := NewRootCommand()
		command.SetArgs([]string{"diag", "diagnose"})
		if err := command.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext() error = %v", err)
		}
		if !called {
			t.Fatal("expected runDiagDiagnoseCommand called")
		}
	})

	t.Run("diag auto status", func(t *testing.T) {
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
		command.SetArgs([]string{"diag", "auto", "status"})
		if err := command.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext() error = %v", err)
		}
		if !captured.QueryOnly {
			t.Fatal("expected QueryOnly=true for status")
		}
	})

	t.Run("defaultDiagAutoCommandRunner errors", func(t *testing.T) {
		originalQuery := queryAutoModeFn
		originalSend := sendAutoModeSignalFn
		t.Cleanup(func() { queryAutoModeFn = originalQuery })
		t.Cleanup(func() { sendAutoModeSignalFn = originalSend })

		queryAutoModeFn = func(context.Context, string) (bool, error) { return false, errors.New("query failed") }
		if err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
			SocketPath: "/tmp/diag.sock",
			QueryOnly:  true,
		}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "query failed") {
			t.Fatalf("defaultDiagAutoCommandRunner(query error) err = %v", err)
		}

		sendAutoModeSignalFn = func(context.Context, string, bool) error { return errors.New("send failed") }
		if err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
			SocketPath: "/tmp/diag.sock",
			Enabled:    false,
		}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "send failed") {
			t.Fatalf("defaultDiagAutoCommandRunner(send error) err = %v", err)
		}
	})
}

func TestShellAndDiagCommandAdditionalBranches(t *testing.T) {
	t.Run("shell args validation without init", func(t *testing.T) {
		command := newShellCommand()
		command.SetArgs([]string{"extra"})
		err := command.ExecuteContext(context.Background())
		if err == nil {
			t.Fatal("expected no-args validation error")
		}
	})

	t.Run("diag diagnose with explicit socket flag", func(t *testing.T) {
		originalRunner := runDiagDiagnoseCommand
		t.Cleanup(func() { runDiagDiagnoseCommand = originalRunner })

		called := false
		runDiagDiagnoseCommand = func(_ context.Context, options diagCommandOptions) error {
			called = true
			if options.SocketPath != "/tmp/diag-explicit.sock" {
				t.Fatalf("socket = %q, want /tmp/diag-explicit.sock", options.SocketPath)
			}
			return nil
		}

		command := NewRootCommand()
		command.SetArgs([]string{"diag", "diagnose", "--socket", "/tmp/diag-explicit.sock"})
		if err := command.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext() error = %v", err)
		}
		if !called {
			t.Fatal("expected runDiagDiagnoseCommand called")
		}
	})

	t.Run("resolveDiagSocketPath prefers socket flag", func(t *testing.T) {
		path, err := resolveDiagSocketPath(" /tmp/from-flag.sock ")
		if err != nil {
			t.Fatalf("resolveDiagSocketPath() error = %v", err)
		}
		if path != "/tmp/from-flag.sock" {
			t.Fatalf("path = %q, want /tmp/from-flag.sock", path)
		}
	})

	t.Run("defaultDiagAutoCommandRunner disabled output", func(t *testing.T) {
		originalSend := sendAutoModeSignalFn
		t.Cleanup(func() { sendAutoModeSignalFn = originalSend })
		sendAutoModeSignalFn = func(_ context.Context, socketPath string, enabled bool) error {
			if socketPath != "/tmp/diag.sock" {
				t.Fatalf("socketPath = %q", socketPath)
			}
			if enabled {
				t.Fatal("expected enabled=false")
			}
			return nil
		}

		stdout := &bytes.Buffer{}
		err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
			SocketPath: "/tmp/diag.sock",
			Enabled:    false,
		}, stdout)
		if err != nil {
			t.Fatalf("defaultDiagAutoCommandRunner() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "disabled") {
			t.Fatalf("stdout = %q, want disabled", stdout.String())
		}
	})
}

func TestDiagCommandBuildersExposeSocketFlags(t *testing.T) {
	diag := newDiagDiagnoseCommand()
	if diag.Flags().Lookup("socket") == nil {
		t.Fatal("diag diagnose command should expose --socket flag")
	}

	auto := newDiagAutoCommand()
	if auto.Flags().Lookup("socket") == nil {
		t.Fatal("diag auto command should expose --socket flag")
	}
}

func TestDiagInteractiveCommandUsesIDMSocket(t *testing.T) {
	originalInteractive := runDiagInteractive
	originalDiagRunner := runDiagCommand
	originalResolveIDM := resolveLatestIDMPath
	t.Cleanup(func() { runDiagInteractive = originalInteractive })
	t.Cleanup(func() { runDiagCommand = originalDiagRunner })
	t.Cleanup(func() { resolveLatestIDMPath = originalResolveIDM })

	resolveLatestIDMPath = func() (string, error) { return "/tmp/idm.sock", nil }
	runDiagCommand = func(context.Context, diagCommandOptions) error {
		t.Fatal("runDiagCommand should not be called in interactive mode")
		return nil
	}

	var captured diagCommandOptions
	var called bool
	runDiagInteractive = func(_ context.Context, options diagCommandOptions) error {
		called = true
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "-i"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !called {
		t.Fatal("expected runDiagInteractive to be called")
	}
	if !captured.Interactive {
		t.Fatal("captured.Interactive = false, want true")
	}
	if captured.SocketPath != "/tmp/idm.sock" {
		t.Fatalf("captured.SocketPath = %q, want /tmp/idm.sock", captured.SocketPath)
	}
}

func TestDiagInteractiveCommandDerivesIDMSocketFromSocketFlag(t *testing.T) {
	originalInteractive := runDiagInteractive
	originalDiagRunner := runDiagCommand
	t.Cleanup(func() { runDiagInteractive = originalInteractive })
	t.Cleanup(func() { runDiagCommand = originalDiagRunner })

	runDiagCommand = func(context.Context, diagCommandOptions) error {
		t.Fatal("runDiagCommand should not be called in interactive mode")
		return nil
	}

	var captured diagCommandOptions
	runDiagInteractive = func(_ context.Context, options diagCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "-i", "--socket", "/tmp/custom.sock"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if filepath.Clean(captured.SocketPath) != filepath.Clean("/tmp/custom-idm.sock") {
		t.Fatalf("captured.SocketPath = %q, want %q", captured.SocketPath, "/tmp/custom-idm.sock")
	}
}

func TestDefaultDiagInteractiveCommandRunner(t *testing.T) {
	originalSend := sendIDMEnterSignalFn
	t.Cleanup(func() { sendIDMEnterSignalFn = originalSend })

	var captured string
	sendIDMEnterSignalFn = func(_ context.Context, socketPath string) error {
		captured = socketPath
		return nil
	}

	if err := defaultDiagInteractiveCommandRunner(context.Background(), diagCommandOptions{
		SocketPath: "/tmp/idm.sock",
	}); err != nil {
		t.Fatalf("defaultDiagInteractiveCommandRunner() error = %v", err)
	}
	if captured != "/tmp/idm.sock" {
		t.Fatalf("captured socket = %q, want /tmp/idm.sock", captured)
	}
}

func TestResolveIDMDiagSocketPath(t *testing.T) {
	t.Run("socket flag derives idm path", func(t *testing.T) {
		path, err := resolveIDMDiagSocketPath(" /tmp/flag.sock ")
		if err != nil {
			t.Fatalf("resolveIDMDiagSocketPath() error = %v", err)
		}
		if filepath.Clean(path) != filepath.Clean("/tmp/flag-idm.sock") {
			t.Fatalf("path = %q, want %q", path, "/tmp/flag-idm.sock")
		}
	})

	t.Run("socket flag keeps explicit idm path", func(t *testing.T) {
		path, err := resolveIDMDiagSocketPath(" /tmp/flag-idm.sock ")
		if err != nil {
			t.Fatalf("resolveIDMDiagSocketPath() error = %v", err)
		}
		if filepath.Clean(path) != filepath.Clean("/tmp/flag-idm.sock") {
			t.Fatalf("path = %q, want %q", path, "/tmp/flag-idm.sock")
		}
	})

	t.Run("idm env has priority over diag env and discovered path", func(t *testing.T) {
		originalReadEnv := readDiagEnvValue
		originalResolve := resolveLatestIDMPath
		t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
		t.Cleanup(func() { resolveLatestIDMPath = originalResolve })

		readDiagEnvValue = func(key string) string {
			if key == ptyproxy.IDMDiagSocketEnv {
				return "/tmp/from-idm-env.sock"
			}
			if key == ptyproxy.DiagSocketEnv {
				return "/tmp/from-diag-env.sock"
			}
			return ""
		}
		resolveLatestIDMPath = func() (string, error) { return "/tmp/discovered-idm.sock", nil }
		path, err := resolveIDMDiagSocketPath("")
		if err != nil {
			t.Fatalf("resolveIDMDiagSocketPath() error = %v", err)
		}
		if path != "/tmp/from-idm-env.sock" {
			t.Fatalf("path = %q, want /tmp/from-idm-env.sock", path)
		}
	})

	t.Run("diag env derives idm path when idm env is absent", func(t *testing.T) {
		originalReadEnv := readDiagEnvValue
		originalResolve := resolveLatestIDMPath
		t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
		t.Cleanup(func() { resolveLatestIDMPath = originalResolve })

		readDiagEnvValue = func(key string) string {
			if key == ptyproxy.DiagSocketEnv {
				return "/tmp/from-diag-env.sock"
			}
			return ""
		}
		resolveLatestIDMPath = func() (string, error) { return "/tmp/discovered-idm.sock", nil }
		path, err := resolveIDMDiagSocketPath("")
		if err != nil {
			t.Fatalf("resolveIDMDiagSocketPath() error = %v", err)
		}
		if filepath.Clean(path) != filepath.Clean("/tmp/from-diag-env-idm.sock") {
			t.Fatalf("path = %q, want %q", path, "/tmp/from-diag-env-idm.sock")
		}
	})

	t.Run("fallback to latest discovered path", func(t *testing.T) {
		originalReadEnv := readDiagEnvValue
		originalResolve := resolveLatestIDMPath
		t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
		t.Cleanup(func() { resolveLatestIDMPath = originalResolve })
		readDiagEnvValue = func(string) string { return "" }
		resolveLatestIDMPath = func() (string, error) { return "/tmp/discovered-idm.sock", nil }
		path, err := resolveIDMDiagSocketPath("")
		if err != nil {
			t.Fatalf("resolveIDMDiagSocketPath() error = %v", err)
		}
		if path != "/tmp/discovered-idm.sock" {
			t.Fatalf("path = %q, want /tmp/discovered-idm.sock", path)
		}
	})

	t.Run("missing path returns actionable error", func(t *testing.T) {
		originalReadEnv := readDiagEnvValue
		originalResolve := resolveLatestIDMPath
		t.Cleanup(func() { readDiagEnvValue = originalReadEnv })
		t.Cleanup(func() { resolveLatestIDMPath = originalResolve })
		readDiagEnvValue = func(string) string { return "" }
		resolveLatestIDMPath = func() (string, error) { return "", errors.New("not found") }

		_, err := resolveIDMDiagSocketPath("")
		if err == nil {
			t.Fatal("expected missing idm socket error")
		}
		if !strings.Contains(err.Error(), "idm socket is empty") {
			t.Fatalf("err = %v, want contains idm socket is empty", err)
		}
		if !strings.Contains(err.Error(), ptyproxy.IDMDiagSocketEnv) {
			t.Fatalf("err = %v, want contains %s", err, ptyproxy.IDMDiagSocketEnv)
		}
	})
}
