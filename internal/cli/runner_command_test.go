package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/runner"
)

func TestNewRunnerCommandForwardsFlags(t *testing.T) {
	originalRunner := runRunnerCommandFn
	t.Cleanup(func() { runRunnerCommandFn = originalRunner })

	var captured runnerCommandOptions
	runRunnerCommandFn = func(ctx context.Context, options runnerCommandOptions) error {
		captured = options
		return nil
	}

	cmd := newRunnerCommand()
	cmd.SetArgs([]string{
		"--gateway-address", "127.0.0.1:9000",
		"--token-file", "/tmp/token",
		"--runner-id", "runner-1",
		"--runner-name", "Local Runner",
		"--workdir", "/tmp/work",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.GatewayAddress != "127.0.0.1:9000" || captured.TokenFile != "/tmp/token" || captured.RunnerID != "runner-1" || captured.RunnerName != "Local Runner" || captured.Workdir != "/tmp/work" {
		t.Fatalf("captured options = %#v", captured)
	}
}

func TestDefaultRunRunnerReadsTokenFileError(t *testing.T) {
	err := defaultRunRunner(context.Background(), runnerCommandOptions{TokenFile: filepath.Join(t.TempDir(), "missing.token")})
	if err == nil || !strings.Contains(err.Error(), "read token file") {
		t.Fatalf("defaultRunRunner() error = %v", err)
	}
}

type stubRunnerService struct {
	runFn    func(context.Context) error
	stopCall int
}

func (s *stubRunnerService) Run(ctx context.Context) error {
	if s.runFn != nil {
		return s.runFn(ctx)
	}
	return nil
}

func (s *stubRunnerService) Stop() {
	s.stopCall++
}

func TestRootCommandIncludesRunnerSubcommand(t *testing.T) {
	cmd := NewRootCommand()
	found := false
	for _, child := range cmd.Commands() {
		if child.Name() == "runner" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("runner subcommand not registered on root command")
	}
}

func TestNewRunnerCommandAllowsDefaultFlags(t *testing.T) {
	originalRunner := runRunnerCommandFn
	t.Cleanup(func() { runRunnerCommandFn = originalRunner })

	var captured runnerCommandOptions
	runRunnerCommandFn = func(ctx context.Context, options runnerCommandOptions) error {
		captured = options
		return nil
	}

	cmd := newRunnerCommand()
	cmd.SetArgs([]string{})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured != (runnerCommandOptions{}) {
		t.Fatalf("captured options = %#v, want zero-value defaults before runtime resolution", captured)
	}
}

func TestDefaultRunRunnerUsesResolvedDefaultsAndToken(t *testing.T) {
	originalNewRunnerService := newRunnerServiceFn
	t.Cleanup(func() { newRunnerServiceFn = originalNewRunnerService })

	tempDir := t.TempDir()
	tokenFile := filepath.Join(tempDir, "runner.token")
	if err := os.WriteFile(tokenFile, []byte(" secret-token \n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	hostName, err := os.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}

	var capturedCfg runner.Config
	stub := &stubRunnerService{runFn: func(context.Context) error { return context.Canceled }}
	newRunnerServiceFn = func(cfg runner.Config) (runnerService, error) {
		capturedCfg = cfg
		return stub, nil
	}

	err = defaultRunRunner(context.Background(), runnerCommandOptions{
		TokenFile:  tokenFile,
		RunnerName: " Local Runner ",
	})
	if err != nil {
		t.Fatalf("defaultRunRunner() error = %v", err)
	}
	if capturedCfg.GatewayAddress != "127.0.0.1:8080" {
		t.Fatalf("GatewayAddress = %q", capturedCfg.GatewayAddress)
	}
	if capturedCfg.Workdir != tempDir {
		t.Fatalf("Workdir = %q, want %q", capturedCfg.Workdir, tempDir)
	}
	if capturedCfg.RunnerID != hostName {
		t.Fatalf("RunnerID = %q, want %q", capturedCfg.RunnerID, hostName)
	}
	if capturedCfg.RunnerName != "Local Runner" {
		t.Fatalf("RunnerName = %q, want %q", capturedCfg.RunnerName, "Local Runner")
	}
	if capturedCfg.Token != "secret-token" {
		t.Fatalf("Token = %q, want %q", capturedCfg.Token, "secret-token")
	}
}

func TestDefaultRunRunnerWrapsRunnerCreationAndRunErrors(t *testing.T) {
	originalNewRunnerService := newRunnerServiceFn
	t.Cleanup(func() { newRunnerServiceFn = originalNewRunnerService })

	t.Run("runner creation failure", func(t *testing.T) {
		newRunnerServiceFn = func(cfg runner.Config) (runnerService, error) {
			return nil, errors.New("boom")
		}
		err := defaultRunRunner(context.Background(), runnerCommandOptions{RunnerID: "runner-1", GatewayAddress: "127.0.0.1:8080"})
		if err == nil || !strings.Contains(err.Error(), "create runner: boom") {
			t.Fatalf("defaultRunRunner() error = %v", err)
		}
	})

	t.Run("runner run failure", func(t *testing.T) {
		newRunnerServiceFn = func(cfg runner.Config) (runnerService, error) {
			return &stubRunnerService{runFn: func(context.Context) error { return errors.New("run failed") }}, nil
		}
		err := defaultRunRunner(context.Background(), runnerCommandOptions{RunnerID: "runner-1", GatewayAddress: "127.0.0.1:8080"})
		if err == nil || !strings.Contains(err.Error(), "runner: run failed") {
			t.Fatalf("defaultRunRunner() error = %v", err)
		}
	})
}

func TestNewRunnerServiceFnDelegatesToRunnerNew(t *testing.T) {
	service, err := newRunnerServiceFn(runner.Config{})
	if err == nil || !strings.Contains(err.Error(), "runner_id is required") {
		t.Fatalf("newRunnerServiceFn() error = %v", err)
	}
	if service != nil && !strings.Contains(err.Error(), "runner_id is required") {
		t.Fatalf("unexpected service for invalid config: %#v", service)
	}
}
