package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider/builtin"
	providercatalog "neo-code/internal/provider/catalog"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/security"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/filesystem"
	"neo-code/internal/tools/webfetch"
	"neo-code/internal/tui"
)

const utf8CodePage = 65001

var (
	setConsoleOutputCodePage = platformSetConsoleOutputCodePage
	setConsoleInputCodePage  = platformSetConsoleInputCodePage
)

// ensureConsoleUTF8 is best-effort and should never block app startup.
func ensureConsoleUTF8() {
	if err := setConsoleOutputCodePage(utf8CodePage); err != nil {
		return
	}
	_ = setConsoleInputCodePage(utf8CodePage)
}

func NewProgram(ctx context.Context) (*tea.Program, error) {

	ensureConsoleUTF8()

	loader := config.NewLoader("", config.DefaultConfig())
	manager := config.NewManager(loader)
	if _, err := manager.Load(ctx); err != nil {
		return nil, err
	}

	providerRegistry, err := builtin.NewRegistry()
	if err != nil {
		return nil, err
	}
	modelCatalogs := providercatalog.NewService(manager.BaseDir(), providerRegistry, nil)
	providerSelection := config.NewSelectionService(manager, providerRegistry, providerRegistry, modelCatalogs)
	if _, err := providerSelection.EnsureSelection(ctx); err != nil {
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
		providerRegistry,
		agentcontext.NewBuilder(),
	)

	tuiApp, err := tui.New(&cfg, manager, runtimeSvc, providerSelection)
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
	engine, err := security.NewRecommendedPolicyEngine()
	if err != nil {
		return nil, err
	}
	return tools.NewManager(registry, engine, security.NewWorkspaceSandbox())
}
