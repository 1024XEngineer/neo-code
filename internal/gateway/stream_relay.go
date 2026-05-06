package gateway

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"neo-code/internal/gateway/protocol"
)

const (
	// DefaultStreamBindingTTL 定义连接绑定关系的默认生存时长（滑动续期）。
	DefaultStreamBindingTTL = 15 * time.Minute
	// DefaultStreamCleanupInterval 定义路由表过期清理扫描间隔。
	DefaultStreamCleanupInterval = 30 * time.Second
	// DefaultStreamQueueSize 定义每个连接的默认发送队列容量。
	DefaultStreamQueueSize = 256
	// DefaultStreamMaxBindingsPerConnection 定义单连接可维护的最大绑定数，防止路由表被无限放大。
	DefaultStreamMaxBindingsPerConnection = 128
)

type relayMessageKind string

const (
	relayMessageKindJSON relayMessageKind = "json"
	relayMessageKindSSE  relayMessageKind = "sse"
)

// RelayMessage 表示发往具体连接的统一出站消息。
type RelayMessage struct {
	Kind     relayMessageKind
	Event    string
	Payload  any
	Enqueued time.Time
}

// ConnectionRegistration 描述连接注册到中继器所需的写入与关闭钩子。
type ConnectionRegistration struct {
	ConnectionID ConnectionID
	Channel      StreamChannel
	Context      context.Context
	Cancel       context.CancelFunc
	Write        func(message RelayMessage) error
	Close        func()
}

// StreamBinding 描述连接绑定到会话路由表的一条订阅关系。
type StreamBinding struct {
	SessionID string
	RunID     string
	Channel   StreamChannel
	Explicit  bool
}

// StreamRelayOptions 描述会话路由与流式中继的可选配置。
type StreamRelayOptions struct {
	Logger          *log.Logger
	BindingTTL      time.Duration
	CleanupInterval time.Duration
	QueueSize       int
	// MaxBindingsPerConnection 控制单连接可建立的会话绑定上限。
	MaxBindingsPerConnection int
	// Metrics 为可选指标收集器，用于上报连接与丢弃统计。
	Metrics *GatewayMetrics
}

type relayConnection struct {
	id      ConnectionID
	channel StreamChannel
	ctx     context.Context
	cancel  context.CancelFunc
	writeFn func(message RelayMessage) error
	closeFn func()
	queue   chan RelayMessage
	writeMu sync.Mutex
}

type bindingKey struct {
	sessionID string
	runID     string
}

type bindingState struct {
	sessionID string
	runID     string
	channel   StreamChannel
	explicit  bool
	expireAt  time.Time
	lastSeen  time.Time
}

// StreamRelay 维护连接-会话-运行态映射，并负责运行事件的精确中继。
type StreamRelay struct {
	logger          *log.Logger
	bindingTTL      time.Duration
	cleanupInterval time.Duration
	queueSize       int
	maxBindings     int
	metrics         *GatewayMetrics

	mu                     sync.RWMutex
	connections            map[ConnectionID]*relayConnection
	connectionBindings     map[ConnectionID]map[bindingKey]*bindingState
	sessionIndex           map[string]map[ConnectionID]struct{}
	sessionRunIndex        map[string]map[ConnectionID]struct{}
	cleanupStarted         bool
	eventPumpStarted       bool
	cleanupLoopCancel      context.CancelFunc
	runtimeEventLoopCancel context.CancelFunc
	cleanupLoopGeneration  uint64
	eventLoopGeneration    uint64
}

// NewStreamRelay 创建会话路由与流式中继实例。
func NewStreamRelay(options StreamRelayOptions) *StreamRelay {
	logger := options.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "gateway-relay: ", log.LstdFlags)
	}

	bindingTTL := options.BindingTTL
	if bindingTTL <= 0 {
		bindingTTL = DefaultStreamBindingTTL
	}

	cleanupInterval := options.CleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultStreamCleanupInterval
	}

	queueSize := options.QueueSize
	if queueSize <= 0 {
		queueSize = DefaultStreamQueueSize
	}

	maxBindings := options.MaxBindingsPerConnection
	if maxBindings <= 0 {
		maxBindings = DefaultStreamMaxBindingsPerConnection
	}

	return &StreamRelay{
		logger:             logger,
		bindingTTL:         bindingTTL,
		cleanupInterval:    cleanupInterval,
		queueSize:          queueSize,
		maxBindings:        maxBindings,
		metrics:            options.Metrics,
		connections:        make(map[ConnectionID]*relayConnection),
		connectionBindings: make(map[ConnectionID]map[bindingKey]*bindingState),
		sessionIndex:       make(map[string]map[ConnectionID]struct{}),
		sessionRunIndex:    make(map[string]map[ConnectionID]struct{}),
	}
}

// Start 启动中继器后台任务（过期清理与运行事件消费），多次调用可重入。
func (r *StreamRelay) Start(ctx context.Context, runtimePort RuntimePort) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.mu.Lock()
	if !r.cleanupStarted {
		cleanupCtx, cleanupCancel := context.WithCancel(ctx)
		r.cleanupLoopCancel = cleanupCancel
		r.cleanupStarted = true
		r.cleanupLoopGeneration++
		cleanupGeneration := r.cleanupLoopGeneration
		go r.runCleanupLoop(cleanupCtx, cleanupGeneration)
	}

	if runtimePort != nil && !r.eventPumpStarted {
		eventCtx, eventCancel := context.WithCancel(ctx)
		r.runtimeEventLoopCancel = eventCancel
		r.eventPumpStarted = true
		r.eventLoopGeneration++
		eventGeneration := r.eventLoopGeneration
		go r.runRuntimeEventLoop(eventCtx, runtimePort, eventGeneration)
	}
	r.mu.Unlock()
}

// Stop 停止后台任务并主动断开全部登记连接，保证网关退出时状态可回收。
func (r *StreamRelay) Stop() {
	if r == nil {
		return
	}

	r.mu.Lock()
	cleanupCancel := r.cleanupLoopCancel
	r.cleanupLoopCancel = nil
	r.cleanupStarted = false
	r.cleanupLoopGeneration++

	eventCancel := r.runtimeEventLoopCancel
	r.runtimeEventLoopCancel = nil
	r.eventPumpStarted = false
	r.eventLoopGeneration++

	connectionIDs := make([]ConnectionID, 0, len(r.connections))
	for connectionID := range r.connections {
		connectionIDs = append(connectionIDs, connectionID)
	}
	r.mu.Unlock()

	if cleanupCancel != nil {
		cleanupCancel()
	}
	if eventCancel != nil {
		eventCancel()
	}

	for _, connectionID := range connectionIDs {
		r.dropConnection(connectionID)
	}
}

// RegisterConnection 注册一个物理连接，并为其启动单写协程和有界队列。
func (r *StreamRelay) RegisterConnection(registration ConnectionRegistration) error {
	if r == nil {
		return fmt.Errorf("stream relay is nil")
	}

	connectionID := NormalizeConnectionID(registration.ConnectionID)
	if connectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	if registration.Context == nil {
		return fmt.Errorf("connection context is required")
	}
	if registration.Cancel == nil {
		return fmt.Errorf("connection cancel function is required")
	}
	if registration.Write == nil {
		return fmt.Errorf("connection write function is required")
	}
	if registration.Close == nil {
		return fmt.Errorf("connection close function is required")
	}
	if _, ok := ParseStreamChannel(string(registration.Channel)); !ok || registration.Channel == StreamChannelAll {
		return fmt.Errorf("invalid connection channel %q", registration.Channel)
	}

	r.mu.Lock()
	if _, exists := r.connections[connectionID]; exists {
		r.mu.Unlock()
		return fmt.Errorf("connection %s already registered", connectionID)
	}

	connection := &relayConnection{
		id:      connectionID,
		channel: registration.Channel,
		ctx:     registration.Context,
		cancel:  registration.Cancel,
		writeFn: registration.Write,
		closeFn: registration.Close,
		queue:   make(chan RelayMessage, r.queueSize),
	}
	r.connections[connectionID] = connection
	r.updateActiveConnectionMetricsLocked()
	r.mu.Unlock()

	go r.runConnectionWriter(connection)
	return nil
}

// SnapshotConnectionCounts 返回当前不同通道的活跃连接数量快照。
func (r *StreamRelay) SnapshotConnectionCounts() map[StreamChannel]int {
	if r == nil {
		return map[StreamChannel]int{}
	}
	snapshot := map[StreamChannel]int{
		StreamChannelIPC: 0,
		StreamChannelWS:  0,
		StreamChannelSSE: 0,
	}

	r.mu.RLock()
	for _, connection := range r.connections {
		if connection == nil {
			continue
		}
		snapshot[connection.channel]++
	}
	r.mu.RUnlock()
	return snapshot
}

// SendJSONRPCResponse 将 JSON-RPC 响应写入连接发送队列。
func (r *StreamRelay) SendJSONRPCResponse(connectionID ConnectionID, response protocol.JSONRPCResponse) bool {
	return r.enqueueMessage(connectionID, RelayMessage{
		Kind:     relayMessageKindJSON,
		Payload:  response,
		Enqueued: time.Now(),
	})
}

// SendJSONRPCResponseSync 通过连接统一串行写路径同步发送 JSON-RPC 响应，适用于协议错误等即时反馈场景。
func (r *StreamRelay) SendJSONRPCResponseSync(connectionID ConnectionID, response protocol.JSONRPCResponse) bool {
	if r == nil {
		return false
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return false
	}

	r.mu.RLock()
	connection := r.connections[normalizedConnectionID]
	r.mu.RUnlock()
	if connection == nil {
		return false
	}

	writeErr := r.writeConnectionMessage(connection, RelayMessage{
		Kind:     relayMessageKindJSON,
		Payload:  response,
		Enqueued: time.Now(),
	})
	if writeErr == nil {
		return true
	}

	r.logger.Printf("connection %s sync write failed: %v", normalizedConnectionID, writeErr)
	r.dropConnection(normalizedConnectionID)
	return false
}

// SendJSONRPCPayload 将任意 JSON 载荷写入连接发送队列，适用于 IPC/WS 心跳与响应。
func (r *StreamRelay) SendJSONRPCPayload(connectionID ConnectionID, payload any) bool {
	return r.enqueueMessage(connectionID, RelayMessage{
		Kind:     relayMessageKindJSON,
		Payload:  payload,
		Enqueued: time.Now(),
	})
}

// SendSSEEvent 将结构化负载作为指定事件写入 SSE 连接发送队列。
func (r *StreamRelay) SendSSEEvent(connectionID ConnectionID, eventName string, payload any) bool {
	return r.enqueueMessage(connectionID, RelayMessage{
		Kind:     relayMessageKindSSE,
		Event:    strings.TrimSpace(eventName),
		Payload:  payload,
		Enqueued: time.Now(),
	})
}

// BindConnection 将连接绑定到指定会话与运行态，并刷新绑定 TTL。
func (r *StreamRelay) BindConnection(connectionID ConnectionID, binding StreamBinding) *FrameError {
	if r == nil {
		return NewFrameError(ErrorCodeInternalError, "stream relay is nil")
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return NewMissingRequiredFieldError("connection_id")
	}

	sessionID := strings.TrimSpace(binding.SessionID)
	if sessionID == "" {
		return NewMissingRequiredFieldError("session_id")
	}
	runID := strings.TrimSpace(binding.RunID)

	channel := binding.Channel
	if channel == "" {
		channel = StreamChannelAll
	}
	if _, ok := ParseStreamChannel(string(channel)); !ok {
		return NewFrameError(ErrorCodeInvalidAction, "invalid bind channel")
	}

	now := time.Now()

	r.mu.Lock()
	connection := r.connections[normalizedConnectionID]
	if connection == nil {
		r.mu.Unlock()
		return NewFrameError(ErrorCodeInvalidAction, "connection is not registered")
	}
	if channel != StreamChannelAll && channel != connection.channel {
		r.mu.Unlock()
		return NewFrameError(ErrorCodeInvalidAction, "bind channel does not match connection channel")
	}

	key := bindingKey{sessionID: sessionID, runID: runID}
	connectionBindingMap := r.connectionBindings[normalizedConnectionID]
	if connectionBindingMap == nil {
		connectionBindingMap = make(map[bindingKey]*bindingState)
		r.connectionBindings[normalizedConnectionID] = connectionBindingMap
	}
	for existingKey, existingState := range connectionBindingMap {
		if existingState == nil || existingState.expireAt.Before(now) {
			delete(connectionBindingMap, existingKey)
			if existingState != nil {
				r.removeConnectionFromIndexesLocked(normalizedConnectionID, existingState.sessionID, existingState.runID)
			}
		}
	}
	if _, exists := connectionBindingMap[key]; !exists && len(connectionBindingMap) >= r.maxBindings {
		r.mu.Unlock()
		return NewFrameError(ErrorCodeInvalidAction, "too many stream bindings for connection")
	}
	connectionBindingMap[key] = &bindingState{
		sessionID: sessionID,
		runID:     runID,
		channel:   channel,
		explicit:  binding.Explicit,
		expireAt:  now.Add(r.bindingTTL),
		lastSeen:  now,
	}
	r.addConnectionToSessionIndexLocked(sessionID, normalizedConnectionID)
	if runID != "" {
		r.addConnectionToSessionRunIndexLocked(sessionID, runID, normalizedConnectionID)
	}
	r.mu.Unlock()
	return nil
}

// ResolveFallbackSessionID 返回连接当前可用绑定中的会话兜底值（取最近续期的绑定）。
func (r *StreamRelay) ResolveFallbackSessionID(connectionID ConnectionID) string {
	if r == nil {
		return ""
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return ""
	}

	now := time.Now()

	r.mu.RLock()
	connectionBindingMap := r.connectionBindings[normalizedConnectionID]
	var (
		latestSessionID string
		latestSeen      time.Time
	)
	for _, state := range connectionBindingMap {
		if state == nil || state.expireAt.Before(now) {
			continue
		}
		if state.lastSeen.After(latestSeen) {
			latestSeen = state.lastSeen
			latestSessionID = state.sessionID
		}
	}
	r.mu.RUnlock()
	return strings.TrimSpace(latestSessionID)
}

// RefreshConnectionBindings 刷新连接下全部绑定 TTL，供 ping 保活和活跃续期使用。
func (r *StreamRelay) RefreshConnectionBindings(connectionID ConnectionID) bool {
	if r == nil {
		return false
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return false
	}

	now := time.Now()
	refreshed := false

	r.mu.Lock()
	connectionBindingMap := r.connectionBindings[normalizedConnectionID]
	for key, state := range connectionBindingMap {
		if state == nil {
			delete(connectionBindingMap, key)
			continue
		}
		if state.expireAt.Before(now) {
			delete(connectionBindingMap, key)
			r.removeConnectionFromIndexesLocked(normalizedConnectionID, state.sessionID, state.runID)
			continue
		}
		state.expireAt = now.Add(r.bindingTTL)
		refreshed = true
	}
	r.mu.Unlock()
	return refreshed
}

// ClearConnectionBindings 清除指定连接的所有 session 绑定，用于工作区切换等场景。
func (r *StreamRelay) ClearConnectionBindings(connectionID ConnectionID) {
	if r == nil {
		return
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return
	}

	r.mu.Lock()
	connectionBindingMap := r.connectionBindings[normalizedConnectionID]
	for key, state := range connectionBindingMap {
		if state != nil {
			r.removeConnectionFromIndexesLocked(normalizedConnectionID, state.sessionID, state.runID)
		}
		delete(connectionBindingMap, key)
	}
	if len(connectionBindingMap) == 0 {
		delete(r.connectionBindings, normalizedConnectionID)
	}
	r.mu.Unlock()
}

// AutoBindFromFrame 根据请求帧中的 session/run 信息执行自动续绑。
func (r *StreamRelay) AutoBindFromFrame(connectionID ConnectionID, frame MessageFrame) {
	if r == nil {
		return
	}

	sessionID := strings.TrimSpace(frame.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(extractSessionIDFromPayload(frame.Payload))
	}
	if sessionID == "" {
		return
	}

	_ = r.BindConnection(connectionID, StreamBinding{
		SessionID: sessionID,
		RunID:     strings.TrimSpace(frame.RunID),
		Channel:   StreamChannelAll,
		Explicit:  false,
	})
}

// PublishRuntimeEvent 将运行时事件转换为标准通知，并按会话路由表精准投递。
func (r *StreamRelay) PublishRuntimeEvent(event RuntimeEvent) {
	if r == nil {
		return
	}

	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return
	}
	runID := strings.TrimSpace(event.RunID)

	eventFrame := MessageFrame{
		Type:      FrameTypeEvent,
		Action:    FrameActionRun,
		SessionID: sessionID,
		RunID:     runID,
		Payload: map[string]any{
			"event_type": event.Type,
			"payload":    event.Payload,
		},
	}
	notification := protocol.NewJSONRPCNotification(protocol.MethodGatewayEvent, eventFrame)

	for _, connectionID := range r.matchConnectionsForEvent(sessionID, runID) {
		r.sendEventNotification(connectionID, notification)
	}
}

// runConnectionWriter 串行消费连接发送队列并执行底层写入，保证单连接单写协程。
func (r *StreamRelay) runConnectionWriter(connection *relayConnection) {
	if connection == nil {
		return
	}

	defer r.unregisterConnection(connection.id, false)

	for {
		select {
		case <-connection.ctx.Done():
			return
		case message, ok := <-connection.queue:
			if !ok {
				return
			}
			if err := r.writeConnectionMessage(connection, message); err != nil {
				r.logger.Printf("connection %s write failed: %v", connection.id, err)
				if r.metrics != nil {
					r.metrics.IncStreamDropped("write_failed")
				}
				r.dropConnection(connection.id)
				return
			}
		}
	}
}

// writeConnectionMessage 复用每连接单写互斥，保证队列写与同步写不会并发交错。
func (r *StreamRelay) writeConnectionMessage(connection *relayConnection, message RelayMessage) error {
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	return connection.writeFn(message)
}

// enqueueMessage 将消息尝试放入连接队列；队列满会触发慢连接剔除。
func (r *StreamRelay) enqueueMessage(connectionID ConnectionID, message RelayMessage) bool {
	if r == nil {
		return false
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return false
	}

	r.mu.RLock()
	connection := r.connections[normalizedConnectionID]
	r.mu.RUnlock()
	if connection == nil {
		return false
	}

	select {
	case connection.queue <- message:
		return true
	default:
		r.logger.Printf("connection %s queue is full, dropping slow connection", normalizedConnectionID)
		if r.metrics != nil {
			r.metrics.IncStreamDropped("queue_full")
		}
		r.dropConnection(normalizedConnectionID)
		return false
	}
}

// sendEventNotification 按连接通道选择合适封装发送 gateway.event 通知。
func (r *StreamRelay) sendEventNotification(connectionID ConnectionID, notification protocol.JSONRPCNotification) {
	channel, exists := r.connectionChannel(connectionID)
	if !exists {
		return
	}

	if channel == StreamChannelSSE {
		_ = r.SendSSEEvent(connectionID, protocol.MethodGatewayEvent, notification)
		return
	}
	_ = r.SendJSONRPCPayload(connectionID, notification)
}

// connectionChannel 返回连接所属通道类型。
func (r *StreamRelay) connectionChannel(connectionID ConnectionID) (StreamChannel, bool) {
	if r == nil {
		return "", false
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return "", false
	}

	r.mu.RLock()
	connection := r.connections[normalizedConnectionID]
	r.mu.RUnlock()
	if connection == nil {
		return "", false
	}
	return connection.channel, true
}

// dropConnection 主动剔除连接并触发取消、关闭与路由清理。
func (r *StreamRelay) dropConnection(connectionID ConnectionID) {
	connection := r.unregisterConnection(connectionID, true)
	if connection == nil {
		return
	}
}

// unregisterConnection 从中继器注销连接并回收所有路由索引。
func (r *StreamRelay) unregisterConnection(connectionID ConnectionID, shouldClose bool) *relayConnection {
	if r == nil {
		return nil
	}

	normalizedConnectionID := NormalizeConnectionID(connectionID)
	if normalizedConnectionID == "" {
		return nil
	}

	r.mu.Lock()
	connection := r.connections[normalizedConnectionID]
	if connection == nil {
		r.mu.Unlock()
		return nil
	}

	delete(r.connections, normalizedConnectionID)

	connectionBindingMap := r.connectionBindings[normalizedConnectionID]
	delete(r.connectionBindings, normalizedConnectionID)
	for _, state := range connectionBindingMap {
		if state == nil {
			continue
		}
		r.removeConnectionFromIndexesLocked(normalizedConnectionID, state.sessionID, state.runID)
	}
	r.updateActiveConnectionMetricsLocked()
	r.mu.Unlock()

	if shouldClose {
		connection.cancel()
		connection.closeFn()
	}
	return connection
}

// updateActiveConnectionMetricsLocked 在持锁状态下刷新连接活跃数指标。
func (r *StreamRelay) updateActiveConnectionMetricsLocked() {
	if r.metrics == nil {
		return
	}
	counts := map[StreamChannel]int{
		StreamChannelIPC: 0,
		StreamChannelWS:  0,
		StreamChannelSSE: 0,
	}
	for _, connection := range r.connections {
		if connection == nil {
			continue
		}
		counts[connection.channel]++
	}
	r.metrics.SetConnectionsActive(string(StreamChannelIPC), counts[StreamChannelIPC])
	r.metrics.SetConnectionsActive(string(StreamChannelWS), counts[StreamChannelWS])
	r.metrics.SetConnectionsActive(string(StreamChannelSSE), counts[StreamChannelSSE])
}

// runCleanupLoop 周期性扫描并清理过期绑定，避免路由表长期膨胀。
func (r *StreamRelay) runCleanupLoop(ctx context.Context, generation uint64) {
	ticker := time.NewTicker(r.cleanupInterval)
	defer ticker.Stop()
	defer func() {
		r.mu.Lock()
		if r.cleanupLoopGeneration == generation {
			r.cleanupLoopCancel = nil
			r.cleanupStarted = false
		}
		r.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.cleanupExpiredBindings()
		}
	}
}

// runRuntimeEventLoop 订阅运行时事件流并触发中继投递。
func (r *StreamRelay) runRuntimeEventLoop(ctx context.Context, runtimePort RuntimePort, generation uint64) {
	defer func() {
		r.mu.Lock()
		if r.eventLoopGeneration == generation {
			r.runtimeEventLoopCancel = nil
			r.eventPumpStarted = false
		}
		r.mu.Unlock()
	}()

	if runtimePort == nil {
		return
	}
	eventChannel := runtimePort.Events()
	if eventChannel == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChannel:
			if !ok {
				return
			}
			r.PublishRuntimeEvent(event)
		}
	}
}

// cleanupExpiredBindings 移除所有到期绑定并同步更新反向索引。
func (r *StreamRelay) cleanupExpiredBindings() {
	if r == nil {
		return
	}

	now := time.Now()
	r.mu.Lock()
	for connectionID, connectionBindingMap := range r.connectionBindings {
		for key, state := range connectionBindingMap {
			if state == nil || state.expireAt.Before(now) {
				delete(connectionBindingMap, key)
				if state != nil {
					r.removeConnectionFromIndexesLocked(connectionID, state.sessionID, state.runID)
				}
			}
		}
		if len(connectionBindingMap) == 0 {
			delete(r.connectionBindings, connectionID)
		}
	}
	r.mu.Unlock()
}

// matchConnectionsForEvent 返回满足 session/run/channel 条件的目标连接列表。
func (r *StreamRelay) matchConnectionsForEvent(sessionID, runID string) []ConnectionID {
	if r == nil {
		return nil
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil
	}
	normalizedRunID := strings.TrimSpace(runID)
	now := time.Now()

	r.mu.RLock()
	candidateSet := make(map[ConnectionID]struct{})
	for connectionID := range r.sessionIndex[normalizedSessionID] {
		candidateSet[connectionID] = struct{}{}
	}
	if normalizedRunID != "" {
		for connectionID := range r.sessionRunIndex[buildSessionRunKey(normalizedSessionID, normalizedRunID)] {
			candidateSet[connectionID] = struct{}{}
		}
	}

	matched := make([]ConnectionID, 0, len(candidateSet))
	for connectionID := range candidateSet {
		connection := r.connections[connectionID]
		if connection == nil {
			continue
		}
		if r.connectionMatchesEventLocked(connectionID, connection.channel, normalizedSessionID, normalizedRunID, now) {
			matched = append(matched, connectionID)
		}
	}
	r.mu.RUnlock()
	return matched
}

// connectionMatchesEventLocked 判断连接在当前时刻是否命中目标事件的路由条件。
func (r *StreamRelay) connectionMatchesEventLocked(
	connectionID ConnectionID,
	connectionChannel StreamChannel,
	sessionID string,
	runID string,
	now time.Time,
) bool {
	connectionBindingMap := r.connectionBindings[connectionID]
	for _, state := range connectionBindingMap {
		if state == nil {
			continue
		}
		if state.expireAt.Before(now) {
			continue
		}
		if state.sessionID != sessionID {
			continue
		}
		if state.channel != StreamChannelAll && state.channel != connectionChannel {
			continue
		}
		if runID == "" {
			if state.runID == "" {
				return true
			}
			continue
		}
		if state.runID == "" || state.runID == runID {
			return true
		}
	}
	return false
}

// addConnectionToSessionIndexLocked 将连接加入 session 维度索引。
func (r *StreamRelay) addConnectionToSessionIndexLocked(sessionID string, connectionID ConnectionID) {
	sessionSet := r.sessionIndex[sessionID]
	if sessionSet == nil {
		sessionSet = make(map[ConnectionID]struct{})
		r.sessionIndex[sessionID] = sessionSet
	}
	sessionSet[connectionID] = struct{}{}
}

// addConnectionToSessionRunIndexLocked 将连接加入 session+run 维度索引。
func (r *StreamRelay) addConnectionToSessionRunIndexLocked(sessionID, runID string, connectionID ConnectionID) {
	runSet := r.sessionRunIndex[buildSessionRunKey(sessionID, runID)]
	if runSet == nil {
		runSet = make(map[ConnectionID]struct{})
		r.sessionRunIndex[buildSessionRunKey(sessionID, runID)] = runSet
	}
	runSet[connectionID] = struct{}{}
}

// removeConnectionFromIndexesLocked 将连接从 session 与 session+run 索引中移除。
func (r *StreamRelay) removeConnectionFromIndexesLocked(connectionID ConnectionID, sessionID, runID string) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID != "" {
		if sessionSet := r.sessionIndex[normalizedSessionID]; sessionSet != nil {
			delete(sessionSet, connectionID)
			if len(sessionSet) == 0 {
				delete(r.sessionIndex, normalizedSessionID)
			}
		}
	}

	normalizedRunID := strings.TrimSpace(runID)
	if normalizedSessionID != "" && normalizedRunID != "" {
		runKey := buildSessionRunKey(normalizedSessionID, normalizedRunID)
		if runSet := r.sessionRunIndex[runKey]; runSet != nil {
			delete(runSet, connectionID)
			if len(runSet) == 0 {
				delete(r.sessionRunIndex, runKey)
			}
		}
	}
}

// buildSessionRunKey 构建 session+run 组合索引键。
func buildSessionRunKey(sessionID, runID string) string {
	return strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(runID)
}

// extractSessionIDFromPayload 尝试从不同 payload 结构中提取 session_id 字段。
func extractSessionIDFromPayload(payload any) string {
	switch typed := payload.(type) {
	case protocol.WakeIntent:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.WakeIntent:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.BindStreamParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.BindStreamParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.CreateSessionParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.CreateSessionParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.ActivateSessionSkillParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.ActivateSessionSkillParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.DeactivateSessionSkillParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.DeactivateSessionSkillParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.ListSessionSkillsParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.ListSessionSkillsParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.ListSessionTodosParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.ListSessionTodosParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case protocol.ListAvailableSkillsParams:
		return strings.TrimSpace(typed.SessionID)
	case *protocol.ListAvailableSkillsParams:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(typed.SessionID)
	case map[string]any:
		if rawSessionID, exists := typed["session_id"]; exists {
			if sessionID, ok := rawSessionID.(string); ok {
				return strings.TrimSpace(sessionID)
			}
		}
	}
	return ""
}
