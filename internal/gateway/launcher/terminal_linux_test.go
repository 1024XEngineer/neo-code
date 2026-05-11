//go:build !windows && !darwin

package launcher

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

func TestLaunchTerminalLinuxUsesGnomeFirst(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific behavior")
	}
	originalLookup := lookupPathForTerminalLinux
	originalExec := execCommandForTerminalLinux
	t.Cleanup(func() {
		lookupPathForTerminalLinux = originalLookup
		execCommandForTerminalLinux = originalExec
	})

	lookupPathForTerminalLinux = func(binary string) (string, error) {
		if binary == "gnome-terminal" {
			return "/usr/bin/gnome-terminal", nil
		}
		return "", errors.New("not found")
	}
	called := make([]string, 0, 1)
	execCommandForTerminalLinux = func(name string, args ...string) *exec.Cmd {
		called = append(called, name)
		return exec.Command("sh", "-c", "exit 0")
	}

	if err := launchTerminal("neocode --session s-1"); err != nil {
		t.Fatalf("launchTerminal() error = %v", err)
	}
	if len(called) != 1 || called[0] != "gnome-terminal" {
		t.Fatalf("called = %#v, want [gnome-terminal]", called)
	}
}

func TestLaunchTerminalLinuxFallsBackToXTerminalEmulator(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific behavior")
	}
	originalLookup := lookupPathForTerminalLinux
	originalExec := execCommandForTerminalLinux
	t.Cleanup(func() {
		lookupPathForTerminalLinux = originalLookup
		execCommandForTerminalLinux = originalExec
	})

	lookupPathForTerminalLinux = func(binary string) (string, error) {
		if binary == "x-terminal-emulator" {
			return "/usr/bin/x-terminal-emulator", nil
		}
		return "", errors.New("not found")
	}
	called := make([]string, 0, 2)
	execCommandForTerminalLinux = func(name string, args ...string) *exec.Cmd {
		called = append(called, name)
		return exec.Command("sh", "-c", "exit 0")
	}

	if err := launchTerminal("neocode --session s-2"); err != nil {
		t.Fatalf("launchTerminal() error = %v", err)
	}
	if len(called) != 1 || called[0] != "x-terminal-emulator" {
		t.Fatalf("called = %#v, want [x-terminal-emulator]", called)
	}
}

func TestLaunchTerminalLinuxUnsupportedWhenNoTerminal(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific behavior")
	}
	originalLookup := lookupPathForTerminalLinux
	originalExec := execCommandForTerminalLinux
	t.Cleanup(func() {
		lookupPathForTerminalLinux = originalLookup
		execCommandForTerminalLinux = originalExec
	})

	lookupPathForTerminalLinux = func(binary string) (string, error) {
		return "", errors.New("not found")
	}
	execCommandForTerminalLinux = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "exit 0")
	}

	err := launchTerminal("neocode --session s-3")
	if !errors.Is(err, ErrTerminalUnsupported) {
		t.Fatalf("error = %v, want wrapped %v", err, ErrTerminalUnsupported)
	}
}
