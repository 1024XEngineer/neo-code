package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	program, err := NewProgram(context.Background())
	if err != nil {
		t.Fatalf("NewProgram() error = %v", err)
	}
	if program == nil {
		t.Fatalf("expected tea program")
	}

	configPath := filepath.Join(home, ".neocode", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created at %q: %v", configPath, err)
	}
}
