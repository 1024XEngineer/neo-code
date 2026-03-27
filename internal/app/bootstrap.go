package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dust/neo-code/internal/config"
	agentruntime "github.com/dust/neo-code/internal/runtime"
	"github.com/dust/neo-code/internal/tools"
	"github.com/dust/neo-code/internal/tools/bash"
	"github.com/dust/neo-code/internal/tools/filesystem"
	"github.com/dust/neo-code/internal/tools/webfetch"
	"github.com/dust/neo-code/internal/tui"
)

func NewProgram(ctx context.Context) (*tea.Program, error) {
	loader := config.NewLoader("")
	manager := config.NewManager(loader)
	cfg, err := manager.Load(ctx)
	if err != nil {
		return nil, err
	}

	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(filesystem.New(cfg.Workdir))
	toolRegistry.Register(filesystem.NewWrite(cfg.Workdir))
	toolRegistry.Register(filesystem.NewGrep(cfg.Workdir))
	toolRegistry.Register(filesystem.NewGlob(cfg.Workdir))
	toolRegistry.Register(filesystem.NewEdit(cfg.Workdir))
	toolRegistry.Register(bash.New(cfg.Workdir, cfg.Shell, time.Duration(cfg.ToolTimeoutSec)*time.Second))
	toolRegistry.Register(webfetch.New(time.Duration(cfg.ToolTimeoutSec) * time.Second))

	sessionStore := agentruntime.NewSessionStore(loader.BaseDir())
	runtimeSvc := agentruntime.New(manager, toolRegistry, sessionStore)

	tuiApp, err := tui.New(&cfg, manager, runtimeSvc)
	if err != nil {
		return nil, err
	}
	return tea.NewProgram(
		tuiApp,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	), nil
}
