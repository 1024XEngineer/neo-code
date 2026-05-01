package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"neo-code/internal/app"
	"neo-code/internal/config"
	"neo-code/internal/gateway"
	"neo-code/internal/tools"
	"neo-code/internal/updater"
)

func init() {
	runSilentUpdateCheck = func(context.Context) {}
}

func TestNewRootCommandPassesWorkdirFlagToLauncher(t *testing.T) {
	originalLauncher := launchRootProgram
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--workdir", `D:\椤圭洰\涓枃鐩綍`, "--session", "session-flag"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Workdir != `D:\椤圭洰\涓枃鐩綍` {
		t.Fatalf("expected workdir to be forwarded, got %q", captured.Workdir)
	}
	if captured.SessionID != "session-flag" {
		t.Fatalf("expected session id to be forwarded, got %q", captured.SessionID)
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
	if captured.SessionID != "" {
		t.Fatalf("expected empty session override, got %q", captured.SessionID)
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
	os.Args = []string{"neocode", "--workdir", `D:\椤圭洰\涓枃鐩綍`, "--session", "session-execute"}

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if captured.Workdir != `D:\椤圭洰\涓枃鐩綍` {
		t.Fatalf("expected Execute to forward workdir, got %q", captured.Workdir)
	}
	if captured.SessionID != "session-execute" {
		t.Fatalf("expected Execute to forward session id, got %q", captured.SessionID)
	}
}

func TestNewRootCommandPassesWakeInputFlagToLauncher(t *testing.T) {
	originalLauncher := launchRootProgram
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--wake-input-b64", "payload-123"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.WakeInputB64 != "payload-123" {
		t.Fatalf("wake input = %q, want %q", captured.WakeInputB64, "payload-123")
	}
}

func TestExecuteUsesOSArgsWithWakeInput(t *testing.T) {
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
	os.Args = []string{"neocode", "--wake-input-b64", "payload-xyz"}

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if captured.WakeInputB64 != "payload-xyz" {
		t.Fatalf("wake input = %q, want %q", captured.WakeInputB64, "payload-xyz")
	}
}

func TestExecuteWaitsForSilentUpdateCheckCompletion(t *testing.T) {
	originalLauncher := launchRootProgram
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	originalArgs := os.Args
	t.Cleanup(func() {
		launchRootProgram = originalLauncher
		runGlobalPreload = originalPreload
		runSilentUpdateCheck = originalSilentCheck
		os.Args = originalArgs
	})

	_ = ConsumeUpdateNotice()
	runGlobalPreload = func(context.Context) error { return nil }
	launchRootProgram = func(context.Context, app.BootstrapOptions) error { return nil }
	runSilentUpdateCheck = func(context.Context) {
		done := make(chan struct{})
		setSilentUpdateCheckDone(done)
		go func() {
			time.Sleep(50 * time.Millisecond)
			setUpdateNotice("鍙戠幇鏂扮増鏈? v0.2.1")
			close(done)
		}()
	}
	os.Args = []string{"neocode"}

	if err := Execute(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := ConsumeUpdateNotice(); got == "" {
		t.Fatal("expected update notice after Execute waits for silent check")
	}
}

func TestWaitSilentUpdateCheckDoneReturnsOnTimeout(t *testing.T) {
	blocked := make(chan struct{})
	setSilentUpdateCheckDone(blocked)
	t.Cleanup(func() { setSilentUpdateCheckDone(nil) })

	start := time.Now()
	waitSilentUpdateCheckDone(30 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Fatalf("wait duration out of expected range, got %s", elapsed)
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

	if err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{Workdir: `D:\椤圭洰\涓枃鐩綍`}); err != nil {
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

func TestMustReadInheritedWorkdirBranches(t *testing.T) {
	if got := mustReadInheritedWorkdir(nil); got != "" {
		t.Fatalf("mustReadInheritedWorkdir(nil) = %q, want empty", got)
	}

	cmd := &cobra.Command{}
	if got := mustReadInheritedWorkdir(cmd); got != "" {
		t.Fatalf("mustReadInheritedWorkdir(cmd without workdir flag) = %q, want empty", got)
	}
}

func TestDefaultGatewayCommandRunnerSuccess(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()
	newAuthManager = stubGatewayAuthManagerBuilder()

	server := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return server, nil
	}
	networkServer := &stubGatewayServer{listenAddress: "127.0.0.1:8080"}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
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

func TestDefaultGatewayCommandRunnerReturnsBuildRuntimePortError(t *testing.T) {
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	newAuthManager = stubGatewayAuthManagerBuilder()

	buildGatewayRuntimePort = func(context.Context, string) (gateway.RuntimePort, func() error, error) {
		return nil, nil, errors.New("build runtime port failed")
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if err == nil {
		t.Fatal("expected build runtime port error")
	}
	if !strings.Contains(err.Error(), "initialize gateway runtime") {
		t.Fatalf("error = %v, want contains initialize gateway runtime", err)
	}
}

func TestDefaultGatewayCommandRunnerReturnsConstructorError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()
	newAuthManager = stubGatewayAuthManagerBuilder()

	expected := errors.New("new gateway server failed")
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return nil, expected
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
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

func TestDefaultGatewayCommandRunnerReturnsLoadConfigError(t *testing.T) {
	prepareGatewayCommandRunnerTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := defaultGatewayCommandRunner(ctx, gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if err == nil {
		t.Fatal("expected load config error")
	}
}

func TestDefaultGatewayCommandRunnerReturnsAuthManagerError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	originalNewAuthManager := newAuthManager
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()

	newAuthManager = func(string) (gateway.TokenAuthenticator, error) {
		return nil, errors.New("auth manager failed")
	}
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return &stubGatewayServer{listenAddress: "stub://gateway"}, nil
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
		return &stubGatewayServer{listenAddress: "127.0.0.1:8080"}, nil
	}

	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
	})
	if err == nil || !strings.Contains(err.Error(), "initialize gateway auth manager") {
		t.Fatalf("expected auth manager error, got %v", err)
	}
}

func TestDefaultGatewayCommandRunnerReturnsServeError(t *testing.T) {
	originalNewGatewayServer := newGatewayServer
	originalNewGatewayNetwork := newGatewayNetwork
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()
	newAuthManager = stubGatewayAuthManagerBuilder()

	expected := errors.New("serve failed")
	server := &stubGatewayServer{
		listenAddress: "stub://gateway",
		serveErr:      expected,
	}
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return server, nil
	}
	networkServer := &stubGatewayServer{listenAddress: "127.0.0.1:8080"}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
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
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()
	newAuthManager = stubGatewayAuthManagerBuilder()

	ipcServer := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return ipcServer, nil
	}
	networkServer := &stubGatewayServer{
		listenAddress: "127.0.0.1:8080",
		serveErr:      errors.New("bind: address already in use"),
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
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
	originalBuildGatewayRuntimePort := buildGatewayRuntimePort
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newGatewayServer = originalNewGatewayServer })
	t.Cleanup(func() { newGatewayNetwork = originalNewGatewayNetwork })
	t.Cleanup(func() { buildGatewayRuntimePort = originalBuildGatewayRuntimePort })
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	buildGatewayRuntimePort = stubGatewayRuntimePortBuilder()
	newAuthManager = stubGatewayAuthManagerBuilder()

	networkErr := errors.New("new network server failed")
	ipcServer := &stubGatewayServer{listenAddress: "stub://gateway"}
	newGatewayServer = func(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
		return ipcServer, nil
	}
	newGatewayNetwork = func(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
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

func TestDefaultGatewayCommandRunnerRejectsInvalidACLMode(t *testing.T) {
	originalNewAuthManager := newAuthManager
	t.Cleanup(func() { newAuthManager = originalNewAuthManager })
	prepareGatewayCommandRunnerTestEnv(t)
	newAuthManager = stubGatewayAuthManagerBuilder()
	err := defaultGatewayCommandRunner(context.Background(), gatewayCommandOptions{
		ListenAddress: "stub://gateway",
		HTTPAddress:   "127.0.0.1:8080",
		LogLevel:      "info",
		ACLMode:       "custom",
	})
	if err == nil {
		t.Fatal("expected invalid acl mode error")
	}
	if !strings.Contains(err.Error(), "gateway config override invalid") {
		t.Fatalf("error = %v, want contains %q", err, "gateway config override invalid")
	}
	if !strings.Contains(err.Error(), "acl_mode must be") {
		t.Fatalf("error = %v, want contains %q", err, "acl_mode must be")
	}
}

func TestBuildGatewayControlPlaneACL(t *testing.T) {
	t.Run("strict mode", func(t *testing.T) {
		acl, err := buildGatewayControlPlaneACL("strict")
		if err != nil {
			t.Fatalf("buildGatewayControlPlaneACL() error = %v", err)
		}
		if acl == nil {
			t.Fatal("expected non-nil acl")
		}
	})

	t.Run("empty mode uses strict", func(t *testing.T) {
		acl, err := buildGatewayControlPlaneACL("  ")
		if err != nil {
			t.Fatalf("buildGatewayControlPlaneACL() error = %v", err)
		}
		if acl == nil {
			t.Fatal("expected non-nil acl")
		}
	})

	t.Run("unsupported mode", func(t *testing.T) {
		acl, err := buildGatewayControlPlaneACL("allow-all")
		if err == nil {
			t.Fatal("expected unsupported mode error")
		}
		if acl != nil {
			t.Fatalf("acl = %#v, want nil", acl)
		}
		if !strings.Contains(err.Error(), "unsupported gateway acl mode") {
			t.Fatalf("error = %v, want contains unsupported mode message", err)
		}
	})
}

func TestApplyGatewayFlagOverrides(t *testing.T) {
	t.Run("nil config no-op", func(t *testing.T) {
		applyGatewayFlagOverrides(nil, gatewayCommandOptions{})
	})

	t.Run("all override fields", func(t *testing.T) {
		gatewayConfig := config.StaticDefaults().Gateway
		applyGatewayFlagOverrides(&gatewayConfig, gatewayCommandOptions{
			ACLMode:                  "strict",
			MaxFrameBytes:            2048,
			IPCMaxConnections:        32,
			HTTPMaxRequestBytes:      4096,
			HTTPMaxStreamConnections: 16,
			IPCReadSec:               11,
			IPCWriteSec:              12,
			HTTPReadSec:              13,
			HTTPWriteSec:             14,
			HTTPShutdownSec:          15,
			MetricsEnabledOverridden: true,
			MetricsEnabled:           false,
		})

		if gatewayConfig.Security.ACLMode != "strict" {
			t.Fatalf("acl_mode = %q, want strict", gatewayConfig.Security.ACLMode)
		}
		if gatewayConfig.Limits.MaxFrameBytes != 2048 || gatewayConfig.Limits.IPCMaxConnections != 32 {
			t.Fatalf("limits = %#v, want overrides applied", gatewayConfig.Limits)
		}
		if gatewayConfig.Limits.HTTPMaxRequestBytes != 4096 || gatewayConfig.Limits.HTTPMaxStreamConnections != 16 {
			t.Fatalf("http limits = %#v, want overrides applied", gatewayConfig.Limits)
		}
		if gatewayConfig.Timeouts.IPCReadSec != 11 || gatewayConfig.Timeouts.IPCWriteSec != 12 {
			t.Fatalf("ipc timeouts = %#v, want overrides applied", gatewayConfig.Timeouts)
		}
		if gatewayConfig.Timeouts.HTTPReadSec != 13 || gatewayConfig.Timeouts.HTTPWriteSec != 14 ||
			gatewayConfig.Timeouts.HTTPShutdownSec != 15 {
			t.Fatalf("http timeouts = %#v, want overrides applied", gatewayConfig.Timeouts)
		}
		if gatewayConfig.Observability.MetricsEnabled == nil || *gatewayConfig.Observability.MetricsEnabled {
			t.Fatalf("metrics_enabled = %#v, want false", gatewayConfig.Observability.MetricsEnabled)
		}
	})
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
	if shouldSkipGlobalPreload(&cobra.Command{Use: "gateway"}) {
		t.Fatal("gateway should not skip global preload")
	}
	command := &cobra.Command{
		Use: "daemon",
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload: "true",
		},
	}
	if !shouldSkipGlobalPreload(command) {
		t.Fatal("command with skip annotation should skip global preload")
	}
	if shouldSkipGlobalPreload(nil) {
		t.Fatal("nil command should not skip global preload")
	}
}

func TestNormalizedCommandName(t *testing.T) {
	if got := normalizedCommandName(nil); got != "" {
		t.Fatalf("normalizedCommandName(nil) = %q, want empty", got)
	}
	if got := normalizedCommandName(&cobra.Command{Use: "GaTeWaY"}); got != "gateway" {
		t.Fatalf("normalizedCommandName() = %q, want %q", got, "gateway")
	}
}

func TestShouldSkipSilentUpdateCheck(t *testing.T) {
	if !shouldSkipSilentUpdateCheck(&cobra.Command{Use: "update"}) {
		t.Fatal("update should skip silent update check")
	}
	if !shouldSkipSilentUpdateCheck(&cobra.Command{Use: "version"}) {
		t.Fatal("version should skip silent update check")
	}
	if shouldSkipSilentUpdateCheck(&cobra.Command{Use: "gateway"}) {
		t.Fatal("gateway should not skip silent update check")
	}
	command := &cobra.Command{
		Use: "daemon",
		Annotations: map[string]string{
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
	}
	if !shouldSkipSilentUpdateCheck(command) {
		t.Fatal("command with skip annotation should skip silent update check")
	}
	if shouldSkipSilentUpdateCheck(nil) {
		t.Fatal("nil command should not skip silent update check")
	}
}

func TestRootCommandRunsSilentUpdateCheckAfterPreload(t *testing.T) {
	originalLauncher := launchRootProgram
	originalPreload := runGlobalPreload
	originalSilentCheck := runSilentUpdateCheck
	t.Cleanup(func() { launchRootProgram = originalLauncher })
	t.Cleanup(func() { runGlobalPreload = originalPreload })
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })

	events := make([]string, 0, 3)
	runGlobalPreload = func(context.Context) error {
		events = append(events, "preload")
		return nil
	}
	runSilentUpdateCheck = func(context.Context) {
		events = append(events, "check")
	}
	launchRootProgram = func(context.Context, app.BootstrapOptions) error {
		events = append(events, "run")
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	want := []string{"preload", "check", "run"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestUpdateCommandSkipsSilentUpdateCheck(t *testing.T) {
	originalSilentCheck := runSilentUpdateCheck
	originalRunner := runUpdateCommand
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })
	t.Cleanup(func() { runUpdateCommand = originalRunner })

	var called bool
	runSilentUpdateCheck = func(context.Context) {
		called = true
	}
	runUpdateCommand = func(context.Context, updateCommandOptions) (updater.UpdateResult, error) {
		return updater.UpdateResult{Updated: false, LatestVersion: "v0.2.1"}, nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"update"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if called {
		t.Fatal("expected silent update check to be skipped for update command")
	}
}

func TestVersionCommandSkipsSilentUpdateCheck(t *testing.T) {
	originalSilentCheck := runSilentUpdateCheck
	originalRunner := runVersionCommand
	t.Cleanup(func() { runSilentUpdateCheck = originalSilentCheck })
	t.Cleanup(func() { runVersionCommand = originalRunner })

	var called bool
	runSilentUpdateCheck = func(context.Context) {
		called = true
	}
	runVersionCommand = func(context.Context, versionCommandOptions) (versionCommandResult, error) {
		return versionCommandResult{
			CurrentVersion: "v1.0.0",
			LatestVersion:  "v1.0.0",
			Comparable:     true,
		}, nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"version"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if called {
		t.Fatal("expected silent update check to be skipped for version command")
	}
}

func TestSanitizeVersionForTerminal(t *testing.T) {
	dirty := "\x1b[31mv0.2.1\x1b[0m\t\n\r\x00"
	if got := sanitizeVersionForTerminal(dirty); got != "v0.2.1" {
		t.Fatalf("sanitizeVersionForTerminal() = %q, want %q", got, "v0.2.1")
	}
}

func TestDefaultSilentUpdateCheckSkipsForNonReleaseVersion(t *testing.T) {
	originalVersionReader := readCurrentVersion
	originalCheckLatest := checkLatestRelease
	t.Cleanup(func() { readCurrentVersion = originalVersionReader })
	t.Cleanup(func() { checkLatestRelease = originalCheckLatest })

	readCurrentVersion = func() string { return "dev" }

	var called bool
	checkLatestRelease = func(context.Context, updater.CheckOptions) (updater.CheckResult, error) {
		called = true
		return updater.CheckResult{}, nil
	}

	defaultSilentUpdateCheck(context.Background())
	if called {
		t.Fatal("expected release check to be skipped for non-semver version")
	}
}

func TestDefaultSilentUpdateCheckSetsSanitizedNotice(t *testing.T) {
	_ = ConsumeUpdateNotice()

	originalVersionReader := readCurrentVersion
	originalCheckLatest := checkLatestRelease
	t.Cleanup(func() { readCurrentVersion = originalVersionReader })
	t.Cleanup(func() { checkLatestRelease = originalCheckLatest })

	readCurrentVersion = func() string { return "v0.1.0" }
	done := make(chan struct{})
	checkLatestRelease = func(context.Context, updater.CheckOptions) (updater.CheckResult, error) {
		close(done)
		return updater.CheckResult{
			CurrentVersion:     "v0.1.0",
			LatestVersion:      "\x1b[31mv9.9.9\x1b[0m\t\n\r",
			InstallableVersion: "\x1b[31mv0.2.1\x1b[0m\t\n\r",
			HasUpdate:          true,
		}, nil
	}

	defaultSilentUpdateCheck(context.Background())

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected silent update check goroutine to finish")
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		notice := ConsumeUpdateNotice()
		if notice == "" {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if strings.Contains(notice, "\x1b") {
			t.Fatalf("expected notice without ANSI sequence, got %q", notice)
		}
		if !strings.Contains(notice, "v0.2.1") {
			t.Fatalf("expected sanitized version in notice, got %q", notice)
		}
		return
	}
	t.Fatal("expected update notice to be set")
}

func TestDefaultGlobalPreloadNoop(t *testing.T) {
	restore := captureEnvForRootTest(t, "NEOCODE_PRELOAD_KEEP")
	defer restore()
	if err := os.Setenv("NEOCODE_PRELOAD_KEEP", "process-value"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}

	if err := defaultGlobalPreload(context.Background()); err != nil {
		t.Fatalf("defaultGlobalPreload() error = %v", err)
	}
	if got := os.Getenv("NEOCODE_PRELOAD_KEEP"); got != "process-value" {
		t.Fatalf("defaultGlobalPreload should not mutate process env, got %q", got)
	}
}

func TestDefaultGlobalPreloadReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := defaultGlobalPreload(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

type quitModel struct{}

type stubGatewayServer struct {
	listenAddress string
	serveErr      error
	closeErr      error
	serveCalled   bool
	closeCalled   bool
}

type stubRuntimePort struct{}

type stubGatewayAuthenticator struct{}

func (stubGatewayAuthenticator) ValidateToken(token string) bool {
	return strings.TrimSpace(token) == "test-token"
}

func (stubGatewayAuthenticator) ResolveSubjectID(token string) (string, bool) {
	if strings.TrimSpace(token) != "test-token" {
		return "", false
	}
	return "local_admin", true
}

func (stubRuntimePort) Run(context.Context, gateway.RunInput) error { return nil }

func (stubRuntimePort) Compact(context.Context, gateway.CompactInput) (gateway.CompactResult, error) {
	return gateway.CompactResult{}, nil
}

func (stubRuntimePort) ExecuteSystemTool(context.Context, gateway.ExecuteSystemToolInput) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (stubRuntimePort) ActivateSessionSkill(context.Context, gateway.SessionSkillMutationInput) error {
	return nil
}

func (stubRuntimePort) DeactivateSessionSkill(context.Context, gateway.SessionSkillMutationInput) error {
	return nil
}

func (stubRuntimePort) ListSessionSkills(
	context.Context,
	gateway.ListSessionSkillsInput,
) ([]gateway.SessionSkillState, error) {
	return nil, nil
}

func (stubRuntimePort) ListAvailableSkills(
	context.Context,
	gateway.ListAvailableSkillsInput,
) ([]gateway.AvailableSkillState, error) {
	return nil, nil
}

func (stubRuntimePort) ResolvePermission(context.Context, gateway.PermissionResolutionInput) error {
	return nil
}

func (stubRuntimePort) CancelRun(context.Context, gateway.CancelInput) (bool, error) {
	return false, nil
}

func (stubRuntimePort) Events() <-chan gateway.RuntimeEvent { return nil }

func (stubRuntimePort) ListSessions(context.Context) ([]gateway.SessionSummary, error) {
	return nil, nil
}

func (stubRuntimePort) LoadSession(context.Context, gateway.LoadSessionInput) (gateway.Session, error) {
	return gateway.Session{}, nil
}
func (stubRuntimePort) DeleteSession(context.Context, gateway.DeleteSessionInput) (bool, error) {
	return false, nil
}
func (stubRuntimePort) RenameSession(context.Context, gateway.RenameSessionInput) error {
	return nil
}
func (stubRuntimePort) ListFiles(context.Context, gateway.ListFilesInput) ([]gateway.FileEntry, error) {
	return nil, nil
}
func (stubRuntimePort) ListModels(context.Context, gateway.ListModelsInput) ([]gateway.ModelEntry, error) {
	return nil, nil
}
func (stubRuntimePort) SetSessionModel(context.Context, gateway.SetSessionModelInput) error {
	return nil
}
func (stubRuntimePort) GetSessionModel(context.Context, gateway.GetSessionModelInput) (gateway.SessionModelResult, error) {
	return gateway.SessionModelResult{}, nil
}

func (stubRuntimePort) CreateSession(context.Context, gateway.CreateSessionInput) (string, error) {
	return "", nil
}

func (stubRuntimePort) ListSessionTodos(context.Context, gateway.ListSessionTodosInput) (gateway.TodoSnapshot, error) {
	return gateway.TodoSnapshot{}, nil
}

func (stubRuntimePort) GetRuntimeSnapshot(
	context.Context,
	gateway.GetRuntimeSnapshotInput,
) (gateway.RuntimeSnapshot, error) {
	return gateway.RuntimeSnapshot{}, nil
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

func captureEnvForRootTest(t *testing.T, key string) func() {
	t.Helper()
	value, exists := os.LookupEnv(key)
	return func() {
		if exists {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	}
}

func prepareGatewayCommandRunnerTestEnv(t *testing.T) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_CONFIG_HOME", homeDir)
}

func stubGatewayRuntimePortBuilder() func(context.Context, string) (gateway.RuntimePort, func() error, error) {
	return func(context.Context, string) (gateway.RuntimePort, func() error, error) {
		return stubRuntimePort{}, func() error { return nil }, nil
	}
}

func stubGatewayAuthManagerBuilder() func(string) (gateway.TokenAuthenticator, error) {
	return func(string) (gateway.TokenAuthenticator, error) {
		return stubGatewayAuthenticator{}, nil
	}
}
