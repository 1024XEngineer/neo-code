package tui

import (
	"errors"
	"strings"

	"neo-code/internal/gateway/protocol"
	tuiservices "neo-code/internal/tui/services"
)

// isGatewayUnsupportedActionError 统一判断网关错误是否表示“当前动作不受支持”。
func isGatewayUnsupportedActionError(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *tuiservices.GatewayRPCError
	return errors.As(err, &rpcErr) &&
		rpcErr != nil &&
		strings.EqualFold(strings.TrimSpace(rpcErr.GatewayCode), protocol.GatewayCodeUnsupportedAction)
}
