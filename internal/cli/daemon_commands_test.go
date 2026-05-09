package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"neo-code/internal/gateway/adapters/urlscheme"
)

func TestDaemonServeSubcommandUsesFlags(t *testing.T) {
	originalRunner := runDaemonServeCommand
	t.Cleanup(func() { runDaemonServeCommand = originalRunner })

	var captured daemonServeCommandOptions
	runDaemonServeCommand = func(_ context.Context, options daemonServeCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"daemon", "serve",
		"--listen", " 127.0.0.1:19921 ",
		"--gateway-listen", " /tmp/gateway.sock ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.ListenAddress != "127.0.0.1:19921" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "127.0.0.1:19921")
	}
	if captured.GatewayListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("gateway listen address = %q, want %q", captured.GatewayListenAddress, "/tmp/gateway.sock")
	}
}

func TestDaemonInstallDefaultRunnerUsesCurrentExecutable(t *testing.T) {
	originalRunner := runDaemonInstallCommand
	originalResolveExecutablePath := resolveExecutablePath
	originalInstall := installHTTPDaemon
	originalJSONWriter := daemonInstallJSONWriter
	originalLogWriter := daemonInstallLogWriter
	t.Cleanup(func() { runDaemonInstallCommand = originalRunner })
	t.Cleanup(func() { resolveExecutablePath = originalResolveExecutablePath })
	t.Cleanup(func() { installHTTPDaemon = originalInstall })
	t.Cleanup(func() { daemonInstallJSONWriter = originalJSONWriter })
	t.Cleanup(func() { daemonInstallLogWriter = originalLogWriter })

	runDaemonInstallCommand = defaultDaemonInstallCommandRunner
	resolveExecutablePath = func() (string, error) {
		return "/tmp/neocode", nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	daemonInstallJSONWriter = &stdout
	daemonInstallLogWriter = &stderr
	var captured urlscheme.HTTPDaemonInstallOptions
	installHTTPDaemon = func(options urlscheme.HTTPDaemonInstallOptions) (urlscheme.HTTPDaemonInstallResult, error) {
		captured = options
		return urlscheme.HTTPDaemonInstallResult{
			ListenAddress:      options.ListenAddress,
			AutostartMode:      "test-mode",
			DaemonStarted:      true,
			DaemonStartWarning: "",
		}, nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"daemon", "install", "--listen", "127.0.0.1:19921"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.ExecutablePath != "/tmp/neocode" {
		t.Fatalf("executable path = %q, want %q", captured.ExecutablePath, "/tmp/neocode")
	}
	if captured.ListenAddress != "127.0.0.1:19921" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "127.0.0.1:19921")
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status = %v, want ok", payload["status"])
	}
	if payload["daemon_started"] != true {
		t.Fatalf("daemon_started = %v, want true", payload["daemon_started"])
	}
	if !strings.Contains(stderr.String(), "daemon install succeeded") {
		t.Fatalf("stderr = %q, want success summary", stderr.String())
	}
}

func TestDaemonInstallDefaultRunnerFailureWritesRemedy(t *testing.T) {
	originalRunner := runDaemonInstallCommand
	originalResolveExecutablePath := resolveExecutablePath
	originalInstall := installHTTPDaemon
	originalJSONWriter := daemonInstallJSONWriter
	originalLogWriter := daemonInstallLogWriter
	t.Cleanup(func() { runDaemonInstallCommand = originalRunner })
	t.Cleanup(func() { resolveExecutablePath = originalResolveExecutablePath })
	t.Cleanup(func() { installHTTPDaemon = originalInstall })
	t.Cleanup(func() { daemonInstallJSONWriter = originalJSONWriter })
	t.Cleanup(func() { daemonInstallLogWriter = originalLogWriter })

	runDaemonInstallCommand = defaultDaemonInstallCommandRunner
	resolveExecutablePath = func() (string, error) {
		return "/tmp/neocode", nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	daemonInstallJSONWriter = &stdout
	daemonInstallLogWriter = &stderr
	installHTTPDaemon = func(options urlscheme.HTTPDaemonInstallOptions) (urlscheme.HTTPDaemonInstallResult, error) {
		return urlscheme.HTTPDaemonInstallResult{}, errors.New("boom")
	}

	command := NewRootCommand()
	command.SetArgs([]string{"daemon", "install", "--listen", "127.0.0.1:19921"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected install failure")
	}
	if !strings.Contains(stderr.String(), "remedy: run `/tmp/neocode daemon install --listen 127.0.0.1:19921`") {
		t.Fatalf("stderr = %q, want remedy command", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty on failure, got %q", stdout.String())
	}
}

func TestDaemonServeDoesNotExposeTokenFileFlag(t *testing.T) {
	command := NewRootCommand()
	command.SetArgs([]string{"daemon", "serve", "--token-file", "/tmp/auth.json"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error = %v, want unknown flag", err)
	}
}

func TestDaemonSubcommandSkipsGlobalPreload(t *testing.T) {
	originalPreload := runGlobalPreload
	originalRunner := runDaemonStatusCommand
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runDaemonStatusCommand = originalRunner })

	var called bool
	runGlobalPreload = func(context.Context) error {
		called = true
		return errors.New("should be skipped")
	}
	runDaemonStatusCommand = func(context.Context, daemonStatusCommandOptions) error { return nil }

	command := NewRootCommand()
	command.SetArgs([]string{"daemon", "status"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if called {
		t.Fatal("expected global preload to be skipped for daemon command")
	}
}

func TestDaemonSubcommandSkipsSilentUpdateCheck(t *testing.T) {
	originalSilentCheck := runSilentUpdateCheck
	originalRunner := runDaemonStatusCommand
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })
	t.Cleanup(func() { runDaemonStatusCommand = originalRunner })

	var called bool
	runSilentUpdateCheck = func(context.Context) {
		called = true
	}
	runDaemonStatusCommand = func(context.Context, daemonStatusCommandOptions) error { return nil }

	command := NewRootCommand()
	command.SetArgs([]string{"daemon", "status"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if called {
		t.Fatal("expected silent update check to be skipped for daemon command")
	}
}

func TestDaemonEncodeRunSubcommandUsesFlags(t *testing.T) {
	originalRunner := runDaemonEncodeCommand
	t.Cleanup(func() { runDaemonEncodeCommand = originalRunner })

	var captured daemonEncodeCommandOptions
	runDaemonEncodeCommand = func(_ context.Context, options daemonEncodeCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"daemon", "encode", "run",
		"--prompt", " explain RESTful API ",
		"--workdir", ` C:\project `,
		"--session-id", " session-1 ",
		"--listen", " 127.0.0.1:19921 ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Action != "run" {
		t.Fatalf("action = %q, want %q", captured.Action, "run")
	}
	if captured.Prompt != "explain RESTful API" {
		t.Fatalf("prompt = %q, want %q", captured.Prompt, "explain RESTful API")
	}
	if captured.Workdir != `C:\project` {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, `C:\project`)
	}
	if captured.SessionID != "session-1" {
		t.Fatalf("session id = %q, want %q", captured.SessionID, "session-1")
	}
	if captured.ListenAddress != "127.0.0.1:19921" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "127.0.0.1:19921")
	}
}

func TestDaemonEncodeReviewSubcommandUsesFlags(t *testing.T) {
	originalRunner := runDaemonEncodeCommand
	t.Cleanup(func() { runDaemonEncodeCommand = originalRunner })

	var captured daemonEncodeCommandOptions
	runDaemonEncodeCommand = func(_ context.Context, options daemonEncodeCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"daemon", "encode", "review",
		"--path", " internal/gateway/bootstrap.go ",
		"--workdir", " /repo ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Action != "review" {
		t.Fatalf("action = %q, want %q", captured.Action, "review")
	}
	if captured.Path != "internal/gateway/bootstrap.go" {
		t.Fatalf("path = %q, want %q", captured.Path, "internal/gateway/bootstrap.go")
	}
	if captured.Workdir != "/repo" {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, "/repo")
	}
}

func TestBuildDaemonEncodedWakeURLRunEncodesPromptAndWorkdir(t *testing.T) {
	urlText, err := buildDaemonEncodedWakeURL(daemonEncodeCommandOptions{
		Action:    "run",
		Prompt:    "解释RESTful API",
		Workdir:   `C:\project`,
		SessionID: "",
	})
	if err != nil {
		t.Fatalf("buildDaemonEncodedWakeURL() error = %v", err)
	}

	parsed, err := url.Parse(urlText)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsed.Scheme != "http" {
		t.Fatalf("scheme = %q, want http", parsed.Scheme)
	}
	if parsed.Host != "neocode:18921" {
		t.Fatalf("host = %q, want %q", parsed.Host, "neocode:18921")
	}
	if parsed.Path != "/run" {
		t.Fatalf("path = %q, want %q", parsed.Path, "/run")
	}
	if got := parsed.Query().Get("prompt"); got != "解释RESTful API" {
		t.Fatalf("prompt = %q, want %q", got, "解释RESTful API")
	}
	if got := parsed.Query().Get("workdir"); got != `C:\project` {
		t.Fatalf("workdir = %q, want %q", got, `C:\project`)
	}
}

func TestBuildDaemonEncodedWakeURLRunAllowsSessionOnly(t *testing.T) {
	urlText, err := buildDaemonEncodedWakeURL(daemonEncodeCommandOptions{
		Action:    "run",
		SessionID: "session-42",
	})
	if err != nil {
		t.Fatalf("buildDaemonEncodedWakeURL() error = %v", err)
	}
	parsed, err := url.Parse(urlText)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if got := parsed.Query().Get("session_id"); got != "session-42" {
		t.Fatalf("session_id = %q, want %q", got, "session-42")
	}
	if got := parsed.Query().Get("prompt"); got != "" {
		t.Fatalf("prompt = %q, want empty", got)
	}
}

func TestBuildDaemonEncodedWakeURLReviewRequiresWorkdirWithoutSession(t *testing.T) {
	_, err := buildDaemonEncodedWakeURL(daemonEncodeCommandOptions{
		Action: "review",
		Path:   "internal/gateway/bootstrap.go",
	})
	if err == nil {
		t.Fatal("expected missing workdir error")
	}
	if !strings.Contains(err.Error(), "--workdir") {
		t.Fatalf("error = %v, want contains --workdir", err)
	}
}
