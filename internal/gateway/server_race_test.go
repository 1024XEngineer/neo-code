package gateway

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServeCloseDuringAcceptDoesNotLeakConnection(t *testing.T) {
	t.Parallel()

	listener := newStubListener()
	server, err := NewServer(ServerOptions{
		ListenAddress: "stub://gateway",
		Logger:        log.New(io.Discard, "", 0),
		listenFn: func(string) (net.Listener, error) {
			return listener, nil
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(ctx, nil)
	}()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	closeDone := make(chan error, 1)
	listener.onAccept = func() {
		go func() {
			closeDone <- server.Close(context.Background())
		}()
	}

	listener.acceptCh <- serverConn

	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatalf("close server: %v", closeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close timed out")
	}

	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("serve returned error: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit")
	}

	readDone := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, readErr := clientConn.Read(buf[:])
		readDone <- readErr
	}()

	select {
	case readErr := <-readDone:
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, net.ErrClosed) || errors.Is(readErr, os.ErrClosed) {
			return
		}
		if readErr != nil && strings.Contains(readErr.Error(), "closed pipe") {
			return
		}
		t.Fatalf("expected closed connection after server close, got %v", readErr)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("connection was not closed by server")
	}
}

type stubListener struct {
	acceptCh chan net.Conn
	closeCh  chan struct{}

	onAccept  func()
	closeOnce sync.Once
}

func newStubListener() *stubListener {
	return &stubListener{
		acceptCh: make(chan net.Conn, 1),
		closeCh:  make(chan struct{}),
	}
}

func (l *stubListener) Accept() (net.Conn, error) {
	select {
	case <-l.closeCh:
		return nil, net.ErrClosed
	case conn := <-l.acceptCh:
		if l.onAccept != nil {
			l.onAccept()
		}
		return conn, nil
	}
}

func (l *stubListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closeCh)
	})
	return nil
}

func (l *stubListener) Addr() net.Addr {
	return stubAddr("stub://gateway")
}

type stubAddr string

func (a stubAddr) Network() string {
	return "stub"
}

func (a stubAddr) String() string {
	return string(a)
}
