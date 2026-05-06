package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	defaultGatewayAuthTokenRetryInterval     = 100 * time.Millisecond
	defaultGatewayAuthTokenRetryAttempts     = 10
	defaultGatewayRPCHeartbeatInterval       = 10 * time.Second
	defaultGatewayRPCHeartbeatTimeout        = 5 * time.Second
	defaultGatewayAutoSpawnProbeInterval     = 200 * time.Millisecond
	defaultGatewayAutoSpawnProbeAttempts     = 15
	defaultGatewayAutoSpawnLogRelativePath   = ".neocode/logs/gateway_auto.log"
	defaultGatewayNotificationBuffer         = 64
	defaultGatewayNotificationQueue          = 256
	defaultGatewayNotificationEnqueueTimeout = 3 * time.Second
)

// DefaultGatewayRPCRetryCount 暴露网关 RPC 客户端默认重试次数，供上层适配器复用一致策略。
const DefaultGatewayRPCRetryCount = defaultGatewayRPCRetryCount

const (
	gatewayAutoSpawnLogDirPerm  = 0o700
	gatewayAutoSpawnLogFilePerm = 0o600
)

type gatewayAutoSpawnFunc func(
	ctx context.Context,
	listenAddress string,
	dialFn func(address string) (net.Conn, error),
) (*exec.Cmd, error)

// GatewayRPCClientOptions 描述网关 JSON-RPC 客户端的初始化参数。
type GatewayRPCClientOptions struct {
	ListenAddress        string
	TokenFile            string
	RequestTimeout       time.Duration
	RetryCount           int
	HeartbeatInterval    time.Duration
	HeartbeatTimeout     time.Duration
	DisableHeartbeatLog  bool
	DisableAutoSpawn     bool
	AutoSpawnGateway     gatewayAutoSpawnFunc
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

type Notification struct {
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
	listenAddress       string
	tokenFile           string
	token               string
	requestTimeout      time.Duration
	retryCount          int
	heartbeatInterval   time.Duration
	heartbeatTimeout    time.Duration
	disableHeartbeatLog bool
	dialFn              func(address string) (net.Conn, error)
	disableAutoSpawn    bool
	autoSpawnFn         gatewayAutoSpawnFunc
	autoSpawnAttempt    bool
	spawnedCmd          *exec.Cmd
	spawnedCmdDone      chan struct{}

	closeOnce sync.Once
	closed    chan struct{}

	writeMu sync.Mutex
	stateMu sync.Mutex
	conn    net.Conn
	pending map[string]chan gatewayRPCResponse

	heartbeatCancel context.CancelFunc
	heartbeatWG     sync.WaitGroup

	notifications              chan Notification
	notificationQueue          chan Notification
	notificationEnqueueTimeout time.Duration
	notificationWG             sync.WaitGroup
	notificationStart          sync.Once
	sequence                   uint64
}

// NewGatewayRPCClient 创建网关 RPC 客户端，并在首次鉴权前按需加载认证 Token。
func NewGatewayRPCClient(options GatewayRPCClientOptions) (*GatewayRPCClient, error) {
	resolveListenAddressFn := options.ResolveListenAddress
	if resolveListenAddressFn == nil {
		resolveListenAddressFn = transport.ResolveListenAddress
	}
	listenAddress, err := resolveListenAddressFn(strings.TrimSpace(options.ListenAddress))
	if err != nil {
		return nil, fmt.Errorf("gateway rpc client: resolve listen address: %w", err)
	}

	requestTimeout := options.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultGatewayRPCRequestTimeout
	}

	retryCount := options.RetryCount
	if retryCount <= 0 {
		retryCount = defaultGatewayRPCRetryCount
	}

	heartbeatInterval := options.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultGatewayRPCHeartbeatInterval
	}

	heartbeatTimeout := options.HeartbeatTimeout
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = defaultGatewayRPCHeartbeatTimeout
	}
	if requestTimeout > 0 && heartbeatTimeout > requestTimeout {
		heartbeatTimeout = requestTimeout
	}

	dialFn := options.Dial
	if dialFn == nil {
		dialFn = transport.Dial
	}

	autoSpawnFn := options.AutoSpawnGateway
	if autoSpawnFn == nil {
		autoSpawnFn = defaultAutoSpawnGateway
	}

	return &GatewayRPCClient{
		listenAddress:              listenAddress,
		tokenFile:                  strings.TrimSpace(options.TokenFile),
		requestTimeout:             requestTimeout,
		retryCount:                 retryCount,
		heartbeatInterval:          heartbeatInterval,
		heartbeatTimeout:           heartbeatTimeout,
		disableHeartbeatLog:        options.DisableHeartbeatLog,
		disableAutoSpawn:           options.DisableAutoSpawn,
		autoSpawnFn:                autoSpawnFn,
		dialFn:                     dialFn,
		closed:                     make(chan struct{}),
		pending:                    make(map[string]chan gatewayRPCResponse),
		notifications:              make(chan Notification, defaultGatewayNotificationBuffer),
		notificationQueue:          make(chan Notification, defaultGatewayNotificationQueue),
		notificationEnqueueTimeout: defaultGatewayNotificationEnqueueTimeout,
	}, nil
}

// Notifications 返回网关 JSON-RPC 通知流。
func (c *GatewayRPCClient) Notifications() <-chan Notification {
	return c.notifications
}

// Authenticate 显式调用 gateway.authenticate，建立连接级认证状态。
func (c *GatewayRPCClient) Authenticate(ctx context.Context) error {
	if _, err := c.ensureConnected(ctx); err != nil {
		return err
	}
	token, err := c.ensureGatewayAuthToken(ctx)
	if err != nil {
		return err
	}

	var frame map[string]any
	err = c.CallWithOptions(
		ctx,
		protocol.MethodGatewayAuthenticate,
		protocol.AuthenticateParams{Token: token},
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
		// 当连接级鉴权状态丢失时，先尝试透明重鉴权再重放本次调用。
		if isUnauthorizedGatewayCallError(lastErr) && !isAuthenticateGatewayMethod(method) {
			if reauthErr := c.reauthenticate(ctx, timeout); reauthErr == nil {
				lastErr = c.callOnce(ctx, method, params, result, timeout)
				if lastErr == nil {
					return nil
				}
			}
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
		c.detachSpawnedCmd()
		c.heartbeatWG.Wait()
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

	conn, err := c.ensureConnected(callCtx)
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

func (c *GatewayRPCClient) ensureConnected(ctx context.Context) (net.Conn, error) {
	autoSpawnTriggered := false
	for {
		c.stateMu.Lock()
		if c.conn != nil {
			conn := c.conn
			c.stateMu.Unlock()
			return conn, nil
		}
		select {
		case <-c.closed:
			c.stateMu.Unlock()
			return nil, errors.New("gateway rpc client is closed")
		default:
		}

		conn, err := c.dialFn(c.listenAddress)
		if err == nil {
			heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
			c.conn = conn
			c.heartbeatCancel = heartbeatCancel
			c.heartbeatWG.Add(1)
			c.startNotificationDispatcher()
			c.stateMu.Unlock()
			go c.readLoop(conn)
			c.startHeartbeat(heartbeatCtx, conn)
			return conn, nil
		}

		canAutoSpawn := !autoSpawnTriggered &&
			!c.disableAutoSpawn &&
			!c.autoSpawnAttempt &&
			c.autoSpawnFn != nil &&
			isGatewayUnavailableDialError(err)
		if canAutoSpawn {
			c.autoSpawnAttempt = true
			autoSpawnFn := c.autoSpawnFn
			listenAddress := c.listenAddress
			dialFn := c.dialFn
			c.stateMu.Unlock()
			spawnedCmd, spawnErr := autoSpawnFn(ctx, listenAddress, dialFn)
			if spawnErr != nil {
				c.stateMu.Lock()
				c.autoSpawnAttempt = false
				c.stateMu.Unlock()
				return nil, fmt.Errorf("dial gateway %s: %w; auto-spawn gateway failed: %w", listenAddress, err, spawnErr)
			}
			c.stateMu.Lock()
			select {
			case <-c.closed:
				c.autoSpawnAttempt = false
				c.stateMu.Unlock()
				_ = stopSpawnedGatewayProcess(spawnedCmd, nil)
				return nil, errors.New("gateway rpc client is closed")
			default:
			}
			if spawnedCmd != nil {
				done := make(chan struct{})
				c.spawnedCmd = spawnedCmd
				c.spawnedCmdDone = done
				c.autoSpawnAttempt = true
				go c.watchSpawnedGatewayProcess(spawnedCmd, done)
				c.stateMu.Unlock()
			} else {
				c.autoSpawnAttempt = false
				c.stateMu.Unlock()
			}
			autoSpawnTriggered = true
			continue
		}

		c.stateMu.Unlock()
		if autoSpawnTriggered {
			return nil, fmt.Errorf("dial gateway %s after auto-spawn: %w", c.listenAddress, err)
		}
		return nil, fmt.Errorf("dial gateway %s: %w", c.listenAddress, err)
	}
}

func (c *GatewayRPCClient) detachSpawnedCmd() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.spawnedCmd = nil
	c.spawnedCmdDone = nil
	c.autoSpawnAttempt = false
}

// watchSpawnedGatewayProcess 监听自动拉起的网关子进程退出，并在退出后复位自动拉起状态。
func (c *GatewayRPCClient) watchSpawnedGatewayProcess(cmd *exec.Cmd, done chan struct{}) {
	if cmd == nil {
		close(done)
		return
	}
	_ = cmd.Wait()

	c.stateMu.Lock()
	if c.spawnedCmd == cmd {
		c.spawnedCmd = nil
		c.spawnedCmdDone = nil
		c.autoSpawnAttempt = false
	}
	c.stateMu.Unlock()
	close(done)
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
			notification := Notification{
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
func (c *GatewayRPCClient) enqueueNotification(notification Notification) bool {
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
	heartbeatCancel := c.heartbeatCancel
	c.heartbeatCancel = nil
	c.autoSpawnAttempt = false
	c.stateMu.Unlock()
	if heartbeatCancel != nil {
		heartbeatCancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

func (c *GatewayRPCClient) forceCloseWithError(cause error) error {
	c.stateMu.Lock()
	conn := c.conn
	c.conn = nil
	heartbeatCancel := c.heartbeatCancel
	c.heartbeatCancel = nil
	c.autoSpawnAttempt = false
	pending := c.pending
	c.pending = make(map[string]chan gatewayRPCResponse)
	c.stateMu.Unlock()

	if heartbeatCancel != nil {
		heartbeatCancel()
	}

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

// startHeartbeat 启动连接级保活协程，定期发送 gateway.ping，避免网关在空闲窗口触发读超时断开。
func (c *GatewayRPCClient) startHeartbeat(ctx context.Context, conn net.Conn) {
	interval := c.heartbeatInterval
	if interval <= 0 {
		interval = defaultGatewayRPCHeartbeatInterval
	}
	timeout := c.heartbeatTimeout
	if timeout <= 0 {
		timeout = defaultGatewayRPCHeartbeatTimeout
	}

	go func() {
		defer c.heartbeatWG.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.closed:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			if !c.isConnectionCurrent(conn) {
				return
			}
			// 网关 IPC 连接是串行处理模型：当已有在途 RPC 时，跳过本轮心跳以避免排队超时误判。
			if c.hasPendingRequests(conn) {
				continue
			}

			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := c.CallWithOptions(
				pingCtx,
				protocol.MethodGatewayPing,
				map[string]any{},
				nil,
				GatewayRPCCallOptions{
					Timeout: timeout,
					Retries: 0,
				},
			)
			cancel()

			if err == nil {
				continue
			}
			if !c.isConnectionCurrent(conn) {
				return
			}
			if isUnauthorizedGatewayCallError(err) {
				reauthCtx, reauthCancel := context.WithTimeout(ctx, timeout)
				reauthErr := c.reauthenticate(reauthCtx, timeout)
				reauthCancel()
				if reauthErr == nil {
					continue
				}
				c.logHeartbeatWarningf("warning: gateway rpc heartbeat authorization lost, reset connection: %v", reauthErr)
				c.resetConnection()
				return
			}
			if shouldResetConnectionOnHeartbeatError(err) {
				c.logHeartbeatWarningf("warning: gateway rpc heartbeat ping failed, reset connection: %v", err)
				c.resetConnection()
				return
			}
			c.logHeartbeatWarningf("warning: gateway rpc heartbeat ping failed: %v", err)
		}
	}()
}

// logHeartbeatWarningf 在未禁用时输出心跳告警，避免污染交互终端。
func (c *GatewayRPCClient) logHeartbeatWarningf(format string, args ...any) {
	if c == nil || c.disableHeartbeatLog {
		return
	}
	log.Printf(format, args...)
}

// isConnectionCurrent 判断给定连接是否仍是当前活动连接，用于约束心跳协程不跨连接存活。
func (c *GatewayRPCClient) isConnectionCurrent(conn net.Conn) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.conn == conn
}

// hasPendingRequests 判断当前连接是否存在在途 RPC 请求。
func (c *GatewayRPCClient) hasPendingRequests(conn net.Conn) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.conn != conn {
		return false
	}
	return len(c.pending) > 0
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

// defaultAutoSpawnGateway 在首轮拨号失败且判定网关未启动时，静默拉起后台 gateway 进程并等待就绪。
func defaultAutoSpawnGateway(
	ctx context.Context,
	listenAddress string,
	dialFn func(address string) (net.Conn, error),
) (*exec.Cmd, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current executable: %w", err)
	}

	logSink, err := openGatewayAutoSpawnOutput()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = logSink.Close()
	}()

	cmd := exec.Command(executablePath, "gateway")
	cmd.Stdout = logSink
	cmd.Stderr = logSink
	if startErr := cmd.Start(); startErr != nil {
		return nil, fmt.Errorf("start gateway process: %w", startErr)
	}

	if waitErr := waitGatewayReadyAfterAutoSpawn(ctx, listenAddress, dialFn); waitErr != nil {
		_ = stopSpawnedGatewayProcess(cmd, nil)
		return nil, waitErr
	}
	return cmd, nil
}

// waitGatewayReadyAfterAutoSpawn 轮询探测网关连通性，直到连接可用或超时。
func waitGatewayReadyAfterAutoSpawn(
	ctx context.Context,
	listenAddress string,
	dialFn func(address string) (net.Conn, error),
) error {
	if strings.TrimSpace(listenAddress) == "" {
		return errors.New("gateway listen address is empty")
	}

	totalWindow := time.Duration(defaultGatewayAutoSpawnProbeAttempts) * defaultGatewayAutoSpawnProbeInterval
	var lastErr error
	for attempt := 0; attempt < defaultGatewayAutoSpawnProbeAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		conn, err := dialFn(listenAddress)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		if !isGatewayUnavailableDialError(err) {
			return fmt.Errorf("probe gateway readiness: %w", err)
		}

		if attempt == defaultGatewayAutoSpawnProbeAttempts-1 {
			break
		}
		timer := time.NewTimer(defaultGatewayAutoSpawnProbeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	if lastErr == nil {
		lastErr = errors.New("gateway is unavailable")
	}
	return fmt.Errorf("gateway not ready within %s: %w", totalWindow, lastErr)
}

// openGatewayAutoSpawnOutput 打开后台网关日志输出目标，优先写入 ~/.neocode/logs/gateway_auto.log，失败时回退到 DevNull。
func openGatewayAutoSpawnOutput() (*os.File, error) {
	logPath, pathErr := resolveGatewayAutoSpawnLogPath()
	if pathErr == nil {
		logFile, openErr := openGatewayAutoSpawnLogFile(logPath)
		if openErr == nil {
			return logFile, nil
		}
		pathErr = openErr
	}

	devNullFile, devNullErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if devNullErr != nil {
		if pathErr != nil {
			return nil, fmt.Errorf("resolve gateway auto-spawn log path: %w; open devnull: %v", pathErr, devNullErr)
		}
		return nil, fmt.Errorf("open gateway auto-spawn fallback output: %w", devNullErr)
	}
	return devNullFile, nil
}

// openGatewayAutoSpawnLogFile 在写入新日志前先执行备份轮转，避免单文件无限膨胀并保留上次现场。
func openGatewayAutoSpawnLogFile(logPath string) (*os.File, error) {
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, gatewayAutoSpawnLogDirPerm); err != nil {
		return nil, fmt.Errorf("create gateway auto-spawn log dir: %w", err)
	}
	if err := ensureSafeGatewayAutoSpawnLogDirectory(logDir); err != nil {
		return nil, err
	}
	if err := rotateGatewayAutoSpawnLog(logPath); err != nil {
		return nil, err
	}
	if err := ensureSafeGatewayAutoSpawnLogFilePath(logPath, true); err != nil {
		return nil, err
	}

	logFile, err := openGatewayAutoSpawnLogFileAtomically(logPath)
	if err != nil {
		return nil, fmt.Errorf("open gateway auto-spawn log file: %w", err)
	}
	return logFile, nil
}

// rotateGatewayAutoSpawnLog 将上一轮日志移动到 .bak，覆盖旧备份，确保本轮启动使用全新日志文件。
func rotateGatewayAutoSpawnLog(logPath string) error {
	if err := ensureSafeGatewayAutoSpawnLogFilePath(logPath, true); err != nil {
		return err
	}
	_, err := os.Lstat(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat gateway auto-spawn log file: %w", err)
	}

	backupPath := logPath + ".bak"
	if err := ensureSafeGatewayAutoSpawnLogFilePath(backupPath, true); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove gateway auto-spawn backup log: %w", err)
	}
	if err := os.Rename(logPath, backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup gateway auto-spawn log file: %w", err)
	}
	return nil
}

// resolveGatewayAutoSpawnLogPath 解析自动拉起网关日志文件路径。
func resolveGatewayAutoSpawnLogPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, defaultGatewayAutoSpawnLogRelativePath), nil
}

// stopSpawnedGatewayProcess 结束 Auto-Spawn 产生的后台网关进程，并异步 Wait 回收系统资源。
func stopSpawnedGatewayProcess(cmd *exec.Cmd, done <-chan struct{}) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if state := cmd.ProcessState; state != nil && state.Exited() {
		waitSpawnedGatewayProcess(done, cmd)
		return nil
	}

	killErr := cmd.Process.Kill()
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return fmt.Errorf("kill auto-spawned gateway process: %w", killErr)
	}

	waitSpawnedGatewayProcess(done, cmd)
	return nil
}

// waitSpawnedGatewayProcess 在后台等待子进程回收，若已有专用等待协程则改为等待其完成信号。
func waitSpawnedGatewayProcess(done <-chan struct{}, cmd *exec.Cmd) {
	go func() {
		if done != nil {
			<-done
			return
		}
		_ = cmd.Wait()
	}()
}

// isGatewayUnavailableDialError 判定拨号失败是否属于“网关未启动/不可达”的可自动拉起场景。
func isGatewayUnavailableDialError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}

	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "actively refused") ||
		strings.Contains(message, "no such file") ||
		strings.Contains(message, "does not exist") ||
		strings.Contains(message, "cannot find the file") ||
		strings.Contains(message, "pipe not found") ||
		strings.Contains(message, "no such pipe")
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

// shouldResetConnectionOnHeartbeatError 判断心跳失败是否应立即摘除连接并等待下次请求重建。
func shouldResetConnectionOnHeartbeatError(err error) bool {
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
	return false
}

// isAuthenticateGatewayMethod 判断当前 RPC 方法是否为 gateway.authenticate。
func isAuthenticateGatewayMethod(method string) bool {
	return strings.EqualFold(strings.TrimSpace(method), protocol.MethodGatewayAuthenticate)
}

// isUnauthorizedGatewayCallError 判断错误是否为网关 unauthorized。
func isUnauthorizedGatewayCallError(err error) bool {
	var rpcErr *GatewayRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(rpcErr.GatewayCode), protocol.GatewayCodeUnauthorized)
}

// reauthenticate 在现有连接上执行一次 gateway.authenticate，用于恢复连接级鉴权态。
func (c *GatewayRPCClient) reauthenticate(ctx context.Context, timeout time.Duration) error {
	if c == nil {
		return errors.New("gateway rpc client is nil")
	}
	token, err := c.ensureGatewayAuthToken(ctx)
	if err != nil {
		return err
	}
	var ack map[string]any
	return c.callOnce(
		ctx,
		protocol.MethodGatewayAuthenticate,
		protocol.AuthenticateParams{Token: token},
		&ack,
		timeout,
	)
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

// ensureGatewayAuthToken 在自动拉起完成后按需读取认证 Token，并对落盘竞态执行短重试。
func (c *GatewayRPCClient) ensureGatewayAuthToken(ctx context.Context) (string, error) {
	c.stateMu.Lock()
	token := strings.TrimSpace(c.token)
	tokenFile := c.tokenFile
	c.stateMu.Unlock()
	if token != "" {
		return token, nil
	}

	var lastErr error
	for attempt := 0; attempt < defaultGatewayAuthTokenRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		token, err := loadGatewayAuthToken(tokenFile)
		if err == nil {
			c.stateMu.Lock()
			if strings.TrimSpace(c.token) == "" {
				c.token = token
			}
			resolved := strings.TrimSpace(c.token)
			c.stateMu.Unlock()
			if resolved == "" {
				return "", errors.New("gateway rpc client: auth token is empty")
			}
			return resolved, nil
		}
		lastErr = err
		if !isGatewayAuthTokenRetryableError(err) {
			return "", err
		}
		if attempt == defaultGatewayAuthTokenRetryAttempts-1 {
			break
		}

		timer := time.NewTimer(defaultGatewayAuthTokenRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	if lastErr == nil {
		lastErr = errors.New("gateway rpc client: load auth token failed")
	}
	return "", lastErr
}

// isGatewayAuthTokenRetryableError 判断 token 加载失败是否属于“网关刚启动，文件尚未稳定可读”的可重试场景。
func isGatewayAuthTokenRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "cannot find the file") ||
		strings.Contains(lower, "token is empty") ||
		strings.Contains(lower, "decode auth file")
}

// openGatewayAutoSpawnLogFileAtomically 以“临时文件 + 原子替换”方式创建日志文件，再返回追加写句柄。
func openGatewayAutoSpawnLogFileAtomically(logPath string) (*os.File, error) {
	logDir := filepath.Dir(logPath)
	tempFile, err := os.CreateTemp(logDir, ".gateway_auto.log.tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temp gateway auto-spawn log file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := tempFile.Chmod(gatewayAutoSpawnLogFilePerm); err != nil {
		_ = tempFile.Close()
		return nil, fmt.Errorf("chmod temp gateway auto-spawn log file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp gateway auto-spawn log file: %w", err)
	}

	if err := ensureSafeGatewayAutoSpawnLogFilePath(logPath, true); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, logPath); err != nil {
		return nil, fmt.Errorf("replace gateway auto-spawn log file atomically: %w", err)
	}
	cleanupTemp = false

	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, gatewayAutoSpawnLogFilePerm)
	if err != nil {
		return nil, err
	}
	return logFile, nil
}

// ensureSafeGatewayAutoSpawnLogDirectory 校验日志目录不是符号链接，避免目录级劫持。
func ensureSafeGatewayAutoSpawnLogDirectory(dir string) error {
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect gateway auto-spawn log dir: %w", err)
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("gateway auto-spawn log dir is symbolic link")
	}
	return nil
}

// ensureSafeGatewayAutoSpawnLogFilePath 校验日志文件路径不为软链接/危险硬链接。
func ensureSafeGatewayAutoSpawnLogFilePath(path string, allowNotExist bool) error {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		if allowNotExist && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect gateway auto-spawn log file: %w", err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("gateway auto-spawn log file is symbolic link")
	}
	if isUnsafeGatewayAutoSpawnLogHardLink(fileInfo) {
		return errors.New("gateway auto-spawn log file is hard link")
	}
	return nil
}
