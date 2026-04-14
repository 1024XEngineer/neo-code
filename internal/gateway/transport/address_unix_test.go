//go:build !windows

package transport

import (
	"path/filepath"
	"testing"
)

func TestDefaultListenAddress(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	address, err := DefaultListenAddress()
	if err != nil {
		t.Fatalf("default listen address: %v", err)
	}

	want := filepath.Join(home, defaultUnixSocketRelativePath)
	if address != want {
		t.Fatalf("default listen address = %q, want %q", address, want)
	}
}
