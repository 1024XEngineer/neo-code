package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"neo-code/internal/app"
)

func TestRootCommandSupportsWorkdirShortFlag(t *testing.T) {
	originalLauncher := launchRootProgram
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	var captured app.BootstrapOptions
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		captured = opts
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"-w", "/tmp/project"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Workdir != "/tmp/project" {
		t.Fatalf("workdir = %q, want %q", captured.Workdir, "/tmp/project")
	}
}

func TestUseCommandSupportsModelShortFlag(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newUseCommandWithResolver(staticSelectionResolver(svc))

	originalRunner := runUseCommand
	t.Cleanup(func() { runUseCommand = originalRunner })

	called := false
	runUseCommand = func(c *cobra.Command, gotSvc SelectionService, name string, opts useCommandOptions) error {
		called = true
		if opts.Model != "gpt-4.1" {
			t.Fatalf("opts.Model = %q, want %q", opts.Model, "gpt-4.1")
		}
		return nil
	}

	cmd.SetArgs([]string{"openai", "-m", "gpt-4.1"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !called {
		t.Fatal("expected runUseCommand called")
	}
}

func TestRootCommandSupportsVersionFlags(t *testing.T) {
	originalLauncher := launchRootProgram
	originalVersionRunner := runVersionCommand
	t.Cleanup(func() { launchRootProgram = originalLauncher })
	t.Cleanup(func() { runVersionCommand = originalVersionRunner })

	launchRootProgram = func(context.Context, app.BootstrapOptions) error {
		t.Fatal("launcher should not run when -v/--version is used")
		return nil
	}
	runVersionCommand = func(context.Context, versionCommandOptions) (versionCommandResult, error) {
		return versionCommandResult{
			CurrentVersion: "v1.0.0",
			LatestVersion:  "v1.0.0",
			Comparable:     true,
		}, nil
	}

	for _, args := range [][]string{{"-v"}, {"--version"}} {
		cmd := NewRootCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext(%v) error = %v", args, err)
		}
		text := out.String()
		if !strings.Contains(text, "Current version: v1.0.0") {
			t.Fatalf("output(%v) = %q, want current version", args, text)
		}
	}
}

func TestLegacyFeishuAdapterCommandShowsMigrationHint(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"feishu-adapter"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected migration hint error")
	}
	if !strings.Contains(err.Error(), "adapter feishu") {
		t.Fatalf("err = %v, want contains adapter feishu", err)
	}
}

