package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/memo"
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
	ProviderSelection *configstate.Service
	MemoService       *memo.Service
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
	providerSelection := configstate.NewService(manager, providerRegistry, modelCatalogs)
	if _, err := providerSelection.EnsureSelection(ctx); err != nil {
		return RuntimeBundle{}, err
	}

	cfg := manager.Get()

	toolRegistry, err := buildToolRegistry(cfg)
	if err != nil {
		return RuntimeBundle{}, err
	}
	toolManager, err := buildToolManager(toolRegistry)
	if err != nil {
		return RuntimeBundle{}, err
	}

	// Session Store 绑定到启动时的 workdir 哈希分桶，整个应用生命周期内不可变。
	// 这意味着所有会话都归属到启动时指定的项目目录下，运行时不会因配置变更而迁移存储位置。
	sessionStore := agentsession.NewStore(loader.BaseDir(), cfg.Workdir)

	var contextBuilder agentcontext.Builder = agentcontext.NewBuilderWithToolPolicies(toolRegistry)
	var memoSvc *memo.Service
	if cfg.Memo.Enabled {
		memoStore := memo.NewFileStore(loader.BaseDir(), cfg.Workdir)
		memoSource := memo.NewContextSource(memoStore)
		var sourceInvl func()
		if invalidator, ok := memoSource.(interface{ InvalidateCache() }); ok {
			sourceInvl = invalidator.InvalidateCache
		}
		contextBuilder = agentcontext.NewBuilderWithMemo(toolRegistry, memoSource)
		memoSvc = memo.NewService(memoStore, nil, cfg.Memo, sourceInvl)
	}

	runtimeSvc := agentruntime.NewWithFactory(
		manager,
		toolManager,
		sessionStore,
		providerRegistry,
		contextBuilder,
	)

	return RuntimeBundle{
		Config:            cfg,
		ConfigManager:     manager,
		Runtime:           runtimeSvc,
		ProviderSelection: providerSelection,
		MemoService:       memoSvc,
	}, nil
}

// NewProgram 基于共享运行时依赖构建并返回 TUI 程序。
func NewProgram(ctx context.Context, opts BootstrapOptions) (*tea.Program, error) {
	bundle, err := BuildRuntime(ctx, opts)
	if err != nil {
		return nil, err
	}

	tuiApp, err := tui.NewWithMemo(&bundle.Config, bundle.ConfigManager, bundle.Runtime, bundle.ProviderSelection, bundle.MemoService)
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
	defaultCfg := config.StaticDefaults()
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
	return agentsession.ResolveExistingDir(workdir)
}

func buildToolRegistry(cfg config.Config) (*tools.Registry, error) {
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
