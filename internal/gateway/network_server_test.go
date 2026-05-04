package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"neo-code/internal/gateway/protocol"
)

func TestResolveNetworkListenAddress(t *testing.T) {
	t.Run("default address", func(t *testing.T) {
		address, err := ResolveNetworkListenAddress("")
		if err != nil {
			t.Fatalf("resolve default address: %v", err)
		}
		if address != DefaultNetworkListenAddress {
			t.Fatalf("address = %q, want %q", address, DefaultNetworkListenAddress)
		}
	})

	t.Run("loopback accepted", func(t *testing.T) {
		address, err := ResolveNetworkListenAddress("127.0.0.1:19080")
		if err != nil {
			t.Fatalf("resolve loopback address: %v", err)
		}
		if address != "127.0.0.1:19080" {
			t.Fatalf("address = %q, want %q", address, "127.0.0.1:19080")
		}
	})

	t.Run("non loopback rejected", func(t *testing.T) {
		_, err := ResolveNetworkListenAddress("0.0.0.0:8080")
		if err == nil {
			t.Fatal("expected non-loopback address error")
		}
		if !strings.Contains(err.Error(), "host must be loopback") {
			t.Fatalf("error = %v, want loopback constraint", err)
		}
	})
}

func TestOriginAllowlist(t *testing.T) {
	allowed := []string{
		"http://localhost:3000",
		"http://localhost",
		"http://127.0.0.1:5173",
		"http://127.0.0.1",
		"http://[::1]:3000",
		"http://[::1]",
		"app://desktop-client",
		"file://local",
		"null",
	}
	for _, origin := range allowed {
		if !isAllowedControlPlaneOrigin(origin) {
			t.Fatalf("origin %q should be allowed", origin)
		}
	}

	disallowed := []string{
		"",
		"https://localhost:3000",
		"http://evil.example.com",
	}
	for _, origin := range disallowed {
		if isAllowedControlPlaneOrigin(origin) {
			t.Fatalf("origin %q should be rejected", origin)
		}
	}
}

func TestValidateOriginForWebSocket(t *testing.T) {
	if err := validateOriginForWebSocket(nil); err == nil {
		t.Fatal("expected nil request to be rejected")
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost/ws", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	if err := validateOriginForWebSocket(request); err != nil {
		t.Fatalf("expected allowed origin, got %v", err)
	}

	request.Header.Set("Origin", "http://evil.example")
	if err := validateOriginForWebSocket(request); err == nil {
		t.Fatal("expected disallowed origin to be rejected")
	}
}

func TestWithCORSAllowlistBehavior(t *testing.T) {
	server := &NetworkServer{}
	handler := server.withCORS(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed origin", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/rpc", nil)
		request.Header.Set("Origin", "http://localhost:3000")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Fatalf("allow origin = %q, want %q", got, "http://localhost:3000")
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/rpc", nil)
		request.Header.Set("Origin", "http://evil.example")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("options preflight", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodOptions, "/rpc", nil)
		request.Header.Set("Origin", "http://127.0.0.1:3000")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
		}
	})
}

func TestNetworkServerHTTPRPCAndCORS(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		Authenticator: staticTokenAuthenticator{token: "gateway-token"},
		ACL:           NewStrictControlPlaneACL(),
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

	requestBody := strings.NewReader(`{"jsonrpc":"2.0","id":"http-1","method":"gateway.ping","params":{}}`)
	request, err := http.NewRequest(http.MethodPost, "http://"+listenAddress+"/rpc", requestBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer gateway-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post /rpc: %v", err)
	}
	defer response.Body.Close()

	if response.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("cors allow origin = %q, want %q", response.Header.Get("Access-Control-Allow-Origin"), "http://localhost:3000")
	}

	var rpcResponse protocol.JSONRPCResponse
	if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
		t.Fatalf("decode /rpc response: %v", err)
	}
	if rpcResponse.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", rpcResponse.Error)
	}
	resultFrame, err := decodeJSONRPCResultFrame(rpcResponse)
	if err != nil {
		t.Fatalf("decode result frame: %v", err)
	}
	if resultFrame.Type != FrameTypeAck || resultFrame.Action != FrameActionPing {
		t.Fatalf("result frame = %#v, want ping ack", resultFrame)
	}
}

func TestNetworkServerRejectsDisallowedCORSOrigin(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{})
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

	requestBody := strings.NewReader(`{"jsonrpc":"2.0","id":"http-1","method":"gateway.ping","params":{}}`)
	request, err := http.NewRequest(http.MethodPost, "http://"+listenAddress+"/rpc", requestBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Origin", "http://evil.example")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post /rpc: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

func TestNetworkServerRPCErrorBranches(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		MaxRequestBytes: 16,
		Authenticator:   staticTokenAuthenticator{token: "gateway-token"},
		ACL:             NewStrictControlPlaneACL(),
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

	t.Run("method not allowed", func(t *testing.T) {
		response, err := http.Get("http://" + listenAddress + "/rpc")
		if err != nil {
			t.Fatalf("get /rpc: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, "http://"+listenAddress+"/rpc", strings.NewReader("{bad"))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer gateway-token")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post /rpc: %v", err)
		}
		defer response.Body.Close()
		var rpcResponse protocol.JSONRPCResponse
		if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
			t.Fatalf("decode rpc error response: %v", err)
		}
		if rpcResponse.Error == nil || rpcResponse.Error.Code != protocol.JSONRPCCodeParseError {
			t.Fatalf("rpc error = %#v, want parse error", rpcResponse.Error)
		}
	})

	t.Run("oversized request", func(t *testing.T) {
		request, err := http.NewRequest(
			http.MethodPost,
			"http://"+listenAddress+"/rpc",
			strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"gateway.ping","params":{}}`),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer gateway-token")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post /rpc: %v", err)
		}
		defer response.Body.Close()
		var rpcResponse protocol.JSONRPCResponse
		if err := json.NewDecoder(response.Body).Decode(&rpcResponse); err != nil {
			t.Fatalf("decode rpc error response: %v", err)
		}
		if rpcResponse.Error == nil || rpcResponse.Error.Code != protocol.JSONRPCCodeParseError {
			t.Fatalf("rpc error = %#v, want parse error", rpcResponse.Error)
		}
	})

	t.Run("unauthorized rpc maps to http 401", func(t *testing.T) {
		secureServer := newTestNetworkServer(t, NetworkServerOptions{
			Authenticator: staticTokenAuthenticator{token: "gateway-token"},
			ACL:           NewStrictControlPlaneACL(),
		})
		secureContext, secureCancel := context.WithCancel(context.Background())
		defer secureCancel()

		secureDone := make(chan error, 1)
		go func() {
			secureDone <- secureServer.Serve(secureContext, nil)
		}()
		t.Cleanup(func() {
			_ = secureServer.Close(context.Background())
			select {
			case <-secureDone:
			case <-time.After(2 * time.Second):
				t.Fatal("secure network serve goroutine did not exit")
			}
		})

		secureAddress := waitForNetworkAddress(t, secureServer)
		request, err := http.NewRequest(
			http.MethodPost,
			"http://"+secureAddress+"/rpc",
			strings.NewReader(`{"jsonrpc":"2.0","id":"unauth","method":"gateway.ping","params":{}}`),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post /rpc: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("acl denied rpc maps to http 403", func(t *testing.T) {
		deniedACL := &ControlPlaneACL{
			mode:    ACLModeStrict,
			allow:   map[RequestSource]map[string]struct{}{RequestSourceHTTP: {}},
			enabled: true,
		}
		secureServer := newTestNetworkServer(t, NetworkServerOptions{
			Authenticator: staticTokenAuthenticator{token: "gateway-token"},
			ACL:           deniedACL,
		})
		secureContext, secureCancel := context.WithCancel(context.Background())
		defer secureCancel()

		secureDone := make(chan error, 1)
		go func() {
			secureDone <- secureServer.Serve(secureContext, nil)
		}()
		t.Cleanup(func() {
			_ = secureServer.Close(context.Background())
			select {
			case <-secureDone:
			case <-time.After(2 * time.Second):
				t.Fatal("acl network serve goroutine did not exit")
			}
		})

		secureAddress := waitForNetworkAddress(t, secureServer)
		request, err := http.NewRequest(
			http.MethodPost,
			"http://"+secureAddress+"/rpc",
			strings.NewReader(`{"jsonrpc":"2.0","id":"denied","method":"gateway.ping","params":{}}`),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.Header.Set("Authorization", "Bearer gateway-token")
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post /rpc: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
		}
	})
}

func TestNetworkServerWebSocketAndSSEPing(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{})
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
	wsConn, err := websocket.Dial(wsURL, "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })

	if err := websocket.Message.Send(wsConn, `{"jsonrpc":"2.0","id":"ws-1","method":"gateway.ping","params":{}}`); err != nil {
		t.Fatalf("send websocket ping request: %v", err)
	}

	ackFrame := receiveWSAckFrame(t, wsConn)
	if ackFrame.Action != FrameActionPing {
		t.Fatalf("websocket action = %q, want %q", ackFrame.Action, FrameActionPing)
	}

	sseRequest, err := http.NewRequest(http.MethodGet, "http://"+listenAddress+"/sse?method=gateway.ping&id=sse-1", nil)
	if err != nil {
		t.Fatalf("new sse request: %v", err)
	}
	sseRequest.Header.Set("Origin", "app://desktop-client")
	sseResponse, err := http.DefaultClient.Do(sseRequest)
	if err != nil {
		t.Fatalf("get /sse: %v", err)
	}
	defer sseResponse.Body.Close()
	if sseResponse.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d, want %d", sseResponse.StatusCode, http.StatusOK)
	}

	resultFrame := readSSEResultFrame(t, sseResponse.Body)
	if resultFrame.Action != FrameActionPing {
		t.Fatalf("sse action = %q, want %q", resultFrame.Action, FrameActionPing)
	}
}

func TestNetworkServerWebSocketOriginRejected(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{})
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
	if _, err := websocket.Dial(wsURL, "", "http://evil.example"); err == nil {
		t.Fatal("expected websocket handshake to reject disallowed origin")
	}
}

func TestNetworkServerWebSocketReadTimeoutDoesNotKillIdleConnection(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		ReadTimeout:       80 * time.Millisecond,
		HeartbeatInterval: 30 * time.Millisecond,
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
	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })

	time.Sleep(300 * time.Millisecond)

	if err := websocket.Message.Send(wsConn, `{"jsonrpc":"2.0","id":"ws-idle","method":"gateway.ping","params":{}}`); err != nil {
		t.Fatalf("send ping after idle: %v", err)
	}
	ackFrame := receiveWSAckFrame(t, wsConn)
	if ackFrame.RequestID != "ws-idle" {
		t.Fatalf("request_id = %q, want %q", ackFrame.RequestID, "ws-idle")
	}
}

func TestNetworkServerWebSocketUnauthenticatedConnectionTimeout(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		Authenticator:                staticTokenAuthenticator{token: "gateway-token"},
		ACL:                          NewStrictControlPlaneACL(),
		MaxStreamConnections:         1,
		UnauthenticatedWSGracePeriod: 120 * time.Millisecond,
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
	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })

	waitForWebSocketConnectionCount(t, server, 1, 2*time.Second)
	waitForWebSocketConnectionCount(t, server, 0, 2*time.Second)

	waitForWebSocketClosed(t, wsConn, 2*time.Second)
}

func TestNetworkServerWebSocketAuthenticatedConnectionBypassesTimeout(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		Authenticator:                staticTokenAuthenticator{token: "gateway-token"},
		ACL:                          NewStrictControlPlaneACL(),
		UnauthenticatedWSGracePeriod: 120 * time.Millisecond,
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
	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws?token=gateway-token", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })

	time.Sleep(250 * time.Millisecond)
	if err := websocket.Message.Send(wsConn, `{"jsonrpc":"2.0","id":"ws-auth-ok","method":"gateway.ping","params":{}}`); err != nil {
		t.Fatalf("send ping after auth grace period: %v", err)
	}
	ackFrame := receiveWSAckFrame(t, wsConn)
	if ackFrame.RequestID != "ws-auth-ok" {
		t.Fatalf("request_id = %q, want %q", ackFrame.RequestID, "ws-auth-ok")
	}
}

func TestNetworkServerWebSocketDispatchContextCancelledOnShutdown(t *testing.T) {
	originalDispatch := dispatchRPCRequestFn
	t.Cleanup(func() { dispatchRPCRequestFn = originalDispatch })

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	dispatchRPCRequestFn = func(ctx context.Context, request protocol.JSONRPCRequest, runtimePort RuntimePort) protocol.JSONRPCResponse {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case cancelled <- struct{}{}:
		default:
		}
		return protocol.NewJSONRPCErrorResponse(
			json.RawMessage(`"ws-cancel"`),
			protocol.NewJSONRPCError(protocol.JSONRPCCodeInternalError, "cancelled", protocol.GatewayCodeInternalError),
		)
	}

	server := newTestNetworkServer(t, NetworkServerOptions{})
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(testContext, nil)
	}()
	listenAddress := waitForNetworkAddress(t, server)

	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = wsConn.Close() }()

	if err := websocket.Message.Send(wsConn, `{"jsonrpc":"2.0","id":"ws-block","method":"gateway.ping","params":{}}`); err != nil {
		t.Fatalf("send websocket request: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dispatch function was not invoked")
	}

	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("close network server: %v", err)
	}

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("expected websocket dispatch context to be cancelled on shutdown")
	}

	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("serve returned error: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after close")
	}
}

func TestNetworkServerSSEErrorBranches(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{})

	t.Run("method not allowed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/sse", nil)
		server.handleSSERequest(recorder, request, nil)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("streaming unsupported", func(t *testing.T) {
		writer := &noFlushResponseWriter{header: make(http.Header)}
		request := httptest.NewRequest(http.MethodGet, "/sse", nil)
		server.handleSSERequest(writer, request, nil)
		if writer.status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", writer.status, http.StatusInternalServerError)
		}
	})
}

func TestDecodeJSONRPCRequestFromReaderTrailingJSON(t *testing.T) {
	request, rpcErr := decodeJSONRPCRequestFromReader(strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"gateway.ping"} {"extra":1}`))
	if rpcErr == nil {
		t.Fatalf("expected parse error, got request %#v", request)
	}
	if rpcErr.Code != protocol.JSONRPCCodeParseError {
		t.Fatalf("rpc error code = %d, want %d", rpcErr.Code, protocol.JSONRPCCodeParseError)
	}
}

func TestNetworkServerVersionAndObservabilityAuthHelpers(t *testing.T) {
	server := &NetworkServer{
		authenticator: stubTokenAuthenticator{token: "token-1"},
	}

	t.Run("version method not allowed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/version", nil)
		server.handleVersionRequest(recorder, request)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("version get returns build info", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/version", nil)
		request.Header.Set("Authorization", "Bearer token-1")
		server.handleVersionRequest(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		var payload map[string]string
		if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
			t.Fatalf("decode version response: %v", err)
		}
		if payload["version"] == "" || payload["commit"] == "" {
			t.Fatalf("unexpected version payload: %#v", payload)
		}
	})

	t.Run("version remains public when authenticator enabled", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/version", nil)
		server.handleVersionRequest(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
	})

	t.Run("observability auth uses bearer token", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.Header.Set("Authorization", "Bearer token-1")
		if !server.isObservabilityRequestAuthorized(request) {
			t.Fatal("expected valid bearer token to pass")
		}
		request.Header.Set("Authorization", "Bearer wrong")
		if server.isObservabilityRequestAuthorized(request) {
			t.Fatal("expected invalid token to be rejected")
		}
	})

	t.Run("observability auth denies when authenticator nil", func(t *testing.T) {
		openServer := &NetworkServer{}
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if openServer.isObservabilityRequestAuthorized(request) {
			t.Fatal("expected request to be rejected without authenticator")
		}
	})
}

func TestNetworkServerCloseInterruptsStreams(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{})
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(testContext, nil)
	}()
	listenAddress := waitForNetworkAddress(t, server)

	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = wsConn.Close() }()

	sseResponse, err := http.Get("http://" + listenAddress + "/sse?method=gateway.ping&id=sse-close")
	if err != nil {
		t.Fatalf("open sse stream: %v", err)
	}
	defer sseResponse.Body.Close()

	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("close network server: %v", err)
	}

	websocketClosed := false
	wsCloseDeadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(wsCloseDeadline) {
		_ = wsConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		var wsRawMessage string
		if err := websocket.Message.Receive(wsConn, &wsRawMessage); err != nil {
			websocketClosed = true
			break
		}
	}
	if !websocketClosed {
		t.Fatal("expected websocket receive to fail after server close")
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, sseResponse.Body)
		readDone <- readErr
	}()

	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("sse stream was not closed after network server close")
	}

	select {
	case serveErr := <-serveDone:
		if serveErr != nil {
			t.Fatalf("serve returned error: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after close")
	}
}

func TestNetworkServerStreamsReceiveGatewayEventNotification(t *testing.T) {
	eventCh := make(chan RuntimeEvent, 2)
	runtimePort := &runtimePortEventStub{events: eventCh}

	server := newTestNetworkServer(t, NetworkServerOptions{})
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(testContext, runtimePort)
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
	wsConn, err := websocket.Dial("ws://"+listenAddress+"/ws", "", "http://localhost:3000")
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = wsConn.Close() })

	if err := websocket.Message.Send(wsConn, `{"jsonrpc":"2.0","id":"bind-ws-1","method":"gateway.bindStream","params":{"session_id":"session-relay","run_id":"run-relay","channel":"ws"}}`); err != nil {
		t.Fatalf("send bindStream request: %v", err)
	}
	bindAck := receiveWSAckFrame(t, wsConn)
	if bindAck.Action != FrameActionBindStream {
		t.Fatalf("bind action = %q, want %q", bindAck.Action, FrameActionBindStream)
	}

	sseRequest, err := http.NewRequest(
		http.MethodGet,
		"http://"+listenAddress+"/sse?method=gateway.ping&id=sse-relay-1&session_id=session-relay&run_id=run-relay",
		nil,
	)
	if err != nil {
		t.Fatalf("new sse request: %v", err)
	}
	sseRequest.Header.Set("Origin", "http://localhost:3000")
	sseResponse, err := http.DefaultClient.Do(sseRequest)
	if err != nil {
		t.Fatalf("open sse stream: %v", err)
	}
	t.Cleanup(func() { _ = sseResponse.Body.Close() })
	if sseResponse.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d, want %d", sseResponse.StatusCode, http.StatusOK)
	}
	_ = readSSEResultFrame(t, sseResponse.Body)

	eventCh <- RuntimeEvent{
		Type:      RuntimeEventTypeRunProgress,
		SessionID: "session-relay",
		RunID:     "run-relay",
		Payload: map[string]string{
			"chunk": "hello",
		},
	}

	wsEvent := receiveWSGatewayEventNotification(t, wsConn)
	if wsEvent.SessionID != "session-relay" || wsEvent.RunID != "run-relay" {
		t.Fatalf("ws event frame mismatch: %#v", wsEvent)
	}

	sseEvent := readSSEGatewayEventFrame(t, sseResponse.Body)
	if sseEvent.SessionID != "session-relay" || sseEvent.RunID != "run-relay" {
		t.Fatalf("sse event frame mismatch: %#v", sseEvent)
	}
}

func TestNetworkServerObservabilityEndpointsAuth(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		Authenticator: staticTokenAuthenticator{token: "gateway-token"},
		Metrics:       NewGatewayMetrics(),
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

	healthResponse, err := http.Get("http://" + listenAddress + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want %d", healthResponse.StatusCode, http.StatusOK)
	}

	versionResponse, err := http.Get("http://" + listenAddress + "/version")
	if err != nil {
		t.Fatalf("get /version: %v", err)
	}
	defer versionResponse.Body.Close()
	if versionResponse.StatusCode != http.StatusOK {
		t.Fatalf("/version status = %d, want %d", versionResponse.StatusCode, http.StatusOK)
	}

	metricsResponse, err := http.Get("http://" + listenAddress + "/metrics")
	if err != nil {
		t.Fatalf("get /metrics: %v", err)
	}
	defer metricsResponse.Body.Close()
	if metricsResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/metrics status = %d, want %d", metricsResponse.StatusCode, http.StatusUnauthorized)
	}

	queryTokenMetricsResponse, err := http.Get("http://" + listenAddress + "/metrics?token=gateway-token")
	if err != nil {
		t.Fatalf("get /metrics with query token: %v", err)
	}
	defer queryTokenMetricsResponse.Body.Close()
	if queryTokenMetricsResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/metrics with query token status = %d, want %d", queryTokenMetricsResponse.StatusCode, http.StatusUnauthorized)
	}

	authorizedMetricsRequest, err := http.NewRequest(http.MethodGet, "http://"+listenAddress+"/metrics", nil)
	if err != nil {
		t.Fatalf("new /metrics request: %v", err)
	}
	authorizedMetricsRequest.Header.Set("Authorization", "Bearer gateway-token")
	authorizedMetricsResponse, err := http.DefaultClient.Do(authorizedMetricsRequest)
	if err != nil {
		t.Fatalf("authorized get /metrics: %v", err)
	}
	defer authorizedMetricsResponse.Body.Close()
	if authorizedMetricsResponse.StatusCode != http.StatusOK {
		t.Fatalf("authorized /metrics status = %d, want %d", authorizedMetricsResponse.StatusCode, http.StatusOK)
	}

	authorizedJSONMetricsRequest, err := http.NewRequest(http.MethodGet, "http://"+listenAddress+"/metrics.json", nil)
	if err != nil {
		t.Fatalf("new /metrics.json request: %v", err)
	}
	authorizedJSONMetricsRequest.Header.Set("Authorization", "Bearer gateway-token")
	authorizedJSONMetricsResponse, err := http.DefaultClient.Do(authorizedJSONMetricsRequest)
	if err != nil {
		t.Fatalf("authorized get /metrics.json: %v", err)
	}
	defer authorizedJSONMetricsResponse.Body.Close()
	if authorizedJSONMetricsResponse.StatusCode != http.StatusOK {
		t.Fatalf("authorized /metrics.json status = %d, want %d", authorizedJSONMetricsResponse.StatusCode, http.StatusOK)
	}

	queryTokenJSONMetricsResponse, err := http.Get("http://" + listenAddress + "/metrics.json?token=gateway-token")
	if err != nil {
		t.Fatalf("get /metrics.json with query token: %v", err)
	}
	defer queryTokenJSONMetricsResponse.Body.Close()
	if queryTokenJSONMetricsResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"/metrics.json with query token status = %d, want %d",
			queryTokenJSONMetricsResponse.StatusCode,
			http.StatusUnauthorized,
		)
	}
}

func TestNetworkServerMetricsEndpointReturnsUnavailableWhenDisabled(t *testing.T) {
	server := newTestNetworkServer(t, NetworkServerOptions{
		Metrics: nil,
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
	metricsResponse, err := http.Get("http://" + listenAddress + "/metrics")
	if err != nil {
		t.Fatalf("get /metrics: %v", err)
	}
	defer metricsResponse.Body.Close()
	if metricsResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/metrics status = %d, want %d", metricsResponse.StatusCode, http.StatusServiceUnavailable)
	}
	metricsBody, err := io.ReadAll(metricsResponse.Body)
	if err != nil {
		t.Fatalf("read /metrics response body: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(metricsBody)), "metrics disabled") {
		t.Fatalf("/metrics body = %q, want contains %q", string(metricsBody), "metrics disabled")
	}

	metricsJSONResponse, err := http.Get("http://" + listenAddress + "/metrics.json")
	if err != nil {
		t.Fatalf("get /metrics.json: %v", err)
	}
	defer metricsJSONResponse.Body.Close()
	if metricsJSONResponse.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/metrics.json status = %d, want %d", metricsJSONResponse.StatusCode, http.StatusServiceUnavailable)
	}

	var metricsJSONBody map[string]any
	if err := json.NewDecoder(metricsJSONResponse.Body).Decode(&metricsJSONBody); err != nil {
		t.Fatalf("decode /metrics.json body: %v", err)
	}
	if metricsJSONBody["error"] != "metrics disabled" {
		t.Fatalf("/metrics.json error = %v, want %q", metricsJSONBody["error"], "metrics disabled")
	}
}

func TestWithCORSCustomAllowOrigins(t *testing.T) {
	server := &NetworkServer{
		allowedOrigins: []string{"http://custom.local"},
	}
	handler := server.withCORS(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))

	allowedRequest := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	allowedRequest.Header.Set("Origin", "http://custom.local:3000")
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowedRequest)
	if allowedRecorder.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want %d", allowedRecorder.Code, http.StatusOK)
	}

	rejectedRequest := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	rejectedRequest.Header.Set("Origin", "http://localhost:3000")
	rejectedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rejectedRecorder, rejectedRequest)
	if rejectedRecorder.Code != http.StatusForbidden {
		t.Fatalf("rejected status = %d, want %d", rejectedRecorder.Code, http.StatusForbidden)
	}
}

// newTestNetworkServer 创建默认测试网络服务实例，统一收敛测试参数。
func newTestNetworkServer(t *testing.T, overrides NetworkServerOptions) *NetworkServer {
	t.Helper()

	if strings.TrimSpace(overrides.ListenAddress) == "" {
		overrides.ListenAddress = "127.0.0.1:0"
	}
	if overrides.Logger == nil {
		overrides.Logger = log.New(io.Discard, "", 0)
	}
	if overrides.HeartbeatInterval <= 0 {
		overrides.HeartbeatInterval = 100 * time.Millisecond
	}
	if overrides.ReadTimeout <= 0 {
		overrides.ReadTimeout = 2 * time.Second
	}
	if overrides.WriteTimeout <= 0 {
		overrides.WriteTimeout = 2 * time.Second
	}
	if overrides.ShutdownTimeout <= 0 {
		overrides.ShutdownTimeout = 500 * time.Millisecond
	}

	server, err := NewNetworkServer(overrides)
	if err != nil {
		t.Fatalf("new network server: %v", err)
	}
	return server
}

// waitForNetworkAddress 等待网络服务绑定实际端口，避免使用 127.0.0.1:0 发起请求。
func waitForNetworkAddress(t *testing.T, server *NetworkServer) string {
	t.Helper()

	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for network listen address")
		case <-ticker.C:
			address := server.ListenAddress()
			if !strings.HasSuffix(address, ":0") && strings.TrimSpace(address) != "" {
				return address
			}
		}
	}
}

// waitForWebSocketConnectionCount 轮询等待 WS 连接数达到目标值，便于验证超时剔除是否生效。
func waitForWebSocketConnectionCount(t *testing.T, server *NetworkServer, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			server.mu.Lock()
			got := len(server.wsConns)
			server.mu.Unlock()
			t.Fatalf("timed out waiting websocket connections = %d, got %d", want, got)
		case <-ticker.C:
			server.mu.Lock()
			got := len(server.wsConns)
			server.mu.Unlock()
			if got == want {
				return
			}
		}
	}
}

// waitForWebSocketClosed 循环读取直到连接关闭；会忽略关闭前可能滞留在缓冲区中的心跳消息。
func waitForWebSocketClosed(t *testing.T, wsConn *websocket.Conn, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = wsConn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		var rawMessage string
		err := websocket.Message.Receive(wsConn, &rawMessage)
		if err != nil {
			return
		}
	}
	t.Fatal("expected websocket connection to be closed before timeout")
}

// receiveWSAckFrame 连续读取 WS 消息直到拿到 JSON-RPC ACK 结果帧。
func receiveWSAckFrame(t *testing.T, wsConn *websocket.Conn) MessageFrame {
	t.Helper()
	_ = wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for attempt := 0; attempt < 12; attempt++ {
		var rawResponse string
		if err := websocket.Message.Receive(wsConn, &rawResponse); err != nil {
			t.Fatalf("receive websocket message: %v", err)
		}
		var rpcResponse protocol.JSONRPCResponse
		if err := json.Unmarshal([]byte(rawResponse), &rpcResponse); err != nil {
			continue
		}
		if rpcResponse.JSONRPC == "" {
			continue
		}
		if rpcResponse.Error != nil {
			t.Fatalf("unexpected websocket rpc error: %+v", rpcResponse.Error)
		}
		resultFrame, err := decodeJSONRPCResultFrame(rpcResponse)
		if err != nil {
			t.Fatalf("decode websocket result frame: %v", err)
		}
		return resultFrame
	}
	t.Fatal("did not receive websocket ack frame")
	return MessageFrame{}
}

// readSSEResultFrame 读取 SSE result 事件并解析内部 JSON-RPC 结果帧。
func readSSEResultFrame(t *testing.T, body io.Reader) MessageFrame {
	t.Helper()
	reader := bufio.NewReader(body)
	currentEvent := ""
	timeout := time.After(3 * time.Second)

	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for sse result")
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read sse line: %v", err)
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
				continue
			}
			if currentEvent == "result" && strings.HasPrefix(trimmed, "data:") {
				rawData := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				var rpcResponse protocol.JSONRPCResponse
				if err := json.Unmarshal([]byte(rawData), &rpcResponse); err != nil {
					t.Fatalf("decode sse result: %v", err)
				}
				resultFrame, err := decodeJSONRPCResultFrame(rpcResponse)
				if err != nil {
					t.Fatalf("decode sse result frame: %v", err)
				}
				return resultFrame
			}
		}
	}
}

// receiveWSGatewayEventNotification 读取 WS 消息直到拿到 gateway.event 通知并返回其内层事件帧。
func receiveWSGatewayEventNotification(t *testing.T, wsConn *websocket.Conn) MessageFrame {
	t.Helper()
	_ = wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for attempt := 0; attempt < 20; attempt++ {
		var rawResponse string
		if err := websocket.Message.Receive(wsConn, &rawResponse); err != nil {
			t.Fatalf("receive websocket message: %v", err)
		}

		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(rawResponse), &envelope); err != nil {
			continue
		}
		if envelope.Method != protocol.MethodGatewayEvent {
			continue
		}
		var eventFrame MessageFrame
		if err := json.Unmarshal(envelope.Params, &eventFrame); err != nil {
			t.Fatalf("decode gateway.event params: %v", err)
		}
		return eventFrame
	}
	t.Fatal("did not receive websocket gateway.event notification")
	return MessageFrame{}
}

// readSSEGatewayEventFrame 读取 SSE 流直到捕获 gateway.event 事件并解析其内层事件帧。
func readSSEGatewayEventFrame(t *testing.T, body io.Reader) MessageFrame {
	t.Helper()
	reader := bufio.NewReader(body)
	timeout := time.After(3 * time.Second)
	currentEvent := ""

	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for sse gateway.event")
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read sse line: %v", err)
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
				continue
			}
			if currentEvent != protocol.MethodGatewayEvent || !strings.HasPrefix(trimmed, "data:") {
				continue
			}

			rawData := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			var notification struct {
				JSONRPC string          `json:"jsonrpc"`
				Method  string          `json:"method"`
				Params  json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal([]byte(rawData), &notification); err != nil {
				t.Fatalf("decode sse gateway.event notification: %v", err)
			}
			if notification.Method != protocol.MethodGatewayEvent {
				continue
			}

			var eventFrame MessageFrame
			if err := json.Unmarshal(notification.Params, &eventFrame); err != nil {
				t.Fatalf("decode sse gateway.event params: %v", err)
			}
			return eventFrame
		}
	}
}

type noFlushResponseWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

type staticTokenAuthenticator struct {
	token string
}

func (a staticTokenAuthenticator) ValidateToken(token string) bool {
	return strings.TrimSpace(token) != "" && strings.TrimSpace(token) == strings.TrimSpace(a.token)
}

func (a staticTokenAuthenticator) ResolveSubjectID(token string) (string, bool) {
	if !a.ValidateToken(token) {
		return "", false
	}
	return "local_admin", true
}

func (w *noFlushResponseWriter) Header() http.Header {
	return w.header
}

func (w *noFlushResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(payload)
}

func (w *noFlushResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}
