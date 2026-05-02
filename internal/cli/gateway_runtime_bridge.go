package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"neo-code/internal/app"
	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/gateway"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

const bridgeLocalSubjectID = "local_admin"
const bridgeRuntimeUnavailableErrMsg = "gateway runtime bridge: runtime is unavailable"

type runtimeRunCanceler interface {
	CancelRun(runID string) bool
}

type runtimeSessionCreator interface {
	CreateSession(ctx context.Context, id string) (agentsession.Session, error)
}

type runtimeTodoLister interface {
	ListTodos(ctx context.Context, sessionID string) (agentruntime.TodoSnapshot, error)
}

type runtimeSnapshotGetter interface {
	GetRuntimeSnapshot(ctx context.Context, sessionID string) (agentruntime.RuntimeSnapshot, error)
}

// bridgeSessionStore 定义桥接层对会话存储的最低需求。
type bridgeSessionStore interface {
	DeleteSession(ctx context.Context, sessionID string) error
	UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error
}

type bridgeSessionLoader interface {
	LoadSession(ctx context.Context, id string) (agentsession.Session, error)
}

// defaultBuildGatewayRuntimePort 构建网关运行时 RuntimePort 适配器，并返回对应资源清理函数。
func defaultBuildGatewayRuntimePort(ctx context.Context, workdir string) (gateway.RuntimePort, func() error, error) {
	bundle, err := app.BuildGatewayServerDeps(ctx, app.BootstrapOptions{Workdir: strings.TrimSpace(workdir)})
	if err != nil {
		return nil, nil, err
	}

	bridge, err := newGatewayRuntimePortBridge(ctx, bundle.Runtime, bundle.SessionStore, bundle.ConfigManager, bundle.ProviderSelection)
	if err != nil {
		if bundle.Close != nil {
			_ = bundle.Close()
		}
		return nil, nil, err
	}

	cleanup := func() error {
		var closeErr error
		if bridge != nil {
			closeErr = errors.Join(closeErr, bridge.Close())
		}
		if bundle.Close != nil {
			closeErr = errors.Join(closeErr, bundle.Close())
		}
		return closeErr
	}

	return bridge, cleanup, nil
}

// configManagerPort 定义桥接层对配置管理器的最小需求。
type configManagerPort interface {
	Get() config.Config
	Load(ctx context.Context) (config.Config, error)
	Update(ctx context.Context, mutate func(*config.Config) error) error
	BaseDir() string
}

// providerSelectorPort 定义桥接层对 Provider 选择服务的最小需求。
type providerSelectorPort interface {
	ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error)
	CreateCustomProvider(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
	SelectProvider(ctx context.Context, providerName string) (configstate.Selection, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
}

// gatewayRuntimePortBridge 将 runtime.Runtime 适配为 gateway.RuntimePort，并负责事件流桥接。
type gatewayRuntimePortBridge struct {
	runtime           agentruntime.Runtime
	sessionStore      bridgeSessionStore
	configManager     configManagerPort
	providerSelection providerSelectorPort
	events            chan gateway.RuntimeEvent

	stopOnce sync.Once
	stopCh   chan struct{}
}

// newGatewayRuntimePortBridge 创建 RuntimePort 桥接器，用于把 runtime 事件转换为 gateway 统一事件。
func newGatewayRuntimePortBridge(
	ctx context.Context,
	runtimeSvc agentruntime.Runtime,
	store bridgeSessionStore,
	extras ...any,
) (*gatewayRuntimePortBridge, error) {
	if runtimeSvc == nil {
		return nil, fmt.Errorf("gateway runtime bridge: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cm configManagerPort
	var ps providerSelectorPort
	for _, extra := range extras {
		switch typed := extra.(type) {
		case configManagerPort:
			cm = typed
		case providerSelectorPort:
			ps = typed
		}
	}

	bridge := &gatewayRuntimePortBridge{
		runtime:           runtimeSvc,
		sessionStore:      store,
		configManager:     cm,
		providerSelection: ps,
		events:            make(chan gateway.RuntimeEvent, 128),
		stopCh:            make(chan struct{}),
	}
	go bridge.runEventBridge(ctx)
	return bridge, nil
}

// Run 将 gateway.run 输入转换为 runtime Submit 输入。
func (b *gatewayRuntimePortBridge) Run(ctx context.Context, input gateway.RunInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.Submit(ctx, convertGatewayRunInput(input))
}

// Compact 将 gateway.compact 请求映射到 runtime 紧凑化能力并回填统一结果。
func (b *gatewayRuntimePortBridge) Compact(ctx context.Context, input gateway.CompactInput) (gateway.CompactResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.CompactResult{}, err
	}

	result, err := b.runtime.Compact(ctx, agentruntime.CompactInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
	})
	if err != nil {
		return gateway.CompactResult{}, err
	}

	return gateway.CompactResult{
		Applied:        result.Applied,
		BeforeChars:    result.BeforeChars,
		AfterChars:     result.AfterChars,
		SavedRatio:     result.SavedRatio,
		TriggerMode:    result.TriggerMode,
		TranscriptID:   result.TranscriptID,
		TranscriptPath: result.TranscriptPath,
	}, nil
}

// ExecuteSystemTool 将 gateway.executeSystemTool 请求映射到 runtime 系统工具执行能力。
func (b *gatewayRuntimePortBridge) ExecuteSystemTool(
	ctx context.Context,
	input gateway.ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return tools.ToolResult{}, err
	}

	return b.runtime.ExecuteSystemTool(ctx, agentruntime.SystemToolInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     strings.TrimSpace(input.RunID),
		Workdir:   strings.TrimSpace(input.Workdir),
		ToolName:  strings.TrimSpace(input.ToolName),
		Arguments: append([]byte(nil), input.Arguments...),
	})
}

// ActivateSessionSkill 将 gateway.activateSessionSkill 请求映射到 runtime 会话技能激活能力。
func (b *gatewayRuntimePortBridge) ActivateSessionSkill(
	ctx context.Context,
	input gateway.SessionSkillMutationInput,
) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.ActivateSessionSkill(
		ctx,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.SkillID),
	)
}

// DeactivateSessionSkill 将 gateway.deactivateSessionSkill 请求映射到 runtime 会话技能停用能力。
func (b *gatewayRuntimePortBridge) DeactivateSessionSkill(
	ctx context.Context,
	input gateway.SessionSkillMutationInput,
) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.DeactivateSessionSkill(
		ctx,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.SkillID),
	)
}

// ListSessionSkills 查询会话激活技能列表，并映射为 gateway 契约输出。
func (b *gatewayRuntimePortBridge) ListSessionSkills(
	ctx context.Context,
	input gateway.ListSessionSkillsInput,
) ([]gateway.SessionSkillState, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	states, err := b.runtime.ListSessionSkills(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return nil, err
	}
	return convertRuntimeSessionSkillStates(states), nil
}

// ListAvailableSkills 查询可用技能列表，并映射为 gateway 契约输出。
func (b *gatewayRuntimePortBridge) ListAvailableSkills(
	ctx context.Context,
	input gateway.ListAvailableSkillsInput,
) ([]gateway.AvailableSkillState, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	states, err := b.runtime.ListAvailableSkills(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return nil, err
	}
	return convertRuntimeAvailableSkillStates(states), nil
}

// ResolvePermission 将网关审批决策转换为 runtime 审批输入并提交。
func (b *gatewayRuntimePortBridge) ResolvePermission(ctx context.Context, input gateway.PermissionResolutionInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	return b.runtime.ResolvePermission(ctx, agentruntime.PermissionResolutionInput{
		RequestID: strings.TrimSpace(input.RequestID),
		Decision:  agentruntime.PermissionResolutionDecision(strings.TrimSpace(string(input.Decision))),
	})
}

// CancelRun 转发 gateway.cancel 请求到 runtime 的 run_id 精确取消能力。
func (b *gatewayRuntimePortBridge) CancelRun(_ context.Context, input gateway.CancelInput) (bool, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return false, err
	}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		return false, gateway.ErrRuntimeResourceNotFound
	}
	canceler, ok := b.runtime.(runtimeRunCanceler)
	if !ok {
		return false, fmt.Errorf("gateway runtime bridge: runtime does not support cancel by run_id")
	}
	if !canceler.CancelRun(runID) {
		return false, gateway.ErrRuntimeResourceNotFound
	}
	return true, nil
}

// Events 返回桥接后的 gateway 统一事件流。
func (b *gatewayRuntimePortBridge) Events() <-chan gateway.RuntimeEvent {
	if b == nil {
		return nil
	}
	return b.events
}

// ListSessions 返回会话摘要列表，并转换为 gateway 契约结构。
func (b *gatewayRuntimePortBridge) ListSessions(ctx context.Context) ([]gateway.SessionSummary, error) {
	if b == nil || b.runtime == nil {
		return nil, fmt.Errorf("gateway runtime bridge: runtime is unavailable")
	}

	summaries, err := b.runtime.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, nil
	}

	converted := make([]gateway.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		converted = append(converted, gateway.SessionSummary{
			ID:        strings.TrimSpace(summary.ID),
			Title:     strings.TrimSpace(summary.Title),
			CreatedAt: summary.CreatedAt,
			UpdatedAt: summary.UpdatedAt,
		})
	}
	return converted, nil
}

// LoadSession 加载单个会话详情，并做跨层消息结构映射。
func (b *gatewayRuntimePortBridge) LoadSession(ctx context.Context, input gateway.LoadSessionInput) (gateway.Session, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.Session{}, err
	}

	sessionID := strings.TrimSpace(input.SessionID)
	session, err := b.runtime.LoadSession(ctx, sessionID)
	if err != nil {
		if isRuntimeNotFoundError(err) {
			// TODO: 待 TUI Submit 显式调用 gateway.createSession 后移除此 upsert。
			creator, ok := b.runtime.(runtimeSessionCreator)
			if !ok {
				return gateway.Session{}, gateway.ErrRuntimeResourceNotFound
			}
			created, createErr := creator.CreateSession(ctx, sessionID)
			if createErr != nil {
				return gateway.Session{}, createErr
			}
			return convertRuntimeSessionToGatewaySession(created), nil
		}
		return gateway.Session{}, err
	}

	return convertRuntimeSessionToGatewaySession(session), nil
}

// ListSessionTodos 查询会话 Todo 快照，优先使用 runtime 显式查询接口。
func (b *gatewayRuntimePortBridge) ListSessionTodos(
	ctx context.Context,
	input gateway.ListSessionTodosInput,
) (gateway.TodoSnapshot, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.TodoSnapshot{}, err
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return gateway.TodoSnapshot{}, gateway.ErrRuntimeResourceNotFound
	}

	if lister, ok := b.runtime.(runtimeTodoLister); ok {
		snapshot, err := lister.ListTodos(ctx, sessionID)
		if err != nil {
			return gateway.TodoSnapshot{}, err
		}
		return convertRuntimeTodoSnapshot(snapshot), nil
	}

	session, err := b.runtime.LoadSession(ctx, sessionID)
	if err != nil {
		return gateway.TodoSnapshot{}, err
	}
	return convertRuntimeTodoSnapshot(buildRuntimeTodoSnapshotFromSessionTodos(session.ListTodos())), nil
}

// GetRuntimeSnapshot 查询会话 runtime 快照，供桌面端初始化与重连同步。
func (b *gatewayRuntimePortBridge) GetRuntimeSnapshot(
	ctx context.Context,
	input gateway.GetRuntimeSnapshotInput,
) (gateway.RuntimeSnapshot, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.RuntimeSnapshot{}, err
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return gateway.RuntimeSnapshot{}, gateway.ErrRuntimeResourceNotFound
	}
	getter, ok := b.runtime.(runtimeSnapshotGetter)
	if !ok {
		return gateway.RuntimeSnapshot{}, fmt.Errorf("gateway runtime bridge: runtime does not support runtime snapshot")
	}
	snapshot, err := getter.GetRuntimeSnapshot(ctx, sessionID)
	if err != nil {
		return gateway.RuntimeSnapshot{}, err
	}
	return convertRuntimeSnapshot(snapshot), nil
}

// CreateSession 创建或加载指定会话，并返回最终可用的会话标识。
func (b *gatewayRuntimePortBridge) CreateSession(ctx context.Context, input gateway.CreateSessionInput) (string, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return "", err
	}

	creator, ok := b.runtime.(runtimeSessionCreator)
	if !ok {
		return "", fmt.Errorf("gateway runtime bridge: runtime does not support create_session")
	}
	session, err := creator.CreateSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(session.ID), nil
}

// DeleteSession 删除/归档指定会话。
func (b *gatewayRuntimePortBridge) DeleteSession(ctx context.Context, input gateway.DeleteSessionInput) (bool, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return false, err
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return false, gateway.ErrRuntimeResourceNotFound
	}
	if b.sessionStore == nil {
		return false, fmt.Errorf("gateway runtime bridge: session store is unavailable")
	}
	if err := b.sessionStore.DeleteSession(ctx, sessionID); err != nil {
		return false, err
	}
	return true, nil
}

// RenameSession 重命名指定会话。
func (b *gatewayRuntimePortBridge) RenameSession(ctx context.Context, input gateway.RenameSessionInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	sessionID := strings.TrimSpace(input.SessionID)
	title := strings.TrimSpace(input.Title)
	if sessionID == "" {
		return gateway.ErrRuntimeResourceNotFound
	}
	if title == "" {
		return fmt.Errorf("gateway runtime bridge: title is required for rename")
	}
	if b.sessionStore == nil {
		return fmt.Errorf("gateway runtime bridge: session store is unavailable")
	}
	return b.sessionStore.UpdateSessionState(ctx, agentsession.UpdateSessionStateInput{
		SessionID: sessionID,
		Title:     title,
		UpdatedAt: time.Now().UTC(),
	})
}

// ListFiles 列出工作目录文件树。
func (b *gatewayRuntimePortBridge) ListFiles(ctx context.Context, input gateway.ListFilesInput) ([]gateway.FileEntry, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	root, err := b.resolveListFilesRoot(ctx, input)
	if err != nil {
		return nil, err
	}
	target, relativeBase, err := resolveSafeListFilesPath(root, input.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	result := make([]gateway.FileEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") && entry.Name() == ".git" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, infoErr
		}
		relativePath := filepath.ToSlash(filepath.Join(relativeBase, entry.Name()))
		if relativeBase == "." || relativeBase == "" {
			relativePath = filepath.ToSlash(entry.Name())
		}
		result = append(result, gateway.FileEntry{
			Name:    entry.Name(),
			Path:    relativePath,
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result, nil
}

// ListModels 列出可用模型。
func (b *gatewayRuntimePortBridge) ListModels(ctx context.Context, input gateway.ListModelsInput) ([]gateway.ModelEntry, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	if b.providerSelection == nil {
		return nil, fmt.Errorf("gateway runtime bridge: provider selection is unavailable")
	}
	options, err := b.providerSelection.ListProviderOptions(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]gateway.ModelEntry, 0)
	for _, option := range options {
		for _, model := range option.Models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			name := strings.TrimSpace(model.Name)
			if name == "" {
				name = id
			}
			models = append(models, gateway.ModelEntry{
				ID:       id,
				Name:     name,
				Provider: strings.TrimSpace(option.ID),
			})
		}
	}
	return models, nil
}

// SetSessionModel 设置会话模型。
func (b *gatewayRuntimePortBridge) SetSessionModel(ctx context.Context, input gateway.SetSessionModelInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	if b.sessionStore == nil {
		return fmt.Errorf("gateway runtime bridge: session store is unavailable")
	}
	session, err := b.loadStoredSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return err
	}
	providerID, modelID, err := b.resolveProviderModelForSession(ctx, session, input.ProviderID, input.ModelID)
	if err != nil {
		return err
	}
	head := session.HeadSnapshot()
	head.Provider = providerID
	head.Model = modelID
	return b.sessionStore.UpdateSessionState(ctx, agentsession.UpdateSessionStateInput{
		SessionID: session.ID,
		Title:     session.Title,
		UpdatedAt: time.Now().UTC(),
		Head:      head,
	})
}

// GetSessionModel 获取当前会话模型。
func (b *gatewayRuntimePortBridge) GetSessionModel(ctx context.Context, input gateway.GetSessionModelInput) (gateway.SessionModelResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.SessionModelResult{}, err
	}
	if b.sessionStore == nil {
		return gateway.SessionModelResult{}, fmt.Errorf("gateway runtime bridge: session store is unavailable")
	}
	session, err := b.loadStoredSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return gateway.SessionModelResult{}, err
	}
	providerID, modelID := strings.TrimSpace(session.Provider), strings.TrimSpace(session.Model)
	if providerID == "" || modelID == "" {
		cfg := b.currentConfig()
		if providerID == "" {
			providerID = strings.TrimSpace(cfg.SelectedProvider)
		}
		if modelID == "" {
			modelID = strings.TrimSpace(cfg.CurrentModel)
		}
	}
	return gateway.SessionModelResult{
		ProviderID: providerID,
		ModelID:    modelID,
		ModelName:  b.modelDisplayName(ctx, providerID, modelID),
		Provider:   providerID,
	}, nil
}

// ListProviders 列出前端可管理的 provider 及模型候选。
func (b *gatewayRuntimePortBridge) ListProviders(ctx context.Context, input gateway.ListProvidersInput) ([]gateway.ProviderOption, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	if b.providerSelection == nil {
		return nil, fmt.Errorf("gateway runtime bridge: provider selection is unavailable")
	}
	cfg := b.currentConfig()
	options, err := b.providerSelection.ListProviderOptions(ctx)
	if err != nil {
		return nil, err
	}
	configByName := make(map[string]config.ProviderConfig, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		configByName[strings.ToLower(strings.TrimSpace(providerCfg.Name))] = providerCfg
	}
	result := make([]gateway.ProviderOption, 0, len(options))
	for _, option := range options {
		providerCfg := configByName[strings.ToLower(strings.TrimSpace(option.ID))]
		result = append(result, gateway.ProviderOption{
			ID:        strings.TrimSpace(option.ID),
			Name:      strings.TrimSpace(option.Name),
			Driver:    strings.TrimSpace(providerCfg.Driver),
			BaseURL:   strings.TrimSpace(providerCfg.BaseURL),
			APIKeyEnv: strings.TrimSpace(providerCfg.APIKeyEnv),
			Source:    strings.TrimSpace(string(providerCfg.Source)),
			Selected:  strings.EqualFold(strings.TrimSpace(cfg.SelectedProvider), strings.TrimSpace(option.ID)),
			Models:    append([]providertypes.ModelDescriptor(nil), option.Models...),
		})
	}
	return result, nil
}

// CreateProvider 创建自定义 provider，并在成功后选择该 provider。
func (b *gatewayRuntimePortBridge) CreateProvider(ctx context.Context, input gateway.CreateProviderInput) (gateway.ProviderSelectionResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.ProviderSelectionResult{}, err
	}
	if b.providerSelection == nil {
		return gateway.ProviderSelectionResult{}, fmt.Errorf("gateway runtime bridge: provider selection is unavailable")
	}
	selection, err := b.providerSelection.CreateCustomProvider(ctx, configstate.CreateCustomProviderInput{
		Name:                  strings.TrimSpace(input.Name),
		Driver:                strings.TrimSpace(input.Driver),
		BaseURL:               strings.TrimSpace(input.BaseURL),
		ChatAPIMode:           strings.TrimSpace(input.ChatAPIMode),
		ChatEndpointPath:      strings.TrimSpace(input.ChatEndpointPath),
		APIKeyEnv:             strings.TrimSpace(input.APIKeyEnv),
		APIKey:                strings.TrimSpace(input.APIKey),
		ModelSource:           strings.TrimSpace(input.ModelSource),
		ManualModelsJSON:      marshalManualModelsForGateway(input.Models),
		DiscoveryEndpointPath: strings.TrimSpace(input.DiscoveryEndpointPath),
	})
	if err != nil {
		return gateway.ProviderSelectionResult{}, err
	}
	return gateway.ProviderSelectionResult{ProviderID: selection.ProviderID, ModelID: selection.ModelID}, nil
}

// DeleteProvider 删除自定义 provider 并重载配置快照。
func (b *gatewayRuntimePortBridge) DeleteProvider(ctx context.Context, input gateway.DeleteProviderInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	if b.configManager == nil {
		return fmt.Errorf("gateway runtime bridge: config manager is unavailable")
	}
	providerID := strings.TrimSpace(input.ProviderID)
	cfg := b.configManager.Get()
	providerCfg, err := cfg.ProviderByName(providerID)
	if err != nil {
		return err
	}
	if providerCfg.Source != config.ProviderSourceCustom {
		return fmt.Errorf("gateway runtime bridge: builtin provider %q cannot be deleted", providerID)
	}
	if err := config.DeleteCustomProvider(b.configManager.BaseDir(), providerCfg.Name); err != nil {
		return err
	}
	_, err = b.configManager.Load(ctx)
	return err
}

// SelectProviderModel 设置全局 provider/model 选择。
func (b *gatewayRuntimePortBridge) SelectProviderModel(ctx context.Context, input gateway.SelectProviderModelInput) (gateway.ProviderSelectionResult, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return gateway.ProviderSelectionResult{}, err
	}
	if b.providerSelection == nil {
		return gateway.ProviderSelectionResult{}, fmt.Errorf("gateway runtime bridge: provider selection is unavailable")
	}
	selection, err := b.providerSelection.SelectProvider(ctx, strings.TrimSpace(input.ProviderID))
	if err != nil {
		return gateway.ProviderSelectionResult{}, err
	}
	if modelID := strings.TrimSpace(input.ModelID); modelID != "" {
		selection, err = b.providerSelection.SetCurrentModel(ctx, modelID)
		if err != nil {
			return gateway.ProviderSelectionResult{}, err
		}
	}
	return gateway.ProviderSelectionResult{ProviderID: selection.ProviderID, ModelID: selection.ModelID}, nil
}

// ListMCPServers 列出当前配置中的 MCP server。
func (b *gatewayRuntimePortBridge) ListMCPServers(_ context.Context, input gateway.ListMCPServersInput) ([]gateway.MCPServerEntry, error) {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return nil, err
	}
	cfg := b.currentConfig()
	return cfg.Tools.MCP.Clone().Servers, nil
}

// UpsertMCPServer 新增或更新一个 MCP server 配置。
func (b *gatewayRuntimePortBridge) UpsertMCPServer(ctx context.Context, input gateway.UpsertMCPServerInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	if b.configManager == nil {
		return fmt.Errorf("gateway runtime bridge: config manager is unavailable")
	}
	server := input.Server.Clone()
	server.ID = strings.TrimSpace(server.ID)
	return b.configManager.Update(ctx, func(cfg *config.Config) error {
		servers := cfg.Tools.MCP.Clone().Servers
		replaced := false
		for index := range servers {
			if strings.EqualFold(strings.TrimSpace(servers[index].ID), server.ID) {
				servers[index] = server
				replaced = true
				break
			}
		}
		if !replaced {
			servers = append(servers, server)
		}
		cfg.Tools.MCP.Servers = servers
		return nil
	})
}

// SetMCPServerEnabled 修改 MCP server 启用状态。
func (b *gatewayRuntimePortBridge) SetMCPServerEnabled(ctx context.Context, input gateway.SetMCPServerEnabledInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	if b.configManager == nil {
		return fmt.Errorf("gateway runtime bridge: config manager is unavailable")
	}
	id := strings.TrimSpace(input.ID)
	return b.configManager.Update(ctx, func(cfg *config.Config) error {
		servers := cfg.Tools.MCP.Clone().Servers
		for index := range servers {
			if strings.EqualFold(strings.TrimSpace(servers[index].ID), id) {
				servers[index].Enabled = input.Enabled
				cfg.Tools.MCP.Servers = servers
				return nil
			}
		}
		return fmt.Errorf("%w: mcp server %q not found", gateway.ErrRuntimeResourceNotFound, id)
	})
}

// DeleteMCPServer 删除 MCP server 配置。
func (b *gatewayRuntimePortBridge) DeleteMCPServer(ctx context.Context, input gateway.DeleteMCPServerInput) error {
	if err := b.ensureRuntimeAccess(input.SubjectID); err != nil {
		return err
	}
	if b.configManager == nil {
		return fmt.Errorf("gateway runtime bridge: config manager is unavailable")
	}
	id := strings.TrimSpace(input.ID)
	return b.configManager.Update(ctx, func(cfg *config.Config) error {
		servers := cfg.Tools.MCP.Clone().Servers
		next := servers[:0]
		removed := false
		for _, server := range servers {
			if strings.EqualFold(strings.TrimSpace(server.ID), id) {
				removed = true
				continue
			}
			next = append(next, server)
		}
		if !removed {
			return fmt.Errorf("%w: mcp server %q not found", gateway.ErrRuntimeResourceNotFound, id)
		}
		cfg.Tools.MCP.Servers = next
		return nil
	})
}

// Close 主动停止桥接事件泵，避免网关关闭后后台协程悬挂。
func (b *gatewayRuntimePortBridge) Close() error {
	if b == nil {
		return nil
	}
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	return nil
}

// runEventBridge 持续消费 runtime 事件并输出到 gateway 统一事件通道。
func (b *gatewayRuntimePortBridge) runEventBridge(ctx context.Context) {
	defer close(b.events)
	if b == nil || b.runtime == nil {
		return
	}

	source := b.runtime.Events()
	if source == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stopCh:
			return
		case event, ok := <-source:
			if !ok {
				return
			}
			mappedEvent := convertRuntimeEvent(event)
			select {
			case <-ctx.Done():
				return
			case <-b.stopCh:
				return
			case b.events <- mappedEvent:
			}
		}
	}
}

// convertGatewayRunInput 将 gateway.run 输入转换为 runtime PrepareInput。
func convertGatewayRunInput(input gateway.RunInput) agentruntime.PrepareInput {
	textParts := make([]string, 0, 1)
	if baseText := strings.TrimSpace(input.InputText); baseText != "" {
		textParts = append(textParts, baseText)
	}

	images := make([]agentruntime.UserImageInput, 0)
	for _, part := range input.InputParts {
		switch part.Type {
		case gateway.InputPartTypeText:
			if text := strings.TrimSpace(part.Text); text != "" {
				textParts = append(textParts, text)
			}
		case gateway.InputPartTypeImage:
			if part.Media == nil {
				continue
			}
			path := strings.TrimSpace(part.Media.URI)
			if path == "" {
				continue
			}
			images = append(images, agentruntime.UserImageInput{
				Path:     path,
				MimeType: strings.TrimSpace(part.Media.MimeType),
			})
		}
	}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = strings.TrimSpace(input.RequestID)
	}

	return agentruntime.PrepareInput{
		SessionID: strings.TrimSpace(input.SessionID),
		RunID:     runID,
		Workdir:   strings.TrimSpace(input.Workdir),
		Text:      strings.Join(textParts, "\n"),
		Images:    images,
	}
}

// convertRuntimeEvent 将 runtime 事件映射为 gateway 事件，保证 stream relay 只关心统一契约。
func convertRuntimeEvent(event agentruntime.RuntimeEvent) gateway.RuntimeEvent {
	payload := map[string]any{
		"runtime_event_type": string(event.Type),
		"turn":               event.Turn,
		"phase":              strings.TrimSpace(event.Phase),
		"timestamp":          event.Timestamp,
		"payload_version":    event.PayloadVersion,
		"payload":            event.Payload,
	}
	return gateway.RuntimeEvent{
		Type:      mapRuntimeEventType(event.Type),
		RunID:     strings.TrimSpace(event.RunID),
		SessionID: strings.TrimSpace(event.SessionID),
		Payload:   payload,
	}
}

// mapRuntimeEventType 收敛 runtime 细粒度事件到网关约定的进度/完成/错误三态。
func mapRuntimeEventType(eventType agentruntime.EventType) gateway.RuntimeEventType {
	switch eventType {
	case agentruntime.EventAgentDone:
		return gateway.RuntimeEventTypeRunDone
	case agentruntime.EventError, agentruntime.EventRunCanceled:
		return gateway.RuntimeEventTypeRunError
	default:
		return gateway.RuntimeEventTypeRunProgress
	}
}

func convertRuntimeTodoSnapshot(snapshot agentruntime.TodoSnapshot) gateway.TodoSnapshot {
	converted := gateway.TodoSnapshot{
		Summary: gateway.TodoSummary{
			Total:             snapshot.Summary.Total,
			RequiredTotal:     snapshot.Summary.RequiredTotal,
			RequiredCompleted: snapshot.Summary.RequiredCompleted,
			RequiredFailed:    snapshot.Summary.RequiredFailed,
			RequiredOpen:      snapshot.Summary.RequiredOpen,
		},
	}
	if len(snapshot.Items) == 0 {
		return converted
	}
	converted.Items = make([]gateway.TodoViewItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		converted.Items = append(converted.Items, gateway.TodoViewItem{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        strings.TrimSpace(item.Status),
			Required:      item.Required,
			Artifacts:     append([]string(nil), item.Artifacts...),
			FailureReason: strings.TrimSpace(item.FailureReason),
			BlockedReason: strings.TrimSpace(item.BlockedReason),
			Revision:      item.Revision,
		})
	}
	return converted
}

func convertRuntimeSnapshot(snapshot agentruntime.RuntimeSnapshot) gateway.RuntimeSnapshot {
	return gateway.RuntimeSnapshot{
		RunID:     strings.TrimSpace(snapshot.RunID),
		SessionID: strings.TrimSpace(snapshot.SessionID),
		Phase:     strings.TrimSpace(snapshot.Phase),
		TaskKind:  strings.TrimSpace(snapshot.TaskKind),
		UpdatedAt: snapshot.UpdatedAt,
		Todos:     convertRuntimeTodoSnapshot(snapshot.Todos),
		Facts: map[string]any{
			"runtime_facts": snapshot.Facts.RuntimeFacts,
		},
		Decision: map[string]any{
			"status":                strings.TrimSpace(snapshot.Decision.Status),
			"stop_reason":           strings.TrimSpace(snapshot.Decision.StopReason),
			"missing_facts":         snapshot.Decision.MissingFacts,
			"required_next_actions": snapshot.Decision.RequiredNextActions,
			"user_visible_summary":  strings.TrimSpace(snapshot.Decision.UserVisibleSummary),
			"internal_summary":      strings.TrimSpace(snapshot.Decision.InternalSummary),
		},
		SubAgents: map[string]any{
			"started_count":   snapshot.SubAgents.StartedCount,
			"completed_count": snapshot.SubAgents.CompletedCount,
			"failed_count":    snapshot.SubAgents.FailedCount,
		},
	}
}

func buildRuntimeTodoSnapshotFromSessionTodos(items []agentsession.TodoItem) agentruntime.TodoSnapshot {
	if len(items) == 0 {
		return agentruntime.TodoSnapshot{}
	}
	converted := make([]agentruntime.TodoViewItem, 0, len(items))
	summary := agentruntime.TodoSummary{Total: len(items)}
	requiredTotal := 0
	requiredCompleted := 0
	requiredFailed := 0
	requiredOpen := 0
	for _, item := range items {
		required := item.RequiredValue()
		if required {
			requiredTotal++
			switch item.Status {
			case agentsession.TodoStatusCompleted:
				requiredCompleted++
			case agentsession.TodoStatusFailed:
				requiredFailed++
			case agentsession.TodoStatusCanceled:
			default:
				requiredOpen++
			}
		}
		converted = append(converted, agentruntime.TodoViewItem{
			ID:            strings.TrimSpace(item.ID),
			Content:       strings.TrimSpace(item.Content),
			Status:        strings.TrimSpace(string(item.Status)),
			Required:      required,
			Artifacts:     append([]string(nil), item.Artifacts...),
			FailureReason: strings.TrimSpace(item.FailureReason),
			BlockedReason: strings.TrimSpace(string(item.BlockedReasonValue())),
			Revision:      item.Revision,
		})
	}
	summary.RequiredTotal = requiredTotal
	summary.RequiredCompleted = requiredCompleted
	summary.RequiredFailed = requiredFailed
	summary.RequiredOpen = requiredOpen
	return agentruntime.TodoSnapshot{
		Items:   converted,
		Summary: summary,
	}
}

// convertSessionMessages 将会话消息列表转换为 gateway 统一输出格式。
func convertSessionMessages(messages []providertypes.Message) []gateway.SessionMessage {
	if len(messages) == 0 {
		return nil
	}

	converted := make([]gateway.SessionMessage, 0, len(messages))
	for _, message := range messages {
		convertedMessage := gateway.SessionMessage{
			Role:       strings.TrimSpace(message.Role),
			Content:    renderSessionMessageContent(message.Parts),
			ToolCallID: strings.TrimSpace(message.ToolCallID),
			IsError:    message.IsError,
		}
		if len(message.ToolCalls) > 0 {
			convertedMessage.ToolCalls = make([]gateway.ToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				convertedMessage.ToolCalls = append(convertedMessage.ToolCalls, gateway.ToolCall{
					ID:        strings.TrimSpace(call.ID),
					Name:      strings.TrimSpace(call.Name),
					Arguments: call.Arguments,
				})
			}
		}
		converted = append(converted, convertedMessage)
	}
	return converted
}

// convertRuntimeSessionToGatewaySession 将 runtime 会话结构映射为 gateway 契约返回值。
func convertRuntimeSessionToGatewaySession(session agentsession.Session) gateway.Session {
	return gateway.Session{
		ID:        strings.TrimSpace(session.ID),
		Title:     strings.TrimSpace(session.Title),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Workdir:   strings.TrimSpace(session.Workdir),
		Provider:  strings.TrimSpace(session.Provider),
		Model:     strings.TrimSpace(session.Model),
		Messages:  convertSessionMessages(session.Messages),
	}
}

// convertRuntimeSkillSource 将 runtime 技能来源映射为 gateway 输出结构。
func convertRuntimeSkillSource(source skills.Source) gateway.SkillSource {
	return gateway.SkillSource{
		Kind:     strings.TrimSpace(string(source.Kind)),
		Layer:    strings.TrimSpace(string(source.Layer)),
		RootDir:  strings.TrimSpace(source.RootDir),
		SkillDir: strings.TrimSpace(source.SkillDir),
		FilePath: strings.TrimSpace(source.FilePath),
	}
}

// convertRuntimeSkillDescriptor 将 runtime 技能描述映射为 gateway 输出结构。
func convertRuntimeSkillDescriptor(descriptor skills.Descriptor) gateway.SkillDescriptor {
	return gateway.SkillDescriptor{
		ID:          strings.TrimSpace(descriptor.ID),
		Name:        strings.TrimSpace(descriptor.Name),
		Description: strings.TrimSpace(descriptor.Description),
		Version:     strings.TrimSpace(descriptor.Version),
		Source:      convertRuntimeSkillSource(descriptor.Source),
		Scope:       strings.TrimSpace(string(descriptor.Scope)),
	}
}

// convertRuntimeSessionSkillStates 将 runtime 会话技能状态映射为 gateway 契约结构。
func convertRuntimeSessionSkillStates(states []agentruntime.SessionSkillState) []gateway.SessionSkillState {
	if len(states) == 0 {
		return nil
	}
	converted := make([]gateway.SessionSkillState, 0, len(states))
	for _, state := range states {
		item := gateway.SessionSkillState{
			SkillID: strings.TrimSpace(state.SkillID),
			Missing: state.Missing,
		}
		if state.Descriptor != nil {
			descriptor := convertRuntimeSkillDescriptor(*state.Descriptor)
			item.Descriptor = &descriptor
		}
		converted = append(converted, item)
	}
	return converted
}

// convertRuntimeAvailableSkillStates 将 runtime 可用技能状态映射为 gateway 契约结构。
func convertRuntimeAvailableSkillStates(states []agentruntime.AvailableSkillState) []gateway.AvailableSkillState {
	if len(states) == 0 {
		return nil
	}
	converted := make([]gateway.AvailableSkillState, 0, len(states))
	for _, state := range states {
		converted = append(converted, gateway.AvailableSkillState{
			Descriptor: convertRuntimeSkillDescriptor(state.Descriptor),
			Active:     state.Active,
		})
	}
	return converted
}

// renderSessionMessageContent 将 provider 多段内容渲染为对外展示的单段文本摘要。
func renderSessionMessageContent(parts []providertypes.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}

	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if text := strings.TrimSpace(part.Text); text != "" {
				segments = append(segments, text)
			}
		case providertypes.ContentPartImage:
			segments = append(segments, "[image]")
		}
	}
	return strings.Join(segments, "\n")
}

// ensureBridgeSubjectAllowed 在本地单用户 MVP 中执行最小主体校验。
func ensureBridgeSubjectAllowed(subjectID string) error {
	if strings.TrimSpace(subjectID) != bridgeLocalSubjectID {
		return gateway.ErrRuntimeAccessDenied
	}
	return nil
}

// ensureRuntimeAvailable 校验桥接器内部 runtime 是否可用。
func (b *gatewayRuntimePortBridge) ensureRuntimeAvailable() error {
	if b == nil || b.runtime == nil {
		return fmt.Errorf(bridgeRuntimeUnavailableErrMsg)
	}
	return nil
}

// ensureRuntimeAccess 组合校验 runtime 可用性与请求主体权限。
func (b *gatewayRuntimePortBridge) ensureRuntimeAccess(subjectID string) error {
	if err := b.ensureRuntimeAvailable(); err != nil {
		return err
	}
	return ensureBridgeSubjectAllowed(subjectID)
}

// isRuntimeNotFoundError 判断运行时错误是否属于目标不存在场景。
func isRuntimeNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, agentsession.ErrSessionNotFound) || errors.Is(err, os.ErrNotExist)
}

// resolveListFilesRoot 按请求、会话、全局配置的优先级确定文件树根目录。
func (b *gatewayRuntimePortBridge) resolveListFilesRoot(
	ctx context.Context,
	input gateway.ListFilesInput,
) (string, error) {
	root := strings.TrimSpace(input.Workdir)
	if root == "" && strings.TrimSpace(input.SessionID) != "" && b.sessionStore != nil {
		session, err := b.loadStoredSession(ctx, strings.TrimSpace(input.SessionID))
		if err != nil && !isRuntimeNotFoundError(err) {
			return "", err
		}
		root = strings.TrimSpace(session.Workdir)
	}
	if root == "" {
		root = strings.TrimSpace(b.currentConfig().Workdir)
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

// loadStoredSession 通过可选的会话加载接口读取持久会话。
func (b *gatewayRuntimePortBridge) loadStoredSession(ctx context.Context, sessionID string) (agentsession.Session, error) {
	if b == nil || b.sessionStore == nil {
		return agentsession.Session{}, fmt.Errorf("gateway runtime bridge: session store is unavailable")
	}
	loader, ok := b.sessionStore.(bridgeSessionLoader)
	if !ok {
		return agentsession.Session{}, fmt.Errorf("gateway runtime bridge: session store does not support load session")
	}
	return loader.LoadSession(ctx, strings.TrimSpace(sessionID))
}

// resolveSafeListFilesPath 将前端传入的相对路径限制在根目录内。
func resolveSafeListFilesPath(root string, rawPath string) (string, string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", err
	}
	requested := strings.TrimSpace(rawPath)
	if requested == "" || requested == "." {
		requested = "."
	}
	requested = filepath.Clean(filepath.FromSlash(requested))
	if filepath.IsAbs(requested) {
		return "", "", fmt.Errorf("gateway runtime bridge: listFiles path must be relative")
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, requested))
	if err != nil {
		return "", "", err
	}
	rootForCheck := rootAbs
	if resolvedRoot, resolveErr := filepath.EvalSymlinks(rootAbs); resolveErr == nil {
		rootForCheck = resolvedRoot
	}
	targetForCheck := targetAbs
	if resolvedTarget, resolveErr := filepath.EvalSymlinks(targetAbs); resolveErr == nil {
		targetForCheck = resolvedTarget
	}
	relative, err := filepath.Rel(rootForCheck, targetForCheck)
	if err != nil {
		return "", "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", fmt.Errorf("gateway runtime bridge: listFiles path escapes workdir")
	}
	if relative == "" {
		relative = "."
	}
	return targetAbs, filepath.ToSlash(relative), nil
}

// resolveProviderModelForSession 校验并解析会话级 provider/model 选择。
func (b *gatewayRuntimePortBridge) resolveProviderModelForSession(
	ctx context.Context,
	session agentsession.Session,
	providerID string,
	modelID string,
) (string, string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", "", fmt.Errorf("gateway runtime bridge: model_id is required")
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		providerID = strings.TrimSpace(session.Provider)
	}
	if providerID == "" {
		providerID = strings.TrimSpace(b.currentConfig().SelectedProvider)
	}
	if b.providerSelection == nil {
		return "", "", fmt.Errorf("gateway runtime bridge: provider selection is unavailable")
	}
	options, err := b.providerSelection.ListProviderOptions(ctx)
	if err != nil {
		return "", "", err
	}
	inferredProvider := ""
	for _, option := range options {
		optionID := strings.TrimSpace(option.ID)
		for _, model := range option.Models {
			if strings.EqualFold(strings.TrimSpace(model.ID), modelID) {
				if providerID != "" && strings.EqualFold(providerID, optionID) {
					return optionID, strings.TrimSpace(model.ID), nil
				}
				if inferredProvider == "" {
					inferredProvider = optionID
				}
			}
		}
	}
	if providerID == "" && inferredProvider != "" {
		return inferredProvider, modelID, nil
	}
	if providerID != "" {
		return "", "", fmt.Errorf("gateway runtime bridge: model %q not found for provider %q", modelID, providerID)
	}
	return "", "", fmt.Errorf("gateway runtime bridge: model %q not found", modelID)
}

// modelDisplayName 从 provider 候选中查找模型展示名，找不到时回退模型 ID。
func (b *gatewayRuntimePortBridge) modelDisplayName(ctx context.Context, providerID string, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || b.providerSelection == nil {
		return modelID
	}
	options, err := b.providerSelection.ListProviderOptions(ctx)
	if err != nil {
		return modelID
	}
	for _, option := range options {
		if providerID != "" && !strings.EqualFold(strings.TrimSpace(option.ID), strings.TrimSpace(providerID)) {
			continue
		}
		for _, model := range option.Models {
			if strings.EqualFold(strings.TrimSpace(model.ID), modelID) {
				if name := strings.TrimSpace(model.Name); name != "" {
					return name
				}
				return strings.TrimSpace(model.ID)
			}
		}
	}
	return modelID
}

// currentConfig 返回当前配置快照；桥接器未绑定配置管理器时退回静态默认值。
func (b *gatewayRuntimePortBridge) currentConfig() config.Config {
	if b == nil || b.configManager == nil {
		return config.StaticDefaults().Clone()
	}
	return b.configManager.Get()
}

// marshalManualModelsForGateway 将前端模型描述转换为创建自定义 provider 的手工模型 JSON。
func marshalManualModelsForGateway(models []providertypes.ModelDescriptor) string {
	if len(models) == 0 {
		return ""
	}
	payload := make([]manualModelPayload, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		name := strings.TrimSpace(model.Name)
		if id == "" {
			continue
		}
		if name == "" {
			name = id
		}
		item := manualModelPayload{ID: id, Name: name}
		if model.ContextWindow > 0 {
			item.ContextWindow = model.ContextWindow
		}
		if model.MaxOutputTokens > 0 {
			item.MaxOutputTokens = model.MaxOutputTokens
		}
		payload = append(payload, item)
	}
	if len(payload) == 0 {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

type manualModelPayload struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ContextWindow   int    `json:"context_window,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

var _ gateway.RuntimePort = (*gatewayRuntimePortBridge)(nil)
