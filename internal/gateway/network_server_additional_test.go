package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"neo-code/internal/gateway/protocol"
)

func TestNewNetworkServerDefaultsAndResolveError(t *testing.T) {
	originalResolve := resolveNetworkListenAddressFn
	t.Cleanup(func() {
		resolveNetworkListenAddressFn = originalResolve
	})

	resolveNetworkListenAddressFn = func(override string) (string, error) {
		if strings.TrimSpace(override) == "bad" {
			return "", errors.New("resolve failed")
		}
		return "127.0.0.1:8080", nil
	}

	server, err := NewNetworkServer(NetworkServerOptions{})
	if err != nil {
		t.Fatalf("new network server with defaults: %v", err)
	}
	if server.logger == nil {
		t.Fatal("expected default logger to be created")
	}
	if server.listenFn == nil {
		t.Fatal("expected default listenFn to be configured")
	}
	if server.readTimeout != DefaultNetworkReadTimeout {
		t.Fatalf("read timeout = %v, want %v", server.readTimeout, DefaultNetworkReadTimeout)
	}
	if server.writeTimeout != DefaultNetworkWriteTimeout {
		t.Fatalf("write timeout = %v, want %v", server.writeTimeout, DefaultNetworkWriteTimeout)
	}
	if server.shutdownTimeout != DefaultNetworkShutdownTimeout {
		t.Fatalf("shutdown timeout = %v, want %v", server.shutdownTimeout, DefaultNetworkShutdownTimeout)
	}
	if server.heartbeatInterval != DefaultNetworkHeartbeatInterval {
		t.Fatalf("heartbeat interval = %v, want %v", server.heartbeatInterval, DefaultNetworkHeartbeatInterval)
	}
	if server.maxRequestBytes != DefaultNetworkMaxRequestBytes {
		t.Fatalf("max request bytes = %d, want %d", server.maxRequestBytes, DefaultNetworkMaxRequestBytes)
	}
	if server.maxStreamConnections != DefaultNetworkMaxStreamConnections {
		t.Fatalf("max stream connections = %d, want %d", server.maxStreamConnections, DefaultNetworkMaxStreamConnections)
	}

	_, err = NewNetworkServer(NetworkServerOptions{ListenAddress: "bad"})
	if err == nil || !strings.Contains(err.Error(), "resolve failed") {
		t.Fatalf("expected resolve failure, got %v", err)
	}
}

func TestValidateLoopbackListenAddressParseFailure(t *testing.T) {
	if err := validateLoopbackListenAddress("bad-address"); err == nil {
		t.Fatal("expected split host port error")
	}
}

func TestValidateLoopbackListenAddressHostLookup(t *testing.T) {
	originalLookup := lookupHostIPsFn
	t.Cleanup(func() {
		lookupHostIPsFn = originalLookup
	})

	t.Run("hostname resolves to loopback addresses", func(t *testing.T) {
		lookupHostIPsFn = func(host string) ([]net.IP, error) {
			return []net.IP{
				net.ParseIP("127.0.0.1"),
				net.ParseIP("::1"),
			}, nil
		}
		if err := validateLoopbackListenAddress("localhost:8080"); err != nil {
			t.Fatalf("expected loopback hostname to pass, got %v", err)
		}
	})

	t.Run("hostname resolves to non-loopback address", func(t *testing.T) {
		lookupHostIPsFn = func(host string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("192.168.1.10")}, nil
		}
		err := validateLoopbackListenAddress("localhost:8080")
		if err == nil || !strings.Contains(err.Error(), "host must be loopback") {
			t.Fatalf("expected non-loopback hostname rejection, got %v", err)
		}
	})

	t.Run("hostname lookup failed", func(t *testing.T) {
		lookupHostIPsFn = func(host string) ([]net.IP, error) {
			return nil, errors.New("lookup failed")
		}
		err := validateLoopbackListenAddress("localhost:8080")
		if err == nil || !strings.Contains(err.Error(), "host must resolve to loopback addresses") {
			t.Fatalf("expected lookup failure rejection, got %v", err)
		}
	})
}

func TestNetworkServerServeErrorBranches(t *testing.T) {
	t.Run("listen error", func(t *testing.T) {
		server, err := NewNetworkServer(NetworkServerOptions{
			ListenAddress: "127.0.0.1:0",
			Logger:        log.New(io.Discard, "", 0),
			listenFn: func(network, address string) (net.Listener, error) {
				return nil, errors.New("listen failed")
			},
		})
		if err != nil {
			t.Fatalf("new network server: %v", err)
		}
		if serveErr := server.Serve(context.Background(), nil); serveErr == nil || !strings.Contains(serveErr.Error(), "listen failed") {
			t.Fatalf("expected listen error, got %v", serveErr)
		}
		server.relay.mu.RLock()
		started := server.relay.cleanupStarted || server.relay.eventPumpStarted
		server.relay.mu.RUnlock()
		if started {
			t.Fatal("relay loops should not start when listen failed")
		}
	})

	t.Run("already serving", func(t *testing.T) {
		listener := &trackCloseListener{}
		server, err := NewNetworkServer(NetworkServerOptions{
			ListenAddress: "127.0.0.1:0",
			Logger:        log.New(io.Discard, "", 0),
			listenFn: func(network, address string) (net.Listener, error) {
				return listener, nil
			},
		})
		if err != nil {
			t.Fatalf("new network server: %v", err)
		}
		server.server = &http.Server{}

		serveErr := server.Serve(context.Background(), nil)
		if serveErr == nil || !strings.Contains(serveErr.Error(), "already serving") {
			t.Fatalf("expected already serving error, got %v", serveErr)
		}
		if !listener.closed {
			t.Fatal("expected temporary listener to be closed")
		}
	})

	t.Run("serve accept error", func(t *testing.T) {
		listener := &acceptErrorListener{}
		server, err := NewNetworkServer(NetworkServerOptions{
			ListenAddress: "127.0.0.1:0",
			Logger:        log.New(io.Discard, "", 0),
			listenFn: func(network, address string) (net.Listener, error) {
				return listener, nil
			},
		})
		if err != nil {
			t.Fatalf("new network server: %v", err)
		}

		serveErr := server.Serve(context.Background(), nil)
		if serveErr == nil || !strings.Contains(serveErr.Error(), "serve network") {
			t.Fatalf("expected serve error, got %v", serveErr)
		}
	})
}

func TestNetworkServerCloseStopsRelayWithoutActiveServer(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{
		CleanupInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relay.Start(ctx, nil)
	waitForStreamRelayState(t, relay, true)

	server := &NetworkServer{
		relay: relay,
	}
	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("close server: %v", err)
	}
	waitForStreamRelayState(t, relay, false)
}

func TestNetworkServerIsClosedState(t *testing.T) {
	server := &NetworkServer{}
	if !server.isClosed() {
		t.Fatal("expected nil server to be closed")
	}
	server.server = &http.Server{}
	if server.isClosed() {
		t.Fatal("expected active server to be open")
	}
}

func TestNetworkServerHandleWebSocketParseAndLimitBranches(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		MaxStreamConnections: 1,
	})
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(testContext, nil)
	}()
	t.Cleanup(func() {
		_ = server.Close(context.Background())
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("network serve goroutine did not exit")
		}
	})

	listenAddress := waitForNetworkAddress(t, server)
	wsURL := "ws://" + listenAddress + "/ws"

	firstConn, err := websocket.Dial(wsURL, "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial first ws: %v", err)
	}
	defer func() { _ = firstConn.Close() }()

	if err := websocket.Message.Send(firstConn, "{bad-json"); err != nil {
		t.Fatalf("send invalid json: %v", err)
	}
	_ = firstConn.SetReadDeadline(time.Now().Add(time.Second))
	var parseRaw string
	if err := websocket.Message.Receive(firstConn, &parseRaw); err != nil {
		t.Fatalf("receive parse error response: %v", err)
	}
	var parseResponse protocol.JSONRPCResponse
	if err := json.Unmarshal([]byte(parseRaw), &parseResponse); err != nil {
		t.Fatalf("decode parse response: %v", err)
	}
	if parseResponse.Error == nil || parseResponse.Error.Code != protocol.JSONRPCCodeParseError {
		t.Fatalf("parse response error = %#v, want parse error", parseResponse.Error)
	}

	secondConn, err := websocket.Dial(wsURL, "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial second ws: %v", err)
	}
	defer func() { _ = secondConn.Close() }()
	_ = secondConn.SetReadDeadline(time.Now().Add(time.Second))
	var limitRaw string
	if err := websocket.Message.Receive(secondConn, &limitRaw); err != nil {
		t.Fatalf("receive connection limit response: %v", err)
	}
	if !strings.Contains(limitRaw, "too_many_connections") {
		t.Fatalf("limit response = %q, want too_many_connections", limitRaw)
	}
}

func TestNetworkServerSSELimitAndWriteErrorBranches(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		MaxStreamConnections: 1,
	})
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(testContext, nil)
	}()
	t.Cleanup(func() {
		_ = server.Close(context.Background())
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("network serve goroutine did not exit")
		}
	})

	listenAddress := waitForNetworkAddress(t, server)
	firstConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial first ws: %v", err)
	}
	defer func() { _ = firstConn.Close() }()

	sseResponse, err := http.Get("http://" + listenAddress + "/sse?method=gateway.ping&id=sse-limit")
	if err != nil {
		t.Fatalf("open sse stream: %v", err)
	}
	defer sseResponse.Body.Close()
	if sseResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", sseResponse.StatusCode, http.StatusServiceUnavailable)
	}

	failingWriter := &failingSSEWriter{header: make(http.Header), failWrite: true}
	err = server.writeSSEEvent(failingWriter, failingWriter, "result", map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("expected writeSSEEvent write failure")
	}
	err = server.writeSSEEvent(failingWriter, failingWriter, "result", map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatal("expected writeSSEEvent marshal failure")
	}
}

func TestBuildSSETriggerRequestDefaults(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/sse", nil)
	trigger := buildSSETriggerRequest(request)
	if trigger.Method != protocol.MethodGatewayPing {
		t.Fatalf("method = %q, want %q", trigger.Method, protocol.MethodGatewayPing)
	}
	if strings.TrimSpace(string(trigger.ID)) == "" {
		t.Fatal("expected generated id")
	}
}

func TestRegisterAndSnapshotBranches(t *testing.T) {
	server := &NetworkServer{
		maxStreamConnections: 1,
		wsConns:              make(map[*websocket.Conn]context.CancelFunc),
		sseCancels:           make(map[int]context.CancelFunc),
	}

	cancel := func() {}
	if server.registerWSConnection(nil, cancel) {
		t.Fatal("expected ws register to fail when server is nil")
	}
	server.server = &http.Server{}
	if !server.registerWSConnection(nil, cancel) {
		t.Fatal("expected first ws register to succeed")
	}
	if server.registerWSConnection(nil, cancel) {
		t.Fatal("expected second ws register to hit limit")
	}
	server.unregisterWSConnection(nil)

	server.sseCancels = make(map[int]context.CancelFunc)
	server.nextSSEID = 0
	if id, ok := server.registerSSEConnection(cancel); !ok || id != 0 {
		t.Fatalf("expected first sse register id 0, got id=%d ok=%v", id, ok)
	}
	if _, ok := server.registerSSEConnection(cancel); ok {
		t.Fatal("expected second sse register to hit limit")
	}
	server.unregisterSSEConnection(0)

	server.wsConns = map[*websocket.Conn]context.CancelFunc{
		nil: cancel,
	}
	server.sseCancels = map[int]context.CancelFunc{
		1: cancel,
	}
	wsConnections, wsCancels, sseCancels := server.snapshotStreamConnections()
	if len(wsConnections) != 1 || len(wsCancels) != 1 || len(sseCancels) != 1 {
		t.Fatalf("snapshot sizes mismatch: ws=%d wsCancels=%d sse=%d", len(wsConnections), len(wsCancels), len(sseCancels))
	}
}

func TestIsConnectionClosedErrorBranches(t *testing.T) {
	if isConnectionClosedError(nil) {
		t.Fatal("nil error should not be closed error")
	}
	if !isConnectionClosedError(io.EOF) {
		t.Fatal("EOF should be treated as closed error")
	}
	if !isConnectionClosedError(errors.New("closed network connection")) {
		t.Fatal("closed network connection text should be treated as closed error")
	}
	if isConnectionClosedError(errors.New("boom")) {
		t.Fatal("generic error should not be treated as closed error")
	}
}

type trackCloseListener struct {
	closed bool
}

func (l *trackCloseListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l *trackCloseListener) Close() error {
	l.closed = true
	return nil
}

func (l *trackCloseListener) Addr() net.Addr {
	return stubAddr("track-close")
}

type acceptErrorListener struct{}

func (l *acceptErrorListener) Accept() (net.Conn, error) {
	return nil, errors.New("accept failed")
}

func (l *acceptErrorListener) Close() error {
	return nil
}

func (l *acceptErrorListener) Addr() net.Addr {
	return stubAddr("accept-error")
}

type failingSSEWriter struct {
	header    http.Header
	status    int
	failWrite bool
}

func (w *failingSSEWriter) Header() http.Header {
	return w.header
}

func (w *failingSSEWriter) Write(payload []byte) (int, error) {
	if w.failWrite {
		return 0, errors.New("write failed")
	}
	return len(payload), nil
}

func (w *failingSSEWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *failingSSEWriter) Flush() {}
