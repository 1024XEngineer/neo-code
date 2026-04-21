package gateway

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"neo-code/internal/gateway/protocol"
)

// RequestLogEntry 表示统一结构化请求日志字段。
type RequestLogEntry struct {
	RequestID    string `json:"request_id"`
	SessionID    string `json:"session_id"`
	Method       string `json:"method"`
	Source       string `json:"source"`
	Status       string `json:"status"`
	GatewayCode  string `json:"gateway_code,omitempty"`
	LatencyMS    int64  `json:"latency_ms"`
	ConnectionID string `json:"connection_id,omitempty"`
	AuthState    string `json:"auth_state,omitempty"`
}

// emitRequestLog 输出网关结构化日志。
func emitRequestLog(ctx context.Context, logger *log.Logger, entry RequestLogEntry) {
	if logger == nil {
		return
	}
	if entry.Source == "" {
		entry.Source = string(RequestSourceFromContext(ctx))
	}
	if entry.Source == "" {
		entry.Source = string(RequestSourceUnknown)
	}
	if connectionID, ok := ConnectionIDFromContext(ctx); ok {
		entry.ConnectionID = string(connectionID)
	}
	if authState, ok := ConnectionAuthStateFromContext(ctx); ok && authState.IsAuthenticated() {
		entry.AuthState = "authenticated"
	} else if _, ok := TokenAuthenticatorFromContext(ctx); ok {
		entry.AuthState = "required"
	} else {
		entry.AuthState = "disabled"
	}
	entry.RequestID = strings.TrimSpace(entry.RequestID)
	entry.SessionID = strings.TrimSpace(entry.SessionID)
	entry.Method = strings.TrimSpace(entry.Method)
	if shouldMuteRequestLog(entry.Method) {
		return
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		logger.Printf(`{"status":"error","message":"failed to encode request log"}`)
		return
	}
	logger.Print(string(raw))
}

// shouldMuteRequestLog 判断是否应静音该请求日志，避免高频保活心跳造成日志风暴。
func shouldMuteRequestLog(method string) bool {
	return strings.EqualFold(strings.TrimSpace(method), protocol.MethodGatewayPing)
}

// requestStartTime 返回用于统计请求耗时的起始时间。
func requestStartTime() time.Time {
	return time.Now()
}

// requestLatencyMS 返回请求耗时毫秒值。
func requestLatencyMS(startedAt time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}
	return time.Since(startedAt).Milliseconds()
}
