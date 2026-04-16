package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"neo-code/internal/app"
	"neo-code/internal/gateway"
	"neo-code/internal/gateway/adapters/urlscheme"
)

func TestNewRootCommandPassesWorkdirFlagToLauncher(t *testing.T) {
	originalLauncher := launchRootProgram
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--workdir", `D:\项目\中文目录`})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Workdir != `D:\项目\中文目录` {
		t.Fatalf("expected workdir to be forwarded, got %q", captured.Workdir)
	}
}

func TestNewRootCommandAllowsEmptyWorkdir(t *testing.T) {
	originalLauncher := launchRootProgram
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Workdir != "" {
		t.Fatalf("expected empty workdir override, got %q", captured.Workdir)
	}
}

func TestNewRootCommandReturnsLauncherError(t *testing.T) {
	originalLauncher := launchRootProgram
	originalPreload := runGlobalPreload
	t.Cleanup(func() { launchRootProgram = originalLauncher })
	t.Cleanup(func() { runGlobalPreload = originalPreload })

	expected := errors.New("launch failed")
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		return expected
	}
	runGlobalPreload = func(context.Context) error { return nil }

	cmd := NewRootCommand()
	cmd.SetArgs([]string{})
	err := cmd.ExecuteContext(context.Background())
	if !errors.Is(err, expected) {
		t.Fatalf("expected launcher error %v, got %v", expected, err)
	}
}

func TestExecuteUsesOSArgs(t *testing.T) {
	originalLauncher := launchRootProgram
	originalArgs := os.Args
	t.Cleanup(func() {
		launchRootProgram = originalLauncher
		os.Args = originalArgs
	})

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}
	os.Args = []string{"neocode", "--workdir", `D:\项目\中文目录`}

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if captured.Workdir != `D:\项目\中文目录` {
		t.Fatalf("expected Execute to forward workdir, got %q", captured.Workdir)
	}
}

func TestDefaultRootProgramLauncherRunsProgram(t *testing.T) {
	originalNewProgram := newRootProgram
	t.Cleanup(func() { newRootProgram = originalNewProgram })

	cleanedUp := false
	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, func() error, error) {
		model := quitModel{}
		return tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard)), func() error { cleanedUp = true; return nil }, nil
	}

	if err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{Workdir: `D:\项目\中文目录`}); err != nil {
		t.Fatalf("defaultRootProgramLauncher() error = %v", err)
	}
	if !cleanedUp {
		t.Fatalf("expected cleanup to be called")
	}
}

func TestDefaultRootProgramLauncherReturnsNewProgramError(t *testing.T) {
	originalNewProgram := newRootProgram
	t.Cleanup(func() { newRootProgram = originalNewProgram })

	expected := errors.New("new program failed")
	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, func() error, error) {
		return nil, nil, expected
	}

	err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{})
	if !errors.Is(err, expected) {
		t.Fatalf("expected new program error %v, got %v", expected, err)
	}
}

func TestDefaultRootProgramLauncherReturnsCleanupErrorWhenRunSucceeds(t *testing.T) {
	originalNewProgram := newRootProgram
	t.Cleanup(func() { newRootProgram = originalNewProgram })

	cleanupErr := errors.New("cleanup failed")
	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, func() error, error) {
		model := quitModel{}
		return tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard)), func() error {
			return cleanupErr
		}, nil
	}

	err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{})
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("expected cleanup error %v, got %v", cleanupErr, err)
	}
}

func TestDefaultRootProgramLauncherJoinsRunAndCleanupErrors(t *testing.T) {
	originalNewProgram := newRootProgram
	t.Cleanup(func() { newRootProgram = originalNewProgram })

	runErr := context.Canceled
	cleanupErr := errors.New("cleanup failed")
	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, func() error, error) {
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		return tea.NewProgram(quitModel{}, tea.WithContext(cancelledCtx), tea.WithInput(nil), tea.WithOutput(io.Discard)), func() error {
			return cleanupErr
		}, nil
	}

	err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{})
	if !errors.Is(err, runErr) {
		t.Fatalf("expected joined error to include run error %v, got %v", runErr, err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("expected joined error to include cleanup error %v, got %v", cleanupErr, err)
	}
}

func TestGatewaySubcommandPassesFlagsToRunner(t *testing.T) {
	originalRunner := runGatewayCommand
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGatewayCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	var captured gatewayCommandOptions
	runGatewayCommand = func(ctx context.Context, options gatewayCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"gateway",
		"--listen", "  /tmp/gateway.sock  ",
		"--http-listen", "  127.0.0.1:19080  ",
		"--log-level", " WARN ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.ListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "/tmp/gateway.sock")
	}
	if captured.LogLevel != "warn" {
		t.Fatalf("log level = %q, want %q", captured.LogLevel, "warn")
	}
	if captured.HTTPAddress != "127.0.0.1:19080" {
		t.Fatalf("http address = %q, want %q", captured.HTTPAddress, "127.0.0.1:19080")
	}
}

func TestGatewaySubcommandRejectsInvalidLogLevel(t *testing.T) {
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	command := NewRootCommand()
	command.SetArgs([]string{"gateway", "--log-level", "trace"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected invalid log level error")
	}
	if !strings.Contains(err.Error(), "invalid --log-level") {
		t.Fatalf("error = %v, want contains %q", err, "invalid --log-level")
	}
}

func TestDefaultGatewayCommandRunnerSuccess(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })

	server := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gatewayServer, error) {
		return server, nil
	}
	networkServer := &stubGatewayServer{listenAddress: "127.0.0.1:8080"}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
		return networkServer, nil
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if err != nil {
		t.Fatalf("defaultGatewayCommandRunner() error = %v", err)
	}
	if !server.serveCalled {
		t.Fatal("expected server Serve to be called")
	}
	if !server.closeCalled {
		t.Fatal("expected server Close to be called")
	}
	if !networkServer.closeCalled {
		t.Fatal("expected network server Close to be called")
	}
}

func TestDefaultGatewayCommandRunnerReturnsConstructorError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })

	expected := errors.New("new gateway server failed")
	newGatewayServer = func(options gateway.ServerOptions) (gatewayServer, error) {
		return nil, expected
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
		return &stubGatewayServer{listenAddress: "127.0.0.1:8080"}, nil
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected constructor error %v, got %v", expected, err)
	}
}

func TestDefaultGatewayCommandRunnerReturnsServeError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })

	expected := errors.New("serve failed")
	server := &stubGatewayServer{
		listenAddress: "stub://gateway",
		serveErr:      expected,
	}
	newGatewayServer = func(options gateway.ServerOptions) (gatewayServer, error) {
		return server, nil
	}
	networkServer := &stubGatewayServer{listenAddress: "127.0.0.1:8080"}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
		return networkServer, nil
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected serve error %v, got %v", expected, err)
	}
	if !server.closeCalled {
		t.Fatal("expected server Close to be called")
	}
	if !networkServer.closeCalled {
		t.Fatal("expected network server Close to be called")
	}
}

func TestDefaultGatewayCommandRunnerDegradesWhenNetworkServeFails(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })

	ipcServer := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gatewayServer, error) {
		return ipcServer, nil
	}
	networkServer := &stubGatewayServer{
		listenAddress: "127.0.0.1:8080",
		serveErr:      errors.New("bind: address already in use"),
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
		return networkServer, nil
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if err != nil {
		t.Fatalf("expected graceful degradation on network serve error, got %v", err)
	}
	if !ipcServer.serveCalled {
		t.Fatal("expected ipc server Serve to be called")
	}
	if !ipcServer.closeCalled {
		t.Fatal("expected ipc server Close to be called")
	}
	if !networkServer.closeCalled {
		t.Fatal("expected network server Close to be called")
	}
}

func TestDefaultGatewayCommandRunnerReturnsNetworkConstructorError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })

	networkErr := errors.New("new network server failed")
	ipcServer := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gatewayServer, error) {
		return ipcServer, nil
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
		return nil, networkErr
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if !errors.Is(err, networkErr) {
		t.Fatalf("expected network constructor error %v, got %v", networkErr, err)
	}
	if !ipcServer.closeCalled {
		t.Fatal("expected ipc server Close to be called when network constructor fails")
	}
}

func TestDefaultNewGatewayServer(t *testing.T) {
	server, err := defaultNewGatewayServer(gateway.ServerOptions{
		ListenAddress: "stub://gateway",
	})
	if err != nil {
		t.Fatalf("defaultNewGatewayServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("defaultNewGatewayServer() returned nil server")
	}
}

func TestDefaultNewGatewayNetworkServer(t *testing.T) {
	server, err := defaultNewGatewayNetworkServer(gateway.NetworkServerOptions{
		ListenAddress: "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatalf("defaultNewGatewayNetworkServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("defaultNewGatewayNetworkServer() returned nil server")
	}
}

func TestURLDispatchSubcommandUsesURLFlag(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	var captured urlDispatchCommandOptions
	runURLDispatchCommand = func(ctx context.Context, options urlDispatchCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"url-dispatch",
		"--url", "  neocode://review?path=README.md  ",
		"--listen", "  /tmp/gateway.sock  ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.URL != "neocode://review?path=README.md" {
		t.Fatalf("url = %q, want %q", captured.URL, "neocode://review?path=README.md")
	}
	if captured.ListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("listen address = %q, want %q", captured.ListenAddress, "/tmp/gateway.sock")
	}
}

func TestURLDispatchSubcommandUsesPositionalURL(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	var captured urlDispatchCommandOptions
	runURLDispatchCommand = func(ctx context.Context, options urlDispatchCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.URL != "neocode://review?path=README.md" {
		t.Fatalf("url = %q, want %q", captured.URL, "neocode://review?path=README.md")
	}
}

func TestURLDispatchSubcommandRunnerErrorTriggersExit(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalExitProcess := exitProcess
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	runURLDispatchCommand = func(context.Context, urlDispatchCommandOptions) error {
		return errors.New("runner failed")
	}

	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
}

func TestURLDispatchSubcommandRejectsInvalidScheme(t *testing.T) {
	originalExitProcess := exitProcess
	originalPreload := runGlobalPreload
	originalStderr := os.Stderr
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { os.Stderr = originalStderr })
	runGlobalPreload = func(context.Context) error { return nil }
	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	t.Cleanup(func() { _ = stderrReader.Close() })
	os.Stderr = stderrWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "http://example.com"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stderrWriter.Close()
	stderrOutput, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
	if !strings.Contains(string(stderrOutput), `"status":"error"`) {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), `"status":"error"`)
	}
	if !strings.Contains(string(stderrOutput), `"code":"invalid_scheme"`) {
		t.Fatalf("stderr = %q, want contains invalid_scheme", string(stderrOutput))
	}
}

func TestURLDispatchSubcommandRejectsMissingActionHost(t *testing.T) {
	originalExitProcess := exitProcess
	originalPreload := runGlobalPreload
	originalStderr := os.Stderr
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { os.Stderr = originalStderr })
	runGlobalPreload = func(context.Context) error { return nil }
	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	t.Cleanup(func() { _ = stderrReader.Close() })
	os.Stderr = stderrWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stderrWriter.Close()
	stderrOutput, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
	if !strings.Contains(string(stderrOutput), `"status":"error"`) {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), `"status":"error"`)
	}
	if !strings.Contains(string(stderrOutput), `"code":"missing_required_field"`) {
		t.Fatalf("stderr = %q, want contains missing_required_field", string(stderrOutput))
	}
}

func TestURLDispatchSubcommandRejectsMissingURL(t *testing.T) {
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	runGlobalPreload = func(context.Context) error { return nil }

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch"})
	err := command.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected missing url error")
	}
	if !strings.Contains(err.Error(), "missing required --url or positional <url>") {
		t.Fatalf("error = %v, want missing url message", err)
	}
}

func TestURLDispatchSubcommandDefaultRunnerError(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalDispatch := dispatchURLThroughIPC
	originalExitProcess := exitProcess
	originalWriteDispatchError := writeDispatchError
	originalPreload := runGlobalPreload
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { dispatchURLThroughIPC = originalDispatch })
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { writeDispatchError = originalWriteDispatchError })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	})
	runGlobalPreload = func(context.Context) error { return nil }
	runURLDispatchCommand = defaultURLDispatchCommandRunner
	dispatchURLThroughIPC = func(context.Context, urlscheme.DispatchRequest) (urlscheme.DispatchResult, error) {
		return urlscheme.DispatchResult{}, &urlscheme.DispatchError{
			Code:    gateway.ErrorCodeInvalidAction.String(),
			Message: "unsupported wake action",
		}
	}
	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	t.Cleanup(func() { _ = stderrReader.Close() })
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	t.Cleanup(func() { _ = stdoutReader.Close() })
	os.Stderr = stderrWriter
	os.Stdout = stdoutWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	stdoutOutput, readStdoutErr := io.ReadAll(stdoutReader)
	if readStdoutErr != nil {
		t.Fatalf("read stdout: %v", readStdoutErr)
	}
	if len(strings.TrimSpace(string(stdoutOutput))) != 0 {
		t.Fatalf("stdout = %q, want empty output", string(stdoutOutput))
	}

	stderrOutput, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
	if !strings.Contains(string(stderrOutput), `"status":"error"`) {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), `"status":"error"`)
	}
	if !strings.Contains(string(stderrOutput), gateway.ErrorCodeInvalidAction.String()) {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), gateway.ErrorCodeInvalidAction.String())
	}
	if strings.Contains(string(stderrOutput), "Error:") {
		t.Fatalf("stderr = %q, want pure JSON without cobra prefix", string(stderrOutput))
	}
}

func TestURLDispatchSubcommandDefaultRunnerErrorFallsBackWhenJSONWriteFails(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalDispatch := dispatchURLThroughIPC
	originalExitProcess := exitProcess
	originalWriteDispatchError := writeDispatchError
	originalPreload := runGlobalPreload
	originalStderr := os.Stderr
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { dispatchURLThroughIPC = originalDispatch })
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { writeDispatchError = originalWriteDispatchError })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { os.Stderr = originalStderr })
	runGlobalPreload = func(context.Context) error { return nil }

	runURLDispatchCommand = defaultURLDispatchCommandRunner
	dispatchURLThroughIPC = func(context.Context, urlscheme.DispatchRequest) (urlscheme.DispatchResult, error) {
		return urlscheme.DispatchResult{}, &urlscheme.DispatchError{
			Code:    gateway.ErrorCodeInvalidAction.String(),
			Message: "unsupported wake action",
		}
	}
	writeDispatchError = func(io.Writer, error) error {
		return errors.New("encode error")
	}

	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	t.Cleanup(func() { _ = stderrReader.Close() })
	os.Stderr = stderrWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stderrWriter.Close()
	stderrOutput, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
	if !strings.Contains(string(stderrOutput), fallbackDispatchErrorJSON) {
		t.Fatalf("stderr = %q, want contains fallback json", string(stderrOutput))
	}
}

func TestURLDispatchSubcommandDefaultRunnerSuccess(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalDispatch := dispatchURLThroughIPC
	originalExitProcess := exitProcess
	originalWriteDispatchSuccess := writeDispatchSuccess
	originalPreload := runGlobalPreload
	originalStdout := os.Stdout
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { dispatchURLThroughIPC = originalDispatch })
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { writeDispatchSuccess = originalWriteDispatchSuccess })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { os.Stdout = originalStdout })
	runGlobalPreload = func(context.Context) error { return nil }
	exitProcess = func(code int) {
		t.Fatalf("unexpected exit with code %d", code)
	}

	runURLDispatchCommand = defaultURLDispatchCommandRunner
	dispatchURLThroughIPC = func(context.Context, urlscheme.DispatchRequest) (urlscheme.DispatchResult, error) {
		return urlscheme.DispatchResult{
			ListenAddress: "/tmp/gateway.sock",
			Response: gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-1",
				Payload: map[string]any{
					"message": "wake intent accepted",
				},
			},
		}, nil
	}

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	t.Cleanup(func() { _ = stdoutReader.Close() })
	os.Stdout = stdoutWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stdoutWriter.Close()
	stdoutOutput, readErr := io.ReadAll(stdoutReader)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	if !strings.Contains(string(stdoutOutput), `"status":"ok"`) {
		t.Fatalf("stdout = %q, want contains %q", string(stdoutOutput), `"status":"ok"`)
	}
	if !strings.Contains(string(stdoutOutput), string(gateway.FrameActionWakeOpenURL)) {
		t.Fatalf("stdout = %q, want contains %q", string(stdoutOutput), gateway.FrameActionWakeOpenURL)
	}
}

func TestURLDispatchSubcommandDefaultRunnerSuccessOutputFailure(t *testing.T) {
	originalRunner := runURLDispatchCommand
	originalDispatch := dispatchURLThroughIPC
	originalExitProcess := exitProcess
	originalWriteDispatchSuccess := writeDispatchSuccess
	originalWriteDispatchError := writeDispatchError
	originalPreload := runGlobalPreload
	originalStderr := os.Stderr
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })
	t.Cleanup(func() { dispatchURLThroughIPC = originalDispatch })
	t.Cleanup(func() { exitProcess = originalExitProcess })
	t.Cleanup(func() { writeDispatchSuccess = originalWriteDispatchSuccess })
	t.Cleanup(func() { writeDispatchError = originalWriteDispatchError })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { os.Stderr = originalStderr })
	runGlobalPreload = func(context.Context) error { return nil }

	runURLDispatchCommand = defaultURLDispatchCommandRunner
	dispatchURLThroughIPC = func(context.Context, urlscheme.DispatchRequest) (urlscheme.DispatchResult, error) {
		return urlscheme.DispatchResult{
			ListenAddress: "/tmp/gateway.sock",
			Response: gateway.MessageFrame{
				Type:      gateway.FrameTypeAck,
				Action:    gateway.FrameActionWakeOpenURL,
				RequestID: "wake-1",
				Payload: map[string]any{
					"message": "wake intent accepted",
				},
			},
		}, nil
	}
	writeDispatchSuccess = func(io.Writer, urlscheme.DispatchResult) error {
		return errors.New("stdout write failed")
	}

	exitCode := 0
	exitProcess = func(code int) {
		exitCode = code
	}

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	t.Cleanup(func() { _ = stderrReader.Close() })
	os.Stderr = stderrWriter

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	_ = stderrWriter.Close()
	stderrOutput, readErr := io.ReadAll(stderrReader)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want %d", exitCode, 1)
	}
	if !strings.Contains(string(stderrOutput), `"status":"error"`) {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), `"status":"error"`)
	}
	if !strings.Contains(string(stderrOutput), "stdout write failed") {
		t.Fatalf("stderr = %q, want contains %q", string(stderrOutput), "stdout write failed")
	}
}

func TestNormalizeDispatchURL(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		normalized, err := normalizeDispatchURL("  neocode://review?path=README.md  ")
		if err != nil {
			t.Fatalf("normalizeDispatchURL() error = %v", err)
		}
		if normalized != "neocode://review?path=README.md" {
			t.Fatalf("normalized = %q, want %q", normalized, "neocode://review?path=README.md")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		normalized, err := normalizeDispatchURL("://bad-url")
		if err != nil {
			t.Fatalf("normalizeDispatchURL() error = %v", err)
		}
		if normalized != "://bad-url" {
			t.Fatalf("normalized = %q, want %q", normalized, "://bad-url")
		}
	})

	t.Run("invalid scheme", func(t *testing.T) {
		normalized, err := normalizeDispatchURL("https://example.com")
		if err != nil {
			t.Fatalf("normalizeDispatchURL() error = %v", err)
		}
		if normalized != "https://example.com" {
			t.Fatalf("normalized = %q, want %q", normalized, "https://example.com")
		}
	})

	t.Run("empty value", func(t *testing.T) {
		_, err := normalizeDispatchURL(" ")
		if err == nil {
			t.Fatal("expected empty value error")
		}
		if !strings.Contains(err.Error(), "missing required --url or positional <url>") {
			t.Fatalf("error = %v, want missing value message", err)
		}
	})
}

func TestURLDispatchSkipsGlobalPreload(t *testing.T) {
	originalPreload := runGlobalPreload
	originalRunner := runURLDispatchCommand
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runURLDispatchCommand = originalRunner })

	var called bool
	runGlobalPreload = func(context.Context) error {
		called = true
		return errors.New("should be skipped")
	}
	runURLDispatchCommand = func(context.Context, urlDispatchCommandOptions) error {
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"url-dispatch", "--url", "neocode://review?path=README.md"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if called {
		t.Fatal("expected global preload to be skipped for url-dispatch")
	}
}

func TestGatewayRunsGlobalPreload(t *testing.T) {
	originalPreload := runGlobalPreload
	t.Cleanup(func() { runGlobalPreload = originalPreload })

	expected := errors.New("preload failed")
	runGlobalPreload = func(context.Context) error {
		return expected
	}

	command := NewRootCommand()
	command.SetArgs([]string{"gateway"})
	err := command.ExecuteContext(context.Background())
	if !errors.Is(err, expected) {
		t.Fatalf("expected preload error %v, got %v", expected, err)
	}
}

func TestShouldSkipGlobalPreload(t *testing.T) {
	if !shouldSkipGlobalPreload(&cobra.Command{Use: "url-dispatch"}) {
		t.Fatal("url-dispatch should skip global preload")
	}
	if shouldSkipGlobalPreload(&cobra.Command{Use: "gateway"}) {
		t.Fatal("gateway should not skip global preload")
	}
	if shouldSkipGlobalPreload(nil) {
		t.Fatal("nil command should not skip global preload")
	}
}

func TestWriteURLDispatchSuccessOutput(t *testing.T) {
	var buffer bytes.Buffer
	err := writeURLDispatchSuccessOutput(&buffer, urlscheme.DispatchResult{
		ListenAddress: "/tmp/gateway.sock",
		Response: gateway.MessageFrame{
			Type:      gateway.FrameTypeAck,
			Action:    gateway.FrameActionWakeOpenURL,
			RequestID: "wake-1",
			Payload: map[string]any{
				"message": "wake intent accepted",
			},
		},
	})
	if err != nil {
		t.Fatalf("write success output: %v", err)
	}

	var output map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &output); err != nil {
		t.Fatalf("unmarshal success output: %v", err)
	}
	if output["status"] != "ok" {
		t.Fatalf("status = %v, want %q", output["status"], "ok")
	}
}

func TestWriteURLDispatchErrorOutput(t *testing.T) {
	t.Run("dispatch error", func(t *testing.T) {
		var buffer bytes.Buffer
		err := writeURLDispatchErrorOutput(&buffer, &urlscheme.DispatchError{
			Code:    "invalid_action",
			Message: "unsupported wake action",
		})
		if err != nil {
			t.Fatalf("write error output: %v", err)
		}
		if !strings.Contains(buffer.String(), `"code":"invalid_action"`) {
			t.Fatalf("output = %q, want contains invalid_action", buffer.String())
		}
	})

	t.Run("generic error", func(t *testing.T) {
		var buffer bytes.Buffer
		err := writeURLDispatchErrorOutput(&buffer, errors.New("boom"))
		if err != nil {
			t.Fatalf("write error output: %v", err)
		}
		if !strings.Contains(buffer.String(), `"code":"internal_error"`) {
			t.Fatalf("output = %q, want contains internal_error", buffer.String())
		}
	})
}

type quitModel struct{}

type stubGatewayServer struct {
	listenAddress string
	serveErr      error
	closeErr      error
	serveCalled   bool
	closeCalled   bool
}

func (s *stubGatewayServer) ListenAddress() string {
	return s.listenAddress
}

func (s *stubGatewayServer) Serve(_ context.Context, _ gateway.RuntimePort) error {
	s.serveCalled = true
	return s.serveErr
}

func (s *stubGatewayServer) Close(_ context.Context) error {
	s.closeCalled = true
	return s.closeErr
}

func (quitModel) Init() tea.Cmd {
	return tea.Quit
}

func (quitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return quitModel{}, nil
}

func (quitModel) View() string {
	return ""
}
