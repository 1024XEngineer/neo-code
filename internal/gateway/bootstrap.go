package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/gateway/adapters"
	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/gateway/transport"
)

// BootstrapConfig 描述网关进程启动时的最小配置。
type BootstrapConfig struct {
	Transport string
	Endpoint  string
}

// Bootstrap 负责装配并驱动 Gateway IPC 服务器。
type Bootstrap struct {
	server *transport.Server
}

// NewBootstrap 根据配置与 Core 端口装配可运行的网关实例。
func NewBootstrap(cfg BootstrapConfig, core adapters.CoreClient) (*Bootstrap, error) {
	if core == nil {
		core = adapters.NewCoreMock()
	}

	router := protocol.NewRouter()
	pingHandler := handlers.NewPingHandler(core)
	router.Register(protocol.MethodCorePing, pingHandler.Handle)

	server, err := transport.NewServer(transport.Config{
		Mode:     transport.Mode(strings.TrimSpace(cfg.Transport)),
		Endpoint: strings.TrimSpace(cfg.Endpoint),
	}, router.HandleRaw)
	if err != nil {
		return nil, fmt.Errorf("build gateway server: %w", err)
	}

	return &Bootstrap{server: server}, nil
}

// Endpoint 返回网关当前监听地址。
func (b *Bootstrap) Endpoint() string {
	if b == nil || b.server == nil {
		return ""
	}
	return b.server.Endpoint()
}

// Run 启动网关服务并在上下文取消后退出。
func (b *Bootstrap) Run(ctx context.Context) error {
	if b == nil || b.server == nil {
		return errors.New("gateway bootstrap is not initialized")
	}
	return b.server.Serve(ctx)
}

// Close 主动关闭网关服务监听器。
func (b *Bootstrap) Close() error {
	if b == nil || b.server == nil {
		return nil
	}
	return b.server.Close()
}
