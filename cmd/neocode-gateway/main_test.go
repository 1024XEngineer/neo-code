package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainHelpPathDoesNotExit(t *testing.T) {
	originalArgs := os.Args
	defer func() {
		os.Args = originalArgs
	}()

	os.Args = []string{"neocode-gateway", "--help"}
	main()
}

func TestMainReturnsExitCodeOneOnCommandError(t *testing.T) {
	if os.Getenv("NEOCODE_GATEWAY_MAIN_HELPER") == "1" {
		os.Args = []string{"neocode-gateway", "--log-level", "trace"}
		main()
		return
	}

	command := exec.Command(os.Args[0], "-test.run=TestMainReturnsExitCodeOneOnCommandError")
	command.Env = append(os.Environ(), "NEOCODE_GATEWAY_MAIN_HELPER=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr

	err := command.Run()
	if err == nil {
		t.Fatal("expected subprocess to exit with non-zero status")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error type = %T, want *exec.ExitError", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want %d", exitErr.ExitCode(), 1)
	}
	if !strings.Contains(stderr.String(), "neocode-gateway:") {
		t.Fatalf("stderr = %q, want contains %q", stderr.String(), "neocode-gateway:")
	}
}
