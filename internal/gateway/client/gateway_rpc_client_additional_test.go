package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/gateway"
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
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
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
	client.enqueueNotification(Notification{Method: protocol.MethodGatewayEvent})

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
				req := readRPCRequestOrFail(dec)
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
		notifications:     make(chan Notification, 4),
		notificationQueue: make(chan Notification, 4),
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
			notifications:     make(chan Notification, 4),
			notificationQueue: make(chan Notification, 4),
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
		notifications:     make(chan Notification, 4),
		notificationQueue: make(chan Notification, 4),
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
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
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
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
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
			client.enqueueNotification(Notification{
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
		notifications:              make(chan Notification),
		notificationQueue:          make(chan Notification, 1),
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
		notifications:              make(chan Notification),
		notificationQueue:          make(chan Notification, 1),
		notificationEnqueueTimeout: time.Second,
	}
	client.startNotificationDispatcher()

	// 首条通知占满队列，第二条通知会阻塞在 enqueue，关闭客户端后应立即退出。
	client.notificationQueue <- Notification{Method: protocol.MethodGatewayEvent}

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.enqueueNotification(Notification{Method: protocol.MethodGatewayEvent})
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
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
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

func TestGatewayRPCClientAutoSpawnWhenGatewayUnavailable(t *testing.T) {
	t.Parallel()

	tokenFile, _ := createTestAuthTokenFile(t)

	var dialCount int32
	var autoSpawnCount int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		AutoSpawnGateway: func(
			_ context.Context,
			listenAddress string,
			_ func(address string) (net.Conn, error),
		) (*exec.Cmd, error) {
			if listenAddress != "test://gateway" {
				t.Fatalf("auto spawn listen address = %q", listenAddress)
			}
			atomic.AddInt32(&autoSpawnCount, 1)
			return nil, nil
		},
		Dial: func(_ string) (net.Conn, error) {
			attempt := atomic.AddInt32(&dialCount, 1)
			if attempt == 1 {
				return nil, errors.New("connect failed: no such file or directory")
			}
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				request := readRPCRequestOrFail(decoder)
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionPing,
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var frame gateway.MessageFrame
	if err := client.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayPing,
		map[string]any{},
		&frame,
		GatewayRPCCallOptions{Timeout: time.Second, Retries: 0},
	); err != nil {
		t.Fatalf("CallWithOptions() error = %v", err)
	}
	if atomic.LoadInt32(&autoSpawnCount) != 1 {
		t.Fatalf("auto spawn count = %d, want 1", atomic.LoadInt32(&autoSpawnCount))
	}
	if atomic.LoadInt32(&dialCount) != 2 {
		t.Fatalf("dial count = %d, want 2", atomic.LoadInt32(&dialCount))
	}
}

func TestGatewayRPCClientDoesNotAutoSpawnOnNonUnavailableDialError(t *testing.T) {
	t.Parallel()

	tokenFile, _ := createTestAuthTokenFile(t)
	var autoSpawnCount int32

	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		AutoSpawnGateway: func(
			_ context.Context,
			_ string,
			_ func(address string) (net.Conn, error),
		) (*exec.Cmd, error) {
			atomic.AddInt32(&autoSpawnCount, 1)
			return nil, nil
		},
		Dial: func(_ string) (net.Conn, error) {
			return nil, errors.New("permission denied")
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	callErr := client.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayPing,
		map[string]any{},
		nil,
		GatewayRPCCallOptions{Timeout: time.Second, Retries: 0},
	)
	if callErr == nil {
		t.Fatalf("expected call error")
	}
	if atomic.LoadInt32(&autoSpawnCount) != 0 {
		t.Fatalf("auto spawn count = %d, want 0", atomic.LoadInt32(&autoSpawnCount))
	}
}

func TestIsGatewayUnavailableDialError(t *testing.T) {
	t.Parallel()

	if !isGatewayUnavailableDialError(os.ErrNotExist) {
		t.Fatalf("os.ErrNotExist should be treated as gateway unavailable")
	}
	if !isGatewayUnavailableDialError(errors.New("connect: connection refused")) {
		t.Fatalf("connection refused should be treated as gateway unavailable")
	}
	if !isGatewayUnavailableDialError(errors.New("The system cannot find the file specified")) {
		t.Fatalf("windows pipe not found text should be treated as gateway unavailable")
	}
	if isGatewayUnavailableDialError(errors.New("permission denied")) {
		t.Fatalf("permission denied should not be treated as gateway unavailable")
	}
}

func TestOpenGatewayAutoSpawnLogFileRotatesPreviousLog(t *testing.T) {
	t.Parallel()

	logPath := filepath.Join(t.TempDir(), "gateway_auto.log")
	if err := os.WriteFile(logPath, []byte("previous-run-log"), 0o600); err != nil {
		t.Fatalf("write previous log: %v", err)
	}
	if err := os.WriteFile(logPath+".bak", []byte("old-backup"), 0o600); err != nil {
		t.Fatalf("write old backup log: %v", err)
	}

	logFile, err := openGatewayAutoSpawnLogFile(logPath)
	if err != nil {
		t.Fatalf("openGatewayAutoSpawnLogFile() error = %v", err)
	}
	if _, err := logFile.WriteString("current-run-log"); err != nil {
		_ = logFile.Close()
		t.Fatalf("write current log: %v", err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("close current log: %v", err)
	}

	backupContent, err := os.ReadFile(logPath + ".bak")
	if err != nil {
		t.Fatalf("read backup log: %v", err)
	}
	if string(backupContent) != "previous-run-log" {
		t.Fatalf("backup log content = %q, want previous-run-log", string(backupContent))
	}

	currentContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if string(currentContent) != "current-run-log" {
		t.Fatalf("current log content = %q, want current-run-log", string(currentContent))
	}
}

func TestGatewayRPCClientCloseStopsSpawnedGatewayProcess(t *testing.T) {
	spawnedCmd := startLongRunningProcessForGatewayRPCTest(t)

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
		spawnedCmd:        spawnedCmd,
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if spawnedCmd.ProcessState != nil {
		t.Fatalf("expected spawned process to remain alive after client close in shared gateway mode")
	}
}

func TestGatewayRPCClientWatchSpawnedGatewayProcessResetsAutoSpawnAttempt(t *testing.T) {
	spawnedCmd := startLongRunningProcessForGatewayRPCTest(t)
	done := make(chan struct{})

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
		autoSpawnAttempt:  true,
		spawnedCmd:        spawnedCmd,
		spawnedCmdDone:    done,
	}

	go client.watchSpawnedGatewayProcess(spawnedCmd, done)
	if err := spawnedCmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Kill() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected spawned process monitor to finish")
	}

	if client.autoSpawnAttempt {
		t.Fatal("expected autoSpawnAttempt to be reset after spawned process exit")
	}
	if client.spawnedCmd != nil {
		t.Fatal("expected spawnedCmd to be cleared after spawned process exit")
	}
	if client.spawnedCmdDone != nil {
		t.Fatal("expected spawnedCmdDone to be cleared after spawned process exit")
	}
}

func TestGatewayRPCClientResetConnectionClearsAutoSpawnAttempt(t *testing.T) {
	t.Parallel()

	client := &GatewayRPCClient{
		closed:            make(chan struct{}),
		pending:           make(map[string]chan gatewayRPCResponse),
		notifications:     make(chan Notification, 1),
		notificationQueue: make(chan Notification, 1),
		autoSpawnAttempt:  true,
	}

	client.resetConnection()
	if client.autoSpawnAttempt {
		t.Fatal("expected resetConnection to clear autoSpawnAttempt")
	}
}

func TestGatewayAutoSpawnHelpers(t *testing.T) {
	t.Run("wait ready with empty address", func(t *testing.T) {
		err := waitGatewayReadyAfterAutoSpawn(context.Background(), "   ", func(string) (net.Conn, error) {
			return nil, errors.New("should not dial")
		})
		if err == nil || !strings.Contains(err.Error(), "listen address is empty") {
			t.Fatalf("expected empty listen address error, got %v", err)
		}
	})

	t.Run("wait ready with context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := waitGatewayReadyAfterAutoSpawn(ctx, "ipc://gateway", func(string) (net.Conn, error) {
			return nil, os.ErrNotExist
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	t.Run("wait ready with non unavailable error", func(t *testing.T) {
		err := waitGatewayReadyAfterAutoSpawn(context.Background(), "ipc://gateway", func(string) (net.Conn, error) {
			return nil, errors.New("permission denied")
		})
		if err == nil || !strings.Contains(err.Error(), "probe gateway readiness") {
			t.Fatalf("expected probe error, got %v", err)
		}
	})

	t.Run("wait ready succeeds after retry", func(t *testing.T) {
		var calls int32
		err := waitGatewayReadyAfterAutoSpawn(context.Background(), "ipc://gateway", func(string) (net.Conn, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				return nil, os.ErrNotExist
			}
			c1, c2 := net.Pipe()
			go func() { _ = c2.Close() }()
			return c1, nil
		})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if atomic.LoadInt32(&calls) < 2 {
			t.Fatalf("expected at least 2 dials, got %d", calls)
		}
	})

	t.Run("default auto spawn returns error when gateway not ready", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		cmd, err := defaultAutoSpawnGateway(ctx, "ipc://gateway", func(string) (net.Conn, error) {
			return nil, os.ErrNotExist
		})
		if cmd != nil {
			t.Fatalf("expected nil cmd on failure, got %#v", cmd)
		}
		if err == nil {
			t.Fatalf("expected defaultAutoSpawnGateway() error")
		}
	})
}

func TestGatewayAutoSpawnOutputFallbackAndPath(t *testing.T) {
	t.Run("resolve log path", func(t *testing.T) {
		path, err := resolveGatewayAutoSpawnLogPath()
		if err != nil {
			t.Fatalf("resolveGatewayAutoSpawnLogPath() error = %v", err)
		}
		if !strings.HasSuffix(filepath.Clean(path), filepath.Clean(filepath.FromSlash(defaultGatewayAutoSpawnLogRelativePath))) {
			t.Fatalf("log path = %q", path)
		}
	})

	t.Run("fallback to devnull when log path cannot be created", func(t *testing.T) {
		tempDir := t.TempDir()
		homeFile := filepath.Join(tempDir, "home-file")
		if err := os.WriteFile(homeFile, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		t.Setenv("HOME", homeFile)

		output, err := openGatewayAutoSpawnOutput()
		if err != nil {
			t.Fatalf("openGatewayAutoSpawnOutput() error = %v", err)
		}
		if output == nil {
			t.Fatalf("openGatewayAutoSpawnOutput() should return file")
		}
		_ = output.Close()
	})
}

func TestGatewaySpawnedProcessStopAndWaitHelpers(t *testing.T) {
	t.Run("nil command", func(t *testing.T) {
		if err := stopSpawnedGatewayProcess(nil, nil); err != nil {
			t.Fatalf("stopSpawnedGatewayProcess(nil) error = %v", err)
		}
	})

	t.Run("already exited process", func(t *testing.T) {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", "exit 0")
		} else {
			cmd = exec.Command("sh", "-c", "exit 0")
		}
		if err := cmd.Start(); err != nil {
			t.Skipf("start process failed: %v", err)
		}
		_ = cmd.Wait()
		if err := stopSpawnedGatewayProcess(cmd, nil); err != nil {
			t.Fatalf("stopSpawnedGatewayProcess(exited) error = %v", err)
		}
	})

	t.Run("wait helper with done signal", func(t *testing.T) {
		done := make(chan struct{})
		waitSpawnedGatewayProcess(done, &exec.Cmd{})
		close(done)
	})
}

func TestGatewayRPCClientEnsureConnectedAutoSpawnBranches(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	t.Run("auto spawn function returns error", func(t *testing.T) {
		client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
			ListenAddress: "test://gateway",
			TokenFile:     tokenFile,
			Dial: func(string) (net.Conn, error) {
				return nil, os.ErrNotExist
			},
			AutoSpawnGateway: func(context.Context, string, func(string) (net.Conn, error)) (*exec.Cmd, error) {
				return nil, errors.New("spawn failed")
			},
		})
		if err != nil {
			t.Fatalf("NewGatewayRPCClient() error = %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.ensureConnected(context.Background())
		if err == nil || !strings.Contains(err.Error(), "auto-spawn gateway failed") {
			t.Fatalf("expected auto-spawn failure error, got %v", err)
		}
	})

	t.Run("closed while auto spawn in progress", func(t *testing.T) {
		var client *GatewayRPCClient
		var err error
		client, err = NewGatewayRPCClient(GatewayRPCClientOptions{
			ListenAddress: "test://gateway",
			TokenFile:     tokenFile,
			Dial: func(string) (net.Conn, error) {
				return nil, os.ErrNotExist
			},
			AutoSpawnGateway: func(_ context.Context, _ string, _ func(string) (net.Conn, error)) (*exec.Cmd, error) {
				close(client.closed)
				return startLongRunningProcessForGatewayRPCTest(t), nil
			},
		})
		if err != nil {
			t.Fatalf("NewGatewayRPCClient() error = %v", err)
		}

		_, err = client.ensureConnected(context.Background())
		if err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("expected closed error, got %v", err)
		}
	})

	t.Run("replace previous spawned process reference without stopping process", func(t *testing.T) {
		prev := startLongRunningProcessForGatewayRPCTest(t)
		client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
			ListenAddress: "test://gateway",
			TokenFile:     tokenFile,
		})
		if err != nil {
			t.Fatalf("NewGatewayRPCClient() error = %v", err)
		}
		client.spawnedCmd = prev
		client.spawnedCmdDone = nil
		var dialCount int32
		client.dialFn = func(string) (net.Conn, error) {
			if atomic.AddInt32(&dialCount, 1) == 1 {
				return nil, os.ErrNotExist
			}
			c1, c2 := net.Pipe()
			go func() { _ = c2.Close() }()
			return c1, nil
		}
		client.autoSpawnFn = func(_ context.Context, _ string, _ func(string) (net.Conn, error)) (*exec.Cmd, error) {
			return startLongRunningProcessForGatewayRPCTest(t), nil
		}
		t.Cleanup(func() { _ = client.Close() })

		conn, err := client.ensureConnected(context.Background())
		if err != nil || conn == nil {
			t.Fatalf("ensureConnected() = (%v, %v)", conn, err)
		}

		if prev.ProcessState != nil {
			t.Fatalf("expected previous process to keep running without ownership evidence")
		}
	})

	t.Run("dial still unavailable after auto spawn", func(t *testing.T) {
		client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
			ListenAddress: "test://gateway",
			TokenFile:     tokenFile,
			Dial: func(string) (net.Conn, error) {
				return nil, os.ErrNotExist
			},
			AutoSpawnGateway: func(context.Context, string, func(string) (net.Conn, error)) (*exec.Cmd, error) {
				return nil, nil
			},
		})
		if err != nil {
			t.Fatalf("NewGatewayRPCClient() error = %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		_, err = client.ensureConnected(context.Background())
		if err == nil || !strings.Contains(err.Error(), "after auto-spawn") {
			t.Fatalf("expected dial after auto-spawn error, got %v", err)
		}
	})
}

func TestGatewayRPCClientAuthenticateLoadsTokenAfterGatewayAutoSpawn(t *testing.T) {
	t.Parallel()

	tokenFile := filepath.Join(t.TempDir(), "auth.json")
	var dialCount int32
	serverErrCh := make(chan error, 1)
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		AutoSpawnGateway: func(_ context.Context, _ string, _ func(address string) (net.Conn, error)) (*exec.Cmd, error) {
			writeTestAuthTokenFile(t, tokenFile, "auto-spawn-token")
			return nil, nil
		},
		Dial: func(_ string) (net.Conn, error) {
			attempt := atomic.AddInt32(&dialCount, 1)
			if attempt == 1 {
				return nil, os.ErrNotExist
			}

			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)

				request := readRPCRequestOrFail(decoder)
				if request.Method != protocol.MethodGatewayAuthenticate {
					serverErrCh <- fmt.Errorf("authenticate method = %q", request.Method)
					return
				}
				var params protocol.AuthenticateParams
				if err := json.Unmarshal(request.Params, &params); err != nil {
					serverErrCh <- fmt.Errorf("decode authenticate params: %w", err)
					return
				}
				if strings.TrimSpace(params.Token) == "" {
					serverErrCh <- errors.New("expected non-empty authenticate token")
					return
				}

				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionAuthenticate,
				})
				serverErrCh <- nil
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	select {
	case serverErr := <-serverErrCh:
		if serverErr != nil {
			t.Fatalf("gateway rpc server assertion failed: %v", serverErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for gateway rpc server assertions")
	}
	if atomic.LoadInt32(&dialCount) < 2 {
		t.Fatalf("expected auto-spawn retry dial path, got %d", atomic.LoadInt32(&dialCount))
	}
}

func TestWatchSpawnedGatewayProcessNilCommand(t *testing.T) {
	client := &GatewayRPCClient{}
	done := make(chan struct{})
	go client.watchSpawnedGatewayProcess(nil, done)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("watchSpawnedGatewayProcess(nil) should close done")
	}
}

func TestDefaultAutoSpawnGatewaySuccess(t *testing.T) {
	cmd, err := defaultAutoSpawnGateway(context.Background(), "ipc://gateway", func(string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() { _ = c2.Close() }()
		return c1, nil
	})
	if err != nil {
		t.Fatalf("defaultAutoSpawnGateway() error = %v", err)
	}
	if cmd == nil {
		t.Fatalf("expected spawned command")
	}
	if stopErr := stopSpawnedGatewayProcess(cmd, nil); stopErr != nil {
		t.Fatalf("stopSpawnedGatewayProcess() error = %v", stopErr)
	}
}

func TestWaitGatewayReadyAfterAutoSpawnTimeout(t *testing.T) {
	start := time.Now()
	err := waitGatewayReadyAfterAutoSpawn(context.Background(), "ipc://gateway", func(string) (net.Conn, error) {
		return nil, os.ErrNotExist
	})
	if err == nil || !strings.Contains(err.Error(), "gateway not ready within") {
		t.Fatalf("expected not-ready timeout error, got %v", err)
	}
	if time.Since(start) < 2*time.Second {
		t.Fatalf("expected probe retry window to elapse")
	}
}

func TestGatewayAutoSpawnLogErrorBranches(t *testing.T) {
	t.Run("open log file returns rotate error", func(t *testing.T) {
		base := t.TempDir()
		locked := filepath.Join(base, "locked")
		if err := os.MkdirAll(locked, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		logPath := filepath.Join(locked, "gateway_auto.log")
		if err := os.WriteFile(logPath, []byte("old"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		backupPath := logPath + ".bak"
		if err := os.MkdirAll(backupPath, 0o700); err != nil {
			t.Fatalf("MkdirAll backup dir error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(backupPath, "x"), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile backup payload error = %v", err)
		}

		if _, err := openGatewayAutoSpawnLogFile(logPath); err == nil {
			t.Fatalf("expected rotate backup removal error")
		}
	})

	t.Run("open log file returns open error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permission assertions are not reliable on Windows")
		}
		base := t.TempDir()
		readonlyDir := filepath.Join(base, "ro")
		if err := os.MkdirAll(readonlyDir, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(readonlyDir, 0o500); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(readonlyDir, 0o700) })

		logPath := filepath.Join(readonlyDir, "gateway_auto.log")
		if _, err := openGatewayAutoSpawnLogFile(logPath); err == nil {
			t.Fatalf("expected open log file error")
		}
	})

	t.Run("rotate stat error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permission assertions are not reliable on Windows")
		}
		base := t.TempDir()
		locked := filepath.Join(base, "locked")
		if err := os.MkdirAll(locked, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

		err := rotateGatewayAutoSpawnLog(filepath.Join(locked, "gateway_auto.log"))
		if err == nil {
			t.Fatalf("expected rotate stat error")
		}
	})
}

func TestOpenGatewayAutoSpawnLogFileRejectsSymlink(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "target.log")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatalf("write target log: %v", err)
	}

	logPath := filepath.Join(base, "gateway_auto.log")
	if err := os.Symlink(target, logPath); err != nil {
		t.Skipf("symlink is not available: %v", err)
	}

	if _, err := openGatewayAutoSpawnLogFile(logPath); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
}

func TestRotateGatewayAutoSpawnLogRejectsSymlinkBackup(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	logPath := filepath.Join(base, "gateway_auto.log")
	if err := os.WriteFile(logPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	backupReal := filepath.Join(base, "backup-real.log")
	if err := os.WriteFile(backupReal, []byte("backup"), 0o600); err != nil {
		t.Fatalf("write backup real: %v", err)
	}
	if err := os.Symlink(backupReal, logPath+".bak"); err != nil {
		t.Skipf("symlink is not available: %v", err)
	}

	if err := rotateGatewayAutoSpawnLog(logPath); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected backup symlink rejection error, got %v", err)
	}
}

func TestStopSpawnedGatewayProcessKillErrorAndUnavailableNil(t *testing.T) {
	if isGatewayUnavailableDialError(nil) {
		t.Fatalf("nil error should not be treated as gateway unavailable")
	}
}

func startLongRunningProcessForGatewayRPCTest(t *testing.T) *exec.Cmd {
	t.Helper()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 120 127.0.0.1 >NUL")
	} else {
		cmd = exec.Command("sh", "-c", "sleep 120")
	}

	if err := cmd.Start(); err != nil {
		t.Skipf("start long running process failed: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		go func() {
			_ = cmd.Wait()
		}()
	})
	return cmd
}
