//go:build !windows

package transport

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListenUnixAcceptsConnectionAndCleansSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "run", "gateway.sock")
	socketDir := filepath.Dir(socketPath)
	listener, err := Listen(socketPath)
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

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial unix socket: %v", err)
	}
	_ = conn.Close()

	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket file: %v", err)
	}
	if got := socketInfo.Mode() & os.ModePerm; got != unixSocketFilePerm {
		t.Fatalf("socket file perm = %#o, want %#o", got, unixSocketFilePerm)
	}

	dirInfo, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("stat socket dir: %v", err)
	}
	if got := dirInfo.Mode() & os.ModePerm; got != unixSocketDirPerm {
		t.Fatalf("socket dir perm = %#o, want %#o", got, unixSocketDirPerm)
	}

	select {
	case acceptErr := <-acceptDone:
		if acceptErr != nil {
			t.Fatalf("accept connection: %v", acceptErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accept timed out")
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket file should be removed on close, stat err: %v", err)
	}
}

func TestListenUnixDoesNotChmodExistingDir(t *testing.T) {
	t.Parallel()

	parentDir := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}

	socketPath := filepath.Join(parentDir, "gateway.sock")
	listener, err := Listen(socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	dirInfo, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("stat parent dir: %v", err)
	}
	if got := dirInfo.Mode() & os.ModePerm; got != 0o755 {
		t.Fatalf("existing dir perm = %#o, want %#o", got, 0o755)
	}
}

func TestListenUnixSocketDirPathIsFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	filePath := filepath.Join(baseDir, "not-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	socketPath := filepath.Join(filePath, "gateway.sock")
	_, err := Listen(socketPath)
	if err == nil {
		t.Fatal("expected error when socket dir path is file")
	}
	if !strings.Contains(err.Error(), "is not directory") {
		t.Fatalf("error = %v, want contains %q", err, "is not directory")
	}
}

func TestRemoveStaleUnixSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "gateway.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	_ = listener.Close()

	if err := removeStaleUnixSocket(socketPath); err != nil {
		t.Fatalf("remove stale socket: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed, stat err: %v", err)
	}
}

func TestRemoveStaleUnixSocketNonSocketPath(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	err := removeStaleUnixSocket(filePath)
	if err == nil {
		t.Fatal("expected error when stale path is non-socket")
	}
	if !strings.Contains(err.Error(), "is not socket") {
		t.Fatalf("error = %v, want contains %q", err, "is not socket")
	}
}

func TestRemoveStaleUnixSocketNotExist(t *testing.T) {
	t.Parallel()

	err := removeStaleUnixSocket(filepath.Join(t.TempDir(), "missing.sock"))
	if err != nil {
		t.Fatalf("remove missing stale socket: %v", err)
	}
}

func TestNewCleanupListenerWithoutCleanup(t *testing.T) {
	t.Parallel()

	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer func() {
		_ = baseListener.Close()
	}()

	wrapped := newCleanupListener(baseListener, nil)
	if wrapped != baseListener {
		t.Fatal("expected original listener when cleanup is nil")
	}
}

func TestCleanupListenerCloseReturnsJoinedError(t *testing.T) {
	t.Parallel()

	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}

	cleanupErr := errors.New("cleanup failed")
	wrapped := newCleanupListener(baseListener, func() error {
		return cleanupErr
	})

	closeErr := wrapped.Close()
	if closeErr == nil {
		t.Fatal("expected close error")
	}
	if !errors.Is(closeErr, cleanupErr) {
		t.Fatalf("close error = %v, want contains cleanup err %v", closeErr, cleanupErr)
	}
}
