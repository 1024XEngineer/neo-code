package infra

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestProcessWorkspaceSwitcherSwitchWorkspace(t *testing.T) {
	t.Run("constructor wires defaults", func(t *testing.T) {
		switcher := NewProcessWorkspaceSwitcher()
		if switcher == nil {
			t.Fatalf("expected switcher instance")
		}
		if switcher.resolveExecutable == nil || switcher.startProcess == nil {
			t.Fatalf("expected constructor to wire default dependencies")
		}
	})

	t.Run("empty workdir is rejected", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{}
		err := switcher.SwitchWorkspace(context.Background(), "   ")
		if err == nil || err.Error() != "workspace switch: workdir is empty" {
			t.Fatalf("expected empty workdir error, got %v", err)
		}
	})

	t.Run("nil receiver is rejected", func(t *testing.T) {
		var switcher *ProcessWorkspaceSwitcher
		err := switcher.SwitchWorkspace(context.Background(), "/repo")
		if err == nil || err.Error() != "workspace switch: switcher is nil" {
			t.Fatalf("expected nil switcher error, got %v", err)
		}
	})

	t.Run("missing executable resolver is rejected", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{}
		err := switcher.SwitchWorkspace(context.Background(), "/repo")
		if err == nil || err.Error() != "workspace switch: executable resolver is nil" {
			t.Fatalf("expected missing resolver error, got %v", err)
		}
	})

	t.Run("missing process starter is rejected", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{
			resolveExecutable: func() (string, error) { return "/tmp/neocode", nil },
		}
		err := switcher.SwitchWorkspace(context.Background(), "/repo")
		if err == nil || err.Error() != "workspace switch: process starter is nil" {
			t.Fatalf("expected missing process starter error, got %v", err)
		}
	})

	t.Run("resolver error is wrapped", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{
			resolveExecutable: func() (string, error) { return "", errors.New("resolve failed") },
			startProcess:      func(spec processLaunchSpec) error { return nil },
		}
		err := switcher.SwitchWorkspace(context.Background(), "/repo")
		if err == nil || err.Error() != "workspace switch: resolve executable: resolve failed" {
			t.Fatalf("expected wrapped resolver error, got %v", err)
		}
	})

	t.Run("builds relaunch spec", func(t *testing.T) {
		var captured processLaunchSpec
		switcher := &ProcessWorkspaceSwitcher{
			resolveExecutable: func() (string, error) { return "/tmp/neocode", nil },
			startProcess: func(spec processLaunchSpec) error {
				captured = spec
				return nil
			},
		}

		err := switcher.SwitchWorkspace(context.Background(), " /repo ")
		if err != nil {
			t.Fatalf("SwitchWorkspace() error = %v", err)
		}
		if captured.Path != "/tmp/neocode" {
			t.Fatalf("expected executable path, got %+v", captured)
		}
		if captured.Dir != "/repo" {
			t.Fatalf("expected workdir dir to be used, got %+v", captured)
		}
		if !reflect.DeepEqual(captured.Args, []string{"--workdir", "/repo"}) {
			t.Fatalf("unexpected args: %+v", captured.Args)
		}
		if len(captured.Env) == 0 {
			t.Fatalf("expected environment to be inherited")
		}
	})

	t.Run("starter error is wrapped", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{
			resolveExecutable: func() (string, error) { return "/tmp/neocode", nil },
			startProcess: func(spec processLaunchSpec) error {
				return errors.New("boom")
			},
		}

		err := switcher.SwitchWorkspace(context.Background(), "/repo")
		if err == nil || err.Error() != "workspace switch: start process: boom" {
			t.Fatalf("expected wrapped start error, got %v", err)
		}
	})

	t.Run("canceled context short circuits", func(t *testing.T) {
		switcher := &ProcessWorkspaceSwitcher{
			resolveExecutable: func() (string, error) {
				t.Fatal("resolver should not be called for canceled context")
				return "", nil
			},
			startProcess: func(spec processLaunchSpec) error {
				t.Fatal("starter should not be called for canceled context")
				return nil
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := switcher.SwitchWorkspace(ctx, "/repo"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
}

func TestStartWorkspaceProcessRejectsMissingExecutable(t *testing.T) {
	spec := processLaunchSpec{
		Path: filepathBase("definitely-not-a-real-neocode-binary"),
		Args: []string{"--workdir", os.TempDir()},
		Dir:  os.TempDir(),
		Env:  os.Environ(),
	}

	if err := startWorkspaceProcess(spec); err == nil {
		t.Fatalf("expected missing executable to fail")
	}
}

func filepathBase(name string) string {
	return name
}
