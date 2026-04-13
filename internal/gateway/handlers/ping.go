package handlers

import (
	"context"

	"neo-code/internal/gateway/adapters"
	"neo-code/internal/gateway/protocol"
)

// PingHandler 负责处理 core.ping 请求并返回基础连通性结果。
type PingHandler struct {
	core adapters.CoreClient
}

// NewPingHandler 创建 ping 请求处理器。
func NewPingHandler(core adapters.CoreClient) *PingHandler {
	return &PingHandler{core: core}
}

// Handle 执行 ping 请求并把结果映射为 JSON-RPC 响应。
func (h *PingHandler) Handle(ctx context.Context, req protocol.Request) protocol.Response {
	if h == nil || h.core == nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrorCodeInternalError, "core client unavailable")
	}

	pong, err := h.core.Ping(ctx)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrorCodeInternalError, "core ping failed")
	}

	return protocol.NewSuccessResponse(req.ID, pong)
}
