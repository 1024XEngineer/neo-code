package tui

import (
	"neo-code/internal/config"
	agentruntime "neo-code/internal/runtime"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	tuiapp "neo-code/internal/tui/core/app"
)

type App = tuiapp.App
type ProviderController = tuiapp.ProviderController
type WorkspaceSwitcher = tuiapp.WorkspaceSwitcher

// New 保留 internal/tui 对外入口，内部实现转发到分层后的 core/app。
func New(
	cfg *config.Config,
	configManager *config.Manager,
	runtime agentruntime.Runtime,
	providerSvc ProviderController,
	workspaceSwitcher WorkspaceSwitcher,
) (App, error) {
	return tuiapp.New(cfg, configManager, runtime, providerSvc, workspaceSwitcher)
}

// NewWithBootstrap 保留对外注入入口，内部转发到 core/app。
func NewWithBootstrap(options tuibootstrap.Options) (App, error) {
	return tuiapp.NewWithBootstrap(options)
}
