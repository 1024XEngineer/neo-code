package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	agentcontext "neo-code/internal/context"
	contextcompact "neo-code/internal/context/compact"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

const (
	// defaultProviderRetryMax 是 runtime 层对单次 provider.Chat() 的最大重试次数（不含首次调用）。
	// 与 RetryTransport 的 HTTP 层重试互补：Transport 耗尽后 runtime 仍可重试整个 Chat 调用。
	defaultProviderRetryMax = 2
	// providerRetryBaseWait 是 runtime 层重试的初始等待时间。
	providerRetryBaseWait = 1 * time.Second
	// providerRetryMaxWait 是 runtime 层重试的最大等待时间。
	providerRetryMaxWait = 5 * time.Second
)

// streamAccumulator 在流式事件处理过程中累积本轮对话需要持久化的助手消息状态，
// 包括文本内容和工具调用列表。
type streamAccumulator struct {
	content     strings.Builder
	toolCalls   map[int]*providertypes.ToolCall
	messageDone bool
}

// newStreamAccumulator 创建并初始化一个空的流式事件累积器。
func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolCalls: make(map[int]*providertypes.ToolCall),
	}
}

// accumulateTextDelta 累积文本增量片段。
func (a *streamAccumulator) accumulateTextDelta(text string) {
	a.content.WriteString(text)
}

// ensureToolCall 返回指定索引的工具调用条目，不存在时会先创建占位对象。
func (a *streamAccumulator) ensureToolCall(index int) *providertypes.ToolCall {
	call, exists := a.toolCalls[index]
	if !exists {
		call = &providertypes.ToolCall{}
		a.toolCalls[index] = call
	}
	return call
}

// accumulateToolCallStart 记录新发现的工具调用（首次出现时创建条目）。
func (a *streamAccumulator) accumulateToolCallStart(index int, id, name string) {
	call := a.ensureToolCall(index)
	if strings.TrimSpace(id) != "" {
		call.ID = id
	}
	if strings.TrimSpace(name) != "" {
		call.Name = name
	}
}

// accumulateToolCallDelta 累积工具调用参数增量。
func (a *streamAccumulator) accumulateToolCallDelta(index int, id, argumentsDelta string) {
	call := a.ensureToolCall(index)
	if strings.TrimSpace(id) != "" {
		call.ID = id
	}
	call.Arguments += argumentsDelta
}

// buildMessage 从累积状态构建最终的 assistant Message 对象，并校验工具调用元数据是否完整。
func (a *streamAccumulator) buildMessage() (providertypes.Message, error) {
	ordered := make([]int, 0, len(a.toolCalls))
	for index := range a.toolCalls {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)

	message := providertypes.Message{
		Role:    providertypes.RoleAssistant,
		Content: a.content.String(),
	}
	for _, index := range ordered {
		call := a.toolCalls[index]
		if call == nil {
			continue
		}
		if strings.TrimSpace(call.ID) == "" {
			return providertypes.Message{}, fmt.Errorf("runtime: provider emitted tool call %d without id", index)
		}
		if strings.TrimSpace(call.Name) == "" {
			return providertypes.Message{}, fmt.Errorf("runtime: provider emitted tool call %d without name", index)
		}
		message.ToolCalls = append(message.ToolCalls, *call)
	}
	return message, nil
}

type Runtime interface {
	Run(ctx context.Context, input UserInput) error
	Compact(ctx context.Context, input CompactInput) (CompactResult, error)
	ResolvePermission(ctx context.Context, input PermissionResolutionInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]agentsession.Summary, error)
	LoadSession(ctx context.Context, id string) (agentsession.Session, error)
	SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error)
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
	configManager       *config.Manager      // 配置管理器，提供当前选中的 provider、model、workdir 等配置读取能力。
	sessionStore        agentsession.Store   // 会话持久化接口，负责保存和加载聊天会话。
	toolManager         tools.Manager        // 工具管理器，统一工具 schema 暴露与执行入口。
	providerFactory     ProviderFactory      // Provider 工厂接口，根据配置动态创建具体的 provider 实例。
	contextBuilder      agentcontext.Builder // 上下文构建器，负责组装 system prompt 与本轮发给模型的消息上下文。
	compactRunner       contextcompact.Runner
	events              chan RuntimeEvent
	operationMu         sync.Mutex         // 运行级互斥：串行化 Run 与 Compact，避免并发写同一会话。
	runMu               sync.Mutex         // 仅保护 activeRun* 字段的并发读写。
	activeRunToken      uint64             // 当前活跃运行的令牌标识，用于标记正在执行的 Run 实例。
	nextRunToken        uint64             // 下一个运行令牌的递增计数器，用于区分不同 Run 的生命周期。
	activeRunCancel     context.CancelFunc // 当前活跃 Run 的取消函数。
	sessionInputTokens  int                // 当前会话累计输入 token。
	sessionOutputTokens int                // 当前会话累计输出 token。
}

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
		events:          make(chan RuntimeEvent, 128),
	}
}

func (s *Service) Run(ctx context.Context, input UserInput) error {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()

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
	s.restoreSessionTokens(session)

	userMessage := providertypes.Message{
		Role:    providertypes.RoleUser,
		Content: input.Content,
	}
	session.Messages = append(session.Messages, userMessage)
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.Save(ctx, &session); err != nil {
		return s.handleRunError(ctx, input.RunID, session.ID, err)
	}
	s.emit(ctx, EventUserMessage, input.RunID, session.ID, userMessage)

	autoCompacted := false
	reactiveRetried := false

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
				Workdir:             activeWorkdir,
				Shell:               cfg.Shell,
				Provider:            cfg.SelectedProvider,
				Model:               cfg.CurrentModel,
				SessionInputTokens:  s.sessionInputTokens,
				SessionOutputTokens: s.sessionOutputTokens,
			},
			Compact: agentcontext.CompactOptions{
				DisableMicroCompact:  cfg.Context.Compact.MicroCompactDisabled,
				AutoCompactThreshold: s.autoCompactThreshold(cfg),
			},
		})
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		if builtContext.ShouldAutoCompact && !autoCompacted {
			autoCompacted = true
			var compactResult contextcompact.Result
			session, compactResult, _ = s.runCompactForSession(ctx, input.RunID, session, cfg, contextcompact.ModeManual, false)
			if compactResult.Applied {
				s.resetSessionTokenTotals(&session)
				// 自动 compact 成功后需要在同一轮重建上下文，避免继续沿用压缩前的请求内容。
				attempt--
				continue
			}
		}

		toolSpecs, err := s.toolManager.ListAvailableSpecs(ctx, tools.SpecListInput{
			SessionID: session.ID,
		})
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		acc, err := s.callProviderWithRetry(ctx, input.RunID, session.ID, providertypes.ChatRequest{
			Model:        cfg.CurrentModel,
			SystemPrompt: builtContext.SystemPrompt,
			Messages:     builtContext.Messages,
			Tools:        toolSpecs,
		})
		if err != nil {
			if provider.IsContextTooLong(err) && !reactiveRetried {
				reactiveRetried = true
				var compactResult contextcompact.Result
				session, compactResult, _ = s.runCompactForSession(
					ctx,
					input.RunID,
					session,
					cfg,
					contextcompact.ModeReactive,
					false,
				)
				if compactResult.Applied {
					s.resetSessionTokenTotals(&session)
					autoCompacted = true
				}
				// Don't count the reactive-compact retry as a new reasoning turn.
				attempt--
				continue
			}
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}
		if err := ctx.Err(); err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		metadataChanged := session.Provider != cfg.SelectedProvider || session.Model != cfg.CurrentModel
		session.Provider = cfg.SelectedProvider
		session.Model = cfg.CurrentModel
		session.TokenInputTotal = s.sessionInputTokens
		session.TokenOutputTotal = s.sessionOutputTokens

		assistant, err := acc.buildMessage()
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}
		if strings.TrimSpace(assistant.Role) == "" {
			assistant.Role = providertypes.RoleAssistant
		}

		if strings.TrimSpace(assistant.Content) != "" || len(assistant.ToolCalls) > 0 {
			session.Messages = append(session.Messages, assistant)
			session.UpdatedAt = time.Now()
			if err := s.sessionStore.Save(ctx, &session); err != nil {
				return s.handleRunError(ctx, input.RunID, session.ID, err)
			}
		} else if metadataChanged {
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

			result, execErr := s.executeToolCallWithPermission(ctx, permissionExecutionInput{
				RunID:       input.RunID,
				SessionID:   session.ID,
				Call:        call,
				Workdir:     activeWorkdir,
				ToolTimeout: time.Duration(cfg.ToolTimeoutSec) * time.Second,
			})
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
			if permissionEvent, ok := permissionEventFromError(execErr); ok {
				s.emit(ctx, EventPermissionResolved, input.RunID, session.ID, permissionEvent.toResolvedPayload())
			}

			toolMessage := providertypes.Message{
				Role:       providertypes.RoleTool,
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

func (s *Service) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return s.sessionStore.ListSummaries(ctx)
}

func (s *Service) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	session, err := s.sessionStore.Load(ctx, id)
	if err != nil {
		return agentsession.Session{}, err
	}
	return session, nil
}

func (s *Service) SetSessionWorkdir(ctx context.Context, sessionID string, workdir string) (agentsession.Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return agentsession.Session{}, errors.New("runtime: session id is empty")
	}

	session, err := s.sessionStore.Load(ctx, sessionID)
	if err != nil {
		return agentsession.Session{}, err
	}

	cfg := s.configManager.Get()
	resolved, err := resolveWorkdirForSession(cfg.Workdir, session.Workdir, workdir)
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

// emit 向事件通道发送事件，并在通道阻塞且上下文取消时返回对应错误。
// 先尝试非阻塞发送，确保即使 context 已取消，只要通道有空间事件就能被投递；
// 仅在通道已满时才通过 ctx.Done() 退出，避免 goroutine 泄漏并向调用方反馈未投递状态。
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

// handleProviderStreamEvent 解析并应用单条 provider 流式事件，缺失载荷或未知类型时返回错误。
func handleProviderStreamEvent(
	event providertypes.StreamEvent,
	acc *streamAccumulator,
	onTextDelta func(string),
	onToolCallStart func(providertypes.ToolCallStartPayload),
	onMessageDone func(providertypes.MessageDonePayload),
) error {
	switch event.Type {
	case providertypes.StreamEventTextDelta:
		payload, err := event.TextDeltaValue()
		if err != nil {
			return err
		}
		if onTextDelta != nil {
			onTextDelta(payload.Text)
		}
		if acc != nil {
			acc.accumulateTextDelta(payload.Text)
		}
	case providertypes.StreamEventToolCallStart:
		payload, err := event.ToolCallStartValue()
		if err != nil {
			return err
		}
		if onToolCallStart != nil {
			onToolCallStart(payload)
		}
		if acc != nil {
			acc.accumulateToolCallStart(payload.Index, payload.ID, payload.Name)
		}
	case providertypes.StreamEventToolCallDelta:
		payload, err := event.ToolCallDeltaValue()
		if err != nil {
			return err
		}
		if acc != nil {
			acc.accumulateToolCallDelta(payload.Index, payload.ID, payload.ArgumentsDelta)
		}
	case providertypes.StreamEventMessageDone:
		payload, err := event.MessageDoneValue()
		if err != nil {
			return err
		}
		if acc != nil {
			acc.messageDone = true
		}
		if onMessageDone != nil {
			onMessageDone(payload)
		}
	default:
		return fmt.Errorf("runtime: unsupported provider stream event type %q", event.Type)
	}
	return nil
}

// forwardProviderEvents 将 provider 流式事件转发为 runtime 事件，同时向 accumulator 累积消息状态。
// 使用 select 同时监听输入通道和 context 取消信号，确保 goroutine 不会因通道阻塞而泄漏。
func (s *Service) forwardProviderEvents(
	ctx context.Context,
	runID string,
	sessionID string,
	input <-chan providertypes.StreamEvent,
	done chan<- error,
	acc *streamAccumulator,
) {
	var forwardErr error
	defer func() {
		done <- forwardErr
	}()

	for {
		select {
		case event, ok := <-input:
			if !ok {
				return
			}
			err := handleProviderStreamEvent(
				event,
				acc,
				func(text string) {
					s.emit(ctx, EventAgentChunk, runID, sessionID, text)
				},
				func(payload providertypes.ToolCallStartPayload) {
					s.emit(ctx, EventToolCallThinking, runID, sessionID, payload.Name)
				},
				func(done providertypes.MessageDonePayload) {
					if done.Usage != nil {
						s.sessionInputTokens += done.Usage.InputTokens
						s.sessionOutputTokens += done.Usage.OutputTokens
						s.emit(ctx, EventTokenUsage, runID, sessionID, TokenUsagePayload{
							InputTokens:         done.Usage.InputTokens,
							OutputTokens:        done.Usage.OutputTokens,
							SessionInputTokens:  s.sessionInputTokens,
							SessionOutputTokens: s.sessionOutputTokens,
						})
					}
				},
			)
			if err != nil && forwardErr == nil {
				// 记录首个协议错误后继续排空事件通道，避免 provider 在后续发送时阻塞。
				forwardErr = err
			}
		case <-ctx.Done():
			return
		}
	}
}

// restoreSessionTokens restores token counters from session persistence,
// or resets them to zero for new sessions.
func (s *Service) restoreSessionTokens(session agentsession.Session) {
	s.sessionInputTokens = session.TokenInputTotal
	s.sessionOutputTokens = session.TokenOutputTotal
}

// autoCompactThreshold returns the configured auto-compact input token threshold,
// or 0 if auto-compact is disabled.
func (s *Service) autoCompactThreshold(cfg config.Config) int {
	if cfg.Context.AutoCompact.Enabled && cfg.Context.AutoCompact.InputTokenThreshold > 0 {
		return cfg.Context.AutoCompact.InputTokenThreshold
	}
	return 0
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
// 每次重试都会重新创建 provider 实例、流式事件转发管道和累积器。
// 非可重试错误、context 取消、重试耗尽时直接返回错误。
// 返回值 acc 保存了本轮流式事件的完整累积状态，供调用方构建 assistant Message 使用。
func (s *Service) callProviderWithRetry(
	ctx context.Context,
	runID string,
	sessionID string,
	req providertypes.ChatRequest,
) (*streamAccumulator, error) {
	acc := newStreamAccumulator()
	var lastErr error

	for retryAttempt := 0; retryAttempt <= defaultProviderRetryMax; retryAttempt++ {
		if retryAttempt > 0 {
			// 重试时重置累积器，避免混入上轮数据
			acc = newStreamAccumulator()
			wait := providerRetryBackoff(retryAttempt)
			s.emit(ctx, EventProviderRetry, runID, sessionID,
				fmt.Sprintf("retrying provider call (attempt %d/%d, wait=%.1fs)...",
					retryAttempt, defaultProviderRetryMax, wait.Seconds()))

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		resolvedProvider, err := s.configManager.ResolvedSelectedProvider()
		if err != nil {
			return nil, err
		}

		modelProvider, err := s.providerFactory.Build(ctx, resolvedProvider)
		if err != nil {
			return nil, err
		}

		streamEvents := make(chan providertypes.StreamEvent, 32)
		streamDone := make(chan error, 1)
		go s.forwardProviderEvents(ctx, runID, sessionID, streamEvents, streamDone, acc)

		err = modelProvider.Chat(ctx, req, streamEvents)
		close(streamEvents)
		forwardErr := <-streamDone
		if forwardErr != nil {
			if err != nil {
				return nil, fmt.Errorf("runtime: provider stream handling failed after provider error: %v: %w", err, forwardErr)
			}
			return nil, forwardErr
		}

		if err == nil {
			if !acc.messageDone {
				return nil, fmt.Errorf("%w: provider stream ended without message_done event", provider.ErrStreamInterrupted)
			}
			return acc, nil
		}

		lastErr = err

		// 非可重试错误或 context 已取消，立即返回。
		if !isRetryableProviderError(err) {
			return nil, lastErr
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if lastErr == nil {
		lastErr = errors.New("max retries exceeded")
	}
	return nil, fmt.Errorf("runtime: max retries exhausted, last error: %w", lastErr)
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

type permissionEventView struct {
	toolName   string
	actionType string
	operation  string
	targetType string
	target     string
	decision   string
	reason     string
	ruleID     string
	scope      string
	resolvedAs string
}

// permissionEventFromError 从工具权限错误中提取可用于 runtime 事件的结构化信息。
func permissionEventFromError(err error) (permissionEventView, bool) {
	var permissionErr *tools.PermissionDecisionError
	if !errors.As(err, &permissionErr) {
		return permissionEventView{}, false
	}

	action := permissionErr.Action()
	decision := strings.TrimSpace(permissionErr.Decision())
	reason := strings.TrimSpace(permissionErr.Reason())
	if reason == "" {
		reason = strings.TrimSpace(err.Error())
	}

	resolvedAs := "denied"
	if strings.EqualFold(decision, "ask") {
		resolvedAs = "rejected"
	}

	return permissionEventView{
		toolName:   action.Payload.ToolName,
		actionType: string(action.Type),
		operation:  action.Payload.Operation,
		targetType: string(action.Payload.TargetType),
		target:     action.Payload.Target,
		decision:   decision,
		reason:     reason,
		ruleID:     strings.TrimSpace(permissionErr.RuleID()),
		scope:      strings.TrimSpace(permissionErr.RememberScope()),
		resolvedAs: resolvedAs,
	}, true
}

// toRequestPayload 将权限事件视图转换为 permission_request 载荷。
func (v permissionEventView) toRequestPayload() PermissionRequestPayload {
	return PermissionRequestPayload{
		ToolName:      v.toolName,
		ActionType:    v.actionType,
		Operation:     v.operation,
		TargetType:    v.targetType,
		Target:        v.target,
		Decision:      v.decision,
		Reason:        v.reason,
		RuleID:        v.ruleID,
		RememberScope: v.scope,
	}
}

// toResolvedPayload 将权限事件视图转换为 permission_resolved 载荷。
func (v permissionEventView) toResolvedPayload() PermissionResolvedPayload {
	return PermissionResolvedPayload{
		ToolName:      v.toolName,
		ActionType:    v.actionType,
		Operation:     v.operation,
		TargetType:    v.targetType,
		Target:        v.target,
		Decision:      v.decision,
		Reason:        v.reason,
		RuleID:        v.ruleID,
		RememberScope: v.scope,
		ResolvedAs:    v.resolvedAs,
	}
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
