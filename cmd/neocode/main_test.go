package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainHelpAndErrorExitPaths(t *testing.T) {
	t.Run("help exits successfully", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessMain", "--", "--help")
		cmd.Env = append(os.Environ(), "GO_WANT_MAIN_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("expected help path to exit successfully, got %v (%s)", err, string(output))
		}
		if !strings.Contains(string(output), "Usage:") {
			t.Fatalf("expected help output, got %q", string(output))
		}
	})

	t.Run("invalid flag exits with error", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessMain", "--", "--bad-flag")
		cmd.Env = append(os.Environ(), "GO_WANT_MAIN_HELPER=1")
		output, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected invalid flag path to exit with error")
		}
		if !strings.Contains(string(output), "unknown flag") {
			t.Fatalf("expected invalid flag output, got %q", string(output))
		}
	})
}

func TestHelperProcessMain(t *testing.T) {
	if os.Getenv("GO_WANT_MAIN_HELPER") != "1" {
		return
	}

	args := []string{"neocode"}
	for _, arg := range os.Args {
		if arg == "--" {
			break
		}
	}
	for i, arg := range os.Args {
		if arg == "--" {
			args = append(args, os.Args[i+1:]...)
			break
		}
	}
	os.Args = args
	main()
}
