package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"neo-code/internal/config"
	"neo-code/internal/provider"
	"neo-code/internal/tools"
)

const maxContextTurns = 10

type Runtime interface {
	Run(ctx context.Context, input UserInput) error
	CancelActiveRun() bool
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	LoadSession(ctx context.Context, id string) (Session, error)
}

type UserInput struct {
	SessionID string
	RunID     string
	Content   string
}

type ProviderFactory interface {
	Build(ctx context.Context, cfg config.ResolvedProviderConfig) (provider.Provider, error)
}

type Service struct {
	configManager   *config.Manager    //配置管理器，提供当前选中的 provider、model、workdir 等配置读取能力
	sessionStore    Store              //会话持久化接口，负责保存和加载聊天会话
	toolRegistry    *tools.Registry    //工具注册表，维护所有工具的schema
	providerFactory ProviderFactory    //provider 工厂接口，根据配置动态创建具体的 provider 实例
	events          chan RuntimeEvent  //事件通道，Runtime 在运行过程中产生的所有事件都通过这个 channel 发送给 TUI 层消费和展示
	runMu           sync.Mutex         //运行互斥锁，保证同一时间只有一个 Run 在执行
	activeRunToken  uint64             //当前活跃运行的令牌标识，用于标记正在执行的 Run 实例，配合 nextRunToken 实现新旧 Run 的安全切换
	nextRunToken    uint64             //下一个运行令牌的递增计数器，每次启动新 Run 时递增并赋给 activeRunToken，用于区分不同 Run 的生命周期
	activeRunCancel context.CancelFunc //当前活跃 Run 的取消函数
}

func NewWithFactory(configManager *config.Manager, toolRegistry *tools.Registry, sessionStore Store, providerFactory ProviderFactory) *Service {
	if providerFactory == nil {
		providerFactory = provider.NewRegistry()
	}

	return &Service{
		configManager:   configManager,
		sessionStore:    sessionStore,
		toolRegistry:    toolRegistry,
		providerFactory: providerFactory,
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

	session, err := s.loadOrCreateSession(ctx, input.SessionID, input.Content)
	if err != nil {
		return s.handleRunError(ctx, input.RunID, input.SessionID, err)
	}

	userMessage := provider.Message{
		Role:    "user",
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
		maxLoops := cfg.MaxLoops
		if maxLoops <= 0 {
			maxLoops = 8
		}
		if attempt >= maxLoops {
			err := errors.New("runtime: max loop reached")
			s.emit(ctx, EventError, input.RunID, session.ID, err.Error())
			return err
		}

		resolvedProvider, err := s.configManager.ResolvedSelectedProvider()
		if err != nil {
			s.emit(ctx, EventError, input.RunID, session.ID, err.Error())
			return err
		}

		modelProvider, err := s.providerFactory.Build(ctx, resolvedProvider)
		if err != nil {
			s.emit(ctx, EventError, input.RunID, session.ID, err.Error())
			return err
		}

		streamEvents := make(chan provider.StreamEvent, 32)
		streamDone := make(chan struct{})
		go s.forwardProviderEvents(ctx, input.RunID, session.ID, streamEvents, streamDone)

		resp, err := modelProvider.Chat(ctx, provider.ChatRequest{
			Model:        cfg.CurrentModel,
			SystemPrompt: s.systemPrompt(),
			Messages:     s.trimMessages(session.Messages),
			Tools:        s.toolRegistry.GetSpecs(),
		}, streamEvents)
		close(streamEvents)
		<-streamDone
		if err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}
		if err := ctx.Err(); err != nil {
			return s.handleRunError(ctx, input.RunID, session.ID, err)
		}

		assistant := resp.Message
		if strings.TrimSpace(assistant.Role) == "" {
			assistant.Role = "assistant"
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
			result, execErr := s.toolRegistry.Execute(runCtx, tools.ToolCallInput{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: []byte(call.Arguments),
				Workdir:   cfg.Workdir,
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
				Role:       "tool",
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
	return s.sessionStore.Load(ctx, id)
}

func (s *Service) loadOrCreateSession(ctx context.Context, sessionID string, title string) (Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		session := newSession(title)
		if err := s.sessionStore.Save(ctx, &session); err != nil {
			return Session{}, err
		}
		return session, nil
	}
	return s.sessionStore.Load(ctx, sessionID)
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

	s.emit(ctx, EventError, runID, sessionID, err.Error())
	return err
}

func (s *Service) isRunCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func (s *Service) trimMessages(messages []provider.Message) []provider.Message {
	if len(messages) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	type span struct {
		start int
		end   int
	}

	spans := make([]span, 0, len(messages))
	for i := 0; i < len(messages); {
		start := i
		i++

		if messages[start].Role == "assistant" && len(messages[start].ToolCalls) > 0 {
			for i < len(messages) && messages[i].Role == "tool" {
				i++
			}
		}

		spans = append(spans, span{start: start, end: i})
	}

	if len(spans) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	start := spans[len(spans)-maxContextTurns].start
	clipped := append([]provider.Message(nil), messages[start:]...)
	return clipped
}

func (s *Service) systemPrompt() string {
	return `You are NeoCode, a local coding agent.

	Be concise and accurate.
	Use tools when necessary.
	When a tool fails, inspect the error and continue safely.
	 Stay within the workspace and avoid destructive behavior unless clearly requested.`
}
