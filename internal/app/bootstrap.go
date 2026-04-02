package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/security"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/filesystem"
	"neo-code/internal/tools/webfetch"
	"neo-code/internal/tui"
)

func NewProgram(ctx context.Context) (*tea.Program, error) {
	loader := config.NewLoader("", builtin.DefaultConfig())
	manager := config.NewManager(loader)
	if _, err := manager.Load(ctx); err != nil {
		return nil, err
	}

	providerRegistry, err := builtin.NewRegistry()
	if err != nil {
		return nil, err
	}
	providerService := provider.NewService(manager, providerRegistry, nil)
	if _, err := providerService.EnsureSelection(ctx); err != nil {
		return nil, err
	}

	cfg := manager.Get()

	toolRegistry := buildToolRegistry(cfg)
	toolManager, err := buildToolManager(toolRegistry)
	if err != nil {
		return nil, err
	}

	sessionStore := agentruntime.NewSessionStore(loader.BaseDir())
	runtimeSvc := agentruntime.NewWithFactory(
		manager,
		toolManager,
		sessionStore,
		providerService,
		agentcontext.NewBuilder(),
	)

	tuiApp, err := tui.New(&cfg, manager, runtimeSvc, providerService)
	if err != nil {
		return nil, err
	}
	return tea.NewProgram(
		tuiApp,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	), nil
}

func buildToolRegistry(cfg config.Config) *tools.Registry {
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(filesystem.New(cfg.Workdir))
	toolRegistry.Register(filesystem.NewWrite(cfg.Workdir))
	toolRegistry.Register(filesystem.NewGrep(cfg.Workdir))
	toolRegistry.Register(filesystem.NewGlob(cfg.Workdir))
	toolRegistry.Register(filesystem.NewEdit(cfg.Workdir))
	toolRegistry.Register(bash.New(cfg.Workdir, cfg.Shell, time.Duration(cfg.ToolTimeoutSec)*time.Second))
	toolRegistry.Register(webfetch.New(webfetch.Config{
		Timeout:               time.Duration(cfg.ToolTimeoutSec) * time.Second,
		MaxResponseBytes:      cfg.Tools.WebFetch.MaxResponseBytes,
		SupportedContentTypes: cfg.Tools.WebFetch.SupportedContentTypes,
	}))
	return toolRegistry
}

func buildToolManager(registry *tools.Registry) (tools.Manager, error) {
	engine, err := security.NewStaticGateway(security.DecisionAllow, nil)
	if err != nil {
		return nil, err
	}
	return tools.NewManager(registry, engine, security.NewWorkspaceSandbox())
}
