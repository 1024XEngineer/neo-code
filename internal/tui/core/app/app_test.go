package tui

import (
	"context"
	"testing"

	"neo-code/internal/config"
	tuibootstrap "neo-code/internal/tui/bootstrap"
)

func TestNewDefaultsWorkspaceBindingForLegacyConstructor(t *testing.T) {
	workdir := t.TempDir()

	app, err := New(&config.Config{
		Workdir:          workdir,
		SelectedProvider: "openai",
		CurrentModel:     "gpt-5",
		Shell:            "bash",
	}, &config.Manager{}, &workspaceTestRuntime{}, &workspaceTestProvider{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if app.workspaceRoot != workdir {
		t.Fatalf("expected workspace root %q, got %q", workdir, app.workspaceRoot)
	}
	if app.state.CurrentWorkdir != workdir {
		t.Fatalf("expected current workdir %q, got %q", workdir, app.state.CurrentWorkdir)
	}
}

func TestNewWithBootstrapPreservesRebuildWorkspace(t *testing.T) {
	workdir := t.TempDir()
	rebuild := func(ctx context.Context, requestedPath string) (tuibootstrap.WorkspaceBinding, error) {
		return tuibootstrap.WorkspaceBinding{Workdir: requestedPath}, nil
	}

	app, err := NewWithBootstrap(tuibootstrap.Options{
		Config: &config.Config{
			Workdir:          workdir,
			SelectedProvider: "openai",
			CurrentModel:     "gpt-5",
			Shell:            "bash",
		},
		ConfigManager:    &config.Manager{},
		Runtime:          &workspaceTestRuntime{},
		ProviderService:  &workspaceTestProvider{},
		WorkspaceRoot:    workdir,
		Workdir:          workdir,
		RebuildWorkspace: rebuild,
	})
	if err != nil {
		t.Fatalf("NewWithBootstrap() error = %v", err)
	}

	if app.rebuildWorkspace == nil {
		t.Fatalf("expected rebuild workspace func to be set")
	}
}
