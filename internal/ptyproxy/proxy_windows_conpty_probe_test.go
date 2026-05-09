//go:build windows

package ptyproxy

import (
	"io"
	"os"
	"testing"
	"time"
)

func TestWindowsConPTYStartsCommandShellWithoutInitFailure(t *testing.T) {
	commandShell := os.Getenv("COMSPEC")
	if commandShell == "" {
		t.Skip("COMSPEC is not set")
	}

	shell, err := startWindowsConPTYShell(commandShell, []string{"/d", "/c", "exit", "0"}, "", nil)
	if err != nil {
		t.Fatalf("startWindowsConPTYShell() error = %v", err)
	}
	defer shell.Close()
	_ = shell.CloseOutputReader()
	waitCh := make(chan error, 1)
	go func() {
		_, _ = io.Copy(io.Discard, shell.OutputReader())
	}()
	go func() {
		waitCh <- shell.Wait()
	}()
	select {
	case err := <-waitCh:
		if err != nil {
			t.Fatalf("shell wait error = %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = shell.Terminate()
		t.Fatal("shell did not exit")
	}
}
