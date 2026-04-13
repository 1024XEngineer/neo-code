package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/approval"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const (
	defaultProviderRetryMax = 2
	providerRetryBaseWait   = 1 * time.Second
	providerRetryMaxWait    = 5 * time.Second
	defaultMaxLoops         = 8
)

// Runtime 定义 runtime 对外暴露的运行、压缩与审批接口。
type Runtime interface {
	Run(ctx context.Context, input UserInput) error
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]agentsession.Summary, error)
	LoadSession(ctx context.Context, id string) (agentsession.Session, error)
}

// UserInput 描述一次用户输入请求的最小运行参数。
type UserInput struct {
	SessionID string
	RunID     string
	Content   string
	Workdir   string
}

// ProviderFactory 负责基于运行期配置创建 provider 实例。
type ProviderFactory interface {
	Build(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error)
}

// MemoExtractor 定义 runtime 层调用记忆提取的最小能力。
// 通过接口注入避免 runtime 直接依赖 memo 子系统实现细节。
type MemoExtractor interface {
	// ExtractAndStore 从对话消息中提取并落盘记忆，失败由实现方自行处理。
	ExtractAndStore(ctx context.Context, messages []providertypes.Message)
}

// Service 是 runtime 的默认实现，负责组织一次完整的 agent 运行闭环。
type Service struct {
	configManager   *config.Manager
	sessionStore    agentsession.Store
	toolManager     tools.Manager
	providerFactory ProviderFactory
	contextBuilder  agentcontext.Builder
	compactRunner   contextcompact.Runner
	approvalBroker  *approval.Broker
	memoExtractor   MemoExtractor

	events          chan RuntimeEvent
	sessionMu       sync.Mutex
	sessionLocks    map[string]*sync.Mutex
	runMu           sync.Mutex
	activeRunToken  uint64
	nextRunToken    uint64
	activeRunCancel context.CancelFunc
}

// NewWithFactory 使用注入依赖构建默认 runtime Service。
func NewWithFactory(
	configManager *config.Manager,
	toolManager tools.Manager,
	sessionStore agentsession.Store,
	providerFactory ProviderFactory,
	contextBuilder agentcontext.Builder,
) *Service {
	if providerFactory == nil {
		providerFactory = provider.NewRegistry()
	}
	if toolManager == nil {
		toolManager = tools.NewRegistry()
	}
	if contextBuilder == nil {
		contextBuilder = agentcontext.NewBuilderWithToolPolicies(toolManager)
	}

	return &Service{
		configManager:   configManager,
		sessionStore:    sessionStore,
		toolManager:     toolManager,
		providerFactory: providerFactory,
		contextBuilder:  contextBuilder,
		approvalBroker:  approval.NewBroker(),
		events:          make(chan RuntimeEvent, 128),
		sessionLocks:    make(map[string]*sync.Mutex),
	}
}

// SetMemoExtractor 设置可选记忆提取钩子，由 Run 在结束时异步触发。
func (s *Service) SetMemoExtractor(extractor MemoExtractor) {
	s.memoExtractor = extractor
}

// CancelActiveRun 尝试取消当前正在执行的 Run。
func (s *Service) CancelActiveRun() bool {
	s.runMu.Lock()
	cancel := s.activeRunCancel
	s.runMu.Unlock()
	if cancel == nil {
		return false
	}

	cancel()
	return true
}

// Events 返回 runtime 事件通道，供上层 UI 订阅。
func (s *Service) Events() <-chan RuntimeEvent {
	return s.events
}

// ListSessions 返回当前会话存储中的所有摘要。
func (s *Service) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return s.sessionStore.ListSummaries(ctx)
}

// LoadSession 按 id 加载完整会话内容。
func (s *Service) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	session, err := s.sessionStore.Load(ctx, id)
	if err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

// loadOrCreateSession 负责在运行开始时解析工作目录并加载或创建会话。
func (s *Service) loadOrCreateSession(
	ctx context.Context,
	sessionID string,
	title string,
	defaultWorkdir string,
	requestedWorkdir string,
) (agentsession.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionWorkdir, err := resolveWorkdirForSession(defaultWorkdir, "", requestedWorkdir)
		if err != nil {
			return agentsession.Session{}, err
		}
		session := agentsession.NewWithWorkdir(title, sessionWorkdir)
		if err := s.sessionStore.Save(ctx, &session); err != nil {
			return agentsession.Session{}, err
		}
		return session, nil
	}

	session, err := s.sessionStore.Load(ctx, sessionID)
	if err != nil {
		return agentsession.Session{}, err
	}
	if strings.TrimSpace(requestedWorkdir) == "" && strings.TrimSpace(session.Workdir) != "" {
		return session, nil
	}

	resolved, err := resolveWorkdirForSession(defaultWorkdir, session.Workdir, requestedWorkdir)
	if err != nil {
		return agentsession.Session{}, err
	}
	if session.Workdir == resolved {
		return session, nil
	}

	session.Workdir = resolved
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.Save(ctx, &session); err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

// emit 将 runtime 事件投递到事件通道，并在通道阻塞且上下文取消时返回错误。
func (s *Service) emit(ctx context.Context, kind EventType, runID string, sessionID string, payload any) error {
	evt := RuntimeEvent{
		Type:      kind,
		RunID:     runID,
		SessionID: sessionID,
		Payload:   payload,
	}
	select {
	case s.events <- evt:
		return nil
	default:
	}
	select {
	case s.events <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startRun 记录当前激活的运行取消句柄，并分配一个新的运行令牌。
func (s *Service) startRun(cancel context.CancelFunc) uint64 {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.nextRunToken++
	token := s.nextRunToken
	s.activeRunToken = token
	s.activeRunCancel = cancel
	return token
}

// finishRun 在运行结束时释放当前激活的取消句柄。
func (s *Service) finishRun(token uint64) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	if s.activeRunToken != token {
		return
	}

	s.activeRunToken = 0
	s.activeRunCancel = nil
}

// acquireSessionLock 获取指定会话的互斥锁，确保同一会话的 Run/Compact 串行执行。
// 不同会话的锁互不干扰，支持多会话并行。
func (s *Service) acquireSessionLock(sessionID string) *sync.Mutex {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	mu, ok := s.sessionLocks[sessionID]
	if !ok {
		mu = &sync.Mutex{}
		s.sessionLocks[sessionID] = mu
	}
	return mu
}

func resolveWorkdirForSession(defaultWorkdir string, currentWorkdir string, requestedWorkdir string) (string, error) {
	base := agentsession.EffectiveWorkdir(currentWorkdir, defaultWorkdir)
	if strings.TrimSpace(requestedWorkdir) == "" {
		return agentsession.ResolveExistingDir(base)
	}

	target := strings.TrimSpace(requestedWorkdir)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return agentsession.ResolveExistingDir(target)
}
