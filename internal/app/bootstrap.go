package app

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/memo"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	providercatalog "neo-code/internal/provider/catalog"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/filesystem"
	"neo-code/internal/tools/mcp"
	memotool "neo-code/internal/tools/memo"
	"neo-code/internal/tools/todo"
	"neo-code/internal/tools/webfetch"
	"neo-code/internal/tui"
)

const utf8CodePage = 65001

var (
	setConsoleOutputCodePage = platformSetConsoleOutputCodePage
	setConsoleInputCodePage  = platformSetConsoleInputCodePage
	buildToolManagerFunc     = buildToolManager
	newTUIWithMemo           = tui.NewWithMemo
)

// BootstrapOptions 描述应用启动时可注入的运行时选项。
type BootstrapOptions struct {
	Workdir string
}

type memoExtractorScheduler interface {
	ScheduleWithExtractor(sessionID string, messages []providertypes.Message, extractor memo.Extractor)
}

func newMemoExtractorAdapter(
	factory agentruntime.ProviderFactory,
	cm *config.Manager,
	scheduler memoExtractorScheduler,
) agentruntime.MemoExtractor {
	return runtimeMemoExtractorFunc(func(sessionID string, messages []providertypes.Message) {
		if scheduler == nil {
			return
		}

		cfg := cm.Get()
		resolved, err := config.ResolveSelectedProvider(cfg)
		if err != nil {
			log.Printf("memo: resolve selected provider failed: %v", err)
			return
		}

		generator := textGenAdapter(func(ctx context.Context, prompt string, msgs []providertypes.Message) (string, error) {
			p, err := factory.Build(ctx, resolved.ToRuntimeConfig())
			if err != nil {
				return "", err
			}

			return provider.GenerateText(ctx, p, providertypes.GenerateRequest{
				Model:        cfg.CurrentModel,
				SystemPrompt: prompt,
				Messages:     append([]providertypes.Message(nil), msgs...),
			})
		})

		scheduler.ScheduleWithExtractor(sessionID, messages, memo.NewLLMExtractor(generator))
	})
}

// RuntimeBundle 聚合 CLI 与 TUI 共享的运行时依赖。
type RuntimeBundle struct {
	Config            config.Config
	ConfigManager     *config.Manager
	Runtime           agentruntime.Runtime
	ProviderSelection *configstate.Service
	MemoService       *memo.Service
	Close             func() error // 用于清理 bundle 运行期间拉起的系统资源
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

	toolRegistry, toolsCleanup, err := buildToolRegistry(cfg)
	if err != nil {
		return RuntimeBundle{}, err
	}
	needCleanup := true
	defer func() {
		if needCleanup && toolsCleanup != nil {
			_ = toolsCleanup()
		}
	}()

	toolManager, err := buildToolManagerFunc(toolRegistry)
	if err != nil {
		return RuntimeBundle{}, err
	}

	// Session Store 绑定到启动时的 workdir 哈希分桶，整个应用生命周期内不可变。
	// 这意味着所有会话都归属到启动时指定的项目目录下，运行时不会因配置变更而迁移存储位置。
	sessionStore := agentsession.NewStore(loader.BaseDir(), cfg.Workdir)

	// 注册内置工具的内容摘要器，使 micro-compact 在清理旧工具结果时保留关键上下文。
	tools.RegisterBuiltinSummarizers(toolRegistry)

	var contextBuilder agentcontext.Builder = agentcontext.NewBuilderWithToolPoliciesAndSummarizers(toolRegistry, toolRegistry)
	var memoSvc *memo.Service
	if cfg.Memo.Enabled {
		memoStore := memo.NewFileStore(loader.BaseDir(), cfg.Workdir)
		memoSource := memo.NewContextSource(memoStore)
		var sourceInvl func()
		if invalidator, ok := memoSource.(interface{ InvalidateCache() }); ok {
			sourceInvl = invalidator.InvalidateCache
		}
		contextBuilder = agentcontext.NewBuilderWithMemoAndSummarizers(toolRegistry, toolRegistry, memoSource)
		memoSvc = memo.NewService(memoStore, nil, cfg.Memo, sourceInvl)
		toolRegistry.Register(memotool.NewRememberTool(memoSvc))
		toolRegistry.Register(memotool.NewRecallTool(memoSvc))
	}

	runtimeSvc := agentruntime.NewWithFactory(
		manager,
		toolManager,
		sessionStore,
		providerRegistry,
		contextBuilder,
	)
	runtimeSvc.SetSessionAssetStore(sessionStore)
	runtimeSvc.SetSkillsRegistry(buildSkillsRegistry(ctx, loader.BaseDir()))
	runtimeSvc.SetAutoCompactThresholdResolver(runtimeAutoCompactThresholdResolverFunc(
		func(ctx context.Context, cfg config.Config) (int, error) {
			resolution, err := configstate.ResolveAutoCompactThreshold(ctx, cfg, modelCatalogs)
			if err != nil {
				return 0, err
			}
			return resolution.Threshold, nil
		},
	))

	// 注入记忆提取钩子：当 AutoExtract 启用且 memoSvc 可用时，ReAct 循环完成后异步提取记忆。
	if memoSvc != nil && cfg.Memo.AutoExtract {
		runtimeSvc.SetMemoExtractor(newMemoExtractorAdapter(
			providerRegistry,
			manager,
			memo.NewAutoExtractor(nil, memoSvc),
		))
	}
	needCleanup = false

	closeBundle := combineRuntimeClosers(toolsCleanup, sessionStore.Close)

	return RuntimeBundle{
		Config:            cfg,
		ConfigManager:     manager,
		Runtime:           runtimeSvc,
		ProviderSelection: providerSelection,
		MemoService:       memoSvc,
		Close:             closeBundle,
	}, nil
}

// NewProgram 基于共享运行时依赖构建并返回 TUI 程序，同时返回退出时应调用的资源清理函数。
func NewProgram(ctx context.Context, opts BootstrapOptions) (*tea.Program, func() error, error) {
	bundle, err := BuildRuntime(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	tuiApp, err := newTUIWithMemo(&bundle.Config, bundle.ConfigManager, bundle.Runtime, bundle.ProviderSelection, bundle.MemoService)
	if err != nil {
		if bundle.Close != nil {
			_ = bundle.Close()
		}
		return nil, nil, err
	}
	return tea.NewProgram(
		tuiApp,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	), bundle.Close, nil
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

func buildToolRegistry(cfg config.Config) (*tools.Registry, func() error, error) {
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
	toolRegistry.Register(todo.New())
	mcpRegistry, err := buildMCPRegistry(cfg)
	if err != nil {
		return nil, nil, err
	}
	if mcpRegistry != nil {
		toolRegistry.SetMCPRegistry(mcpRegistry)
		toolRegistry.SetMCPExposureFilter(mcp.NewExposureFilter(mcp.ExposureFilterConfig{
			Allowlist: cfg.Tools.MCP.Exposure.Allowlist,
			Denylist:  cfg.Tools.MCP.Exposure.Denylist,
			Agents:    buildMCPAgentExposureRules(cfg.Tools.MCP.Exposure.Agents),
		}))
	}
	if mcpRegistry == nil {
		return toolRegistry, nil, nil
	}
	return toolRegistry, mcpRegistry.Close, nil
}

// buildSkillsRegistry 负责以最小代价初始化本地 skills registry，refresh 失败时仅记录日志并保留 registry 实例。
func buildSkillsRegistry(ctx context.Context, baseDir string) skills.Registry {
	root := filepath.Join(baseDir, "skills")
	registry := skills.NewRegistry(skills.NewLocalLoader(root))
	if err := registry.Refresh(ctx); err != nil {
		log.Printf("skills: initialize registry from %s failed: %v", root, err)
	}
	return registry
}

// buildMCPAgentExposureRules 将配置层的 agent 过滤规则转换为 tools/mcp 层输入。
func buildMCPAgentExposureRules(configs []config.MCPAgentExposureConfig) []mcp.AgentExposureRule {
	if len(configs) == 0 {
		return nil
	}
	rules := make([]mcp.AgentExposureRule, 0, len(configs))
	for _, item := range configs {
		rules = append(rules, mcp.AgentExposureRule{
			Agent:     item.Agent,
			Allowlist: append([]string(nil), item.Allowlist...),
		})
	}
	return rules
}

func buildToolManager(registry *tools.Registry) (tools.Manager, error) {
	engine, err := security.NewRecommendedPolicyEngine()
	if err != nil {
		return nil, err
	}
	return tools.NewManager(registry, engine, security.NewWorkspaceSandbox())
}

type runtimeMemoExtractorFunc func(sessionID string, messages []providertypes.Message)

func (f runtimeMemoExtractorFunc) Schedule(sessionID string, messages []providertypes.Message) {
	f(sessionID, messages)
}

type textGenAdapter func(ctx context.Context, prompt string, msgs []providertypes.Message) (string, error)

func (f textGenAdapter) Generate(ctx context.Context, prompt string, msgs []providertypes.Message) (string, error) {
	return f(ctx, prompt, msgs)
}

type runtimeAutoCompactThresholdResolverFunc func(ctx context.Context, cfg config.Config) (int, error)

func (f runtimeAutoCompactThresholdResolverFunc) ResolveAutoCompactThreshold(ctx context.Context, cfg config.Config) (int, error) {
	return f(ctx, cfg)
}

// combineRuntimeClosers 按顺序执行 runtime 初始化阶段注册的清理函数。
func combineRuntimeClosers(closers ...func() error) func() error {
	return func() error {
		var firstErr error
		for _, closer := range closers {
			if closer == nil {
				continue
			}
			if err := closer(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
}
