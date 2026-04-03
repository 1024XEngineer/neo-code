package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	"neo-code/internal/provider"
	"neo-code/internal/tools"
)

const maxContextTurns = 10

const (
	// defaultProviderRetryMax 是 runtime 层对单次 provider.Chat() 的最大重试次数（不含首次调用）。
	// 与 RetryTransport 的 HTTP 层重试互补：Transport 耗尽后 runtime 仍可重试整个 Chat 调用。
	defaultProviderRetryMax = 2
	// providerRetryBaseWait 是 runtime 层重试的初始等待时间。
	providerRetryBaseWait = 1 * time.Second
	// providerRetryMaxWait 是 runtime 层重试的最大等待时间。
	providerRetryMaxWait = 5 * time.Second
)

var runtimeSessionWorkdirs = struct {
	mu   sync.RWMutex
	data map[string]string
}{
	data: make(map[string]string),
}

type Runtime interface {
	Run(ctx context.Context, input UserInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	LoadSession(ctx context.Context, id string) (Session, error)
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (Session, error)
}

type UserInput struct {
	SessionID string
	RunID     string
	Content   string
	Workdir   string
}

type ProviderFactory interface {
	Build(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error)
}

type Service struct {
	configManager   *config.Manager      // 配置管理器，提供当前选中的 provider、model、workdir 等配置读取能力。
	sessionStore    Store                // 会话持久化接口，负责保存和加载聊天会话。
	toolManager     tools.Manager        // 工具管理器，统一工具 schema 暴露与执行入口。
	providerFactory ProviderFactory      // Provider 工厂接口，根据配置动态创建具体的 provider 实例。
	contextBuilder  agentcontext.Builder // 上下文构建器，负责组装 system prompt 与本轮发给模型的消息上下文。
	events          chan RuntimeEvent    // 事件通道，Runtime 在运行过程中产生的事件都通过该通道发送给 TUI 层消费和展示。
	runMu           sync.Mutex           // 运行互斥锁，保证同一时间只有一个 Run 在执行。
	activeRunToken  uint64               // 当前活跃运行的令牌标识，用于标记正在执行的 Run 实例。
	nextRunToken    uint64               // 下一个运行令牌的递增计数器，用于区分不同 Run 的生命周期。
	activeRunCancel context.CancelFunc   // 当前活跃 Run 的取消函数。
}

func NewWithFactory(
	configManager *config.Manager,
	toolManager tools.Manager,
	sessionStore Store,
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
		contextBuilder = agentcontext.NewBuilder()
	}

	return &Service{
		configManager:   configManager,
		sessionStore:    sessionStore,
		toolManager:     toolManager,
		providerFactory: providerFactory,
		contextBuilder:  contextBuilder,
		events:          make(chan RuntimeEvent, 128),
	}
}

func (s *Service) Run(ctx context.Context, input UserInput) error {
	runCtx, cancel := context.WithCancel(ctx)
	runToken := s.startRun(cancel)
	defer func() {
		cancel()
		s.finishRun(runToken)
	}()
	ctx = runCtx

	if strings.TrimSpace(input.Content) == "" {
		return errors.New("runtime: input content is empty")
	}

	initialCfg := s.configManager.Get()
	session, err := s.loadOrCreateSession(ctx, input.SessionID, input.Content, initialCfg.Workdir, input.Workdir)
	if err != nil {
		return s.handleRunError(ctx, input.RunID, input.SessionID, err)
	}

	userMessage := provider.Message{
		Role:    provider.RoleUser,
		Content: input.Content,
	}
	session.Messages = append(session.Messages, userMessage)
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.Save(ctx, &session); err != nil {
		return s.handleRunError(ctx, input.RunID, session.ID, err)
	}
	s.emit(ctx, EventUserMessage, input.RunID, session.ID, userMessage)

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		cfg := s.configManager.Get()
		activeWorkdir := effectiveSessionWorkdir(session.Workdir, cfg.Workdir)
		maxLoops := cfg.MaxLoops
		if maxLoops <= 0 {
			maxLoops = 8
		}
		if attempt >= maxLoops {
			err := errors.New("runtime: max loop reached")
			s.emit(ctx, EventError, input.RunID, session.ID, err.Error())
			return err
		}

		builtContext, err := s.contextBuilder.Build(ctx, agentcontext.BuildInput{
			Messages: session.Messages,
			Metadata: agentcontext.Metadata{
				Workdir:  activeWorkdir,
				Shell:    cfg.Shell,
				Provider: cfg.SelectedProvider,
				Model:    cfg.CurrentModel,
			},
		})
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		toolSpecs, err := s.toolManager.ListAvailableSpecs(ctx, tools.SpecListInput{
			SessionID: session.ID,
		})
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		resp, err := s.callProviderWithRetry(ctx, input.RunID, session.ID, provider.ChatRequest{
			Model:        cfg.CurrentModel,
			SystemPrompt: builtContext.SystemPrompt,
			Messages:     builtContext.Messages,
			Tools:        toolSpecs,
		})
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}
		if err := ctx.Err(); err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		assistant := resp.Message
		if strings.TrimSpace(assistant.Role) == "" {
			assistant.Role = provider.RoleAssistant
		}

		if strings.TrimSpace(assistant.Content) != "" || len(assistant.ToolCalls) > 0 {
			session.Messages = append(session.Messages, assistant)
			session.UpdatedAt = time.Now()
			if err := s.sessionStore.Save(ctx, &session); err != nil {
				return s.handleRunError(ctx, input.RunID, session.ID, err)
			}
		}

		if err := ctx.Err(); err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}
		if len(assistant.ToolCalls) == 0 {
			s.emit(ctx, EventAgentDone, input.RunID, session.ID, assistant)
			return nil
		}

		for _, call := range assistant.ToolCalls {
			if err := ctx.Err(); err != nil {
				return s.handleRunError(ctx, input.RunID, session.ID, err)
			}
			s.emit(ctx, EventToolStart, input.RunID, session.ID, call)

			runCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ToolTimeoutSec)*time.Second)
			result, execErr := s.toolManager.Execute(runCtx, tools.ToolCallInput{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: []byte(call.Arguments),
				Workdir:   activeWorkdir,
				SessionID: session.ID,
				EmitChunk: func(chunk []byte) {
					s.emit(ctx, EventToolChunk, input.RunID, session.ID, string(chunk))
				},
			})
			cancel()
			if s.isRunCanceled(execErr) {
				return s.handleRunError(ctx, input.RunID, session.ID, execErr)
			}
			if execErr == nil {
				if err := ctx.Err(); err != nil {
					return s.handleRunError(ctx, input.RunID, session.ID, err)
				}
			}

			if execErr != nil && strings.TrimSpace(result.Content) == "" {
				result.Content = execErr.Error()
			}

			toolMessage := provider.Message{
				Role:       provider.RoleTool,
				Content:    result.Content,
				ToolCallID: call.ID,
				IsError:    result.IsError,
			}
			session.Messages = append(session.Messages, toolMessage)
			session.UpdatedAt = time.Now()
			if err := s.sessionStore.Save(ctx, &session); err != nil {
				if execErr != nil && errors.Is(err, context.Canceled) {
					s.emit(ctx, EventToolResult, input.RunID, session.ID, result)
				}
				return s.handleRunError(ctx, input.RunID, session.ID, err)
			}
			if err := ctx.Err(); err != nil {
				if execErr == nil {
					return s.handleRunError(ctx, input.RunID, session.ID, err)
				}
			}

			s.emit(ctx, EventToolResult, input.RunID, session.ID, result)
			if execErr != nil {
				if err := ctx.Err(); err != nil {
					return s.handleRunError(ctx, input.RunID, session.ID, err)
				}
			}
		}
	}
}

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

func (s *Service) Events() <-chan RuntimeEvent {
	return s.events
}

func (s *Service) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	return s.sessionStore.ListSummaries(ctx)
}

func (s *Service) LoadSession(ctx context.Context, id string) (Session, error) {
	session, err := s.sessionStore.Load(ctx, id)
	if err != nil {
		return Session{}, err
	}
	session.Workdir = s.sessionWorkdir(id, session.Workdir)
	return session, nil
}

func (s *Service) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Session{}, errors.New("runtime: session id is empty")
	}

	session, err := s.sessionStore.Load(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session.Workdir = s.sessionWorkdir(sessionID, session.Workdir)

	cfg := s.configManager.Get()
	resolved, err := resolveWorkdirForSession(cfg.Workdir, session.Workdir, workdir)
	if err != nil {
		return Session{}, err
	}
	if session.Workdir == resolved {
		return session, nil
	}

	session.Workdir = resolved
	s.setSessionWorkdir(sessionID, resolved)
	return session, nil
}

func (s *Service) sessionWorkdir(sessionID string, fallback string) string {
	key := s.sessionWorkdirKey(sessionID)
	runtimeSessionWorkdirs.mu.RLock()
	value, ok := runtimeSessionWorkdirs.data[key]
	runtimeSessionWorkdirs.mu.RUnlock()
	if ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) setSessionWorkdir(sessionID string, workdir string) {
	key := s.sessionWorkdirKey(sessionID)
	runtimeSessionWorkdirs.mu.Lock()
	runtimeSessionWorkdirs.data[key] = strings.TrimSpace(workdir)
	runtimeSessionWorkdirs.mu.Unlock()
}

func (s *Service) sessionWorkdirKey(sessionID string) string {
	return fmt.Sprintf("%p:%s", s, strings.TrimSpace(sessionID))
}

func (s *Service) loadOrCreateSession(
	ctx context.Context,
	sessionID string,
	title string,
	defaultWorkdir string,
	requestedWorkdir string,
) (Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionWorkdir, err := resolveWorkdirForSession(defaultWorkdir, "", requestedWorkdir)
		if err != nil {
			return Session{}, err
		}
		session := newSessionWithWorkdir(title, sessionWorkdir)
		s.setSessionWorkdir(session.ID, sessionWorkdir)
		if err := s.sessionStore.Save(ctx, &session); err != nil {
			return Session{}, err
		}
		return session, nil
	}
	session, err := s.sessionStore.Load(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	session.Workdir = s.sessionWorkdir(sessionID, session.Workdir)
	if strings.TrimSpace(requestedWorkdir) == "" && strings.TrimSpace(session.Workdir) != "" {
		return session, nil
	}

	resolved, err := resolveWorkdirForSession(defaultWorkdir, session.Workdir, requestedWorkdir)
	if err != nil {
		return Session{}, err
	}
	if session.Workdir == resolved {
		return session, nil
	}
	session.Workdir = resolved
	s.setSessionWorkdir(sessionID, resolved)
	return session, nil
}

// emit 向事件通道发送事件。
// 先尝试非阻塞发送，确保即使 context 已取消，只要通道有空间事件就能被投递；
// 仅在通道已满时才通过 ctx.Done() 退出，避免 goroutine 泄漏。
func (s *Service) emit(ctx context.Context, kind EventType, runID string, sessionID string, payload any) {
	evt := RuntimeEvent{
		Type:      kind,
		RunID:     runID,
		SessionID: sessionID,
		Payload:   payload,
	}
	select {
	case s.events <- evt:
		return
	default:
	}
	select {
	case s.events <- evt:
	case <-ctx.Done():
	}
}

// forwardProviderEvents 将 provider 流式事件转发为 runtime 事件。
// 使用 select 同时监听输入通道和 context 取消信号，确保 goroutine 不会因通道阻塞而泄漏。
func (s *Service) forwardProviderEvents(ctx context.Context, runID string, sessionID string, input <-chan provider.StreamEvent, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case event, ok := <-input:
			if !ok {
				return
			}
			switch event.Type {
			case provider.StreamEventTextDelta:
				s.emit(ctx, EventAgentChunk, runID, sessionID, event.Text)
			case provider.StreamEventToolCallStart:
				s.emit(ctx, EventToolCallThinking, runID, sessionID, event.ToolName)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) startRun(cancel context.CancelFunc) uint64 {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	s.nextRunToken++
	token := s.nextRunToken
	s.activeRunToken = token
	s.activeRunCancel = cancel
	return token
}

func (s *Service) finishRun(token uint64) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	if s.activeRunToken != token {
		return
	}

	s.activeRunToken = 0
	s.activeRunCancel = nil
}

func (s *Service) handleRunError(ctx context.Context, runID string, sessionID string, err error) error {
	if s.isRunCanceled(err) {
		s.emit(ctx, EventRunCanceled, runID, sessionID, nil)
		return context.Canceled
	}

	// 提取 ProviderError 信息用于日志增强。
	var pErr *provider.ProviderError
	if errors.As(err, &pErr) {
		log.Printf("runtime: provider error (status=%d, code=%s, retryable=%v): %s",
			pErr.StatusCode, pErr.Code, pErr.Retryable, pErr.Message)
	}

	s.emit(ctx, EventError, runID, sessionID, err.Error())
	return err
}

// isRetryableProviderError 判断 error 是否为可重试的 ProviderError。
func isRetryableProviderError(err error) bool {
	var pErr *provider.ProviderError
	if !errors.As(err, &pErr) {
		return false
	}
	return pErr.Retryable
}

// callProviderWithRetry 在可重试的 ProviderError 上自动重试 provider.Chat() 调用。
// 每次重试都会重新构建 provider 实例和流式事件转发管道。
// 非可重试错误、context 取消、重试耗尽时直接返回错误。
func (s *Service) callProviderWithRetry(
	ctx context.Context,
	runID string,
	sessionID string,
	req provider.ChatRequest,
) (provider.ChatResponse, error) {
	var lastErr error

	for retryAttempt := 0; retryAttempt <= defaultProviderRetryMax; retryAttempt++ {
		if retryAttempt > 0 {
			wait := providerRetryBackoff(retryAttempt)
			s.emit(ctx, EventProviderRetry, runID, sessionID,
				fmt.Sprintf("retrying provider call (attempt %d/%d, wait=%.1fs)...",
					retryAttempt, defaultProviderRetryMax, wait.Seconds()))

			select {
			case <-ctx.Done():
				return provider.ChatResponse{}, ctx.Err()
			case <-time.After(wait):
			}
		}

		resolvedProvider, err := s.configManager.ResolvedSelectedProvider()
		if err != nil {
			return provider.ChatResponse{}, err
		}

		modelProvider, err := s.providerFactory.Build(ctx, resolvedProvider)
		if err != nil {
			return provider.ChatResponse{}, err
		}

		streamEvents := make(chan provider.StreamEvent, 32)
		streamDone := make(chan struct{})
		go s.forwardProviderEvents(ctx, runID, sessionID, streamEvents, streamDone)

		resp, err := modelProvider.Chat(ctx, req, streamEvents)
		close(streamEvents)
		<-streamDone

		if err == nil {
			return resp, nil
		}

		lastErr = err

		// 非可重试错误或 context 已取消，立即返回。
		if !isRetryableProviderError(err) {
			return provider.ChatResponse{}, err
		}
		if ctx.Err() != nil {
			return provider.ChatResponse{}, ctx.Err()
		}
	}

	return provider.ChatResponse{}, lastErr
}

// providerRetryBackoff 计算指数退避 + 随机抖动的等待时间。
// attempt 从 1 开始（首次重试）。
func providerRetryBackoff(attempt int) time.Duration {
	wait := providerRetryBaseWait << (attempt - 1)

	// 随机抖动：[0.5, 1.5) * wait
	jitter := float64(wait) * (0.5 + rand.Float64())
	wait = time.Duration(jitter)

	if wait > providerRetryMaxWait {
		wait = providerRetryMaxWait
	}

	return wait
}

func (s *Service) isRunCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func effectiveSessionWorkdir(sessionWorkdir string, defaultWorkdir string) string {
	workdir := strings.TrimSpace(sessionWorkdir)
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(defaultWorkdir)
}

func resolveWorkdirForSession(defaultWorkdir string, currentWorkdir string, requestedWorkdir string) (string, error) {
	base := effectiveSessionWorkdir(currentWorkdir, defaultWorkdir)
	if strings.TrimSpace(requestedWorkdir) == "" {
		return normalizeExistingWorkdir(base)
	}

	target := strings.TrimSpace(requestedWorkdir)
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return normalizeExistingWorkdir(target)
}

func normalizeExistingWorkdir(workdir string) (string, error) {
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" {
		return "", errors.New("runtime: workdir is empty")
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("runtime: resolve workdir: %w", err)
	}

	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("runtime: resolve workdir: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("runtime: workdir %q is not a directory", absolute)
	}

	return filepath.Clean(absolute), nil
}
