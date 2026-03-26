package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"neocode/internal/provider"
)

// Session is the in-memory conversation aggregate.
type Session struct {
	ID        string
	Title     string
	Messages  []provider.Message
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionSummary is the sidebar-friendly summary view.
type SessionSummary struct {
	ID           string
	Title        string
	MessageCount int
	UpdatedAt    time.Time
}

// SessionStore manages in-memory sessions.
type SessionStore struct {
	mu       sync.RWMutex
	path     string
	order    []string
	sessions map[string]*Session
}

type sessionSnapshot struct {
	Order    []string   `json:"order"`
	Sessions []*Session `json:"sessions"`
}

// NewSessionStore creates a store and optionally loads it from disk.
func NewSessionStore(path string) (*SessionStore, error) {
	store := &SessionStore{
		path:     path,
		order:    make([]string, 0, 4),
		sessions: make(map[string]*Session),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

// Create adds a new empty session.
func (s *SessionStore) Create(title string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	id := fmt.Sprintf("session-%d", now.UnixNano())
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("Session %d", len(s.order)+1)
	}

	session := &Session{
		ID:        id,
		Title:     title,
		Messages:  make([]provider.Message, 0, 16),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.sessions[id] = session
	s.order = append(s.order, id)

	if err := s.saveLocked(); err != nil {
		delete(s.sessions, id)
		s.order = s.order[:len(s.order)-1]
		return Session{}, err
	}

	return cloneSession(*session), nil
}

// Exists reports whether a session exists.
func (s *SessionStore) Exists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.sessions[id]
	return ok
}

// Get returns a deep copy of a session.
func (s *SessionStore) Get(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}

	return cloneSession(*session), true
}

// List returns the ordered session summaries.
func (s *SessionStore) List() []SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summaries := make([]SessionSummary, 0, len(s.order))
	for _, id := range s.order {
		session := s.sessions[id]
		summaries = append(summaries, SessionSummary{
			ID:           session.ID,
			Title:        session.Title,
			MessageCount: len(session.Messages),
			UpdatedAt:    session.UpdatedAt,
		})
	}

	return summaries
}

// Append adds a message to a session.
func (s *SessionStore) Append(id string, message provider.Message) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return Session{}, fmt.Errorf("session %q not found", id)
	}

	session.Messages = append(session.Messages, cloneMessage(message))
	session.UpdatedAt = time.Now()

	if err := s.saveLocked(); err != nil {
		session.Messages = session.Messages[:len(session.Messages)-1]
		return Session{}, err
	}

	return cloneSession(*session), nil
}

// SetTitle updates the session title.
func (s *SessionStore) SetTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	previous := session.Title
	session.Title = strings.TrimSpace(title)
	session.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		session.Title = previous
		return err
	}
	return nil
}

func (s *SessionStore) load() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read session store: %w", err)
	}

	var snapshot sessionSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		if strings.HasPrefix(strings.TrimSpace(string(payload)), "[") {
			return fmt.Errorf(
				"parse session store: legacy array session store format is not supported; delete or migrate %q to the current snapshot object format",
				s.path,
			)
		}
		return fmt.Errorf("parse session store: %w", err)
	}

	s.order = append([]string(nil), snapshot.Order...)
	s.sessions = make(map[string]*Session, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		if session == nil || strings.TrimSpace(session.ID) == "" {
			continue
		}
		if session.Messages == nil {
			session.Messages = make([]provider.Message, 0)
		}
		cloned := cloneSession(*session)
		s.sessions[cloned.ID] = &cloned
	}

	filteredOrder := make([]string, 0, len(s.order))
	for _, id := range s.order {
		if _, ok := s.sessions[id]; ok {
			filteredOrder = append(filteredOrder, id)
		}
	}
	s.order = filteredOrder
	for id := range s.sessions {
		if !containsSessionID(s.order, id) {
			s.order = append(s.order, id)
		}
	}

	return nil
}

func (s *SessionStore) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}

	snapshot := sessionSnapshot{
		Order:    append([]string(nil), s.order...),
		Sessions: make([]*Session, 0, len(s.order)),
	}
	for _, id := range s.order {
		session, ok := s.sessions[id]
		if !ok {
			continue
		}
		cloned := cloneSession(*session)
		snapshot.Sessions = append(snapshot.Sessions, &cloned)
	}

	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session store: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create session store directory: %w", err)
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write session store: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace session store: %w", err)
	}
	return nil
}

func cloneSession(session Session) Session {
	clone := session
	clone.Messages = make([]provider.Message, len(session.Messages))
	for idx, message := range session.Messages {
		clone.Messages[idx] = cloneMessage(message)
	}
	return clone
}

func cloneMessage(message provider.Message) provider.Message {
	clone := message
	clone.ToolCalls = append([]provider.ToolCall(nil), message.ToolCalls...)
	return clone
}

func containsSessionID(order []string, id string) bool {
	for _, existing := range order {
		if existing == id {
			return true
		}
	}
	return false
}
