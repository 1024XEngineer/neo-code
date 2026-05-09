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

// StreamRole 表示连接在同一会话内声明的角色，用于精准路由控制类通知。
type StreamRole string

const (
	// StreamRoleNone 表示未声明角色，保持与历史客户端兼容。
	StreamRoleNone StreamRole = ""
	// StreamRoleShell 表示 shell 代理连接。
	StreamRoleShell StreamRole = "shell"
	// StreamRoleCLI 表示命令行控制连接。
	StreamRoleCLI StreamRole = "cli"
	// StreamRoleTUI 表示桌面/TUI 控制连接。
	StreamRoleTUI StreamRole = "tui"
)

// ConnectionID 表示网关侧分配给物理连接的全局唯一标识。
type ConnectionID string

type connectionIDContextKey struct{}
type streamRelayContextKey struct{}
type runnerRegistryContextKey struct{}
type runnerToolManagerContextKey struct{}

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

// ParseStreamRole 解析并校验连接角色参数。
func ParseStreamRole(raw string) (StreamRole, bool) {
	normalized := StreamRole(strings.ToLower(strings.TrimSpace(raw)))
	switch normalized {
	case StreamRoleNone, StreamRoleShell, StreamRoleCLI, StreamRoleTUI:
		return normalized, true
	default:
		return "", false
	}
}

// WithRunnerRegistry 将 RunnerRegistry 注入上下文。
func WithRunnerRegistry(ctx context.Context, registry *RunnerRegistry) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runnerRegistryContextKey{}, registry)
}

// RunnerRegistryFromContext 从上下文读取 RunnerRegistry。
func RunnerRegistryFromContext(ctx context.Context) *RunnerRegistry {
	if ctx == nil {
		return nil
	}
	registry, _ := ctx.Value(runnerRegistryContextKey{}).(*RunnerRegistry)
	return registry
}

// WithRunnerToolManager 将 RunnerToolManager 注入上下文。
func WithRunnerToolManager(ctx context.Context, manager *RunnerToolManager) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runnerToolManagerContextKey{}, manager)
}

// RunnerToolManagerFromContext 从上下文读取 RunnerToolManager。
func RunnerToolManagerFromContext(ctx context.Context) *RunnerToolManager {
	if ctx == nil {
		return nil
	}
	manager, _ := ctx.Value(runnerToolManagerContextKey{}).(*RunnerToolManager)
	return manager
}

// NormalizeConnectionID 将连接标识归一化为空白裁剪后的稳定值。
func NormalizeConnectionID(connectionID ConnectionID) ConnectionID {
	return ConnectionID(strings.TrimSpace(string(connectionID)))
}
