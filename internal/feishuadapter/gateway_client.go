package feishuadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

// GatewayClientConfig 表示构造网关 RPC 客户端时使用的参数。
type GatewayClientConfig struct {
	ListenAddress  string
	TokenFile      string
	RequestTimeout time.Duration
}

type gatewayRPCClient struct {
	client *gatewayclient.GatewayRPCClient
}

// NewGatewayRPCClient 基于现有网关 JSON-RPC 客户端构造 Feishu 适配层网关客户端。
func NewGatewayRPCClient(cfg GatewayClientConfig) (GatewayClient, error) {
	client, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress:  cfg.ListenAddress,
		TokenFile:      cfg.TokenFile,
		RequestTimeout: cfg.RequestTimeout,
		RetryCount:     gatewayclient.DefaultGatewayRPCRetryCount,
	})
	if err != nil {
		return nil, err
	}
	return &gatewayRPCClient{client: client}, nil
}

// Authenticate 建立网关认证状态。
func (c *gatewayRPCClient) Authenticate(ctx context.Context) error {
	return c.client.Authenticate(ctx)
}

// BindStream 绑定网关事件流订阅。
func (c *gatewayRPCClient) BindStream(ctx context.Context, sessionID string, runID string) error {
	var result map[string]any
	return c.client.Call(ctx, protocol.MethodGatewayBindStream, protocol.BindStreamParams{
		SessionID: sessionID,
		RunID:     runID,
		Channel:   "all",
	}, &result)
}

// Run 提交一次网关运行请求。
func (c *gatewayRPCClient) Run(ctx context.Context, sessionID string, runID string, inputText string) error {
	var result map[string]any
	return c.client.Call(ctx, protocol.MethodGatewayRun, protocol.RunParams{
		SessionID: sessionID,
		RunID:     runID,
		InputText: inputText,
	}, &result)
}

// ResolvePermission 提交一次审批决策。
func (c *gatewayRPCClient) ResolvePermission(ctx context.Context, requestID string, decision string) error {
	var result map[string]any
	return c.client.Call(ctx, protocol.MethodGatewayResolvePermission, protocol.ResolvePermissionParams{
		RequestID: requestID,
		Decision:  decision,
	}, &result)
}

// Ping 调用网关保活接口，触发自动重连与链路健康检查。
func (c *gatewayRPCClient) Ping(ctx context.Context) error {
	var result map[string]any
	return c.client.Call(ctx, protocol.MethodGatewayPing, map[string]any{}, &result)
}

// Notifications 返回网关原始通知流。
func (c *gatewayRPCClient) Notifications() <-chan GatewayNotification {
	source := c.client.Notifications()
	out := make(chan GatewayNotification, 64)
	go func() {
		defer close(out)
		for notification := range source {
			out <- GatewayNotification{
				Method: notification.Method,
				Params: cloneRawMessage(notification.Params),
			}
		}
	}()
	return out
}

// Close 关闭网关客户端连接。
func (c *gatewayRPCClient) Close() error {
	return c.client.Close()
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return cloned
}

// parseGatewayRuntimeEvent 从 gateway.event 通知中解析事件类型与运行字段。
func parseGatewayRuntimeEvent(raw json.RawMessage) (string, string, string, map[string]any, error) {
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		return "", "", "", nil, fmt.Errorf("decode gateway event frame: %w", err)
	}
	sessionID := readString(frame, "session_id")
	runID := readString(frame, "run_id")
	payload, _ := frame["payload"].(map[string]any)
	if payload == nil {
		return "", sessionID, runID, nil, nil
	}
	eventType := readString(payload, "event_type")
	envelope, _ := payload["payload"].(map[string]any)
	return eventType, sessionID, runID, envelope, nil
}

// readString 从松散 map 中读取字符串字段并做空值兜底。
func readString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	value, _ := raw.(string)
	return value
}
