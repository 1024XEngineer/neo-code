package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"neocode/internal/provider"
	"neocode/internal/tools"
)

const defaultMaxTurns = 8

// UserInput is a normalized input submitted from TUI.
type UserInput struct {
	SessionID string
	Content   string
}

// ProviderBinding connects a provider client with the model used for that provider.
type ProviderBinding struct {
	Name   string
	Model  string
	Client provider.Provider
}

// ProviderSummary is the TUI-friendly view of an available provider.
type ProviderSummary struct {
	Name  string
	Model string
}

// Status describes the runtime state visible to the TUI.
type Status struct {
	IsRunning bool
	Text      string
	Provider  string
	Model     string
	Workdir   string
}

// Option customizes runtime construction.
type Option func(*Service)

// WithMaxTurns overrides the default tool loop turn cap.
func WithMaxTurns(maxTurns int) Option {
	return func(s *Service) {
		if maxTurns > 0 {
			s.maxTurns = maxTurns
		}
	}
}

// WithProviders registers all available providers and selects the active provider.
func WithProviders(bindings []ProviderBinding, selected string) Option {
	return func(s *Service) {
		if len(bindings) == 0 {
			return
		}

		s.providers = make(map[string]ProviderBinding, len(bindings))
		s.providerOrder = make([]string, 0, len(bindings))
		for _, binding := range bindings {
			if binding.Client == nil {
				continue
			}
			name := strings.TrimSpace(binding.Name)
			if name == "" {
				name = binding.Client.Name()
			}
			binding.Name = name
			s.providers[name] = binding
			s.providerOrder = append(s.providerOrder, name)
		}
		if len(s.providerOrder) == 0 {
			return
		}
		if _, ok := s.providers[selected]; ok {
			s.activeProvider = selected
			s.model = s.providers[selected].Model
			return
		}

		first := s.providers[s.providerOrder[0]]
		s.activeProvider = first.Name
		s.model = first.Model
	}
}

// WithSessionStorePath enables on-disk session persistence.
func WithSessionStorePath(path string) Option {
	return func(s *Service) {
		s.sessionStorePath = strings.TrimSpace(path)
	}
}

// Service orchestrates sessions, prompts, provider calls, tool execution, and event dispatch.
type Service struct {
	registry         *tools.Registry
	sessions         *SessionStore
	prompts          *PromptBuilder
	bus              *EventBus
	model            string
	workdir          string
	maxTurns         int
	sessionStorePath string

	running int32

	providersMu    sync.RWMutex
	providers      map[string]ProviderBinding
	providerOrder  []string
	activeProvider string

	statusMu sync.RWMutex
	status   Status
}

// New constructs a runtime service with an initial session.
func New(modelProvider provider.Provider, registry *tools.Registry, model, workdir string, opts ...Option) (*Service, error) {
	service := &Service{
		registry: registry,
		prompts:  NewPromptBuilder(workdir),
		bus:      NewEventBus(),
		model:    model,
		workdir:  workdir,
		maxTurns: defaultMaxTurns,
		providers: map[string]ProviderBinding{
			modelProvider.Name(): {
				Name:   modelProvider.Name(),
				Model:  model,
				Client: modelProvider,
			},
		},
		providerOrder:  []string{modelProvider.Name()},
		activeProvider: modelProvider.Name(),
	}

	for _, opt := range opts {
		opt(service)
	}

	active, ok := service.currentBinding()
	if !ok {
		return nil, fmt.Errorf("no active provider configured")
	}

	service.status = Status{
		Text:     "Ready",
		Provider: active.Name,
		Model:    active.Model,
		Workdir:  workdir,
	}

	store, err := NewSessionStore(service.sessionStorePath)
	if err != nil {
		return nil, err
	}
	service.sessions = store

	if len(service.sessions.List()) == 0 {
		if _, err := service.CreateSession(""); err != nil {
			return nil, err
		}
	}

	return service, nil
}

// Subscribe attaches a new runtime event subscriber.
func (s *Service) Subscribe(buffer int) <-chan Event {
	return s.bus.Subscribe(buffer)
}

// CreateSession adds a new empty session and emits a session event.
func (s *Service) CreateSession(title string) (SessionSummary, error) {
	session, err := s.sessions.Create(title)
	if err != nil {
		return SessionSummary{}, err
	}
	s.publish(Event{
		Type:      EventSessionCreated,
		SessionID: session.ID,
		Payload:   session,
		At:        time.Now(),
	})

	return SessionSummary{
		ID:           session.ID,
		Title:        session.Title,
		MessageCount: len(session.Messages),
		UpdatedAt:    session.UpdatedAt,
	}, nil
}

// Session returns a copy of the selected session.
func (s *Service) Session(id string) (Session, bool) {
	return s.sessions.Get(id)
}

// Sessions returns ordered session summaries.
func (s *Service) Sessions() []SessionSummary {
	return s.sessions.List()
}

// ProviderSummaries returns all available providers in UI order.
func (s *Service) ProviderSummaries() []ProviderSummary {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()

	summaries := make([]ProviderSummary, 0, len(s.providerOrder))
	for _, name := range s.providerOrder {
		binding, ok := s.providers[name]
		if !ok {
			continue
		}
		summaries = append(summaries, ProviderSummary{
			Name:  binding.Name,
			Model: binding.Model,
		})
	}

	return summaries
}

// Status returns a copy of the runtime status.
func (s *Service) Status() Status {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()

	return s.status
}

// SwitchProvider changes the active provider used for future runs.
func (s *Service) SwitchProvider(name string) error {
	if atomic.LoadInt32(&s.running) != 0 {
		return fmt.Errorf("cannot switch provider while the agent is running")
	}

	s.providersMu.Lock()
	binding, ok := s.providers[name]
	if !ok {
		s.providersMu.Unlock()
		return fmt.Errorf("provider %q not found", name)
	}
	s.activeProvider = name
	s.model = binding.Model
	s.providersMu.Unlock()

	s.statusMu.Lock()
	s.status.Provider = binding.Name
	s.status.Model = binding.Model
	if !s.status.IsRunning {
		s.status.Text = "Ready"
	}
	s.statusMu.Unlock()

	s.publish(Event{
		Type:    EventStatus,
		Payload: s.Status(),
		At:      time.Now(),
	})
	return nil
}

// Run executes the full agent loop for a single user input.
func (s *Service) Run(ctx context.Context, input UserInput) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("agent is already running")
	}
	defer atomic.StoreInt32(&s.running, 0)

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return fmt.Errorf("input cannot be empty")
	}
	if !s.sessions.Exists(input.SessionID) {
		return fmt.Errorf("session %q not found", input.SessionID)
	}

	userMessage := provider.Message{
		Role:    provider.RoleUser,
		Content: content,
	}

	if _, err := s.sessions.Append(input.SessionID, userMessage); err != nil {
		return err
	}

	if session, ok := s.sessions.Get(input.SessionID); ok && len(session.Messages) == 1 {
		title := deriveTitle(content)
		_ = s.sessions.SetTitle(input.SessionID, title)
	}

	s.publish(Event{
		Type:      EventUserMessage,
		SessionID: input.SessionID,
		Payload:   userMessage,
		At:        time.Now(),
	})

	s.setStatus("Thinking...", true)
	err := s.executeLoop(ctx, input)
	if err != nil {
		s.publish(Event{
			Type:      EventError,
			SessionID: input.SessionID,
			Payload:   err.Error(),
			At:        time.Now(),
		})
	}
	s.setStatus("Ready", false)
	return err
}

func (s *Service) currentBinding() (ProviderBinding, bool) {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()

	binding, ok := s.providers[s.activeProvider]
	return binding, ok
}

func (s *Service) setStatus(text string, running bool) {
	s.statusMu.Lock()
	s.status.Text = text
	s.status.IsRunning = running
	s.statusMu.Unlock()

	s.publish(Event{
		Type:    EventStatus,
		Payload: s.Status(),
		At:      time.Now(),
	})
}

func (s *Service) publish(event Event) {
	s.bus.Publish(event)
}

func deriveTitle(input string) string {
	title := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if len(title) > 36 {
		title = title[:36] + "..."
	}
	if title == "" {
		return "Untitled Session"
	}
	return title
}
