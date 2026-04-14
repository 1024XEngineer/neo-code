//go:build windows

package transport

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestDialWindowsNamedPipe(t *testing.T) {
	originalDialPipeFn := dialPipeFn
	t.Cleanup(func() {
		dialPipeFn = originalDialPipeFn
	})

	serverConn, clientConn := net.Pipe()
	dialPipeFn = func(address string, _ *time.Duration) (net.Conn, error) {
		if address != `\\.\pipe\neocode-gateway` {
			t.Fatalf("address = %q, want %q", address, `\\.\pipe\neocode-gateway`)
		}
		return clientConn, nil
	}

	conn, err := Dial(`\\.\pipe\neocode-gateway`)
	if err != nil {
		t.Fatalf("dial named pipe: %v", err)
	}
	_ = conn.Close()
	_ = serverConn.Close()
}

func TestDialWindowsNamedPipeError(t *testing.T) {
	originalDialPipeFn := dialPipeFn
	t.Cleanup(func() {
		dialPipeFn = originalDialPipeFn
	})

	expected := errors.New("dial failed")
	dialPipeFn = func(string, *time.Duration) (net.Conn, error) {
		return nil, expected
	}

	_, err := Dial(`\\.\pipe\neocode-gateway`)
	if !errors.Is(err, expected) {
		t.Fatalf("dial error = %v, want %v", err, expected)
	}
}
