//go:build windows

package launcher

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestLaunchTerminalWindowsPrefersWT(t *testing.T) {
	originalLookup := lookupPathForTerminalWindows
	originalExec := execCommandForTerminalWindows
	t.Cleanup(func() {
		lookupPathForTerminalWindows = originalLookup
		execCommandForTerminalWindows = originalExec
	})

	lookupPathForTerminalWindows = func(file string) (string, error) {
		if file == "wt.exe" {
			return `C:\\Windows\\System32\\wt.exe`, nil
		}
		return "", errors.New("unexpected binary")
	}

	called := make([][]string, 0, 2)
	execCommandForTerminalWindows = func(name string, args ...string) *exec.Cmd {
		called = append(called, append([]string{name}, args...))
		return exec.Command("cmd", "/c", "exit", "0")
	}

	if err := launchTerminal("neocode --session s-1"); err != nil {
		t.Fatalf("launchTerminal() error = %v", err)
	}
	if len(called) != 1 {
		t.Fatalf("call count = %d, want 1", len(called))
	}
	want := []string{"wt.exe", "new-tab", "cmd", "/k", "neocode --session s-1"}
	if !reflect.DeepEqual(called[0], want) {
		t.Fatalf("called[0] = %#v, want %#v", called[0], want)
	}
}

func TestLaunchTerminalWindowsFallsBackToCmdStart(t *testing.T) {
	originalLookup := lookupPathForTerminalWindows
	originalExec := execCommandForTerminalWindows
	t.Cleanup(func() {
		lookupPathForTerminalWindows = originalLookup
		execCommandForTerminalWindows = originalExec
	})

	lookupPathForTerminalWindows = func(file string) (string, error) {
		return "", errors.New("not found")
	}

	called := make([][]string, 0, 2)
	execCommandForTerminalWindows = func(name string, args ...string) *exec.Cmd {
		called = append(called, append([]string{name}, args...))
		return exec.Command("cmd", "/c", "exit", "0")
	}

	if err := launchTerminal("neocode --session s-2"); err != nil {
		t.Fatalf("launchTerminal() error = %v", err)
	}
	if len(called) != 1 {
		t.Fatalf("call count = %d, want 1", len(called))
	}
	want := []string{"cmd", "/c", "start", "", "cmd", "/k", "neocode --session s-2"}
	if !reflect.DeepEqual(called[0], want) {
		t.Fatalf("called[0] = %#v, want %#v", called[0], want)
	}
}

func TestLaunchTerminalWindowsFallsBackWhenWTFails(t *testing.T) {
	originalLookup := lookupPathForTerminalWindows
	originalExec := execCommandForTerminalWindows
	t.Cleanup(func() {
		lookupPathForTerminalWindows = originalLookup
		execCommandForTerminalWindows = originalExec
	})

	lookupPathForTerminalWindows = func(file string) (string, error) {
		if file == "wt.exe" {
			return `C:\\Windows\\System32\\wt.exe`, nil
		}
		return "", errors.New("not found")
	}

	called := make([][]string, 0, 3)
	execCommandForTerminalWindows = func(name string, args ...string) *exec.Cmd {
		called = append(called, append([]string{name}, args...))
		if name == "wt.exe" {
			return exec.Command("cmd", "/c", "exit", "1")
		}
		return exec.Command("cmd", "/c", "exit", "0")
	}

	if err := launchTerminal("neocode --session s-3"); err != nil {
		t.Fatalf("launchTerminal() error = %v", err)
	}
	if len(called) != 2 {
		t.Fatalf("call count = %d, want 2", len(called))
	}
	if called[0][0] != "wt.exe" || called[1][0] != "cmd" {
		t.Fatalf("call order = %#v, want wt.exe then cmd", called)
	}
}
