package cli

import (
	"context"

	"neo-code/internal/app"
)

// ExecuteGatewayServer 执行 gateway-only 独立命令入口，保持与 `neocode gateway` 一致的参数与行为。
func ExecuteGatewayServer(ctx context.Context, args []string) error {
	app.EnsureConsoleUTF8()
	command := NewGatewayStandaloneCommand()
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}
