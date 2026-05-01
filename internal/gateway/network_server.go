package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/websocket"

	"neo-code/internal/gateway/protocol"
)

const (
	// DefaultNetworkListenAddress 定义网关网络访问面的默认监听地址，仅允许本机环回访问。
	DefaultNetworkListenAddress = "127.0.0.1:8080"
	// DefaultNetworkReadTimeout 定义网络入口单次读取超时时间，防止慢连接长期占用资源。
	DefaultNetworkReadTimeout = 15 * time.Second
	// DefaultNetworkWriteTimeout 定义网络入口单次写入超时时间，避免写阻塞导致协程泄漏。
	DefaultNetworkWriteTimeout = 15 * time.Second
	// DefaultNetworkShutdownTimeout 定义网络入口优雅关闭的最大等待时间。
	DefaultNetworkShutdownTimeout = 2 * time.Second
	// DefaultNetworkHeartbeatInterval 定义 WS/SSE 长连接保活心跳周期。
	DefaultNetworkHeartbeatInterval = 3 * time.Second
	// DefaultNetworkMaxRequestBytes 定义 HTTP/WS 单次请求体最大字节数。
	DefaultNetworkMaxRequestBytes int64 = MaxFrameSize
	// DefaultNetworkMaxStreamConnections 定义 WS/SSE 长连接总上限。
	DefaultNetworkMaxStreamConnections = 128
	// DefaultWSUnauthenticatedTimeout 定义 WS 未认证连接的最大等待时间。
	DefaultWSUnauthenticatedTimeout = 3 * time.Second
)

var (
	resolveNetworkListenAddressFn = ResolveNetworkListenAddress
	lookupHostIPsFn               = net.LookupIP
	dispatchRPCRequestFn          = dispatchRPCRequest
)

// NetworkServerOptions 描述网关网络访问面服务启动所需的可选配置。
type NetworkServerOptions struct {
	ListenAddress        string
	Logger               *log.Logger
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	ShutdownTimeout      time.Duration
	HeartbeatInterval    time.Duration
	MaxRequestBytes      int64
	MaxStreamConnections int
	// UnauthenticatedWSGracePeriod 定义 WS 连接未认证时的容忍时长。
	UnauthenticatedWSGracePeriod time.Duration
	Relay                        *StreamRelay
	Authenticator                TokenAuthenticator
	ACL                          *ControlPlaneACL
	Metrics                      *GatewayMetrics
	AllowedOrigins               []string
	// ConnectionCountChanged 在活跃长连接数变化时回调当前总数，用于空闲退出治理。
	ConnectionCountChanged func(active int)
	listenFn               func(network, address string) (net.Listener, error)
}

// NetworkServer 提供 HTTP/WebSocket/SSE 网络访问面的统一入口服务。
type NetworkServer struct {
	listenAddress        string
	logger               *log.Logger
	readTimeout          time.Duration
	writeTimeout         time.Duration
	shutdownTimeout      time.Duration
	heartbeatInterval    time.Duration
	unauthenticatedWSTTL time.Duration
	maxRequestBytes      int64
	maxStreamConnections int
	listenFn             func(network, address string) (net.Listener, error)
	relay                *StreamRelay
	authenticator        TokenAuthenticator
	acl                  *ControlPlaneACL
	metrics              *GatewayMetrics
	allowedOrigins       []string
	connectionCountChanged func(active int)
	startedAt            time.Time

	mu         sync.Mutex
	server     *http.Server
	listener   net.Listener
	wsConns    map[*websocket.Conn]context.CancelFunc
	sseCancels map[int]context.CancelFunc
	nextSSEID  int
}

// NewNetworkServer 创建网关网络访问面服务实例，并执行监听地址合法性校验。
func NewNetworkServer(options NetworkServerOptions) (*NetworkServer, error) {
	listenAddress, err := resolveNetworkListenAddressFn(options.ListenAddress)
	if err != nil {
		return nil, err
	}

	logger := options.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "gateway-network: ", log.LstdFlags)
	}

	listenFn := options.listenFn
	if listenFn == nil {
		listenFn = net.Listen
	}

	readTimeout := options.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = DefaultNetworkReadTimeout
	}

	writeTimeout := options.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultNetworkWriteTimeout
	}

	shutdownTimeout := options.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = DefaultNetworkShutdownTimeout
	}

	heartbeatInterval := options.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = DefaultNetworkHeartbeatInterval
	}

	maxRequestBytes := options.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = DefaultNetworkMaxRequestBytes
	}

	maxStreamConnections := options.MaxStreamConnections
	if maxStreamConnections <= 0 {
		maxStreamConnections = DefaultNetworkMaxStreamConnections
	}
	unauthenticatedWSTTL := options.UnauthenticatedWSGracePeriod
	if unauthenticatedWSTTL <= 0 {
		unauthenticatedWSTTL = DefaultWSUnauthenticatedTimeout
	}

	relay := options.Relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{
			Logger:  logger,
			Metrics: options.Metrics,
		})
	}

	authenticator := options.Authenticator
	acl := options.ACL
	if acl == nil && authenticator != nil {
		acl = NewStrictControlPlaneACL()
	}

	metrics := options.Metrics
	allowedOrigins := normalizeControlPlaneOrigins(options.AllowedOrigins)
	if len(allowedOrigins) == 0 {
		allowedOrigins = defaultControlPlaneOrigins()
	}

	return &NetworkServer{
		listenAddress:        listenAddress,
		logger:               logger,
		readTimeout:          readTimeout,
		writeTimeout:         writeTimeout,
		shutdownTimeout:      shutdownTimeout,
		heartbeatInterval:    heartbeatInterval,
		unauthenticatedWSTTL: unauthenticatedWSTTL,
		maxRequestBytes:      maxRequestBytes,
		maxStreamConnections: maxStreamConnections,
		listenFn:             listenFn,
		relay:                relay,
		authenticator:        authenticator,
		acl:                  acl,
		metrics:              metrics,
		allowedOrigins:       allowedOrigins,
		connectionCountChanged: options.ConnectionCountChanged,
		startedAt:            time.Now().UTC(),
		wsConns:              make(map[*websocket.Conn]context.CancelFunc),
		sseCancels:           make(map[int]context.CancelFunc),
	}, nil
}

// ResolveNetworkListenAddress 解析网络访问面监听地址，默认值固定为本机环回地址。
func ResolveNetworkListenAddress(override string) (string, error) {
	address := strings.TrimSpace(override)
	if address == "" {
		address = DefaultNetworkListenAddress
	}
	if err := validateLoopbackListenAddress(address); err != nil {
		return "", err
	}
	return address, nil
}

// validateLoopbackListenAddress 校验网络监听地址只能绑定到环回接口，避免暴露到外网。
func validateLoopbackListenAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid --http-listen %q: %w", address, err)
	}
	normalizedHost := strings.TrimSpace(host)
	if normalizedHost == "" {
		return fmt.Errorf("invalid --http-listen %q: host must be loopback", address)
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if !ip.IsLoopback() {
			return fmt.Errorf("invalid --http-listen %q: host must be loopback", address)
		}
		return nil
	}

	resolvedHostIPs, lookupErr := lookupHostIPsFn(normalizedHost)
	if lookupErr != nil || len(resolvedHostIPs) == 0 {
		return fmt.Errorf("invalid --http-listen %q: host must resolve to loopback addresses", address)
	}
	for _, resolvedIP := range resolvedHostIPs {
		if resolvedIP == nil || !resolvedIP.IsLoopback() {
			return fmt.Errorf("invalid --http-listen %q: host must be loopback", address)
		}
	}
	return nil
}

// ListenAddress 返回网络访问面当前绑定的监听地址。
func (s *NetworkServer) ListenAddress() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listenAddress
}

// Serve 启动网络访问面服务，并注册 HTTP/WebSocket/SSE 三类入口。
func (s *NetworkServer) Serve(ctx context.Context, runtimePort RuntimePort) error {
	if s.relay == nil {
		s.relay = NewStreamRelay(StreamRelayOptions{
			Logger:  s.logger,
			Metrics: s.metrics,
		})
	}

	listener, err := s.listenFn("tcp", s.listenAddress)
	if err != nil {
		return fmt.Errorf("gateway network listen failed: %w", err)
	}

	httpServer := &http.Server{
		Handler:      s.withCORS(s.buildHandler(runtimePort)),
		ReadTimeout:  s.readTimeout,
		WriteTimeout: s.writeTimeout,
	}

	s.mu.Lock()
	if s.server != nil {
		s.mu.Unlock()
		_ = listener.Close()
		return fmt.Errorf("gateway: network server is already serving")
	}
	s.server = httpServer
	s.listener = listener
	s.listenAddress = listener.Addr().String()
	s.mu.Unlock()

	s.relay.Start(ctx, runtimePort)
	s.logger.Printf("network listening on %s", listener.Addr().String())

	go func() {
		<-ctx.Done()
		_ = s.Close(context.Background())
	}()

	if err := httpServer.Serve(listener); err != nil {
		if errors.Is(err, http.ErrServerClosed) || ctx.Err() != nil || s.isClosed() {
			return nil
		}
		return fmt.Errorf("gateway: serve network: %w", err)
	}
	return nil
}

// Close 关闭网络访问面并主动中断 WS/SSE 长连接，避免进程退出被长连接阻塞。
func (s *NetworkServer) Close(ctx context.Context) error {
	s.mu.Lock()
	httpServer := s.server
	listener := s.listener
	relay := s.relay
	s.server = nil
	s.listener = nil
	s.mu.Unlock()

	if relay != nil {
		relay.Stop()
	}

	if httpServer == nil && listener == nil {
		return nil
	}

	s.forceCloseStreamConnections()

	var closeErr error
	if httpServer != nil {
		shutdownCtx := context.Background()
		if ctx != nil {
			shutdownCtx = ctx
		}
		if s.shutdownTimeout > 0 {
			var cancel context.CancelFunc
			shutdownCtx, cancel = context.WithTimeout(shutdownCtx, s.shutdownTimeout)
			defer cancel()
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			closeErr = errors.Join(closeErr, err)
			closeErr = errors.Join(closeErr, httpServer.Close())
		}
	}

	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

// isClosed 判断网络服务是否已经处于关闭状态。
func (s *NetworkServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.server == nil
}

// buildHandler 构建网络访问面的路由入口，并将请求统一转入网关分发链路。
func (s *NetworkServer) buildHandler(runtimePort RuntimePort) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthzRequest)
	mux.HandleFunc("/version", s.handleVersionRequest)
	mux.HandleFunc("/metrics", s.handlePrometheusMetrics)
	mux.HandleFunc("/metrics.json", s.handleJSONMetrics)
	mux.HandleFunc("/rpc", func(writer http.ResponseWriter, request *http.Request) {
		s.handleRPCRequest(writer, request, runtimePort)
	})
	mux.Handle("/ws", websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			return s.validateWebSocketOrigin(request)
		},
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			s.handleWebSocket(conn, runtimePort)
		}),
	})
	mux.HandleFunc("/sse", func(writer http.ResponseWriter, request *http.Request) {
		s.handleSSERequest(writer, request, runtimePort)
	})
	return mux
}

// withCORS 为网络入口注入 CORS 头，仅对白名单 Origin 回显允许值。
func (s *NetworkServer) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := strings.TrimSpace(request.Header.Get("Origin"))
		if origin != "" {
			if !s.isAllowedOrigin(origin) {
				http.Error(writer, "origin is not allowed", http.StatusForbidden)
				return
			}
			writer.Header().Set("Access-Control-Allow-Origin", origin)
			writer.Header().Set("Vary", "Origin")
		}

		writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// handleHealthzRequest 返回网关健康状态与连接快照。
func (s *NetworkServer) handleHealthzRequest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	connectionSnapshot := map[string]int{}
	if s.relay != nil {
		for channel, count := range s.relay.SnapshotConnectionCounts() {
			connectionSnapshot[strings.TrimSpace(string(channel))] = count
		}
	}

	payload := map[string]any{
		"status":      "ok",
		"listen":      strings.TrimSpace(s.listenAddress),
		"uptime_sec":  int(time.Since(s.startedAt).Seconds()),
		"connections": connectionSnapshot,
	}
	writeJSONResponse(writer, http.StatusOK, payload)
}

// handleVersionRequest 返回网关构建版本信息。
func (s *NetworkServer) handleVersionRequest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(writer, http.StatusOK, ResolvedBuildInfo())
}

// handlePrometheusMetrics 输出 Prometheus 文本指标。
func (s *NetworkServer) handlePrometheusMetrics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.metrics == nil {
		http.Error(writer, "metrics disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.isObservabilityRequestAuthorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.metrics.Registry() == nil {
		http.Error(writer, "metrics unavailable", http.StatusServiceUnavailable)
		return
	}
	promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{}).ServeHTTP(writer, request)
}

// handleJSONMetrics 输出 JSON 指标快照。
func (s *NetworkServer) handleJSONMetrics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.metrics == nil {
		writeJSONResponse(writer, http.StatusServiceUnavailable, map[string]any{
			"error": "metrics disabled",
		})
		return
	}
	if !s.isObservabilityRequestAuthorized(request) {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSONResponse(writer, http.StatusOK, map[string]any{
		"metrics": s.metrics.Snapshot(),
	})
}

// isObservabilityRequestAuthorized 校验 metrics 端点访问 Token。
func (s *NetworkServer) isObservabilityRequestAuthorized(request *http.Request) bool {
	return s.isControlPlaneHTTPRequestAuthorized(request)
}

// isControlPlaneHTTPRequestAuthorized 校验 HTTP 控制面请求是否携带并通过 Bearer Token。
func (s *NetworkServer) isControlPlaneHTTPRequestAuthorized(request *http.Request) bool {
	if s.authenticator == nil {
		return false
	}
	token := extractBearerToken(request.Header.Get("Authorization"))
	return s.authenticator.ValidateToken(token)
}

// handleRPCRequest 处理 POST /rpc 请求并返回单次 JSON-RPC 响应。
func (s *NetworkServer) handleRPCRequest(writer http.ResponseWriter, request *http.Request, runtimePort RuntimePort) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isControlPlaneHTTPRequestAuthorized(request) {
		writeJSONRPCHTTPResponse(
			writer,
			http.StatusUnauthorized,
			protocol.NewJSONRPCErrorResponse(
				nil,
				protocol.NewJSONRPCError(
					protocol.MapGatewayCodeToJSONRPCCode(ErrorCodeUnauthorized.String()),
					"unauthorized",
					ErrorCodeUnauthorized.String(),
				),
			),
		)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, s.maxRequestBytes)
	rpcRequest, rpcErr := decodeJSONRPCRequestFromReader(request.Body)
	if rpcErr != nil {
		writeJSONRPCHTTPResponse(writer, http.StatusOK, protocol.NewJSONRPCErrorResponse(nil, rpcErr))
		return
	}

	token := extractBearerToken(request.Header.Get("Authorization"))
	rpcCtx := s.decorateRequestContext(request.Context(), RequestSourceHTTP, token)
	rpcResponse := dispatchRPCRequestFn(rpcCtx, rpcRequest, runtimePort)
	statusCode := resolveJSONRPCHTTPStatusCode(rpcResponse)
	writeJSONRPCHTTPResponse(writer, statusCode, rpcResponse)
}

// handleWebSocket 处理 WS 入口请求，连接上下文会在关停或异常时主动取消。
func (s *NetworkServer) handleWebSocket(conn *websocket.Conn, runtimePort RuntimePort) {
	parentContext := context.Background()
	if request := conn.Request(); request != nil && request.Context() != nil {
		parentContext = request.Context()
	}
	connectionContext, cancelConnection := context.WithCancel(parentContext)
	defer cancelConnection()

	relay := s.relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{
			Logger:  s.logger,
			Metrics: s.metrics,
		})
	}

	connectionID := NewConnectionID()
	requestToken := ""
	if request := conn.Request(); request != nil {
		requestToken = extractBearerToken(request.Header.Get("Authorization"))
		if requestToken == "" && request.URL != nil {
			requestToken = strings.TrimSpace(request.URL.Query().Get("token"))
		}
	}
	connectionContext = s.decorateRequestContext(connectionContext, RequestSourceWS, requestToken)
	connectionContext = WithConnectionID(connectionContext, connectionID)
	connectionContext = WithStreamRelay(connectionContext, relay)

	if !s.registerWSConnection(conn, cancelConnection) {
		_ = conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		_ = websocket.Message.Send(conn, `{"status":"error","code":"too_many_connections","message":"stream connection limit exceeded"}`)
		_ = conn.Close()
		return
	}

	encoder := json.NewEncoder(conn)
	registerErr := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelWS,
		Context:      connectionContext,
		Cancel:       cancelConnection,
		Write: func(message RelayMessage) error {
			if message.Kind != relayMessageKindJSON {
				return fmt.Errorf("websocket connection only supports json payload")
			}
			if s.writeTimeout > 0 {
				if err := conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
					return err
				}
			}
			payload, err := json.Marshal(message.Payload)
			if err != nil {
				return err
			}
			if err := encoder.Encode(json.RawMessage(payload)); err != nil {
				return err
			}
			return nil
		},
		Close: func() {
			_ = conn.Close()
		},
	})
	if registerErr != nil {
		s.unregisterWSConnection(conn)
		s.logger.Printf("register websocket connection failed: %v", registerErr)
		_ = conn.Close()
		return
	}
	authState, _ := ConnectionAuthStateFromContext(connectionContext)
	stopAuthenticationGuard := s.startWSUnauthenticatedConnectionGuard(conn, cancelConnection, authState)
	defer stopAuthenticationGuard()

	defer func() {
		s.unregisterWSConnection(conn)
		relay.dropConnection(connectionID)
		_ = conn.Close()
	}()

	if s.maxRequestBytes > 0 {
		conn.MaxPayloadBytes = int(s.maxRequestBytes)
	}

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go s.runWSHeartbeatLoop(relay, connectionID, stopHeartbeat)

	for {
		select {
		case <-connectionContext.Done():
			return
		default:
		}

		// Set a generous read deadline to detect dead connections.
		// Clients send pings every 5 minutes, so 7 minutes allows for jitter.
		// This prevents zombie connections from blocking read goroutines indefinitely.
		_ = conn.SetReadDeadline(time.Now().Add(7 * time.Minute))

		var rawMessage string
		if err := websocket.Message.Receive(conn, &rawMessage); err != nil {
			if isConnectionClosedError(err) {
				return
			}
			s.logger.Printf("websocket read failed: %v", err)
			return
		}

		// Reset read deadline after successful read
		_ = conn.SetReadDeadline(time.Time{})

		rpcRequest, rpcErr := decodeJSONRPCRequestFromBytes([]byte(rawMessage))
		var rpcResponse protocol.JSONRPCResponse
		if rpcErr != nil {
			rpcResponse = protocol.NewJSONRPCErrorResponse(nil, rpcErr)
		} else {
			rpcResponse = dispatchRPCRequestFn(connectionContext, rpcRequest, runtimePort)
		}

		if !relay.SendJSONRPCResponse(connectionID, rpcResponse) {
			return
		}
	}
}

// runWSHeartbeatLoop 周期性推送 WebSocket 心跳帧，保证长连接可观测与保活。
func (s *NetworkServer) runWSHeartbeatLoop(relay *StreamRelay, connectionID ConnectionID, stop <-chan struct{}) {
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !relay.SendJSONRPCPayload(connectionID, map[string]any{
				"type":      "heartbeat",
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			}) {
				return
			}
		}
	}
}

// startWSUnauthenticatedConnectionGuard 在连接建立后启动未认证超时守卫，防止连接池被长期占位。
func (s *NetworkServer) startWSUnauthenticatedConnectionGuard(
	conn *websocket.Conn,
	cancel context.CancelFunc,
	authState *ConnectionAuthState,
) func() {
	if conn == nil || cancel == nil || authState == nil || s.authenticator == nil {
		return func() {}
	}
	if s.unauthenticatedWSTTL <= 0 || authState.IsAuthenticated() {
		return func() {}
	}

	done := make(chan struct{})
	timer := time.NewTimer(s.unauthenticatedWSTTL)
	go func() {
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			if authState.IsAuthenticated() {
				return
			}
			cancel()
			_ = conn.SetDeadline(time.Now())
			_ = conn.Close()
		}
	}()

	return func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

// handleSSERequest 处理 SSE 入口请求，先返回一次结果事件，再持续发送心跳事件。
func (s *NetworkServer) handleSSERequest(writer http.ResponseWriter, request *http.Request, runtimePort RuntimePort) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming not supported", http.StatusInternalServerError)
		return
	}

	requestToken := ""
	if request.URL != nil {
		requestToken = strings.TrimSpace(request.URL.Query().Get("token"))
	}
	if s.authenticator != nil && !s.authenticator.ValidateToken(requestToken) {
		// authenticator 存在且 token 无效时拒绝；本地模式（authenticator == nil）允许空 token
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}

	streamCtx, cancel := context.WithCancel(request.Context())
	streamCtx = s.decorateRequestContext(streamCtx, RequestSourceSSE, requestToken)
	connectionTag, registered := s.registerSSEConnection(cancel)
	if !registered {
		cancel()
		http.Error(writer, "stream connection limit exceeded", http.StatusServiceUnavailable)
		return
	}
	sseMessageCh := make(chan RelayMessage, DefaultStreamQueueSize)

	relay := s.relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{
			Logger:  s.logger,
			Metrics: s.metrics,
		})
	}
	streamConnectionID := NewConnectionID()
	streamCtx = WithConnectionID(streamCtx, streamConnectionID)
	streamCtx = WithStreamRelay(streamCtx, relay)

	registerErr := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: streamConnectionID,
		Channel:      StreamChannelSSE,
		Context:      streamCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			if message.Kind != relayMessageKindSSE {
				return fmt.Errorf("sse connection only supports sse events")
			}
			select {
			case <-streamCtx.Done():
				return context.Canceled
			case sseMessageCh <- message:
				return nil
			}
		},
		Close: func() {},
	})
	if registerErr != nil {
		cancel()
		s.unregisterSSEConnection(connectionTag)
		http.Error(writer, "failed to register stream connection", http.StatusInternalServerError)
		return
	}

	defer func() {
		cancel()
		s.unregisterSSEConnection(connectionTag)
		relay.dropConnection(streamConnectionID)
	}()

	queryValues := request.URL.Query()
	sessionID := strings.TrimSpace(queryValues.Get("session_id"))
	if sessionID != "" {
		runID := strings.TrimSpace(queryValues.Get("run_id"))
		if bindErr := relay.BindConnection(streamConnectionID, StreamBinding{
			SessionID: sessionID,
			RunID:     runID,
			Channel:   StreamChannelSSE,
			Explicit:  true,
		}); bindErr != nil {
			http.Error(writer, bindErr.Message, http.StatusBadRequest)
			return
		}
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	rpcRequest := buildSSETriggerRequest(request)
	rpcResponse := dispatchRPCRequestFn(streamCtx, rpcRequest, runtimePort)
	if !relay.SendSSEEvent(streamConnectionID, "result", rpcResponse) {
		return
	}

	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case <-ticker.C:
			if !relay.SendSSEEvent(streamConnectionID, "heartbeat", map[string]string{
				"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
			}) {
				return
			}
		case message := <-sseMessageCh:
			if strings.TrimSpace(message.Event) == "" {
				return
			}
			if err := s.writeSSEEvent(writer, flusher, message.Event, message.Payload); err != nil {
				return
			}
		}
	}
}

// writeSSEEvent 将结构化数据写入 SSE 事件通道，并在每次发送后立即刷新。
func (s *NetworkServer) writeSSEEvent(writer http.ResponseWriter, flusher http.Flusher, eventName string, payload any) error {
	if s.writeTimeout > 0 {
		_ = http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(s.writeTimeout))
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "event: %s\n", strings.TrimSpace(eventName)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", string(rawPayload)); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// buildSSETriggerRequest 从 SSE 查询参数构建一次 JSON-RPC 触发请求，默认方法为 gateway.ping。
func buildSSETriggerRequest(request *http.Request) protocol.JSONRPCRequest {
	queryValues := request.URL.Query()
	method := strings.TrimSpace(queryValues.Get("method"))
	if method == "" {
		method = protocol.MethodGatewayPing
	}

	requestID := strings.TrimSpace(queryValues.Get("id"))
	if requestID == "" {
		requestID = fmt.Sprintf("sse-%d", time.Now().UnixNano())
	}

	return protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(strconv.Quote(requestID)),
		Method:  method,
		Params:  json.RawMessage(`{}`),
	}
}

// decodeJSONRPCRequestFromBytes 解析字节流中的 JSON-RPC 请求并检查是否包含多值 JSON。
func decodeJSONRPCRequestFromBytes(raw []byte) (protocol.JSONRPCRequest, *protocol.JSONRPCError) {
	return decodeJSONRPCRequestFromReader(bytes.NewReader(raw))
}

// decodeJSONRPCRequestFromReader 解析 Reader 中的 JSON-RPC 请求并转换为标准协议错误。
func decodeJSONRPCRequestFromReader(reader io.Reader) (protocol.JSONRPCRequest, *protocol.JSONRPCError) {
	decoder := json.NewDecoder(reader)

	var request protocol.JSONRPCRequest
	if err := decoder.Decode(&request); err != nil {
		return protocol.JSONRPCRequest{}, protocol.NewJSONRPCError(
			protocol.JSONRPCCodeParseError,
			"invalid json-rpc request",
			protocol.GatewayCodeInvalidFrame,
		)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return protocol.JSONRPCRequest{}, protocol.NewJSONRPCError(
			protocol.JSONRPCCodeParseError,
			"invalid json-rpc request",
			protocol.GatewayCodeInvalidFrame,
		)
	}
	return request, nil
}

// writeJSONRPCHTTPResponse 以 JSON 形式写回 HTTP JSON-RPC 响应，并按状态码输出 HTTP 头。
func writeJSONRPCHTTPResponse(writer http.ResponseWriter, statusCode int, response protocol.JSONRPCResponse) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(response)
}

// resolveJSONRPCHTTPStatusCode 根据网关错误码映射 HTTP 响应状态，未命中时回退 200。
func resolveJSONRPCHTTPStatusCode(response protocol.JSONRPCResponse) int {
	gatewayCode := protocol.GatewayCodeFromJSONRPCError(response.Error)
	switch gatewayCode {
	case ErrorCodeUnauthorized.String():
		return http.StatusUnauthorized
	case ErrorCodeAccessDenied.String():
		return http.StatusForbidden
	default:
		return http.StatusOK
	}
}

// writeJSONResponse 以 JSON 形式输出普通 HTTP 响应。
func writeJSONResponse(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

// decorateRequestContext 为网络请求注入统一 source/auth/acl/metrics/logger 上下文。
func (s *NetworkServer) decorateRequestContext(base context.Context, source RequestSource, token string) context.Context {
	ctx := WithRequestSource(base, source)
	authState := NewConnectionAuthState()
	ctx = WithConnectionAuthState(ctx, authState)

	trimmedToken := strings.TrimSpace(token)
	if trimmedToken != "" {
		ctx = WithRequestToken(ctx, trimmedToken)
	}
	if s.authenticator != nil {
		ctx = WithTokenAuthenticator(ctx, s.authenticator)
		if trimmedToken != "" {
			if subjectID, valid := s.authenticator.ResolveSubjectID(trimmedToken); valid && strings.TrimSpace(subjectID) != "" {
				authState.MarkAuthenticated(subjectID)
			}
		}
	}
	if s.acl != nil {
		ctx = WithRequestACL(ctx, s.acl)
	}
	if s.metrics != nil {
		ctx = WithGatewayMetrics(ctx, s.metrics)
	}
	ctx = WithGatewayLogger(ctx, s.logger)
	return ctx
}

// registerWSConnection 登记一个 WebSocket 长连接，并执行统一并发上限控制。
func (s *NetworkServer) registerWSConnection(conn *websocket.Conn, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return false
	}
	if len(s.wsConns)+len(s.sseCancels) >= s.maxStreamConnections {
		return false
	}
	s.wsConns[conn] = cancel
	s.updateActiveConnectionMetricsLocked()
	s.notifyConnectionCountChanged()
	return true
}

// unregisterWSConnection 在 WebSocket 连接结束后移除连接登记。
func (s *NetworkServer) unregisterWSConnection(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.wsConns, conn)
	s.updateActiveConnectionMetricsLocked()
	s.notifyConnectionCountChanged()
}

// registerSSEConnection 登记一个 SSE 长连接并返回连接标识，用于后续主动中断。
func (s *NetworkServer) registerSSEConnection(cancel context.CancelFunc) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return 0, false
	}
	if len(s.wsConns)+len(s.sseCancels) >= s.maxStreamConnections {
		return 0, false
	}
	connectionID := s.nextSSEID
	s.nextSSEID++
	s.sseCancels[connectionID] = cancel
	s.updateActiveConnectionMetricsLocked()
	s.notifyConnectionCountChanged()
	return connectionID, true
}

// unregisterSSEConnection 在 SSE 连接结束后移除连接登记。
func (s *NetworkServer) unregisterSSEConnection(connectionID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sseCancels, connectionID)
	s.updateActiveConnectionMetricsLocked()
	s.notifyConnectionCountChanged()
}

// updateActiveConnectionMetricsLocked 在持锁状态下刷新活跃连接指标。
func (s *NetworkServer) updateActiveConnectionMetricsLocked() {
	if s.metrics == nil {
		return
	}
	s.metrics.SetConnectionsActive(string(StreamChannelWS), len(s.wsConns))
	s.metrics.SetConnectionsActive(string(StreamChannelSSE), len(s.sseCancels))
}

// notifyConnectionCountChanged 向外层报告当前活跃长连接总数。
func (s *NetworkServer) notifyConnectionCountChanged() {
	if s.connectionCountChanged == nil {
		return
	}
	s.connectionCountChanged(len(s.wsConns) + len(s.sseCancels))
}

// forceCloseStreamConnections 在关停流程中主动切断 WS/SSE 长连接，避免退出被阻塞。
func (s *NetworkServer) forceCloseStreamConnections() {
	wsConnections, wsCancels, sseCancels := s.snapshotStreamConnections()
	for _, cancel := range wsCancels {
		cancel()
	}
	for _, cancel := range sseCancels {
		cancel()
	}
	for _, conn := range wsConnections {
		_ = conn.SetDeadline(time.Now())
		_ = conn.Close()
	}
}

// snapshotStreamConnections 拍平当前长连接快照并清空登记表，供关闭流程安全遍历。
func (s *NetworkServer) snapshotStreamConnections() ([]*websocket.Conn, []context.CancelFunc, []context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wsConnections := make([]*websocket.Conn, 0, len(s.wsConns))
	wsCancels := make([]context.CancelFunc, 0, len(s.wsConns))
	for conn, cancel := range s.wsConns {
		wsConnections = append(wsConnections, conn)
		wsCancels = append(wsCancels, cancel)
	}
	s.wsConns = make(map[*websocket.Conn]context.CancelFunc)

	sseCancels := make([]context.CancelFunc, 0, len(s.sseCancels))
	for connectionID, cancel := range s.sseCancels {
		sseCancels = append(sseCancels, cancel)
		delete(s.sseCancels, connectionID)
	}

	s.updateActiveConnectionMetricsLocked()
	return wsConnections, wsCancels, sseCancels
}

// isAllowedControlPlaneOrigin 校验请求来源是否命中本地控制面允许的 Origin 白名单。
func isAllowedControlPlaneOrigin(origin string) bool {
	return isAllowedControlPlaneOriginWithAllowlist(origin, defaultControlPlaneOrigins())
}

func isAllowedControlPlaneOriginWithAllowlist(origin string, allowlist []string) bool {
	normalizedOrigin := strings.ToLower(strings.TrimSpace(origin))
	if normalizedOrigin == "" {
		return false
	}
	for _, allow := range allowlist {
		if originMatchesAllowRule(normalizedOrigin, allow) {
			return true
		}
	}
	return false
}

// validateOriginForWebSocket 在握手阶段校验 Origin 白名单，阻断非可信网页来源。
func validateOriginForWebSocket(request *http.Request) error {
	if request == nil {
		return errors.New("invalid websocket request")
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	if !isAllowedControlPlaneOrigin(origin) {
		return fmt.Errorf("websocket origin %q is not allowed", origin)
	}
	return nil
}

// isAllowedOrigin 使用服务实例配置的 allowlist 校验来源。
func (s *NetworkServer) isAllowedOrigin(origin string) bool {
	allowlist := s.allowedOrigins
	if len(allowlist) == 0 {
		allowlist = defaultControlPlaneOrigins()
	}
	return isAllowedControlPlaneOriginWithAllowlist(origin, allowlist)
}

// validateWebSocketOrigin 在握手阶段基于实例 allowlist 校验 WebSocket 来源。
func (s *NetworkServer) validateWebSocketOrigin(request *http.Request) error {
	if request == nil {
		return errors.New("invalid websocket request")
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	if !s.isAllowedOrigin(origin) {
		return fmt.Errorf("websocket origin %q is not allowed", origin)
	}
	return nil
}

func defaultControlPlaneOrigins() []string {
	return []string{"http://localhost", "http://127.0.0.1", "http://[::1]", "app://"}
}

func normalizeControlPlaneOrigins(origins []string) []string {
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		trimmed := strings.ToLower(strings.TrimSpace(origin))
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func originMatchesAllowRule(normalizedOrigin, normalizedAllow string) bool {
	if normalizedAllow == "" {
		return false
	}
	if strings.HasSuffix(normalizedAllow, "://") {
		return strings.HasPrefix(normalizedOrigin, normalizedAllow)
	}
	if normalizedOrigin == normalizedAllow {
		return true
	}
	if strings.HasPrefix(normalizedAllow, "http://[") && strings.HasSuffix(normalizedAllow, "]") {
		return strings.HasPrefix(normalizedOrigin, normalizedAllow+":")
	}
	if strings.HasPrefix(normalizedAllow, "http://") && !strings.Contains(strings.TrimPrefix(normalizedAllow, "http://"), ":") {
		return strings.HasPrefix(normalizedOrigin, normalizedAllow+":")
	}
	return false
}

// extractBearerToken 从 Authorization 头中提取 Bearer Token。
func extractBearerToken(authorization string) string {
	trimmed := strings.TrimSpace(authorization)
	if trimmed == "" {
		return ""
	}
	const prefix = "bearer "
	if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(trimmed[len(prefix):])
}

// isConnectionClosedError 判断错误是否由连接关闭触发，便于安静退出读写循环。
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	lowerMessage := strings.ToLower(err.Error())
	return strings.Contains(lowerMessage, "closed network connection") || strings.Contains(lowerMessage, "closed pipe")
}
