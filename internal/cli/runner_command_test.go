package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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
