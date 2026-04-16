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
		"file://local",
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
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Content-Type", "application/json")
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
	server := newTestNetworkServer(t, NetworkServerOptions{MaxRequestBytes: 16})
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

	_ = wsConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var wsRawMessage string
	if err := websocket.Message.Receive(wsConn, &wsRawMessage); err == nil {
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

type noFlushResponseWriter struct {
	header http.Header
	status int
	body   strings.Builder
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
