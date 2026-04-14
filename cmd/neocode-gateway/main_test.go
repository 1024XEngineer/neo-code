package main

import (
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
)

func TestParseFlagsValid(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--listen", "  /tmp/gateway.sock  ", "--log-level", " WARN "}, func() {
		listen, level, err := parseFlags()
		if err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		if listen != "/tmp/gateway.sock" {
			t.Fatalf("listen = %q, want %q", listen, "/tmp/gateway.sock")
		}
		if level != "warn" {
			t.Fatalf("log level = %q, want %q", level, "warn")
		}
	})
}

func TestParseFlagsHelp(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--help"}, func() {
		_, _, err := parseFlags()
		if !errors.Is(err, errHelpRequested) {
			t.Fatalf("parse flags error = %v, want %v", err, errHelpRequested)
		}
	})
}

func TestParseFlagsInvalidLogLevel(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--log-level", "trace"}, func() {
		_, _, err := parseFlags()
		if err == nil {
			t.Fatal("expected invalid log level error")
		}
		if !strings.Contains(err.Error(), "invalid --log-level") {
			t.Fatalf("error = %v, want contains %q", err, "invalid --log-level")
		}
	})
}

func TestParseFlagsUnknownFlag(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--unknown"}, func() {
		_, _, err := parseFlags()
		if err == nil {
			t.Fatal("expected parse error")
		}
		if errors.Is(err, flag.ErrHelp) {
			t.Fatalf("error = %v, should not be help error", err)
		}
	})
}

func TestRunHelp(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--help"}, func() {
		if err := run(); err != nil {
			t.Fatalf("run help: %v", err)
		}
	})
}

func TestRunInvalidLogLevel(t *testing.T) {
	withArgs(t, []string{"neocode-gateway", "--log-level", "trace"}, func() {
		err := run()
		if err == nil {
			t.Fatal("expected run error")
		}
		if !strings.Contains(err.Error(), "invalid --log-level") {
			t.Fatalf("error = %v, want contains %q", err, "invalid --log-level")
		}
	})
}

func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()

	originalArgs := os.Args
	os.Args = args
	defer func() {
		os.Args = originalArgs
	}()

	fn()
}
