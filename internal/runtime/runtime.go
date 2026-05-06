package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"neo-code/internal/checkpoint"
	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/context/repository"
	"neo-code/internal/provider"
	"neo-code/internal/provider/builtin"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/approval"
	runtimehooks "neo-code/internal/runtime/hooks"
	"neo-code/internal/security"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

const (
	defaultToolParallelism = 4

	terminationEventEmitTimeout = 500 * time.Millisecond
)

// Runtime 定义 runtime 对外暴露的运行、压缩与审批接口。
type Runtime interface {
	Submit(ctx context.Context, input PrepareInput) error
	PrepareUserInput(ctx context.Context, input PrepareInput) (UserInput, error)
	Run(ctx context.Context, input UserInput) error
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	ExecuteSystemTool(ctx context.Context, input SystemToolInput) (tools.ToolResult, error)
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]agentsession.Summary, error)
	LoadSession(ctx context.Context, id string) (agentsession.Session, error)
	ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	ListSessionSkills(ctx context.Context, sessionID string) ([]SessionSkillState, error)
	ListAvailableSkills(ctx context.Context, sessionID string) ([]AvailableSkillState, error)
}

// PlanApprover 定义显式批准当前完整计划 revision 的可选 runtime 能力。
type PlanApprover interface {
	ApproveCurrentPlan(ctx context.Context, input ApproveCurrentPlanInput) error
}

// UserInput 描述一次用户输入请求的最小运行参数。
type UserInput struct {
	SessionID       string
	RunID           string
	Parts           []providertypes.ContentPart
	Workdir         string
	Mode            string
	TaskID          string
	AgentID         string
	CapabilityToken *security.CapabilityToken
}

// UserImageInput 表示用户输入中附带的单个图片引用（路径 + MIME）。
type UserImageInput struct {
	Path     string
	MimeType string
}

// PrepareInput 表示进入 runtime 归一化前的领域输入（仅包含文本/图片/会话上下文）。
type PrepareInput struct {
	SessionID        string
	RunID            string
	Workdir          string
	Mode             string
	Text             string
	Images           []UserImageInput
	ThinkingOverride *ThinkingOverride `json:"thinking_override,omitempty"`
}

// ThinkingOverride 表示用户对 thinking 能力的运行时偏好。
type ThinkingOverride struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

// SystemToolInput 描述一次由系统入口触发的确定性工具执行请求。
type SystemToolInput struct {
	SessionID string
	RunID     string
	Workdir   string
	ToolName  string
	Arguments []byte
}

// PreparedInputResult 描述输入归一化完成后的结果快照（标准 UserInput + 本轮保存附件元数据）。
type PreparedInputResult struct {
	UserInput   UserInput
	SavedAssets []agentsession.AssetMeta
}

// ApproveCurrentPlanInput 描述一次显式批准当前完整计划 revision 的最小输入。
type ApproveCurrentPlanInput struct {
	SessionID string
	PlanID    string
	Revision  int
}

// UserInputPreparer 定义 runtime 输入归一化能力：会话绑定、附件持久化与 parts 组装。
type UserInputPreparer interface {
	Prepare(ctx context.Context, input PrepareInput, defaultWorkdir string) (PreparedInputResult, error)
}

// ProviderFactory 负责基于运行期配置创建 provider 实例。
type ProviderFactory interface {
	Build(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error)
}

// MemoExtractor 定义 runtime 层调用记忆提取的最小能力。
// 通过接口注入避免 runtime 直接依赖 memo 子系统实现细节。
type MemoExtractor interface {
	// Schedule 从消息中安排一次后台记忆提取，失败由实现方自行处理。
	Schedule(sessionID string, messages []providertypes.Message)
}

// BudgetResolution 描述 budget 解析的结构化结果。
type BudgetResolution struct {
	PromptBudget  int
	Source        string
	ContextWindow int
}

// BudgetResolver 定义 prompt budget 解析能力，避免 runtime 直接处理模型目录细节。
type BudgetResolver interface {
	ResolvePromptBudget(ctx context.Context, cfg config.Config) (BudgetResolution, error)
}

// repositoryFactService 约束 runtime 条件化获取仓库事实所需的最小能力。
type repositoryFactService interface {
	Inspect(ctx context.Context, workdir string, opts repository.InspectOptions) (repository.InspectResult, error)
	Retrieve(ctx context.Context, workdir string, query repository.RetrievalQuery) (repository.RetrievalResult, error)
}

type runtimeSessionTitleUpdater interface {
	UpdateSessionTitle(ctx context.Context, input agentsession.UpdateSessionTitleInput) error
}

type Service struct {
	configManager     *config.Manager
	sessionStore      agentsession.Store
	sessionAssetStore agentsession.AssetStore
	userInputPreparer UserInputPreparer
	toolManager       tools.Manager
	providerFactory   ProviderFactory
	contextBuilder    agentcontext.Builder
	repositoryService repositoryFactService
	compactRunner     contextcompact.Runner
	approvalBroker    *approval.Broker
	memoExtractor     MemoExtractor
	skillsRegistry    skills.Registry
	budgetResolver    BudgetResolver
	hookExecutor      HookExecutor
	checkpointStore   checkpoint.CheckpointStore
	perEditStore      *checkpoint.PerEditSnapshotStore

	events             chan RuntimeEvent
	runtimeSnapshotMu  sync.Mutex
	runtimeSnapshots   map[string]RuntimeSnapshot
	sessionMu          sync.Mutex
	sessionLocks       map[string]*sessionLockEntry
	runMu              sync.Mutex
	activeRunToken     uint64
	nextRunToken       uint64
	activeRunCancels   map[uint64]context.CancelFunc
	activeRunByID      map[string]uint64
	activeRunTokenIDs  map[uint64]string
	activeRunStates    map[uint64]*runState
	permissionAskMapMu sync.Mutex
	permissionAskLocks map[string]*permissionAskLockEntry

	thinkingEnabled bool
}

// sessionLockEntry 维护单个会话读写锁及其当前引用计数，用于在无引用时回收 map 项。
type sessionLockEntry struct {
	mu   sync.RWMutex
	refs int
}

// permissionAskLockEntry 维护单个运行的审批串行锁与引用计数。
type permissionAskLockEntry struct {
	mu   sync.Mutex
	refs int
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
		registry, err := builtin.NewRegistry()
		if err != nil {
			panic(fmt.Sprintf("runtime: init builtin provider registry: %v", err))
		}
		providerFactory = registry
	}
	if toolManager == nil {
		toolManager = tools.NewRegistry()
	}
	if contextBuilder == nil {
		contextBuilder = agentcontext.NewConfiguredBuilder(agentcontext.MicroCompactConfig{
			Policies:    toolManager,
			Summarizers: toolManager,
		})
	}

	service := &Service{
		configManager:      configManager,
		sessionStore:       sessionStore,
		toolManager:        toolManager,
		providerFactory:    providerFactory,
		contextBuilder:     contextBuilder,
		repositoryService:  repository.NewService(),
		approvalBroker:     approval.NewBroker(),
		events:             make(chan RuntimeEvent, 128),
		runtimeSnapshots:   make(map[string]RuntimeSnapshot),
		sessionLocks:       make(map[string]*sessionLockEntry),
		permissionAskLocks: make(map[string]*permissionAskLockEntry),
		activeRunCancels:   make(map[uint64]context.CancelFunc),
		activeRunByID:      make(map[string]uint64),
		activeRunTokenIDs:  make(map[uint64]string),
		activeRunStates:    make(map[uint64]*runState),
		thinkingEnabled:    true,
	}
	baseHookExecutor := runtimehooks.NewExecutor(runtimehooks.NewRegistry(), newHookRuntimeEventEmitter(service), runtimehooks.DefaultHookTimeout)
	baseHookExecutor.SetAsyncResultSink(newHookAsyncResultSink(service))
	service.hookExecutor = baseHookExecutor
	return service
}

// SetMemoExtractor 设置可选记忆提取钩子，由 Run 在结束时异步触发。
func (s *Service) SetMemoExtractor(extractor MemoExtractor) {
	s.memoExtractor = extractor
}

// SetSessionAssetStore 设置会话附件存储实现，用于 provider 请求阶段读取 session_asset。
func (s *Service) SetSessionAssetStore(store agentsession.AssetStore) {
	s.sessionAssetStore = store
}

// SetUserInputPreparer 设置输入归一化能力实现；runtime 仅做编排调用，不承载具体存储细节。
func (s *Service) SetUserInputPreparer(preparer UserInputPreparer) {
	s.userInputPreparer = preparer
}

// SetSkillsRegistry 设置运行时可选的 skills registry，用于激活校验与上下文注入。
func (s *Service) SetSkillsRegistry(registry skills.Registry) {
	s.skillsRegistry = registry
}

// SetThinkingEnabled 设置进程级 thinking 全局开关。
func (s *Service) SetThinkingEnabled(enabled bool) {
	s.runMu.Lock()
	s.thinkingEnabled = enabled
	s.runMu.Unlock()
}

// IsThinkingEnabled 返回当前 thinking 全局开关状态。
func (s *Service) IsThinkingEnabled() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.thinkingEnabled
}

// CancelActiveRun 尝试取消最近一次仍在执行的 Run。
func (s *Service) CancelActiveRun() bool {
	s.runMu.Lock()
	if s.activeRunToken == 0 {
		s.runMu.Unlock()
		return false
	}
	cancel := s.activeRunCancels[s.activeRunToken]
	s.runMu.Unlock()
	if cancel == nil {
		return false
	}

	cancel()
	return true
}

// CancelRun 按 run_id 精确取消指定运行任务。
func (s *Service) CancelRun(runID string) bool {
	normalizedRunID := strings.TrimSpace(runID)
	if normalizedRunID == "" {
		return false
	}

	s.runMu.Lock()
	token, exists := s.activeRunByID[normalizedRunID]
	if !exists {
		s.runMu.Unlock()
		return false
	}
	cancel := s.activeRunCancels[token]
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

// loadConfigSnapshot 在运行关键路径前主动从磁盘刷新一次配置快照，确保跨进程修改后的 provider/model 可见。
func (s *Service) loadConfigSnapshot(ctx context.Context) (config.Config, error) {
	if s == nil || s.configManager == nil {
		return config.Config{}, errors.New("runtime: config manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return s.configManager.Get(), nil
	}
	return s.configManager.Load(ctx)
}

// ListSessions 返回当前会话存储中的所有摘要。
func (s *Service) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	summaries, err := s.sessionStore.ListSummaries(ctx)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return summaries, nil
	}

	rewritten := make([]agentsession.Summary, 0, len(summaries))
	for _, summary := range summaries {
		current := summary
		if !isDefaultSessionTitle(current.Title) {
			rewritten = append(rewritten, current)
			continue
		}

		session, loadErr := s.sessionStore.LoadSession(ctx, current.ID)
		if loadErr != nil {
			rewritten = append(rewritten, current)
			continue
		}
		derived := sessionTitleFromMessages(session.Messages)
		if !shouldPromoteSessionTitle(current.Title, derived) {
			rewritten = append(rewritten, current)
			continue
		}

		current.Title = derived
		if titleUpdater, ok := s.sessionStore.(runtimeSessionTitleUpdater); ok {
			_ = titleUpdater.UpdateSessionTitle(ctx, agentsession.UpdateSessionTitleInput{
				SessionID: current.ID,
				UpdatedAt: current.UpdatedAt,
				Title:     derived,
			})
		}
		rewritten = append(rewritten, current)
	}
	return rewritten, nil
}

// LoadSession 按 id 加载完整会话内容。
func (s *Service) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	session, err := s.sessionStore.LoadSession(ctx, id)
	if err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

// CreateSession 按给定 id 执行会话创建/加载（Upsert）并返回可用会话头。
func (s *Service) CreateSession(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	sessionID := strings.TrimSpace(id)
	if sessionID == "" {
		sessionID = agentsession.NewID("session")
	}
	defaultWorkdir := ""
	if s.configManager != nil {
		defaultWorkdir = strings.TrimSpace(s.configManager.Get().Workdir)
	}
	sessionWorkdir, err := resolveWorkdirForSession(defaultWorkdir, "", "")
	if err != nil {
		return agentsession.Session{}, err
	}

	existing, err := s.sessionStore.LoadSession(ctx, sessionID)
	if err == nil {
		return existing, nil
	}
	if !isRuntimeSessionNotFoundError(err) {
		return agentsession.Session{}, err
	}

	newSession := agentsession.NewWithWorkdir("New Session", sessionWorkdir)
	newSession.ID = sessionID
	establishSessionVerificationProfile(&newSession)
	created, createErr := s.sessionStore.CreateSession(ctx, createSessionInputFromSession(newSession))
	if createErr == nil {
		return created, nil
	}
	if isRuntimeSessionAlreadyExistsError(createErr) {
		return s.sessionStore.LoadSession(ctx, sessionID)
	}
	return agentsession.Session{}, createErr
}

// isRuntimeSessionNotFoundError 判断错误是否代表会话文件/记录不存在。
func isRuntimeSessionNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, agentsession.ErrSessionNotFound)
}

// isRuntimeSessionAlreadyExistsError 判断错误是否代表会话已被并发创建。
func isRuntimeSessionAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, agentsession.ErrSessionAlreadyExists) || errors.Is(err, os.ErrExist)
}

func sessionTitleFromMessages(messages []providertypes.Message) string {
	for _, message := range messages {
		if strings.TrimSpace(message.Role) != providertypes.RoleUser {
			continue
		}
		if title := sessionTitleFromParts(message.Parts); strings.TrimSpace(title) != "" && !isImageOnlySessionTitle(title) {
			return strings.TrimSpace(title)
		}
	}
	return ""
}

func isDefaultSessionTitle(title string) bool {
	return strings.EqualFold(strings.TrimSpace(title), "New Session")
}

func isImageOnlySessionTitle(title string) bool {
	return strings.EqualFold(strings.TrimSpace(title), "Image Message")
}

func shouldPromoteSessionTitle(current string, next string) bool {
	trimmedCurrent := strings.TrimSpace(current)
	trimmedNext := strings.TrimSpace(next)
	if trimmedNext == "" || isDefaultSessionTitle(trimmedNext) || isImageOnlySessionTitle(trimmedNext) {
		return false
	}
	return isDefaultSessionTitle(trimmedCurrent)
}

// SetBudgetResolver 注入 prompt budget 解析能力，避免 runtime 直接感知模型目录细节。
func (s *Service) SetBudgetResolver(resolver BudgetResolver) {
	s.budgetResolver = resolver
}

// SetHookExecutor 设置 runtime 生命周期 hook 执行器；传入 nil 可禁用 hook 执行。
func (s *Service) SetHookExecutor(executor HookExecutor) {
	if base, ok := executor.(*runtimehooks.Executor); ok {
		// 保证外部注入执行器后仍然具备 async_rewake -> runtime 队列回灌能力。
		base.SetAsyncResultSink(newHookAsyncResultSink(s))
	}
	s.hookExecutor = executor
}

// SetCheckpointDependencies 注入 checkpoint 存储与版本化文件历史快照后端，用于 pre-write checkpoint gate。
func (s *Service) SetCheckpointDependencies(store checkpoint.CheckpointStore, perEdit *checkpoint.PerEditSnapshotStore) {
	s.checkpointStore = store
	s.perEditStore = perEdit
}
