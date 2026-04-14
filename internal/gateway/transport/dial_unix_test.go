//go:build !windows

package transport

import (
	"net"
	"path/filepath"
	"testing"
)

func TestDialUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	acceptDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			acceptDone <- acceptErr
			return
		}
		_ = conn.Close()
		acceptDone <- nil
	}()

	conn, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("dial unix socket: %v", err)
	}
	_ = conn.Close()

	if acceptErr := <-acceptDone; acceptErr != nil {
		t.Fatalf("accept unix socket: %v", acceptErr)
	}
}
