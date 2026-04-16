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
	// DefaultNetworkHeartbeatInterval 定义 WS/SSE 长连接的保活心跳周期。
	DefaultNetworkHeartbeatInterval = 3 * time.Second
	// DefaultNetworkMaxRequestBytes 定义 HTTP/WS 单次请求体的最大字节数。
	DefaultNetworkMaxRequestBytes int64 = MaxFrameSize
	// DefaultNetworkMaxStreamConnections 定义 WS/SSE 长连接总上限。
	DefaultNetworkMaxStreamConnections = 128
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
	Relay                *StreamRelay
	listenFn             func(network, address string) (net.Listener, error)
}

// NetworkServer 提供 HTTP/WebSocket/SSE 网络访问面的统一入口服务。
type NetworkServer struct {
	listenAddress        string
	logger               *log.Logger
	readTimeout          time.Duration
	writeTimeout         time.Duration
	shutdownTimeout      time.Duration
	heartbeatInterval    time.Duration
	maxRequestBytes      int64
	maxStreamConnections int
	listenFn             func(network, address string) (net.Listener, error)
	relay                *StreamRelay

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

	relay := options.Relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{
			Logger: logger,
		})
	}

	return &NetworkServer{
		listenAddress:        listenAddress,
		logger:               logger,
		readTimeout:          readTimeout,
		writeTimeout:         writeTimeout,
		shutdownTimeout:      shutdownTimeout,
		heartbeatInterval:    heartbeatInterval,
		maxRequestBytes:      maxRequestBytes,
		maxStreamConnections: maxStreamConnections,
		listenFn:             listenFn,
		relay:                relay,
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

// validateLoopbackListenAddress 校验网络监听地址只能绑定到环回接口，避免开放到外网。
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
		s.relay = NewStreamRelay(StreamRelayOptions{Logger: s.logger})
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
	mux.HandleFunc("/rpc", func(writer http.ResponseWriter, request *http.Request) {
		s.handleRPCRequest(writer, request, runtimePort)
	})
	mux.Handle("/ws", websocket.Server{
		Handshake: func(config *websocket.Config, request *http.Request) error {
			return validateOriginForWebSocket(request)
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
			if !isAllowedControlPlaneOrigin(origin) {
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

// handleRPCRequest 处理 POST /rpc 请求并返回单次 JSON-RPC 响应。
func (s *NetworkServer) handleRPCRequest(writer http.ResponseWriter, request *http.Request, runtimePort RuntimePort) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, s.maxRequestBytes)
	rpcRequest, rpcErr := decodeJSONRPCRequestFromReader(request.Body)
	if rpcErr != nil {
		writeJSONRPCHTTPResponse(writer, protocol.NewJSONRPCErrorResponse(nil, rpcErr))
		return
	}

	response := dispatchRPCRequestFn(request.Context(), rpcRequest, runtimePort)
	writeJSONRPCHTTPResponse(writer, response)
}

// handleWebSocket 处理 WS 入口请求，连接上下文会在关停或异常时主动取消。
func (s *NetworkServer) handleWebSocket(conn *websocket.Conn, runtimePort RuntimePort) {
	parentContext := context.Background()
	if request := conn.Request(); request != nil && request.Context() != nil {
		parentContext = request.Context()
	}
	connectionContext, cancelConnection := context.WithCancel(parentContext)
	relay := s.relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{Logger: s.logger})
	}
	connectionID := NewConnectionID()
	connectionContext = WithConnectionID(connectionContext, connectionID)
	connectionContext = WithStreamRelay(connectionContext, relay)

	if !s.registerWSConnection(conn, cancelConnection) {
		cancelConnection()
		_ = conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		_ = websocket.Message.Send(conn, `{"status":"error","code":"too_many_connections","message":"stream connection limit exceeded"}`)
		_ = conn.Close()
		return
	}

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
			rawPayload, err := json.Marshal(message.Payload)
			if err != nil {
				return err
			}
			return websocket.Message.Send(conn, string(rawPayload))
		},
		Close: func() {
			_ = conn.Close()
		},
	})
	if registerErr != nil {
		cancelConnection()
		s.unregisterWSConnection(conn)
		_ = conn.Close()
		s.logger.Printf("register websocket connection failed: %v", registerErr)
		return
	}

	defer func() {
		cancelConnection()
		s.unregisterWSConnection(conn)
		relay.dropConnection(connectionID)
		_ = conn.Close()
	}()

	maxPayloadBytes := int(s.maxRequestBytes)
	if maxPayloadBytes > 0 {
		conn.MaxPayloadBytes = maxPayloadBytes
	}

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go s.runWSHeartbeatLoop(relay, connectionID, stopHeartbeat)

	for {
		// 注意：此处不再强制上行读超时，避免单向推送场景下误杀健康连接。
		var rawMessage string
		if err := websocket.Message.Receive(conn, &rawMessage); err != nil {
			cancelConnection()
			if isConnectionClosedError(err) {
				return
			}
			s.logger.Printf("websocket read failed: %v", err)
			return
		}

		rpcRequest, rpcErr := decodeJSONRPCRequestFromBytes([]byte(rawMessage))
		var rpcResponse protocol.JSONRPCResponse
		if rpcErr != nil {
			rpcResponse = protocol.NewJSONRPCErrorResponse(nil, rpcErr)
		} else {
			rpcResponse = dispatchRPCRequestFn(connectionContext, rpcRequest, runtimePort)
		}

		if !relay.SendJSONRPCResponse(connectionID, rpcResponse) {
			cancelConnection()
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

	streamCtx, cancel := context.WithCancel(request.Context())
	connectionID, registered := s.registerSSEConnection(cancel)
	if !registered {
		cancel()
		http.Error(writer, "stream connection limit exceeded", http.StatusServiceUnavailable)
		return
	}
	sseMessageCh := make(chan RelayMessage, DefaultStreamQueueSize)

	relay := s.relay
	if relay == nil {
		relay = NewStreamRelay(StreamRelayOptions{Logger: s.logger})
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
		s.unregisterSSEConnection(connectionID)
		http.Error(writer, "failed to register stream connection", http.StatusInternalServerError)
		return
	}

	defer func() {
		cancel()
		s.unregisterSSEConnection(connectionID)
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

// decodeJSONRPCRequestFromBytes 解析字节流中的 JSON-RPC 请求并检查是否包含多余 JSON 值。
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

// writeJSONRPCHTTPResponse 以 JSON 形式写回 HTTP JSON-RPC 响应。
func writeJSONRPCHTTPResponse(writer http.ResponseWriter, response protocol.JSONRPCResponse) {
	writer.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(response)
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
	return true
}

// unregisterWSConnection 在 WebSocket 连接结束后移除连接登记。
func (s *NetworkServer) unregisterWSConnection(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.wsConns, conn)
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
	return connectionID, true
}

// unregisterSSEConnection 在 SSE 连接结束后移除连接登记。
func (s *NetworkServer) unregisterSSEConnection(connectionID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sseCancels, connectionID)
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

	return wsConnections, wsCancels, sseCancels
}

// isAllowedControlPlaneOrigin 校验请求来源是否命中本地控制面允许的 Origin 白名单。
func isAllowedControlPlaneOrigin(origin string) bool {
	normalizedOrigin := strings.ToLower(strings.TrimSpace(origin))
	switch {
	case normalizedOrigin == "":
		return false
	case strings.HasPrefix(normalizedOrigin, "http://localhost:"),
		normalizedOrigin == "http://localhost",
		strings.HasPrefix(normalizedOrigin, "http://127.0.0.1:"),
		normalizedOrigin == "http://127.0.0.1",
		strings.HasPrefix(normalizedOrigin, "http://[::1]:"),
		normalizedOrigin == "http://[::1]",
		strings.HasPrefix(normalizedOrigin, "app://"):
		return true
	default:
		return false
	}
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

// isConnectionClosedError 判断错误是否由连接关闭触发，便于安静退出读写循环。
func isConnectionClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	lowerMessage := strings.ToLower(err.Error())
	return strings.Contains(lowerMessage, "closed network connection") ||
		strings.Contains(lowerMessage, "closed pipe")
}
