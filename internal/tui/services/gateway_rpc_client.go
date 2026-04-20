package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gatewayauth "neo-code/internal/gateway/auth"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
)

const (
	defaultGatewayRPCRequestTimeout          = 8 * time.Second
	defaultGatewayRPCRetryCount              = 1
	defaultGatewayNotificationBuffer         = 64
	defaultGatewayNotificationQueue          = 256
	defaultGatewayNotificationEnqueueTimeout = 3 * time.Second
)

// GatewayRPCClientOptions 描述网关 JSON-RPC 客户端的初始化参数。
type GatewayRPCClientOptions struct {
	ListenAddress        string
	TokenFile            string
	RequestTimeout       time.Duration
	RetryCount           int
	Dial                 func(address string) (net.Conn, error)
	ResolveListenAddress func(override string) (string, error)
}

// GatewayRPCCallOptions 描述单次 RPC 调用的覆盖参数。
type GatewayRPCCallOptions struct {
	Timeout time.Duration
	Retries int
}

// GatewayRPCError 描述网关返回的结构化 RPC 错误。
type GatewayRPCError struct {
	Method      string
	Code        int
	GatewayCode string
	Message     string
}

func (e *GatewayRPCError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.GatewayCode) != "" {
		return fmt.Sprintf("gateway rpc %s failed (%s): %s", e.Method, e.GatewayCode, e.Message)
	}
	return fmt.Sprintf("gateway rpc %s failed: %s", e.Method, e.Message)
}

type gatewayRPCTransportError struct {
	Method string
	Err    error
}

func (e *gatewayRPCTransportError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Method) == "" {
		return fmt.Sprintf("gateway rpc transport error: %v", e.Err)
	}
	return fmt.Sprintf("gateway rpc %s transport error: %v", e.Method, e.Err)
}

func (e *gatewayRPCTransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type gatewayRPCNotification struct {
	Method string
	Params json.RawMessage
}

type gatewayRPCResponse struct {
	ID           string
	Result       json.RawMessage
	RPCError     *protocol.JSONRPCError
	TransportErr error
}

// GatewayRPCClient 维护与 Gateway 的长连接、请求关联与通知分发。
type GatewayRPCClient struct {
	listenAddress  string
	token          string
	requestTimeout time.Duration
	retryCount     int
	dialFn         func(address string) (net.Conn, error)

	closeOnce sync.Once
	closed    chan struct{}

	writeMu sync.Mutex
	stateMu sync.Mutex
	conn    net.Conn
	pending map[string]chan gatewayRPCResponse

	notifications              chan gatewayRPCNotification
	notificationQueue          chan gatewayRPCNotification
	notificationEnqueueTimeout time.Duration
	notificationWG             sync.WaitGroup
	notificationStart          sync.Once
	sequence                   uint64
}

// NewGatewayRPCClient 创建网关 RPC 客户端，并在启动时静默读取认证 Token。
func NewGatewayRPCClient(options GatewayRPCClientOptions) (*GatewayRPCClient, error) {
	resolveListenAddressFn := options.ResolveListenAddress
	if resolveListenAddressFn == nil {
		resolveListenAddressFn = transport.ResolveListenAddress
	}
	listenAddress, err := resolveListenAddressFn(strings.TrimSpace(options.ListenAddress))
	if err != nil {
		return nil, fmt.Errorf("gateway rpc client: resolve listen address: %w", err)
	}

	token, err := loadGatewayAuthToken(options.TokenFile)
	if err != nil {
		return nil, err
	}

	requestTimeout := options.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultGatewayRPCRequestTimeout
	}

	retryCount := options.RetryCount
	if retryCount <= 0 {
		retryCount = defaultGatewayRPCRetryCount
	}

	dialFn := options.Dial
	if dialFn == nil {
		dialFn = transport.Dial
	}

	return &GatewayRPCClient{
		listenAddress:              listenAddress,
		token:                      token,
		requestTimeout:             requestTimeout,
		retryCount:                 retryCount,
		dialFn:                     dialFn,
		closed:                     make(chan struct{}),
		pending:                    make(map[string]chan gatewayRPCResponse),
		notifications:              make(chan gatewayRPCNotification, defaultGatewayNotificationBuffer),
		notificationQueue:          make(chan gatewayRPCNotification, defaultGatewayNotificationQueue),
		notificationEnqueueTimeout: defaultGatewayNotificationEnqueueTimeout,
	}, nil
}

// Notifications 返回网关 JSON-RPC 通知流。
func (c *GatewayRPCClient) Notifications() <-chan gatewayRPCNotification {
	return c.notifications
}

// Authenticate 显式调用 gateway.authenticate，建立连接级认证状态。
func (c *GatewayRPCClient) Authenticate(ctx context.Context) error {
	var frame map[string]any
	err := c.CallWithOptions(
		ctx,
		protocol.MethodGatewayAuthenticate,
		protocol.AuthenticateParams{Token: c.token},
		&frame,
		GatewayRPCCallOptions{
			Timeout: c.requestTimeout,
			Retries: c.retryCount,
		},
	)
	if err != nil {
		return err
	}
	return nil
}

// Call 按默认超时与重试策略发起一次 JSON-RPC 调用。
func (c *GatewayRPCClient) Call(ctx context.Context, method string, params any, result any) error {
	return c.CallWithOptions(ctx, method, params, result, GatewayRPCCallOptions{
		Timeout: c.requestTimeout,
		Retries: c.retryCount,
	})
}

// CallWithOptions 发起一次可覆盖超时与重试策略的 JSON-RPC 调用。
func (c *GatewayRPCClient) CallWithOptions(
	ctx context.Context,
	method string,
	params any,
	result any,
	options GatewayRPCCallOptions,
) error {
	method = strings.TrimSpace(method)
	if method == "" {
		return errors.New("gateway rpc client: method is empty")
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = c.requestTimeout
	}
	retries := options.Retries
	if retries < 0 {
		retries = c.retryCount
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		lastErr = c.callOnce(ctx, method, params, result, timeout)
		if lastErr == nil {
			return nil
		}
		if !isRetryableGatewayCallError(lastErr) || attempt == retries {
			return lastErr
		}
		c.resetConnection()
	}
	return lastErr
}

// Close 关闭客户端连接并结束内部通知流。
func (c *GatewayRPCClient) Close() error {
	var firstErr error
	c.closeOnce.Do(func() {
		close(c.closed)
		firstErr = c.forceCloseWithError(errors.New("gateway rpc client closed"))
		c.notificationWG.Wait()
		close(c.notifications)
	})
	return firstErr
}

func (c *GatewayRPCClient) callOnce(
	ctx context.Context,
	method string,
	params any,
	result any,
	timeout time.Duration,
) error {
	callCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	if cancel != nil {
		defer cancel()
	}
	if err := callCtx.Err(); err != nil {
		return err
	}

	conn, err := c.ensureConnected()
	if err != nil {
		return &gatewayRPCTransportError{Method: method, Err: err}
	}

	requestID := fmt.Sprintf("tui-%d", atomic.AddUint64(&c.sequence, 1))
	idRaw, err := marshalJSONRawMessage(requestID)
	if err != nil {
		return fmt.Errorf("gateway rpc client: encode request id: %w", err)
	}

	request := protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idRaw,
		Method:  method,
	}
	if params != nil {
		paramsRaw, marshalErr := marshalJSONRawMessage(params)
		if marshalErr != nil {
			return fmt.Errorf("gateway rpc client: encode request params: %w", marshalErr)
		}
		request.Params = paramsRaw
	}

	responseCh := make(chan gatewayRPCResponse, 1)
	if !c.registerPending(requestID, responseCh) {
		return &gatewayRPCTransportError{Method: method, Err: errors.New("gateway rpc client is closed")}
	}
	defer c.unregisterPending(requestID)

	if writeErr := c.writeRequest(conn, request); writeErr != nil {
		return &gatewayRPCTransportError{Method: method, Err: writeErr}
	}

	select {
	case <-c.closed:
		return &gatewayRPCTransportError{Method: method, Err: errors.New("gateway rpc client is closed")}
	case <-callCtx.Done():
		return callCtx.Err()
	case response := <-responseCh:
		if response.TransportErr != nil {
			return &gatewayRPCTransportError{Method: method, Err: response.TransportErr}
		}
		if response.RPCError != nil {
			return mapGatewayRPCError(method, response.RPCError)
		}
		if result == nil {
			return nil
		}
		if len(response.Result) == 0 {
			return &gatewayRPCTransportError{Method: method, Err: errors.New("gateway rpc response result is empty")}
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("gateway rpc client: decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *GatewayRPCClient) writeRequest(conn net.Conn, request protocol.JSONRPCRequest) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		c.resetConnection()
		return fmt.Errorf("write rpc request failed: %w", err)
	}
	return nil
}

func (c *GatewayRPCClient) ensureConnected() (net.Conn, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if c.conn != nil {
		return c.conn, nil
	}
	select {
	case <-c.closed:
		return nil, errors.New("gateway rpc client is closed")
	default:
	}

	conn, err := c.dialFn(c.listenAddress)
	if err != nil {
		return nil, fmt.Errorf("dial gateway %s: %w", c.listenAddress, err)
	}
	c.conn = conn
	c.startNotificationDispatcher()
	go c.readLoop(conn)
	return conn, nil
}

func (c *GatewayRPCClient) readLoop(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	for {
		var envelope map[string]json.RawMessage
		if err := decoder.Decode(&envelope); err != nil {
			_ = c.forceCloseWithError(err)
			return
		}

		if methodRaw, hasMethod := envelope["method"]; hasMethod {
			method := decodeRawJSONString(methodRaw)
			if strings.TrimSpace(method) == "" {
				continue
			}
			notification := gatewayRPCNotification{
				Method: method,
			}
			if paramsRaw, hasParams := envelope["params"]; hasParams {
				notification.Params = cloneJSONRawMessage(paramsRaw)
			}
			if !c.enqueueNotification(notification) {
				return
			}
			continue
		}

		if idRaw, hasID := envelope["id"]; hasID {
			response, err := decodeGatewayRPCResponse(envelope)
			if err != nil {
				continue
			}
			response.ID = normalizeJSONRPCResponseID(idRaw)
			c.dispatchResponse(response)
		}
	}
}

// startNotificationDispatcher 启动通知转发协程，配合 enqueue 超时保护避免 readLoop 长时间背压阻塞。
func (c *GatewayRPCClient) startNotificationDispatcher() {
	c.notificationStart.Do(func() {
		c.notificationWG.Add(1)
		go func() {
			defer c.notificationWG.Done()
			for {
				select {
				case <-c.closed:
					return
				case notification, ok := <-c.notificationQueue:
					if !ok {
						return
					}
					select {
					case <-c.closed:
						return
					case c.notifications <- notification:
					}
				}
			}
		}()
	})
}

// enqueueNotification 投递通知到内部队列；若背压持续超时则主动断开连接，避免 readLoop 无限阻塞。
func (c *GatewayRPCClient) enqueueNotification(notification gatewayRPCNotification) bool {
	enqueueTimeout := c.notificationEnqueueTimeout
	if enqueueTimeout <= 0 {
		enqueueTimeout = defaultGatewayNotificationEnqueueTimeout
	}
	timer := time.NewTimer(enqueueTimeout)
	defer timer.Stop()

	select {
	case <-c.closed:
		return false
	case c.notificationQueue <- notification:
		return true
	case <-timer.C:
		err := fmt.Errorf("gateway rpc client: notification queue blocked for %s", enqueueTimeout)
		log.Printf("warning: gateway rpc client force close due to notification backpressure method=%s err=%v", notification.Method, err)
		_ = c.forceCloseWithError(err)
		return false
	}
}

func (c *GatewayRPCClient) dispatchResponse(response gatewayRPCResponse) {
	if strings.TrimSpace(response.ID) == "" {
		return
	}
	c.stateMu.Lock()
	ch := c.pending[response.ID]
	delete(c.pending, response.ID)
	c.stateMu.Unlock()
	if ch == nil {
		return
	}
	ch <- response
}

func (c *GatewayRPCClient) registerPending(requestID string, ch chan gatewayRPCResponse) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	select {
	case <-c.closed:
		return false
	default:
	}
	c.pending[requestID] = ch
	return true
}

func (c *GatewayRPCClient) unregisterPending(requestID string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	delete(c.pending, requestID)
}

func (c *GatewayRPCClient) resetConnection() {
	c.stateMu.Lock()
	conn := c.conn
	c.conn = nil
	c.stateMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (c *GatewayRPCClient) forceCloseWithError(cause error) error {
	c.stateMu.Lock()
	conn := c.conn
	c.conn = nil
	pending := c.pending
	c.pending = make(map[string]chan gatewayRPCResponse)
	c.stateMu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}

	transportErr := cause
	if transportErr == nil {
		transportErr = errors.New("gateway rpc connection closed")
	}
	for _, ch := range pending {
		ch <- gatewayRPCResponse{TransportErr: transportErr}
	}
	return nil
}

func mapGatewayRPCError(method string, rpcError *protocol.JSONRPCError) error {
	if rpcError == nil {
		return &GatewayRPCError{
			Method:      method,
			Code:        protocol.JSONRPCCodeInternalError,
			GatewayCode: protocol.GatewayCodeInternalError,
			Message:     "gateway returned empty rpc error",
		}
	}

	message := strings.TrimSpace(rpcError.Message)
	if message == "" {
		message = "gateway returned empty rpc error message"
	}
	return &GatewayRPCError{
		Method:      method,
		Code:        rpcError.Code,
		GatewayCode: strings.TrimSpace(protocol.GatewayCodeFromJSONRPCError(rpcError)),
		Message:     message,
	}
}

func decodeGatewayRPCResponse(envelope map[string]json.RawMessage) (gatewayRPCResponse, error) {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return gatewayRPCResponse{}, err
	}
	var response protocol.JSONRPCResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return gatewayRPCResponse{}, err
	}
	return gatewayRPCResponse{
		Result:   cloneJSONRawMessage(response.Result),
		RPCError: response.Error,
	}, nil
}

func normalizeJSONRPCResponseID(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	return trimmed
}

func decodeRawJSONString(raw json.RawMessage) string {
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// marshalJSONRawMessage 将任意值编码为独立的 RawMessage，避免复用外部可变切片。
func marshalJSONRawMessage(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return cloneJSONRawMessage(raw), nil
}

// cloneJSONRawMessage 复制 RawMessage 底层字节，避免跨协程共享同一底层数组。
func cloneJSONRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return json.RawMessage(cloned)
}

func isRetryableGatewayCallError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var transportErr *gatewayRPCTransportError
	if errors.As(err, &transportErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var rpcErr *GatewayRPCError
	if errors.As(err, &rpcErr) {
		return strings.EqualFold(strings.TrimSpace(rpcErr.GatewayCode), protocol.GatewayCodeTimeout)
	}
	return false
}

// loadGatewayAuthToken 读取 Gateway 静默认证 Token。
func loadGatewayAuthToken(tokenFile string) (string, error) {
	token, err := gatewayauth.LoadTokenFromFile(strings.TrimSpace(tokenFile))
	if err != nil {
		return "", fmt.Errorf("gateway rpc client: load auth token: %w", err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New("gateway rpc client: auth token is empty")
	}
	return token, nil
}
