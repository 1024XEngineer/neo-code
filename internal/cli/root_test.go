package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/app"
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
	t.Cleanup(func() { launchRootProgram = originalLauncher })

	expected := errors.New("launch failed")
	launchRootProgram = func(ctx context.Context, opts app.BootstrapOptions) error {
		return expected
	}

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

	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, error) {
		model := quitModel{}
		return tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(io.Discard)), nil
	}

	if err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{Workdir: `D:\项目\中文目录`}); err != nil {
		t.Fatalf("defaultRootProgramLauncher() error = %v", err)
	}
}

func TestDefaultRootProgramLauncherReturnsNewProgramError(t *testing.T) {
	originalNewProgram := newRootProgram
	t.Cleanup(func() { newRootProgram = originalNewProgram })

	expected := errors.New("new program failed")
	newRootProgram = func(ctx context.Context, opts app.BootstrapOptions) (*tea.Program, error) {
		return nil, expected
	}

	err := defaultRootProgramLauncher(context.Background(), app.BootstrapOptions{})
	if !errors.Is(err, expected) {
		t.Fatalf("expected new program error %v, got %v", expected, err)
	}
}

type quitModel struct{}

func (quitModel) Init() tea.Cmd {
	return tea.Quit
}

func (quitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return quitModel{}, nil
}

func (quitModel) View() string {
	return ""
}
