package ptyproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

// demuxGatewayNotifications 将网关通知拆分为 runtime 事件流与控制通知流，避免多方竞争同一通知通道。
func demuxGatewayNotifications(
	ctx context.Context,
	source <-chan gatewayclient.Notification,
	eventSink chan<- gatewayclient.Notification,
	controlSink chan<- gatewayclient.Notification,
) {
	if source == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-source:
			if !ok {
				return
			}
			switch strings.TrimSpace(notification.Method) {
			case protocol.MethodGatewayEvent:
				if !forwardGatewayNotification(ctx, eventSink, notification) {
					return
				}
			case protocol.MethodGatewayNotification:
				if !forwardGatewayNotification(ctx, controlSink, notification) {
					return
				}
			}
		}
	}
}

// forwardGatewayNotification 在上下文可用时转发通知，避免退出阶段 goroutine 堵塞。
func forwardGatewayNotification(
	ctx context.Context,
	target chan<- gatewayclient.Notification,
	notification gatewayclient.Notification,
) bool {
	if target == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case target <- notification:
		return true
	}
}

// decodeGatewayNotificationPayload 解码 gateway.notification 参数载荷。
func decodeGatewayNotificationPayload(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

// readMapString 从 map 中读取字符串字段。
func readMapString(container map[string]any, key string) string {
	if container == nil {
		return ""
	}
	value, exists := container[strings.TrimSpace(key)]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
