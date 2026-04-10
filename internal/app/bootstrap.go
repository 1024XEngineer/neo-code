package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider/builtin"
	providercatalog "neo-code/internal/provider/catalog"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/filesystem"
	"neo-code/internal/tools/webfetch"
	"neo-code/internal/tui"
	tuibootstrap "neo-code/internal/tui/bootstrap"
	agentworkspace "neo-code/internal/workspace"
)

const utf8CodePage = 65001

var (
	setConsoleOutputCodePage = platformSetConsoleOutputCodePage
	setConsoleInputCodePage  = platformSetConsoleInputCodePage
)

// BootstrapOptions 描述应用启动时可注入的运行时选项。
type BootstrapOptions struct {
	Workdir string
}

// RuntimeBundle 聚合 CLI 与 TUI 共享的运行时依赖。
type RuntimeBundle struct {
	Config            config.Config
	ConfigManager     *config.Manager
	Runtime           agentruntime.Runtime
	ProviderSelection *config.SelectionService
	WorkspaceRoot     string
	Workdir           string
}

// EnsureConsoleUTF8 负责在 Windows 控制台中尽量启用 UTF-8 编码。
func EnsureConsoleUTF8() {
	if err := setConsoleOutputCodePage(utf8CodePage); err != nil {
		return
	}
	_ = setConsoleInputCodePage(utf8CodePage)
}

// BuildRuntime 构建 CLI 与 TUI 共用的运行时依赖。
func BuildRuntime(ctx context.Context, opts BootstrapOptions) (RuntimeBundle, error) {
	defaultCfg, err := bootstrapDefaultConfig(opts)
	if err != nil {
		return RuntimeBundle{}, err
	}

	loader := config.NewLoader("", defaultCfg)
	manager := config.NewManager(loader)
	if _, err := manager.Load(ctx); err != nil {
		return RuntimeBundle{}, err
	}

	providerRegistry, err := builtin.NewRegistry()
	if err != nil {
		return RuntimeBundle{}, err
	}
	modelCatalogs := providercatalog.NewService(manager.BaseDir(), providerRegistry, nil)
	providerSelection := config.NewSelectionService(manager, providerRegistry, providerRegistry, modelCatalogs)
	if _, err := providerSelection.EnsureSelection(ctx); err != nil {
		return RuntimeBundle{}, err
	}

	cfg := manager.Get()
	workspaceResolution, err := agentworkspace.Resolve(cfg.Workdir)
	if err != nil {
		return RuntimeBundle{}, err
	}
	if cfg.Workdir != workspaceResolution.Workdir {
		if err := manager.Update(ctx, func(cfg *config.Config) error {
			cfg.Workdir = workspaceResolution.Workdir
			return nil
		}); err != nil {
			return RuntimeBundle{}, err
		}
		cfg = manager.Get()
	}

	toolRegistry, err := buildToolRegistry(cfg, workspaceResolution.WorkspaceRoot)
	if err != nil {
		return RuntimeBundle{}, err
	}
	toolManager, err := buildToolManager(toolRegistry)
	if err != nil {
		return RuntimeBundle{}, err
	}

	sessionStore := agentsession.NewStore(loader.BaseDir(), workspaceResolution.WorkspaceRoot)
	runtimeSvc := agentruntime.NewWithWorkspace(
		manager,
		workspaceResolution.WorkspaceRoot,
		workspaceResolution.Workdir,
		toolManager,
		sessionStore,
		providerRegistry,
		agentcontext.NewBuilderWithToolPolicies(toolRegistry),
	)

	return RuntimeBundle{
		Config:            cfg,
		ConfigManager:     manager,
		Runtime:           runtimeSvc,
		ProviderSelection: providerSelection,
		WorkspaceRoot:     workspaceResolution.WorkspaceRoot,
		Workdir:           workspaceResolution.Workdir,
	}, nil
}

// NewProgram 基于共享运行时依赖构建并返回 TUI 程序。
func NewProgram(ctx context.Context, opts BootstrapOptions) (*tea.Program, error) {
	bundle, err := BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}

	tuiApp, err := tui.NewWithBootstrap(tuibootstrap.Options{
		Config:          &bundle.Config,
		ConfigManager:   bundle.ConfigManager,
		Runtime:         bundle.Runtime,
		ProviderService: bundle.ProviderSelection,
		WorkspaceRoot:   bundle.WorkspaceRoot,
		Workdir:         bundle.Workdir,
		RebuildWorkspace: func(ctx context.Context, requestedPath string) (tuibootstrap.WorkspaceBinding, error) {
			rebuilt, err := BuildRuntime(ctx, BootstrapOptions{Workdir: requestedPath})
			if err != nil {
				return tuibootstrap.WorkspaceBinding{}, err
			}
			return tuibootstrap.WorkspaceBinding{
				Config:          rebuilt.Config,
				ConfigManager:   rebuilt.ConfigManager,
				Runtime:         rebuilt.Runtime,
				ProviderService: rebuilt.ProviderSelection,
				WorkspaceRoot:   rebuilt.WorkspaceRoot,
				Workdir:         rebuilt.Workdir,
			}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return tea.NewProgram(
		tuiApp,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	), nil
}

// bootstrapDefaultConfig 负责计算本次启动应使用的默认配置快照。
func bootstrapDefaultConfig(opts BootstrapOptions) (*config.Config, error) {
	defaultCfg := config.DefaultConfig()
	workdir := strings.TrimSpace(opts.Workdir)
	if workdir == "" {
		return defaultCfg, nil
	}

	resolved, err := resolveBootstrapWorkdir(workdir)
	if err != nil {
		return nil, err
	}
	defaultCfg.Workdir = resolved
	return defaultCfg, nil
}

// resolveBootstrapWorkdir 将 CLI 传入的工作区解析为存在的绝对目录。
func resolveBootstrapWorkdir(workdir string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		return "", fmt.Errorf("app: workdir is empty")
	}
	resolved, err := agentworkspace.Resolve(workdir)
	if err != nil {
		return "", fmt.Errorf("app: resolve workdir %q: %w", workdir, err)
	}
	return resolved.Workdir, nil
}

func buildToolRegistry(cfg config.Config, workspaceRoot string) (*tools.Registry, error) {
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(filesystem.New(workspaceRoot))
	toolRegistry.Register(filesystem.NewWrite(workspaceRoot))
	toolRegistry.Register(filesystem.NewGrep(workspaceRoot))
	toolRegistry.Register(filesystem.NewGlob(workspaceRoot))
	toolRegistry.Register(filesystem.NewEdit(workspaceRoot))
	toolRegistry.Register(bash.New(workspaceRoot, cfg.Shell, time.Duration(cfg.ToolTimeoutSec)*time.Second))
	toolRegistry.Register(webfetch.New(webfetch.Config{
		Timeout:               time.Duration(cfg.ToolTimeoutSec) * time.Second,
		MaxResponseBytes:      cfg.Tools.WebFetch.MaxResponseBytes,
		SupportedContentTypes: cfg.Tools.WebFetch.SupportedContentTypes,
	}))
	mcpRegistry, err := buildMCPRegistry(cfg)
	if err != nil {
		return nil, err
	}
	if mcpRegistry != nil {
		toolRegistry.SetMCPRegistry(mcpRegistry)
	}
	return toolRegistry, nil
}

func buildToolManager(registry *tools.Registry) (tools.Manager, error) {
	engine, err := security.NewRecommendedPolicyEngine()
	if err != nil {
		return nil, err
	}
	return tools.NewManager(registry, engine, security.NewWorkspaceSandbox())
}
