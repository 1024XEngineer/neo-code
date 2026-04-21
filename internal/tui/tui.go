package tui

import (
	"neo-code/internal/config"
	"neo-code/internal/memo"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiapp "neo-code/internal/tui/core/app"
	tuiservices "neo-code/internal/tui/services"
)

type App = tuiapp.App
type ProviderController = tuiapp.ProviderController

// New 保留 internal/tui 对外入口，内部实现转发到分层后的 core/app。
func New(cfg *config.Config, configManager *config.Manager, runtime tuiservices.Runtime, providerSvc ProviderController) (App, error) {
	return tuiapp.New(cfg, configManager, runtime, providerSvc)
}

// NewWithMemo 创建带 memo 服务的 TUI App。
func NewWithMemo(
	cfg *config.Config,
	configManager *config.Manager,
	runtime tuiservices.Runtime,
	providerSvc ProviderController,
	memoSvc *memo.Service,
) (App, error) {
	return tuiapp.NewWithMemo(cfg, configManager, runtime, providerSvc, memoSvc)
}

// NewWithBootstrap 保留对外注入入口，内部转发到 core/app。
func NewWithBootstrap(options tuibootstrap.Options) (App, error) {
	return tuiapp.NewWithBootstrap(options)
}
