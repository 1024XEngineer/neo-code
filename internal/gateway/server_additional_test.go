package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewServerUsesDefaultsAndOverrides(t *testing.T) {
	originalResolveListenAddress := resolveListenAddressFn
	resolveListenAddressFn = func(override string) (string, error) {
		if override != "" {
			return strings.TrimSpace(override), nil
		}
		return "default-address", nil
	}
	t.Cleanup(func() {
		resolveListenAddressFn = originalResolveListenAddress
	})

	server, err := NewServer(ServerOptions{})
	if err != nil {
		t.Fatalf("new server with defaults: %v", err)
	}
	if server.ListenAddress() != "default-address" {
		t.Fatalf("default listen address = %q, want %q", server.ListenAddress(), "default-address")
	}
	if server.logger == nil {
		t.Fatal("default logger should not be nil")
	}
	if server.listenFn == nil {
		t.Fatal("default listen function should not be nil")
	}
	if server.maxConnections != DefaultMaxConnections {
		t.Fatalf("default max connections = %d, want %d", server.maxConnections, DefaultMaxConnections)
	}
	if server.readTimeout != DefaultReadTimeout {
		t.Fatalf("default read timeout = %v, want %v", server.readTimeout, DefaultReadTimeout)
	}
	if server.writeTimeout != DefaultWriteTimeout {
		t.Fatalf("default write timeout = %v, want %v", server.writeTimeout, DefaultWriteTimeout)
	}

	customLogger := log.New(io.Discard, "custom", 0)
	customServer, err := NewServer(ServerOptions{
		ListenAddress:  "  custom-address  ",
		Logger:         customLogger,
		MaxConnections: 7,
		ReadTimeout:    150 * time.Millisecond,
		WriteTimeout:   250 * time.Millisecond,
		listenFn: func(string) (net.Listener, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("new server with custom options: %v", err)
	}
	if customServer.ListenAddress() != "custom-address" {
		t.Fatalf("custom listen address = %q, want %q", customServer.ListenAddress(), "custom-address")
	}
	if customServer.logger != customLogger {
		t.Fatal("custom logger was not used")
	}
	if customServer.maxConnections != 7 {
		t.Fatalf("custom max connections = %d, want %d", customServer.maxConnections, 7)
	}
	if customServer.readTimeout != 150*time.Millisecond {
		t.Fatalf("custom read timeout = %v, want %v", customServer.readTimeout, 150*time.Millisecond)
	}
	if customServer.writeTimeout != 250*time.Millisecond {
		t.Fatalf("custom write timeout = %v, want %v", customServer.writeTimeout, 250*time.Millisecond)
	}
}

func TestNewServerReturnsDefaultAddressError(t *testing.T) {
	originalResolveListenAddress := resolveListenAddressFn
	resolveListenAddressFn = func(string) (string, error) {
		return "", errors.New("default address failed")
	}
	t.Cleanup(func() {
		resolveListenAddressFn = originalResolveListenAddress
	})

	_, err := NewServer(ServerOptions{})
	if err == nil {
		t.Fatal("expected error when default listen address fails")
	}
	if !strings.Contains(err.Error(), "default address failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerIsClosedState(t *testing.T) {
	server := &Server{}
	if !server.isClosed() {
		t.Fatal("expected server to be closed when listener is nil")
	}

	server.listener = &simpleListener{}
	if server.isClosed() {
		t.Fatal("expected server to be open when listener exists")
	}
}

func TestServeReturnsListenError(t *testing.T) {
	server, err := NewServer(ServerOptions{
		ListenAddress: "listen-error",
		Logger:        log.New(io.Discard, "", 0),
		listenFn: func(string) (net.Listener, error) {
			return nil, errors.New("listen failed")
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	serveErr := server.Serve(context.Background(), nil)
	if serveErr == nil || !strings.Contains(serveErr.Error(), "listen failed") {
		t.Fatalf("expected listen failure, got %v", serveErr)
	}
}

func TestServeRejectsAlreadyServing(t *testing.T) {
	created := &simpleListener{}
	server, err := NewServer(ServerOptions{
		ListenAddress: "already-serving",
		Logger:        log.New(io.Discard, "", 0),
		listenFn: func(string) (net.Listener, error) {
			return created, nil
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.listener = &simpleListener{}

	serveErr := server.Serve(context.Background(), nil)
	if serveErr == nil || !strings.Contains(serveErr.Error(), "already serving") {
		t.Fatalf("expected already serving error, got %v", serveErr)
	}
	if !created.closed {
		t.Fatal("newly created listener should be closed when server is already serving")
	}
}

func TestServeReturnsAcceptError(t *testing.T) {
	listener := &scriptedListener{results: []acceptResult{{err: errors.New("accept failed")}}}
	server, err := NewServer(ServerOptions{
		ListenAddress: "accept-error",
		Logger:        log.New(io.Discard, "", 0),
		listenFn: func(string) (net.Listener, error) {
			return listener, nil
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	serveErr := server.Serve(context.Background(), nil)
	if serveErr == nil || !strings.Contains(serveErr.Error(), "accept connection") {
		t.Fatalf("expected accept error, got %v", serveErr)
	}
}

func TestServeSkipsConnectionWhenRegisterRejected(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	listener := &scriptedListener{
		results: []acceptResult{
			{
				conn: serverConn,
			},
			{err: net.ErrClosed},
		},
	}

	server, err := NewServer(ServerOptions{
		ListenAddress: "register-reject",
		Logger:        log.New(io.Discard, "", 0),
		listenFn: func(string) (net.Listener, error) {
			return listener, nil
		},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	listener.results[0].beforeReturn = func() {
		server.mu.Lock()
		server.listener = nil
		server.mu.Unlock()
	}

	serveErr := server.Serve(context.Background(), nil)
	if serveErr != nil {
		t.Fatalf("serve should exit cleanly when listener closed, got %v", serveErr)
	}

	readDone := make(chan error, 1)
	go func() {
		var buf [1]byte
		_, err := clientConn.Read(buf[:])
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if !errors.Is(err, io.EOF) && (err == nil || !strings.Contains(err.Error(), "closed pipe")) {
			t.Fatalf("expected rejected connection to be closed, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rejected connection was not closed")
	}
}

func TestCloseReturnsContextErrorWhenWaitCanceled(t *testing.T) {
	server := &Server{conns: make(map[net.Conn]struct{})}
	server.wg.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := server.Close(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("close error = %v, want context canceled", err)
	}

	server.wg.Done()
}

func TestDecodeFrameTrailingJSON(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(`{"type":"request","action":"ping"} {"extra":1}` + "\n"))
	_, err := decodeFrame(reader)
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing json error, got %v", err)
	}
}

func TestReadFramePayloadBranches(t *testing.T) {
	if _, err := readFramePayload(bufio.NewReader(strings.NewReader("")), MaxFrameSize); !errors.Is(err, io.EOF) {
		t.Fatalf("empty payload error = %v, want io.EOF", err)
	}

	payload, err := readFramePayload(bufio.NewReader(strings.NewReader("{\"type\":\"request\"}")), MaxFrameSize)
	if err != nil {
		t.Fatalf("payload without newline should decode at EOF: %v", err)
	}
	if string(payload) != `{"type":"request"}` {
		t.Fatalf("payload mismatch: %q", string(payload))
	}

	tooLarge := strings.Repeat("a", 5000)
	if _, err := readFramePayload(bufio.NewReaderSize(strings.NewReader(tooLarge), 64), 1024); !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("oversized payload error = %v, want errFrameTooLarge", err)
	}

	if _, err := readFramePayload(bufio.NewReader(&failingReader{}), MaxFrameSize); err == nil || err.Error() != "read failed" {
		t.Fatalf("expected read failure, got %v", err)
	}
}

func TestDispatchFrameNonRequest(t *testing.T) {
	server := &Server{}
	response := server.dispatchFrame(context.Background(), MessageFrame{Type: FrameTypeEvent, Action: FrameActionPing}, nil)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("response error = %#v, want invalid frame", response.Error)
	}
}

func TestDispatchFrameValidationError(t *testing.T) {
	server := &Server{}
	response := server.dispatchFrame(context.Background(), MessageFrame{Type: FrameType("invalid")}, nil)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("response error = %#v, want invalid frame", response.Error)
	}
}

func TestServerHandleConnectionSkipsEmptyFrame(t *testing.T) {
	server := &Server{logger: log.New(io.Discard, "", 0)}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	_, _ = io.WriteString(clientConn, "\n")
	_, _ = io.WriteString(clientConn, `{"type":"request","action":"ping","request_id":"empty-then-ping"}`+"\n")

	decoder := json.NewDecoder(clientConn)
	var response MessageFrame
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Type != FrameTypeAck || response.Action != FrameActionPing {
		t.Fatalf("unexpected response after empty frame: %#v", response)
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

func TestServerHandleConnectionInvalidJSONFrame(t *testing.T) {
	server := &Server{logger: log.New(io.Discard, "", 0)}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	_, _ = io.WriteString(clientConn, "{invalid-json}\n")
	decoder := json.NewDecoder(clientConn)
	var response MessageFrame
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
		t.Fatalf("response error = %#v, want invalid frame", response.Error)
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConnection did not exit")
	}
}

func TestRegisterConnectionRejectsWhenLimitExceeded(t *testing.T) {
	server := &Server{
		listener:       &simpleListener{},
		maxConnections: 1,
		conns:          make(map[net.Conn]struct{}),
	}

	conn1Server, conn1Client := net.Pipe()
	defer conn1Client.Close()
	defer conn1Server.Close()
	if got := server.registerConnection(conn1Server); got != registerConnectionAccepted {
		t.Fatalf("first register result = %v, want accepted", got)
	}

	conn2Server, conn2Client := net.Pipe()
	defer conn2Client.Close()
	defer conn2Server.Close()
	if got := server.registerConnection(conn2Server); got != registerConnectionLimitExceeded {
		t.Fatalf("second register result = %v, want limit exceeded", got)
	}

	server.untrackConnection(conn1Server)
	server.wg.Done()
}

func TestServerHandleConnectionReadTimeoutClosesConnection(t *testing.T) {
	server := &Server{
		logger:      log.New(io.Discard, "", 0),
		readTimeout: 20 * time.Millisecond,
	}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleConnection should exit after read timeout")
	}

	var buf [1]byte
	_, err := clientConn.Read(buf[:])
	if !errors.Is(err, io.EOF) && (err == nil || !strings.Contains(err.Error(), "closed pipe")) {
		t.Fatalf("expected closed connection after timeout, got %v", err)
	}
	_ = clientConn.Close()
}

func TestServerHandleConnectionWriteTimeoutClosesConnection(t *testing.T) {
	server := &Server{
		logger:       log.New(io.Discard, "", 0),
		readTimeout:  time.Second,
		writeTimeout: 20 * time.Millisecond,
	}
	serverConn, clientConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.handleConnection(context.Background(), serverConn, nil)
	}()

	_, err := io.WriteString(clientConn, `{"type":"request","action":"ping","request_id":"write-timeout"}`+"\n")
	if err != nil {
		t.Fatalf("write request: %v", err)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleConnection should exit after write timeout")
	}

	_ = clientConn.Close()
}

type failingReader struct{}

func (r *failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

type simpleListener struct {
	closed bool
}

func (l *simpleListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l *simpleListener) Close() error {
	l.closed = true
	return nil
}

func (l *simpleListener) Addr() net.Addr {
	return stubAddr("simple")
}

type acceptResult struct {
	conn         net.Conn
	err          error
	beforeReturn func()
}

type scriptedListener struct {
	mu      sync.Mutex
	results []acceptResult
	closed  bool
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.results) == 0 {
		return nil, net.ErrClosed
	}
	result := l.results[0]
	l.results = l.results[1:]
	if result.beforeReturn != nil {
		result.beforeReturn()
	}
	if result.err != nil {
		return nil, result.err
	}
	if result.conn == nil {
		return nil, net.ErrClosed
	}
	return result.conn, nil
}

func (l *scriptedListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

func (l *scriptedListener) Addr() net.Addr {
	return stubAddr("scripted")
}
