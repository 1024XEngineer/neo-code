package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// StreamChannel 表示连接所属的流式通道类型。
type StreamChannel string

const (
	// StreamChannelAll 表示绑定对当前连接所属通道不过滤。
	StreamChannelAll StreamChannel = "all"
	// StreamChannelIPC 表示绑定仅用于本地 IPC 连接。
	StreamChannelIPC StreamChannel = "ipc"
	// StreamChannelWS 表示绑定仅用于 WebSocket 连接。
	StreamChannelWS StreamChannel = "ws"
	// StreamChannelSSE 表示绑定仅用于 SSE 连接。
	StreamChannelSSE StreamChannel = "sse"
)

// ConnectionID 表示网关侧分配给物理连接的全局唯一标识。
type ConnectionID string

type connectionIDContextKey struct{}
type streamRelayContextKey struct{}

var (
	connectionSequence   uint64
	connectionStartEpoch = time.Now().Unix()
)

// NewConnectionID 生成全局唯一 ConnectionID，用于连接绑定和路由兜底。
func NewConnectionID() ConnectionID {
	sequence := atomic.AddUint64(&connectionSequence, 1)
	return ConnectionID(fmt.Sprintf("cid_%d_%d", connectionStartEpoch, sequence))
}

// WithConnectionID 将 ConnectionID 注入上下文，供后续路由和提取逻辑读取。
func WithConnectionID(ctx context.Context, connectionID ConnectionID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, connectionIDContextKey{}, NormalizeConnectionID(connectionID))
}

// ConnectionIDFromContext 从上下文读取 ConnectionID。
func ConnectionIDFromContext(ctx context.Context) (ConnectionID, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(connectionIDContextKey{}).(ConnectionID)
	if !ok {
		return "", false
	}
	value = NormalizeConnectionID(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// WithStreamRelay 将流式中继实例注入上下文，供请求处理阶段读取。
func WithStreamRelay(ctx context.Context, relay *StreamRelay) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, streamRelayContextKey{}, relay)
}

// StreamRelayFromContext 从上下文中读取流式中继实例。
func StreamRelayFromContext(ctx context.Context) (*StreamRelay, bool) {
	if ctx == nil {
		return nil, false
	}
	relay, ok := ctx.Value(streamRelayContextKey{}).(*StreamRelay)
	if !ok || relay == nil {
		return nil, false
	}
	return relay, true
}

// ParseStreamChannel 解析并校验连接通道参数。
func ParseStreamChannel(raw string) (StreamChannel, bool) {
	normalized := StreamChannel(strings.ToLower(strings.TrimSpace(raw)))
	switch normalized {
	case StreamChannelAll, StreamChannelIPC, StreamChannelWS, StreamChannelSSE:
		return normalized, true
	default:
		return "", false
	}
}

// NormalizeConnectionID 将连接标识归一化为空白裁剪后的稳定值。
func NormalizeConnectionID(connectionID ConnectionID) ConnectionID {
	return ConnectionID(strings.TrimSpace(string(connectionID)))
}
