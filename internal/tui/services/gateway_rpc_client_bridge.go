package services

import gatewayclient "neo-code/internal/gateway/client"

// GatewayRPCClientOptions 复用 gateway client 包的客户端选项定义。
type GatewayRPCClientOptions = gatewayclient.GatewayRPCClientOptions

// GatewayRPCCallOptions 复用 gateway client 包的单次调用选项定义。
type GatewayRPCCallOptions = gatewayclient.GatewayRPCCallOptions

// GatewayRPCError 复用 gateway client 包的结构化 RPC 错误定义。
type GatewayRPCError = gatewayclient.GatewayRPCError

// GatewayRPCClient 复用 gateway client 包的客户端类型。
type GatewayRPCClient = gatewayclient.GatewayRPCClient

// gatewayRPCNotification 复用网关通知负载结构，保持 TUI 流处理器签名稳定。
type gatewayRPCNotification = gatewayclient.Notification

const defaultGatewayRPCRetryCount = gatewayclient.DefaultGatewayRPCRetryCount

// NewGatewayRPCClient 创建并返回网关 RPC 客户端。
func NewGatewayRPCClient(options GatewayRPCClientOptions) (*GatewayRPCClient, error) {
	return gatewayclient.NewGatewayRPCClient(options)
}
