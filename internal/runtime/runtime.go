package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
	"github.com/dust/neo-code/internal/provider/anthropic"
	"github.com/dust/neo-code/internal/provider/gemini"
	"github.com/dust/neo-code/internal/provider/openai"
	"github.com/dust/neo-code/internal/tools"
)

const maxContextTurns = 10

type Runtime interface {
	Run(ctx context.Context, input UserInput) error
	Events() <-chan RuntimeEvent
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	LoadSession(ctx context.Context, id string) (Session, error)
}

type UserInput struct {
	SessionID string
	Content   string
}

type ProviderFactory interface {
	Build(cfg config.Config) (provider.Provider, error)
}

type DefaultProviderFactory struct{}

type Service struct {
	configManager   *config.Manager
	sessionStore    Store
	toolRegistry    *tools.Registry
	providerFactory ProviderFactory
	events          chan RuntimeEvent
}

func New(configManager *config.Manager, toolRegistry *tools.Registry, sessionStore Store) *Service {
	return NewWithFactory(configManager, toolRegistry, sessionStore, DefaultProviderFactory{})
}

func NewWithFactory(configManager *config.Manager, toolRegistry *tools.Registry, sessionStore Store, providerFactory ProviderFactory) *Service {
	if providerFactory == nil {
		providerFactory = DefaultProviderFactory{}
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
	if strings.TrimSpace(input.Content) == "" {
		return errors.New("runtime: input content is empty")
	}

	session, err := s.loadOrCreateSession(ctx, input.SessionID, input.Content)
	if err != nil {
		s.emit(EventError, "", err.Error())
		return err
	}

	userMessage := provider.Message{
		Role:    "user",
		Content: input.Content,
	}
	session.Messages = append(session.Messages, userMessage)
	session.UpdatedAt = time.Now()
	if err := s.sessionStore.Save(ctx, &session); err != nil {
		s.emit(EventError, session.ID, err.Error())
		return err
	}
	s.emit(EventUserMessage, session.ID, userMessage)

	for attempt := 0; ; attempt++ {
		cfg := s.configManager.Get()
		maxLoops := cfg.MaxLoops
		if maxLoops <= 0 {
			maxLoops = 8
		}
		if attempt >= maxLoops {
			err := errors.New("runtime: max loop reached")
			s.emit(EventError, session.ID, err.Error())
			return err
		}

		modelProvider, err := s.providerFactory.Build(cfg)
		if err != nil {
			s.emit(EventError, session.ID, err.Error())
			return err
		}

		streamEvents := make(chan provider.StreamEvent, 32)
		streamDone := make(chan struct{})
		go s.forwardProviderEvents(session.ID, streamEvents, streamDone)

		resp, err := modelProvider.Chat(ctx, provider.ChatRequest{
			Model:        cfg.CurrentModel,
			SystemPrompt: s.systemPrompt(),
			Messages:     s.trimMessages(session.Messages),
			Tools:        s.toolRegistry.GetSpecs(),
		}, streamEvents)
		close(streamEvents)
		<-streamDone
		if err != nil {
			s.emit(EventError, session.ID, err.Error())
			return err
		}

		assistant := resp.Message
		if strings.TrimSpace(assistant.Role) == "" {
			assistant.Role = "assistant"
		}

		if strings.TrimSpace(assistant.Content) != "" || len(assistant.ToolCalls) > 0 {
			session.Messages = append(session.Messages, assistant)
			session.UpdatedAt = time.Now()
			if err := s.sessionStore.Save(ctx, &session); err != nil {
				s.emit(EventError, session.ID, err.Error())
				return err
			}
		}

		if len(assistant.ToolCalls) == 0 {
			s.emit(EventAgentDone, session.ID, assistant)
			return nil
		}

		for _, call := range assistant.ToolCalls {
			s.emit(EventToolStart, session.ID, call)

			runCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ToolTimeoutSec)*time.Second)
			result, execErr := s.toolRegistry.Execute(runCtx, tools.ToolCallInput{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: []byte(call.Arguments),
				Workdir:   cfg.Workdir,
				SessionID: session.ID,
				EmitChunk: func(chunk []byte) {
					s.emit(EventToolChunk, session.ID, string(chunk))
				},
			})
			cancel()

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
				s.emit(EventError, session.ID, err.Error())
				return err
			}

			s.emit(EventToolResult, session.ID, result)
		}
	}
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

func (s *Service) emit(kind EventType, sessionID string, payload any) {
	s.events <- RuntimeEvent{
		Type:      kind,
		SessionID: sessionID,
		Payload:   payload,
	}
}

func (s *Service) forwardProviderEvents(sessionID string, input <-chan provider.StreamEvent, done chan<- struct{}) {
	defer close(done)
	for event := range input {
		switch event.Type {
		case provider.StreamEventTextDelta:
			s.emit(EventAgentChunk, sessionID, event.Text)
		}
	}
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

func (DefaultProviderFactory) Build(cfg config.Config) (provider.Provider, error) {
	selected, err := cfg.SelectedProviderConfig()
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(selected.Type)) {
	case "openai":
		return openai.New(selected)
	case "anthropic":
		return anthropic.New(selected), nil
	case "gemini":
		return gemini.New(selected), nil
	default:
		return nil, fmt.Errorf("runtime: unsupported provider type %q", selected.Type)
	}
}
