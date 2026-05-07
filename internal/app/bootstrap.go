package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/checkpoint"
	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/memo"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	providercatalog "neo-code/internal/provider/catalog"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/repository"
	agentruntime "neo-code/internal/runtime"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
	"neo-code/internal/tools/bash"
	"neo-code/internal/tools/codebase"
	diagnosetool "neo-code/internal/tools/diagnose"
	"neo-code/internal/tools/filesystem"
	"neo-code/internal/tools/mcp"
	memotool "neo-code/internal/tools/memo"
	"neo-code/internal/tools/spawnsubagent"
	"neo-code/internal/tools/todo"
	"neo-code/internal/tools/webfetch"
	tuiapp "neo-code/internal/tui/core/app"
	"neo-code/internal/tui/services"
)

const utf8CodePage = 65001

var (
	setConsoleOutputCodePage    = platformSetConsoleOutputCodePage
	setConsoleInputCodePage     = platformSetConsoleInputCodePage
	buildToolManagerFunc        = buildToolManager
	newRemoteRuntimeAdapter     = defaultNewRemoteRuntimeAdapter
	newTUIWithMemo              = tuiapp.NewWithMemo
	runConfigMigrationPreflight = defaultRunConfigMigrationPreflight
	bootstrapLogf               = log.Printf
	cleanupExpiredSessions      = func(
		ctx context.Context,
		store agentsession.Store,
		maxAge time.Duration,
	) (int, error) {
		return store.CleanupExpiredSessions(ctx, maxAge)
	}
)

// BootstrapOptions 描述应用启动时可注入的运行时选项。
type BootstrapOptions struct {
	Workdir      string
	SessionID    string
	WakeInputB64 string
}

type memoExtractorScheduler interface {
	ScheduleWithExtractor(sessionID string, messages []providertypes.Message, extractor memo.Extractor)
}

type runtimeWithClose interface {
	services.Runtime
	Close() error
}

type bootstrapSharedBundle struct {
	Config            config.Config
	ConfigManager     *config.Manager
	ProviderSelection *configstate.Service
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
			runtimeConfig, err := resolved.ToRuntimeConfig()
			if err != nil {
				return "", err
			}
			p, err := factory.Build(ctx, runtimeConfig)
			if err != nil {
				return "", err
			}

			return provider.GenerateText(ctx, p, providertypes.GenerateRequest{
				Model:        cfg.CurrentModel,
				SystemPrompt: prompt,
				Messages:     append([]providertypes.Message(nil), msgs...),
			})
		})

		scheduler.ScheduleWithExtractor(sessionID, messages, memo.NewLLMExtractor(generator, cfg.Memo.ExtractRecentMessages))
	})
}

// RuntimeBundle 聚合 CLI 与 TUI 共享的运行时依赖。
type RuntimeBundle struct {
	Config            config.Config
	ConfigManager     *config.Manager
	Runtime           agentruntime.Runtime
	SessionStore      *agentsession.SQLiteStore
	ProviderSelection *configstate.Service
	ToolRegistry      *tools.Registry
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

// BuildGatewayServerDeps 构建 Gateway 服务端运行时依赖，包含 runtime/tool/session 全栈能力。
func BuildGatewayServerDeps(ctx context.Context, opts BootstrapOptions) (RuntimeBundle, error) {
	sharedDeps, providerRegistry, modelCatalogs, err := BuildSharedConfigDeps(ctx, opts)
	if err != nil {
		return RuntimeBundle{}, err
	}
	cfg := sharedDeps.Config

	toolRegistry, toolsCleanup, err := buildToolRegistry(cfg)
	if err != nil {
		return RuntimeBundle{}, err
	}
	var sessionStore *agentsession.SQLiteStore
	needCleanup := true
	defer func() {
		if !needCleanup {
			return
		}
		if sessionStore != nil {
			_ = sessionStore.Close()
		}
		if toolsCleanup != nil {
			_ = toolsCleanup()
		}
	}()

	toolManager, err := buildToolManagerFunc(toolRegistry)
	if err != nil {
		return RuntimeBundle{}, err
	}

	// Session Store 绑定到启动时的 workdir 哈希分桶，整个应用生命周期内不可变。
	// 这意味着所有会话都归属到启动时指定的项目目录下，运行时不会因配置变更而迁移存储位置。
	sessionStore = agentsession.NewSQLiteStore(sharedDeps.ConfigManager.BaseDir(), cfg.Workdir)

	// 启动时自动清理过期会话，避免数据库无限膨胀。
	if _, err := cleanupExpiredSessions(ctx, sessionStore, agentsession.DefaultSessionMaxAge); err != nil {
		log.Printf("session cleanup warning: %v", err)
	}

	// 注册内置工具的内容摘要器，使 micro-compact 在清理旧工具结果时保留关键上下文。
	tools.RegisterBuiltinSummarizers(toolRegistry)

	microCompactCfg := agentcontext.MicroCompactConfig{
		Policies:    toolRegistry,
		Summarizers: toolRegistry,
	}
	var contextBuilder agentcontext.Builder = agentcontext.NewConfiguredBuilder(microCompactCfg)
	var memoSvc *memo.Service
	if cfg.Memo.Enabled {
		memoStore := memo.NewFileStore(sharedDeps.ConfigManager.BaseDir(), cfg.Workdir)
		memoSource := memo.NewContextSource(memoStore)
		var sourceInvl func()
		if invalidator, ok := memoSource.(interface{ InvalidateCache() }); ok {
			sourceInvl = invalidator.InvalidateCache
		}
		contextBuilder = agentcontext.NewConfiguredBuilder(microCompactCfg, memoSource)
		memoSvc = memo.NewService(memoStore, cfg.Memo, sourceInvl)
		toolRegistry.Register(memotool.NewRememberTool(memoSvc))
		toolRegistry.Register(memotool.NewRecallTool(memoSvc))
		toolRegistry.Register(memotool.NewListTool(memoSvc))
		toolRegistry.Register(memotool.NewRemoveTool(memoSvc))
	}

	runtimeSvc := agentruntime.NewWithFactory(
		sharedDeps.ConfigManager,
		toolManager,
		sessionStore,
		providerRegistry,
		contextBuilder,
	)
	runtimeSvc.SetSessionAssetStore(sessionStore)
	runtimeSvc.SetUserInputPreparer(agentruntime.NewSessionInputPreparer(sessionStore, sessionStore))
	runtimeSvc.SetSkillsRegistry(buildSkillsRegistry(ctx, sharedDeps.ConfigManager.BaseDir(), cfg.Workdir))
	runtimeSvc.SetBudgetResolver(runtimeBudgetResolverFunc(
		func(ctx context.Context, cfg config.Config) (agentruntime.BudgetResolution, error) {
			resolution, err := configstate.ResolvePromptBudget(ctx, cfg, modelCatalogs)
			if err != nil {
				return agentruntime.BudgetResolution{}, err
			}
			return agentruntime.BudgetResolution{
				PromptBudget:  resolution.PromptBudget,
				Source:        string(resolution.Source),
				ContextWindow: resolution.ContextWindow,
			}, nil
		},
	))
	if err := agentruntime.ConfigureRuntimeHooks(runtimeSvc, cfg); err != nil {
		return RuntimeBundle{}, err
	}

	// 注入记忆提取钩子：当 AutoExtract 启用且 memoSvc 可用时，ReAct 循环完成后异步提取记忆。
	if memoSvc != nil && cfg.Memo.AutoExtract {
		runtimeSvc.SetMemoExtractor(newMemoExtractorAdapter(
			providerRegistry,
			sharedDeps.ConfigManager,
			memo.NewAutoExtractor(nil, memoSvc, time.Duration(cfg.Memo.ExtractTimeoutSec)*time.Second),
		))
	}

	// Checkpoint 基础设施：SQLite + per-edit 版本化文件历史（不依赖 git）。
	// 优先复用 sessionStore 已打开的 *sql.DB；冷启动尚未建连时显式初始化，
	// 避免 sessionStore.DB() 为 nil 时整条 checkpoint 链路被静默跳过。
	sessionDB := sessionStore.DB()
	if sessionDB == nil {
		if initDB, initErr := sessionStore.InitDB(ctx); initErr == nil {
			sessionDB = initDB
		}
	}
	var checkpointStore *checkpoint.SQLiteCheckpointStore
	if sessionDB != nil {
		checkpointStore = checkpoint.NewSQLiteCheckpointStoreWithDB(sessionDB)
		projectDir := agentsession.HashWorkspaceRoot(cfg.Workdir)
		snapshotRoot := filepath.Join(sharedDeps.ConfigManager.BaseDir(), "projects", projectDir)
		perEditStore := checkpoint.NewPerEditSnapshotStore(snapshotRoot, cfg.Workdir)
		runtimeSvc.SetCheckpointDependencies(checkpointStore, perEditStore)
	}
	// 启动时修复残留的 creating 状态 checkpoint
	if checkpointStore != nil {
		if repaired, err := checkpointStore.RepairCreatingCheckpoints(ctx); err != nil {
			log.Printf("checkpoint repair warning: %v", err)
		} else if repaired > 0 {
			log.Printf("checkpoint repair: fixed %d stale checkpoints", repaired)
		}
	}

	runtimeImpl := agentruntime.Runtime(runtimeSvc)
	closeFns := []func() error{toolsCleanup, checkpointStore.Close, sessionStore.Close}

	needCleanup = false

	closeBundle := combineRuntimeClosers(closeFns...)

	return RuntimeBundle{
		Config:            cfg,
		ConfigManager:     sharedDeps.ConfigManager,
		Runtime:           runtimeImpl,
		SessionStore:      sessionStore,
		ProviderSelection: sharedDeps.ProviderSelection,
		ToolRegistry:      toolRegistry,
		MemoService:       memoSvc,
		Close:             closeBundle,
	}, nil
}

// NewProgram 基于共享运行时依赖构建并返回 TUI 程序，同时返回退出时应调用的资源清理函数。
func NewProgram(ctx context.Context, opts BootstrapOptions) (*tea.Program, func() error, error) {
	bundle, err := BuildTUIClientDeps(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	tuiRuntime, err := newRemoteRuntimeAdapter(services.RemoteRuntimeAdapterOptions{})
	if err != nil {
		if bundle.Close != nil {
			_ = bundle.Close()
		}
		return nil, nil, err
	}
	cleanup := combineRuntimeClosers(tuiRuntime.Close, bundle.Close)

	tuiApp, err := newTUIWithMemo(&bundle.Config, bundle.ConfigManager, tuiRuntime, bundle.ProviderSelection, bundle.MemoService)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, nil, err
	}
	if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
		if err := tuiApp.HydrateSession(ctx, sessionID); err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, nil, err
		}
	}
	if encodedWakeInput := strings.TrimSpace(opts.WakeInputB64); encodedWakeInput != "" {
		wakeInput, decodeErr := protocol.DecodeWakeStartupInput(encodedWakeInput)
		if decodeErr != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, nil, decodeErr
		}
		if configureErr := tuiApp.ConfigureStartupWakeInput(wakeInput.Text, wakeInput.Workdir); configureErr != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return nil, nil, configureErr
		}
	}
	return tea.NewProgram(
		tuiApp,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	), cleanup, nil
}

// BuildSharedConfigDeps 统一构建共享配置依赖：配置、Provider 注册与当前选择服务。
func BuildSharedConfigDeps(
	ctx context.Context,
	opts BootstrapOptions,
) (bootstrapSharedBundle, agentruntime.ProviderFactory, *providercatalog.Service, error) {
	defaultCfg, err := bootstrapDefaultConfig(opts)
	if err != nil {
		return bootstrapSharedBundle{}, nil, nil, err
	}

	loader := config.NewLoader("", defaultCfg)
	manager := config.NewManager(loader)
	if err := runConfigMigrationPreflight(ctx, manager.ConfigPath()); err != nil {
		return bootstrapSharedBundle{}, nil, nil, err
	}
	if _, err := manager.Load(ctx); err != nil {
		return bootstrapSharedBundle{}, nil, nil, err
	}

	providerRegistry, err := builtin.NewRegistry()
	if err != nil {
		return bootstrapSharedBundle{}, nil, nil, err
	}
	modelCatalogs := providercatalog.NewService(manager.BaseDir(), providerRegistry, nil)
	providerSelection := configstate.NewService(manager, providerRegistry, modelCatalogs)
	if _, err := providerSelection.EnsureSelection(ctx); err != nil {
		return bootstrapSharedBundle{}, nil, nil, err
	}

	return bootstrapSharedBundle{
		Config:            manager.Get(),
		ConfigManager:     manager,
		ProviderSelection: providerSelection,
	}, providerRegistry, modelCatalogs, nil
}

// defaultRunConfigMigrationPreflight 在启动装配阶段执行 schema 迁移，并记录一次迁移结果。
func defaultRunConfigMigrationPreflight(ctx context.Context, configPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	result, err := config.UpgradeConfigSchema(configPath)
	if err != nil {
		return err
	}
	if !result.Changed && len(result.Notes) == 0 {
		return nil
	}
	if result.Changed {
		if result.Backup != "" {
			bootstrapLogf("config migration: migrated %s (backup: %s)", result.Path, result.Backup)
		} else {
			bootstrapLogf("config migration: migrated %s", result.Path)
		}
	}
	for _, note := range result.Notes {
		bootstrapLogf("config migration: note: %s", strings.TrimSpace(note))
	}
	return nil
}

// BuildTUIClientDeps 构建 TUI 客户端依赖，仅保留配置与 Provider 选择，不创建本地 runtime/tool 栈。
func BuildTUIClientDeps(ctx context.Context, opts BootstrapOptions) (RuntimeBundle, error) {
	sharedDeps, _, _, err := BuildSharedConfigDeps(ctx, opts)
	if err != nil {
		return RuntimeBundle{}, err
	}
	return RuntimeBundle{
		Config:            sharedDeps.Config,
		ConfigManager:     sharedDeps.ConfigManager,
		ProviderSelection: sharedDeps.ProviderSelection,
		ToolRegistry:      nil,
		MemoService:       nil,
		Close:             nil,
	}, nil
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
	toolRegistry.Register(filesystem.NewMove(cfg.Workdir))
	toolRegistry.Register(filesystem.NewCopy(cfg.Workdir))
	toolRegistry.Register(filesystem.NewDelete(cfg.Workdir))
	toolRegistry.Register(filesystem.NewCreateDir(cfg.Workdir))
	toolRegistry.Register(filesystem.NewRemoveDir(cfg.Workdir))
	toolRegistry.Register(bash.New(cfg.Workdir, cfg.Shell, time.Duration(cfg.ToolTimeoutSec)*time.Second))
	toolRegistry.Register(diagnosetool.New())
	toolRegistry.Register(webfetch.New(webfetch.Config{
		Timeout:               time.Duration(cfg.ToolTimeoutSec) * time.Second,
		MaxResponseBytes:      cfg.Tools.WebFetch.MaxResponseBytes,
		SupportedContentTypes: cfg.Tools.WebFetch.SupportedContentTypes,
	}))
	toolRegistry.Register(todo.New())
	toolRegistry.Register(spawnsubagent.New())
	repoSvc := repository.NewService()
	toolRegistry.Register(codebase.NewRead(repoSvc, cfg.Workdir))
	toolRegistry.Register(codebase.NewSearchText(repoSvc, cfg.Workdir))
	toolRegistry.Register(codebase.NewSearchSymbol(repoSvc, cfg.Workdir))
	mcpRegistry, err := BuildMCPRegistry(cfg)
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

// buildSkillsRegistry 负责按“项目优先全局”顺序构建本地 skills registry。
// RebuildMCPServersForRegistry 根据最新配置重建指定 registry 中的 MCP server；失败时保留旧 registry 不替换。
func RebuildMCPServersForRegistry(registry *tools.Registry, cfg config.Config) error {
	if registry == nil {
		return nil
	}
	newMcpRegistry, err := BuildMCPRegistry(cfg)
	if err != nil {
		return fmt.Errorf("app: build mcp registry: %w", err)
	}
	var filter mcp.ExposureFilter
	if newMcpRegistry != nil {
		filter = mcp.NewExposureFilter(mcp.ExposureFilterConfig{
			Allowlist: cfg.Tools.MCP.Exposure.Allowlist,
			Denylist:  cfg.Tools.MCP.Exposure.Denylist,
			Agents:    buildMCPAgentExposureRules(cfg.Tools.MCP.Exposure.Agents),
		})
	}
	registry.ReplaceMCPRegistry(newMcpRegistry, filter)
	return nil
}

func buildSkillsRegistry(ctx context.Context, baseDir string, workdir string) skills.Registry {
	loaders := make([]skills.Loader, 0, 2)
	projectRoot := resolveWorkspaceSkillsRoot(workdir)
	if strings.TrimSpace(projectRoot) != "" {
		loaders = append(loaders, skills.NewLocalLoaderWithSourceLayer(projectRoot, skills.SourceLayerProject))
	}

	globalRoot := resolveGlobalSkillsRoot(baseDir)
	if strings.TrimSpace(globalRoot) != "" {
		loaders = append(loaders, skills.NewLocalLoaderWithSourceLayer(globalRoot, skills.SourceLayerGlobal))
	}

	registry := skills.NewRegistry(skills.NewMultiSourceLoader(loaders...))
	if err := registry.Refresh(ctx); err != nil {
		log.Printf(
			"skills: initialize registry failed (project=%q global=%q): %v",
			projectRoot,
			globalRoot,
			err,
		)
	}
	return registry
}

// resolveWorkspaceSkillsRoot 解析工作目录对应的项目级 skills 根目录。
func resolveWorkspaceSkillsRoot(workdir string) string {
	trimmedWorkdir := strings.TrimSpace(workdir)
	if trimmedWorkdir == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(trimmedWorkdir), ".neocode", "skills")
}

// resolveGlobalSkillsRoot 解析全局 skills 根目录，并在缺失时回退到 codex 同级目录。
func resolveGlobalSkillsRoot(baseDir string) string {
	primaryRoot := filepath.Join(baseDir, "skills")
	if fallbackRoot, ok := resolveCodexSkillsFallbackRoot(baseDir, primaryRoot); ok {
		log.Printf("skills: primary root %s not found, fallback to %s", primaryRoot, fallbackRoot)
		return fallbackRoot
	}
	return primaryRoot
}

// resolveCodexSkillsFallbackRoot 在 neocode 默认目录缺失时，回退到同级 codex skills 目录。
func resolveCodexSkillsFallbackRoot(baseDir string, primaryRoot string) (string, bool) {
	if isExistingDirectory(primaryRoot) {
		return "", false
	}

	normalizedBaseDir := strings.TrimSpace(baseDir)
	if normalizedBaseDir == "" {
		return "", false
	}
	cleanBaseDir := filepath.Clean(normalizedBaseDir)
	if !strings.EqualFold(filepath.Base(cleanBaseDir), ".neocode") {
		return "", false
	}

	fallbackRoot := filepath.Join(filepath.Dir(cleanBaseDir), ".codex", "skills")
	if !isExistingDirectory(fallbackRoot) {
		return "", false
	}
	return fallbackRoot, true
}

// isExistingDirectory 判断路径是否存在且为目录。
func isExistingDirectory(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return info.IsDir()
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

// defaultNewRemoteRuntimeAdapter 构建默认的 Gateway runtime 适配器。
func defaultNewRemoteRuntimeAdapter(options services.RemoteRuntimeAdapterOptions) (runtimeWithClose, error) {
	adapter, err := services.NewRemoteRuntimeAdapter(options)
	if err != nil {
		return nil, err
	}
	return adapter, nil
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

type runtimeBudgetResolverFunc func(ctx context.Context, cfg config.Config) (agentruntime.BudgetResolution, error)

func (f runtimeBudgetResolverFunc) ResolvePromptBudget(ctx context.Context, cfg config.Config) (agentruntime.BudgetResolution, error) {
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
