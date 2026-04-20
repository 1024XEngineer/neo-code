package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway/protocol"
)

type stubConn struct {
	writeErr error
	closed   bool
	mu       sync.Mutex
}

func (s *stubConn) Read(_ []byte) (int, error) { return 0, io.EOF }
func (s *stubConn) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}
func (s *stubConn) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
func (s *stubConn) LocalAddr() net.Addr                { return &net.UnixAddr{} }
func (s *stubConn) RemoteAddr() net.Addr               { return &net.UnixAddr{} }
func (s *stubConn) SetDeadline(_ time.Time) error      { return nil }
func (s *stubConn) SetReadDeadline(_ time.Time) error  { return nil }
func (s *stubConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestGatewayRPCErrorAndTransportErrorFormatting(t *testing.T) {
	t.Parallel()

	var rpcErr *GatewayRPCError
	if rpcErr.Error() != "" {
		t.Fatalf("nil GatewayRPCError should render empty string")
	}

	errWithCode := (&GatewayRPCError{Method: "gateway.run", GatewayCode: "timeout", Message: "boom"}).Error()
	if !strings.Contains(errWithCode, "timeout") {
		t.Fatalf("GatewayRPCError with code = %q", errWithCode)
	}

	var transportErr *gatewayRPCTransportError
	if transportErr.Error() != "" || transportErr.Unwrap() != nil {
		t.Fatalf("nil transport error should render empty and unwrap nil")
	}

	methodErr := &gatewayRPCTransportError{Method: "gateway.run", Err: errors.New("down")}
	if !strings.Contains(methodErr.Error(), "gateway.run") {
		t.Fatalf("unexpected transport error text: %q", methodErr.Error())
	}
	noMethodErr := (&gatewayRPCTransportError{Err: errors.New("down")}).Error()
	if !strings.Contains(noMethodErr, "transport error") {
		t.Fatalf("unexpected no-method transport error text: %q", noMethodErr)
	}
	if !errors.Is(methodErr, methodErr.Err) {
		t.Fatalf("transport error should unwrap original cause")
	}
}

func TestGatewayRPCClientHelperFunctions(t *testing.T) {
	t.Parallel()

	mapped := mapGatewayRPCError("gateway.ping", nil)
	typed, ok := mapped.(*GatewayRPCError)
	if !ok || typed.GatewayCode != protocol.GatewayCodeInternalError {
		t.Fatalf("mapGatewayRPCError(nil) = %#v", mapped)
	}

	emptyMessage := mapGatewayRPCError("gateway.ping", &protocol.JSONRPCError{Code: protocol.JSONRPCCodeInternalError})
	if !strings.Contains(emptyMessage.Error(), "empty rpc error message") {
		t.Fatalf("empty message fallback missing: %v", emptyMessage)
	}

	if normalizeJSONRPCResponseID(json.RawMessage(`"id-1"`)) != "id-1" {
		t.Fatalf("normalize string id mismatch")
	}
	if normalizeJSONRPCResponseID(json.RawMessage(` 7 `)) != "7" {
		t.Fatalf("normalize numeric id mismatch")
	}
	if normalizeJSONRPCResponseID(json.RawMessage(`null`)) != "" {
		t.Fatalf("normalize null id mismatch")
	}
	if decodeRawJSONString(json.RawMessage(`"  m  "`)) != "m" {
		t.Fatalf("decodeRawJSONString mismatch")
	}
	if decodeRawJSONString(json.RawMessage(`{`)) != "" {
		t.Fatalf("decodeRawJSONString invalid payload should return empty")
	}

	raw, err := marshalJSONRawMessage(map[string]any{"ok": true})
	if err != nil || len(raw) == 0 {
		t.Fatalf("marshalJSONRawMessage() = (%q, %v)", raw, err)
	}
	if _, err := marshalJSONRawMessage(func() {}); err == nil {
		t.Fatalf("expected marshalJSONRawMessage() error for function input")
	}

	if cloneJSONRawMessage(nil) != nil {
		t.Fatalf("clone nil should return nil")
	}
	origin := json.RawMessage(`{"k":"v"}`)
	cloned := cloneJSONRawMessage(origin)
	origin[0] = '['
	if string(cloned) != `{"k":"v"}` {
		t.Fatalf("cloneJSONRawMessage should deep clone, got %q", string(cloned))
	}

	if !isRetryableGatewayCallError(context.DeadlineExceeded) {
		t.Fatalf("deadline exceeded should be retryable")
	}
	if isRetryableGatewayCallError(context.Canceled) {
		t.Fatalf("context canceled should not be retryable")
	}
	if !isRetryableGatewayCallError(&gatewayRPCTransportError{Err: errors.New("x")}) {
		t.Fatalf("transport error should be retryable")
	}
	if !isRetryableGatewayCallError(&GatewayRPCError{GatewayCode: protocol.GatewayCodeTimeout}) {
		t.Fatalf("gateway timeout should be retryable")
	}
	if isRetryableGatewayCallError(errors.New("plain")) {
		t.Fatalf("plain error should not be retryable")
	}
	if isRetryableGatewayCallError(nil) {
		t.Fatalf("nil error should not be retryable")
	}

	if _, err := decodeGatewayRPCResponse(map[string]json.RawMessage{"id": json.RawMessage(`bad`)}); err == nil {
		t.Fatalf("expected decodeGatewayRPCResponse marshal error")
	}
	if _, err := decodeGatewayRPCResponse(map[string]json.RawMessage{"id": json.RawMessage(`"id"`), "result": json.RawMessage(`{`)}); err == nil {
		t.Fatalf("expected decodeGatewayRPCResponse unmarshal error")
	}
}

func TestGatewayRPCClientPendingAndForceCloseBranches(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           map[string]chan gatewayRPCResponse{},
		notifications:     make(chan gatewayRPCNotification, 1),
		notificationQueue: make(chan gatewayRPCNotification, 1),
	}

	ch := make(chan gatewayRPCResponse, 1)
	if ok := client.registerPending("req-1", ch); !ok {
		t.Fatalf("registerPending should succeed")
	}
	client.dispatchResponse(gatewayRPCResponse{ID: "req-1", Result: json.RawMessage(`{"ok":true}`)})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("dispatchResponse did not deliver response")
	}

	client.dispatchResponse(gatewayRPCResponse{ID: ""})
	client.dispatchResponse(gatewayRPCResponse{ID: "missing"})
	client.unregisterPending("missing")

	pendingCh := make(chan gatewayRPCResponse, 1)
	client.pending["req-2"] = pendingCh
	if err := client.forceCloseWithError(nil); err != nil {
		t.Fatalf("forceCloseWithError() error = %v", err)
	}
	select {
	case response := <-pendingCh:
		if response.TransportErr == nil {
			t.Fatalf("expected transport error to be forwarded")
		}
	case <-time.After(time.Second):
		t.Fatalf("pending response channel not notified")
	}

	close(client.closed)
	if ok := client.registerPending("req-3", make(chan gatewayRPCResponse, 1)); ok {
		t.Fatalf("registerPending should fail after client closed")
	}
	client.enqueueNotification(gatewayRPCNotification{Method: protocol.MethodGatewayEvent})

	resetConn := &stubConn{}
	client.conn = resetConn
	client.resetConnection()
	if !resetConn.closed {
		t.Fatalf("resetConnection should close active connection")
	}
}

func TestLoadGatewayAuthTokenErrorBranches(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "missing-token.json")
	if _, err := loadGatewayAuthToken(missingPath); err == nil {
		t.Fatalf("expected load token error for missing file")
	}

	path := filepath.Join(t.TempDir(), "auth.json")
	err := os.WriteFile(path, []byte(`{"version":1,"token":"   ","created_at":"2026-04-20T00:00:00Z","updated_at":"2026-04-20T00:00:00Z"}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := loadGatewayAuthToken(path); err == nil || !strings.Contains(err.Error(), "auth token is empty") {
		if err == nil || !strings.Contains(err.Error(), "token is empty") {
			t.Fatalf("expected empty auth token error, got %v", err)
		}
	}
}

func TestGatewayRPCClientCallWithClosedClientAndInvalidResult(t *testing.T) {
	t.Parallel()

	tokenFile, _ := createTestAuthTokenFile(t)
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				dec := json.NewDecoder(serverConn)
				enc := json.NewEncoder(serverConn)
				req := readRPCRequestOrFail(t, dec)
				response := protocol.JSONRPCResponse{JSONRPC: protocol.JSONRPCVersion, ID: req.ID, Result: json.RawMessage(`1`)}
				if encodeErr := enc.Encode(response); encodeErr != nil {
					t.Errorf("encode response: %v", encodeErr)
				}
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var out map[string]any
	callErr := client.CallWithOptions(context.Background(), protocol.MethodGatewayPing, map[string]any{}, &out, GatewayRPCCallOptions{Timeout: time.Second})
	if callErr == nil || !strings.Contains(callErr.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", callErr)
	}

	_ = client.Close()
	if err := client.CallWithOptions(context.Background(), protocol.MethodGatewayPing, nil, nil, GatewayRPCCallOptions{}); err == nil {
		t.Fatalf("expected closed client call error")
	}
}

func TestNewGatewayRPCClientConstructorBranches(t *testing.T) {
	t.Parallel()

	tokenFile, _ := createTestAuthTokenFile(t)
	_, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "x",
		TokenFile:     tokenFile,
		ResolveListenAddress: func(string) (string, error) {
			return "", errors.New("resolve failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "resolve listen address") {
		t.Fatalf("expected resolve listen address error, got %v", err)
	}

	_, err = NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "x",
		TokenFile:     filepath.Join(t.TempDir(), "missing.json"),
		ResolveListenAddress: func(string) (string, error) {
			return "ipc://x", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "load auth token") {
		t.Fatalf("expected load auth token error, got %v", err)
	}

	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "x",
		TokenFile:     tokenFile,
		ResolveListenAddress: func(string) (string, error) {
			return "ipc://x", nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	if client.requestTimeout != defaultGatewayRPCRequestTimeout || client.retryCount != defaultGatewayRPCRetryCount || client.dialFn == nil {
		t.Fatalf("default options not applied: timeout=%v retry=%d dialFnNil=%v", client.requestTimeout, client.retryCount, client.dialFn == nil)
	}
	_ = client.Close()
}

func TestGatewayRPCClientCallOnceBranches(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		listenAddress:     "x",
		requestTimeout:    time.Second,
		retryCount:        1,
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan gatewayRPCNotification, 4),
		notificationQueue: make(chan gatewayRPCNotification, 4),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.callOnce(ctx, "m", nil, nil, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}

	client.dialFn = func(string) (net.Conn, error) { return nil, errors.New("dial failed") }
	if err := client.callOnce(context.Background(), "m", nil, nil, time.Second); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("expected dial transport error, got %v", err)
	}

	conn := &stubConn{}
	client.conn = conn
	if err := client.callOnce(context.Background(), "m", func() {}, nil, time.Second); err == nil || !strings.Contains(err.Error(), "encode request params") {
		t.Fatalf("expected params encode error, got %v", err)
	}

	close(client.closed)
	if err := client.callOnce(context.Background(), "m", nil, nil, time.Second); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed client error, got %v", err)
	}
}

func TestGatewayRPCClientCallOnceResponseBranches(t *testing.T) {
	t.Parallel()

	newClient := func() *GatewayRPCClient {
		return &GatewayRPCClient{
			listenAddress:     "x",
			requestTimeout:    time.Second,
			retryCount:        1,
			closed:            make(chan struct{}),
			pending:           make(map[string]chan gatewayRPCResponse),
			notifications:     make(chan gatewayRPCNotification, 4),
			notificationQueue: make(chan gatewayRPCNotification, 4),
			conn:              &stubConn{},
		}
	}

	t.Run("transport error", func(t *testing.T) {
		client := newClient()
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.dispatchResponse(gatewayRPCResponse{ID: "tui-1", TransportErr: errors.New("broken")})
		}()
		err := client.callOnce(context.Background(), "m", nil, &map[string]any{}, time.Second)
		if err == nil || !strings.Contains(err.Error(), "transport") {
			t.Fatalf("expected transport response error, got %v", err)
		}
	})

	t.Run("rpc error", func(t *testing.T) {
		client := newClient()
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.dispatchResponse(gatewayRPCResponse{ID: "tui-1", RPCError: &protocol.JSONRPCError{Code: -32000, Message: "bad"}})
		}()
		err := client.callOnce(context.Background(), "m", nil, &map[string]any{}, time.Second)
		if err == nil || !strings.Contains(err.Error(), "gateway rpc m failed") {
			t.Fatalf("expected rpc mapped error, got %v", err)
		}
	})

	t.Run("result nil", func(t *testing.T) {
		client := newClient()
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.dispatchResponse(gatewayRPCResponse{ID: "tui-1"})
		}()
		if err := client.callOnce(context.Background(), "m", nil, nil, time.Second); err != nil {
			t.Fatalf("expected nil result path, got %v", err)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		client := newClient()
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.dispatchResponse(gatewayRPCResponse{ID: "tui-1"})
		}()
		err := client.callOnce(context.Background(), "m", nil, &map[string]any{}, time.Second)
		if err == nil || !strings.Contains(err.Error(), "result is empty") {
			t.Fatalf("expected empty result error, got %v", err)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		client := newClient()
		go func() {
			time.Sleep(10 * time.Millisecond)
			client.dispatchResponse(gatewayRPCResponse{ID: "tui-1", Result: json.RawMessage(`1`)})
		}()
		err := client.callOnce(context.Background(), "m", nil, &map[string]any{}, time.Second)
		if err == nil || !strings.Contains(err.Error(), "decode m response") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})
}

func TestGatewayRPCClientReadLoopAdditionalBranches(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan gatewayRPCNotification, 4),
		notificationQueue: make(chan gatewayRPCNotification, 4),
	}
	client.startNotificationDispatcher()
	go client.readLoop(clientConn)

	encoder := json.NewEncoder(serverConn)
	_ = encoder.Encode(map[string]any{"method": "   "})
	_ = encoder.Encode(map[string]any{"id": json.RawMessage(`\"x\"`), "result": json.RawMessage(`{`)})
	_ = encoder.Encode(map[string]any{"method": protocol.MethodGatewayEvent, "params": map[string]any{"x": 1}})
	_ = serverConn.Close()

	select {
	case <-client.notifications:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected one forwarded notification")
	}

	_ = client.Close()
}

func TestGatewayRPCClientNotificationDispatcherStopsOnCloseSignal(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan gatewayRPCNotification, 1),
		notificationQueue: make(chan gatewayRPCNotification, 1),
	}
	client.startNotificationDispatcher()
	close(client.closed)
	client.notificationWG.Wait()
}

func TestGatewayRPCClientEnqueueNotificationDoesNotDropUnderQueuePressure(t *testing.T) {
	t.Parallel()

	const total = 256
	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan gatewayRPCNotification, 1),
		notificationQueue: make(chan gatewayRPCNotification, 1),
	}
	client.startNotificationDispatcher()
	t.Cleanup(func() { _ = client.Close() })

	receivedCh := make(chan struct{}, total)
	go func() {
		for range client.Notifications() {
			receivedCh <- struct{}{}
		}
	}()

	var enqueueWG sync.WaitGroup
	for i := 0; i < total; i++ {
		enqueueWG.Add(1)
		go func(index int) {
			defer enqueueWG.Done()
			client.enqueueNotification(gatewayRPCNotification{
				Method: protocol.MethodGatewayEvent,
				Params: json.RawMessage(`{"index":` + strconv.Itoa(index) + `}`),
			})
		}(i)
	}

	waitDone := make(chan struct{})
	go func() {
		enqueueWG.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("enqueue notifications timed out under queue pressure")
	}

	for i := 0; i < total; i++ {
		select {
		case <-receivedCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("expected %d notifications, got %d", total, i)
		}
	}
}

func TestGatewayRPCClientReadLoopFailsFastOnNotificationBackpressure(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	client := &GatewayRPCClient{
		closed:                     make(chan struct{}),
		pending:                    make(map[string]chan gatewayRPCResponse),
		notifications:              make(chan gatewayRPCNotification),
		notificationQueue:          make(chan gatewayRPCNotification, 1),
		notificationEnqueueTimeout: 50 * time.Millisecond,
	}
	client.startNotificationDispatcher()
	t.Cleanup(func() { _ = client.Close() })

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		client.readLoop(clientConn)
	}()
	encoder := json.NewEncoder(serverConn)
	if err := encoder.Encode(map[string]any{"method": protocol.MethodGatewayEvent, "params": map[string]any{"idx": 1}}); err != nil {
		t.Fatalf("encode first notification: %v", err)
	}
	if err := encoder.Encode(map[string]any{"method": protocol.MethodGatewayEvent, "params": map[string]any{"idx": 2}}); err != nil {
		t.Fatalf("encode second notification: %v", err)
	}
	if err := encoder.Encode(map[string]any{"method": protocol.MethodGatewayEvent, "params": map[string]any{"idx": 3}}); err != nil {
		t.Fatalf("encode third notification: %v", err)
	}

	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatalf("expected readLoop to fail-fast on sustained notification backpressure")
	}
}

func TestGatewayRPCClientEnqueueNotificationUnblocksOnClose(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		closed:                     make(chan struct{}),
		pending:                    make(map[string]chan gatewayRPCResponse),
		notifications:              make(chan gatewayRPCNotification),
		notificationQueue:          make(chan gatewayRPCNotification, 1),
		notificationEnqueueTimeout: time.Second,
	}
	client.startNotificationDispatcher()

	// 首条通知占满队列，第二条通知会阻塞在 enqueue，关闭客户端后应立即退出。
	client.notificationQueue <- gatewayRPCNotification{Method: protocol.MethodGatewayEvent}

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.enqueueNotification(gatewayRPCNotification{Method: protocol.MethodGatewayEvent})
	}()

	time.Sleep(20 * time.Millisecond)
	_ = client.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("enqueueNotification should unblock when client closes")
	}
}

func TestGatewayRPCClientWriteRequestFailure(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan gatewayRPCNotification, 1),
		notificationQueue: make(chan gatewayRPCNotification, 1),
	}
	conn := &stubConn{writeErr: errors.New("write failed")}
	err := client.writeRequest(conn, protocol.JSONRPCRequest{JSONRPC: protocol.JSONRPCVersion, ID: json.RawMessage(`\"id\"`), Method: "m"})
	if err == nil || !strings.Contains(err.Error(), "write rpc request failed") {
		t.Fatalf("expected write request error, got %v", err)
	}
}

func TestGatewayRPCClientDecodeResponseSuccessAndRetryableNetError(t *testing.T) {
	t.Parallel()

	response, err := decodeGatewayRPCResponse(map[string]json.RawMessage{
		"id":     json.RawMessage(`"id"`),
		"result": json.RawMessage(`{"ok":true}`),
	})
	if err != nil || !bytes.Contains(response.Result, []byte(`ok`)) {
		t.Fatalf("decodeGatewayRPCResponse success mismatch: (%#v, %v)", response, err)
	}

	netErr := &net.DNSError{IsTimeout: true}
	if !isRetryableGatewayCallError(netErr) {
		t.Fatalf("net timeout error should be retryable")
	}
}
