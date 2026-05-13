//go:build windows

package transport

import (
	"strings"
	"testing"
)

func TestResolveListenAddressRejectsTCPAddressOnWindows(t *testing.T) {
	_, err := ResolveListenAddress("127.0.0.1:8080")
	if err == nil {
		t.Fatal("expected tcp listen address to be rejected on windows")
	}
	if !strings.Contains(err.Error(), "Windows IPC listen address must be a named pipe path") {
		t.Fatalf("error = %v, want named pipe hint", err)
	}
}
