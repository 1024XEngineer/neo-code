package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"neo-code/internal/gateway/protocol"
	"neo-code/internal/security"
)

// PendingToolCall 表示一个已分发到 runner 但尚未收到结果的工具调用。
type PendingToolCall struct {
	RequestID  string
	SessionID  string
	RunID      string
	ToolCallID string
	ToolName   string
	ResultChan chan toolResultEnvelope
	CreatedAt  time.Time
	Deadline   time.Time
}

type toolResultEnvelope struct {
	Content string
	IsError bool
}

// RunnerToolManager 负责将工具调用分发到 runner 并收集结果。
type RunnerToolManager struct {
	mu             sync.Mutex
	pending        map[string]*PendingToolCall // keyed by requestID
	registry       *RunnerRegistry
	relay          *StreamRelay
	capabilitySigner *security.CapabilitySigner
	timeout        time.Duration
	logger         *log.Logger
	sequence       atomic.Uint64
}

// NewRunnerToolManager 创建 runner 工具管理器。
func NewRunnerToolManager(registry *RunnerRegistry, relay *StreamRelay, signer *security.CapabilitySigner, timeout time.Duration, logger *log.Logger) *RunnerToolManager {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if logger == nil {
		logger = log.Default()
	}
	return &RunnerToolManager{
		pending:           make(map[string]*PendingToolCall),
		registry:          registry,
		relay:             relay,
		capabilitySigner:  signer,
		timeout:           timeout,
		logger:            logger,
	}
}

// DispatchToolRequest 将工具调用分发到绑定到 session 的 runner。
func (m *RunnerToolManager) DispatchToolRequest(ctx context.Context, sessionID string, runID string, toolCallID string, toolName string, arguments json.RawMessage) (string, bool, error) {
	connectionID, ok := m.registry.LookupBySession(sessionID)
	if !ok || !m.registry.IsOnline(connectionID) {
		return "", false, fmt.Errorf("runner not online for session %s", sessionID)
	}

	requestID := m.generateRequestID()
	deadline := time.Now().Add(m.timeout)

	resultChan := make(chan toolResultEnvelope, 1)
	pending := &PendingToolCall{
		RequestID:  requestID,
		SessionID:  sessionID,
		RunID:      runID,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		ResultChan: resultChan,
		CreatedAt:  time.Now(),
		Deadline:   deadline,
	}

	m.mu.Lock()
	m.pending[requestID] = pending
	m.mu.Unlock()

	// 构建通知并推送到 runner 连接
	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  protocol.MethodGatewayToolRequest,
		"params": map[string]any{
			"request_id":   requestID,
			"session_id":   sessionID,
			"run_id":       runID,
			"tool_call_id": toolCallID,
			"tool_name":    toolName,
			"arguments":    arguments,
		},
	}
	if !m.relay.SendJSONRPCPayload(connectionID, notification) {
		m.mu.Lock()
		delete(m.pending, requestID)
		m.mu.Unlock()
		return "", false, fmt.Errorf("failed to send tool request to runner")
	}

	// 等待结果或超时
	select {
	case <-ctx.Done():
		m.cleanupPending(requestID)
		return "", false, ctx.Err()
	case result := <-resultChan:
		return result.Content, result.IsError, nil
	case <-time.After(m.timeout):
		m.cleanupPending(requestID)
		return "", false, fmt.Errorf("tool execution timed out waiting for runner")
	}
}

// CompleteToolRequest 完成一个待处理的工具调用。
func (m *RunnerToolManager) CompleteToolRequest(requestID string, content string, isError bool) error {
	m.mu.Lock()
	pending, exists := m.pending[requestID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("no pending tool call for request_id=%s", requestID)
	}
	delete(m.pending, requestID)
	m.mu.Unlock()

	select {
	case pending.ResultChan <- toolResultEnvelope{Content: content, IsError: isError}:
		return nil
	default:
		return fmt.Errorf("result channel full for request_id=%s", requestID)
	}
}

// cleanupPending 清理超时的待处理工具调用。
func (m *RunnerToolManager) cleanupPending(requestID string) {
	m.mu.Lock()
	delete(m.pending, requestID)
	m.mu.Unlock()
}

// CleanupLoop 定期清理超时的待处理工具调用。
func (m *RunnerToolManager) CleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanupExpired()
		}
	}
}

func (m *RunnerToolManager) cleanupExpired() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	for requestID, pending := range m.pending {
		if now.After(pending.Deadline) {
			// 通知超时
			select {
			case pending.ResultChan <- toolResultEnvelope{IsError: true, Content: "tool execution timed out"}:
			default:
			}
			delete(m.pending, requestID)
		}
	}
}

// generateRequestID 生成唯一请求 ID。
func (m *RunnerToolManager) generateRequestID() string {
	seq := m.sequence.Add(1)
	return fmt.Sprintf("req_%d_%d", time.Now().UnixNano(), seq)
}

// NewCapabilityToken 为 runner 工具调用签发 capability token。
func (m *RunnerToolManager) NewCapabilityToken(sessionID string, runID string, toolName string, workdir string) (*security.CapabilityToken, error) {
	if m.capabilitySigner == nil {
		return nil, nil // signer 未配置时允许无 token 执行
	}

	now := time.Now().UTC()
	token := security.CapabilityToken{
		ID:            m.generateRequestID(),
		TaskID:        runID,
		AgentID:       sessionID,
		IssuedAt:      now,
		ExpiresAt:     now.Add(5 * time.Minute),
		AllowedTools:  []string{toolName},
		AllowedPaths:  []string{strings.TrimSpace(workdir)},
		NetworkPolicy: security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
	}

	signed, err := m.capabilitySigner.Sign(token)
	if err != nil {
		return nil, fmt.Errorf("sign capability token: %w", err)
	}
	return &signed, nil
}
