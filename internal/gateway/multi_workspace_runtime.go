package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// MultiWorkspaceRuntime 将多个工作区的 runtime 聚合为单个 gateway.RuntimePort。
// 根据连接上下文中的 workspaceHash 路由到对应工作区的 runtime。
type MultiWorkspaceRuntime struct {
	index          *agentsession.WorkspaceIndex
	bundles        map[string]*workspaceBundle
	mu             sync.RWMutex
	buildPort      func(ctx context.Context, workdir string) (RuntimePort, func() error, error)
	defaultHash    string
	managementPort ManagementRuntimePort

	runnerDispatcherInjector func(RuntimePort)

	events    chan RuntimeEvent
	eventSubs map[string]chan<- RuntimeEvent
	eventMu   sync.Mutex
	stopCh    chan struct{}
}

type workspaceBundle struct {
	port    RuntimePort
	cleanup func() error
}

// NewMultiWorkspaceRuntime 创建多工作区路由代理。
// defaultHash 是默认工作区哈希（启动时传入的 workdir），在没有显式切换前使用。
func NewMultiWorkspaceRuntime(
	index *agentsession.WorkspaceIndex,
	defaultHash string,
	buildPort func(ctx context.Context, workdir string) (RuntimePort, func() error, error),
) *MultiWorkspaceRuntime {
	return &MultiWorkspaceRuntime{
		index:       index,
		bundles:     make(map[string]*workspaceBundle),
		buildPort:   buildPort,
		defaultHash: strings.TrimSpace(defaultHash),
		events:      make(chan RuntimeEvent, 128),
		eventSubs:   make(map[string]chan<- RuntimeEvent),
		stopCh:      make(chan struct{}),
	}
}

// getPort 根据上下文中的 workspaceHash 获取对应工作区的 RuntimePort。
// 若上下文中无 workspaceHash，则回退到 defaultHash。
func (m *MultiWorkspaceRuntime) getPort(ctx context.Context) (RuntimePort, error) {
	hash := WorkspaceHashFromContext(ctx)
	if hash == "" {
		m.mu.RLock()
		hash = m.defaultHash
		m.mu.RUnlock()
	}
	if hash == "" {
		// Support startup flows where gateway preloads a default runtime bundle
		// but no explicit workspace hash has been persisted yet.
		m.mu.RLock()
		if preloaded := m.bundles[""]; preloaded != nil {
			port := preloaded.port
			m.mu.RUnlock()
			return port, nil
		}
		m.mu.RUnlock()

		records := m.index.List()
		if len(records) > 0 {
			hash = records[0].Hash
		}
	}
	if hash == "" {
		return nil, fmt.Errorf("%w: workspace hash is empty and no default configured", ErrRuntimeResourceNotFound)
	}
	return m.getPortForHash(hash)
}

func (m *MultiWorkspaceRuntime) getPortForHash(hash string) (RuntimePort, error) {
	m.mu.RLock()
	b := m.bundles[hash]
	m.mu.RUnlock()
	if b != nil {
		return b.port, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	b = m.bundles[hash]
	if b != nil {
		return b.port, nil
	}

	record, ok := m.index.Get(hash)
	if !ok {
		return nil, fmt.Errorf("%w: workspace %s not found", ErrRuntimeResourceNotFound, hash)
	}

	port, cleanup, err := m.buildPort(context.Background(), record.Path)
	if err != nil {
		return nil, fmt.Errorf("build workspace runtime for %s: %w", hash, err)
	}

	if m.runnerDispatcherInjector != nil {
		m.runnerDispatcherInjector(port)
	}

	b = &workspaceBundle{port: port, cleanup: cleanup}
	m.bundles[hash] = b
	m.startEventForwarder(hash, port)
	return port, nil
}

// PreloadWorkspaceBundle 预加载指定工作区的 bundle，避免默认工作区被重复构建。
func (m *MultiWorkspaceRuntime) PreloadWorkspaceBundle(hash string, port RuntimePort, cleanup func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bundles[hash] != nil {
		return
	}
	m.bundles[hash] = &workspaceBundle{port: port, cleanup: cleanup}
	m.startEventForwarder(hash, port)
}

func (m *MultiWorkspaceRuntime) startEventForwarder(hash string, port RuntimePort) {
	src := port.Events()
	if src == nil {
		return
	}
	go func() {
		for {
			select {
			case <-m.stopCh:
				return
			case ev, ok := <-src:
				if !ok {
					return
				}
				select {
				case <-m.stopCh:
					return
				case m.events <- ev:
				}
			}
		}
	}()
}

// InjectRunnerDispatcher 设置 runner tool dispatcher 注入回调。
// fn 对每个已加载或未来创建的 RuntimePort 调用一次。
func (m *MultiWorkspaceRuntime) InjectRunnerDispatcher(fn func(RuntimePort)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.runnerDispatcherInjector = fn

	for _, b := range m.bundles {
		fn(b.port)
	}
}

// Close 优雅关闭所有已加载的工作区 runtime。
func (m *MultiWorkspaceRuntime) Close() error {
	close(m.stopCh)

	m.mu.Lock()
	bundles := make(map[string]*workspaceBundle, len(m.bundles))
	for k, v := range m.bundles {
		bundles[k] = v
	}
	m.mu.Unlock()

	var firstErr error
	for hash, b := range bundles {
		if err := b.cleanup(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close workspace %s: %w", hash, err)
		}
	}
	return firstErr
}

// ---- Workspace management (non-RuntimePort) ----

// SwitchWorkspace 将指定连接的当前工作区切换到目标哈希。
func (m *MultiWorkspaceRuntime) SwitchWorkspace(ctx context.Context, hash string) error {
	_, ok := m.index.Get(hash)
	if !ok {
		return fmt.Errorf("%w: workspace %s not found", ErrRuntimeResourceNotFound, hash)
	}
	// 预加载对应 runtime，确保后续请求可用
	if _, err := m.getPortForHash(hash); err != nil {
		return err
	}
	return nil
}

// ListWorkspaces 返回索引中的所有工作区。
func (m *MultiWorkspaceRuntime) ListWorkspaces() []agentsession.WorkspaceRecord {
	return m.index.List()
}

// CreateWorkspace 注册一个新工作区到索引。
func (m *MultiWorkspaceRuntime) CreateWorkspace(path, name string) (agentsession.WorkspaceRecord, error) {
	record, err := m.index.Register(path, name)
	if err != nil {
		return agentsession.WorkspaceRecord{}, err
	}
	if saveErr := m.index.Save(); saveErr != nil {
		log.Printf("workspace index save after create error: %v", saveErr)
	}
	return record, nil
}

// RenameWorkspace 重命名工作区。
func (m *MultiWorkspaceRuntime) RenameWorkspace(hash, name string) error {
	if _, err := m.index.Rename(hash, name); err != nil {
		return err
	}
	if saveErr := m.index.Save(); saveErr != nil {
		log.Printf("workspace index save after rename error: %v", saveErr)
	}
	return nil
}

// DeleteWorkspace 删除工作区。
func (m *MultiWorkspaceRuntime) DeleteWorkspace(hash string, removeData bool) error {
	_, err := m.index.Delete(hash, removeData)
	if err != nil {
		return err
	}

	m.mu.Lock()
	b, ok := m.bundles[hash]
	if ok {
		delete(m.bundles, hash)
	}
	if strings.EqualFold(strings.TrimSpace(hash), strings.TrimSpace(m.defaultHash)) {
		m.defaultHash = ""
		records := m.index.List()
		if len(records) > 0 {
			m.defaultHash = strings.TrimSpace(records[0].Hash)
		}
	}
	m.mu.Unlock()

	if ok && b != nil && b.cleanup != nil {
		if cerr := b.cleanup(); cerr != nil {
			log.Printf("workspace cleanup %s error: %v", hash, cerr)
		}
	}
	if saveErr := m.index.Save(); saveErr != nil {
		log.Printf("workspace index save after delete error: %v", saveErr)
	}
	return nil
}

// ---- RuntimePort implementation ----

func (m *MultiWorkspaceRuntime) Run(ctx context.Context, input RunInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.Run(ctx, input)
}

// Ask 将 ask 请求路由到当前工作区 RuntimePort。
func (m *MultiWorkspaceRuntime) Ask(ctx context.Context, input AskInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.Ask(ctx, input)
}

// DeleteAskSession 将 ask 会话删除请求路由到当前工作区 RuntimePort。
func (m *MultiWorkspaceRuntime) DeleteAskSession(ctx context.Context, input DeleteAskSessionInput) (bool, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return false, err
	}
	return port.DeleteAskSession(ctx, input)
}

func (m *MultiWorkspaceRuntime) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return CompactResult{}, err
	}
	return port.Compact(ctx, input)
}

func (m *MultiWorkspaceRuntime) ExecuteSystemTool(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return port.ExecuteSystemTool(ctx, input)
}

func (m *MultiWorkspaceRuntime) ActivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.ActivateSessionSkill(ctx, input)
}

func (m *MultiWorkspaceRuntime) DeactivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.DeactivateSessionSkill(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListSessionSkills(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListSessionSkills(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListAvailableSkills(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListAvailableSkills(ctx, input)
}

func (m *MultiWorkspaceRuntime) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.ResolvePermission(ctx, input)
}

func (m *MultiWorkspaceRuntime) CancelRun(ctx context.Context, input CancelInput) (bool, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return false, err
	}
	return port.CancelRun(ctx, input)
}

func (m *MultiWorkspaceRuntime) Events() <-chan RuntimeEvent {
	return m.events
}

func (m *MultiWorkspaceRuntime) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListSessions(ctx)
}

func (m *MultiWorkspaceRuntime) LoadSession(ctx context.Context, input LoadSessionInput) (Session, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return Session{}, err
	}
	return port.LoadSession(ctx, input)
}

func (m *MultiWorkspaceRuntime) CreateSession(ctx context.Context, input CreateSessionInput) (string, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return "", err
	}
	return port.CreateSession(ctx, input)
}

func (m *MultiWorkspaceRuntime) DeleteSession(ctx context.Context, input DeleteSessionInput) (bool, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return false, err
	}
	return port.DeleteSession(ctx, input)
}

func (m *MultiWorkspaceRuntime) RenameSession(ctx context.Context, input RenameSessionInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.RenameSession(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListFiles(ctx context.Context, input ListFilesInput) ([]FileEntry, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListFiles(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListModels(ctx context.Context, input ListModelsInput) ([]ModelEntry, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListModels(ctx, input)
}

func (m *MultiWorkspaceRuntime) SetSessionModel(ctx context.Context, input SetSessionModelInput) error {
	port, err := m.getPort(ctx)
	if err != nil {
		return err
	}
	return port.SetSessionModel(ctx, input)
}

func (m *MultiWorkspaceRuntime) GetSessionModel(ctx context.Context, input GetSessionModelInput) (SessionModelResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return SessionModelResult{}, err
	}
	return port.GetSessionModel(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListCheckpoints(ctx context.Context, input ListCheckpointsInput) ([]CheckpointEntry, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return nil, err
	}
	return port.ListCheckpoints(ctx, input)
}

func (m *MultiWorkspaceRuntime) RestoreCheckpoint(ctx context.Context, input CheckpointRestoreInput) (CheckpointRestoreResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return CheckpointRestoreResult{}, err
	}
	return port.RestoreCheckpoint(ctx, input)
}

func (m *MultiWorkspaceRuntime) UndoRestore(ctx context.Context, input UndoRestoreInput) (CheckpointRestoreResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return CheckpointRestoreResult{}, err
	}
	return port.UndoRestore(ctx, input)
}

func (m *MultiWorkspaceRuntime) CheckpointDiff(ctx context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return CheckpointDiffResult{}, err
	}
	return port.CheckpointDiff(ctx, input)
}

func (m *MultiWorkspaceRuntime) ListSessionTodos(ctx context.Context, input ListSessionTodosInput) (TodoSnapshot, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return TodoSnapshot{}, err
	}
	return port.ListSessionTodos(ctx, input)
}

func (m *MultiWorkspaceRuntime) GetRuntimeSnapshot(ctx context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error) {
	port, err := m.getPort(ctx)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return port.GetRuntimeSnapshot(ctx, input)
}

// SetManagementPort sets the management port for delegating ManagementRuntimePort calls.
func (m *MultiWorkspaceRuntime) SetManagementPort(port ManagementRuntimePort) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.managementPort = port
}

func (m *MultiWorkspaceRuntime) getManagementPort() (ManagementRuntimePort, error) {
	m.mu.RLock()
	mp := m.managementPort
	m.mu.RUnlock()
	if mp == nil {
		return nil, fmt.Errorf("management port is unavailable")
	}
	return mp, nil
}

// syncAllWorkspaceConfigs 让所有已加载工作区从磁盘重新加载配置快照。
// 在管理操作写入配置后调用，确保运行时端口能看到最新状态。
func (m *MultiWorkspaceRuntime) syncAllWorkspaceConfigs(ctx context.Context) {
	m.mu.RLock()
	bundles := make([]*workspaceBundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		bundles = append(bundles, b)
	}
	m.mu.RUnlock()

	for _, b := range bundles {
		type configReloader interface {
			ReloadConfig(ctx context.Context) error
		}
		if reloader, ok := b.port.(configReloader); ok {
			_ = reloader.ReloadConfig(ctx)
		}
	}
}

// syncAllWorkspaceMCP 让所有已加载工作区根据当前配置重建 MCP Server 注册表。
func (m *MultiWorkspaceRuntime) syncAllWorkspaceMCP() {
	m.mu.RLock()
	bundles := make([]*workspaceBundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		bundles = append(bundles, b)
	}
	m.mu.RUnlock()

	for _, b := range bundles {
		type mcpRebuilder interface {
			RebuildMCPServers() error
		}
		if rebuilder, ok := b.port.(mcpRebuilder); ok {
			_ = rebuilder.RebuildMCPServers()
		}
	}
}

// ---- ManagementRuntimePort implementation ----

func (m *MultiWorkspaceRuntime) ListProviders(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error) {
	mp, err := m.getManagementPort()
	if err != nil {
		return nil, err
	}
	return mp.ListProviders(ctx, input)
}

func (m *MultiWorkspaceRuntime) CreateProvider(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error) {
	mp, err := m.getManagementPort()
	if err != nil {
		return ProviderSelectionResult{}, err
	}
	result, err := mp.CreateProvider(ctx, input)
	if err != nil {
		return ProviderSelectionResult{}, err
	}
	m.syncAllWorkspaceConfigs(ctx)
	return result, nil
}

func (m *MultiWorkspaceRuntime) DeleteProvider(ctx context.Context, input DeleteProviderInput) error {
	mp, err := m.getManagementPort()
	if err != nil {
		return err
	}
	if err := mp.DeleteProvider(ctx, input); err != nil {
		return err
	}
	m.syncAllWorkspaceConfigs(ctx)
	return nil
}

func (m *MultiWorkspaceRuntime) SelectProviderModel(ctx context.Context, input SelectProviderModelInput) (ProviderSelectionResult, error) {
	mp, err := m.getManagementPort()
	if err != nil {
		return ProviderSelectionResult{}, err
	}
	result, err := mp.SelectProviderModel(ctx, input)
	if err != nil {
		return ProviderSelectionResult{}, err
	}
	m.syncAllWorkspaceConfigs(ctx)
	return result, nil
}

func (m *MultiWorkspaceRuntime) ListMCPServers(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error) {
	mp, err := m.getManagementPort()
	if err != nil {
		return nil, err
	}
	return mp.ListMCPServers(ctx, input)
}

func (m *MultiWorkspaceRuntime) UpsertMCPServer(ctx context.Context, input UpsertMCPServerInput) error {
	mp, err := m.getManagementPort()
	if err != nil {
		return err
	}
	if err := mp.UpsertMCPServer(ctx, input); err != nil {
		return err
	}
	m.syncAllWorkspaceConfigs(ctx)
	m.syncAllWorkspaceMCP()
	return nil
}

func (m *MultiWorkspaceRuntime) SetMCPServerEnabled(ctx context.Context, input SetMCPServerEnabledInput) error {
	mp, err := m.getManagementPort()
	if err != nil {
		return err
	}
	if err := mp.SetMCPServerEnabled(ctx, input); err != nil {
		return err
	}
	m.syncAllWorkspaceConfigs(ctx)
	m.syncAllWorkspaceMCP()
	return nil
}

func (m *MultiWorkspaceRuntime) DeleteMCPServer(ctx context.Context, input DeleteMCPServerInput) error {
	mp, err := m.getManagementPort()
	if err != nil {
		return err
	}
	if err := mp.DeleteMCPServer(ctx, input); err != nil {
		return err
	}
	m.syncAllWorkspaceConfigs(ctx)
	m.syncAllWorkspaceMCP()
	return nil
}
