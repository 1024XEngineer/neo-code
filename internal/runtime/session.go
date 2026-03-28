package runtime

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

	"neo-code/internal/provider"
)

const sessionsDirName = "sessions"

type Session struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Messages  []provider.Message `json:"messages"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Store interface {
	Save(ctx context.Context, session *Session) error
	Load(ctx context.Context, id string) (Session, error)
	ListSummaries(ctx context.Context) ([]SessionSummary, error)
}

type JSONSessionStore struct {
	mu      sync.RWMutex
	baseDir string
}

func NewJSONSessionStore(baseDir string) *JSONSessionStore {
	return &JSONSessionStore{
		baseDir: filepath.Join(baseDir, sessionsDirName),
	}
}

func NewSessionStore(baseDir string) *JSONSessionStore {
	return NewJSONSessionStore(baseDir)
}

func (s *JSONSessionStore) Save(ctx context.Context, session *Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session == nil {
		return errors.New("runtime: session is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return fmt.Errorf("runtime: create sessions dir: %w", err)
	}

	payload, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("runtime: marshal session: %w", err)
	}
	payload = append(payload, '\n')

	target := s.filePath(session.ID)
	temp := target + ".tmp"
	if err := os.WriteFile(temp, payload, 0o644); err != nil {
		return fmt.Errorf("runtime: write temp session: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("runtime: replace session file: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("runtime: commit session file: %w", err)
	}

	return nil
}

func (s *JSONSessionStore) Load(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath(id))
	if err != nil {
		return Session{}, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, fmt.Errorf("runtime: decode session %s: %w", id, err)
	}
	return session, nil
}

func (s *JSONSessionStore) ListSummaries(ctx context.Context) ([]SessionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("runtime: create sessions dir: %w", err)
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: list sessions dir: %w", err)
	}

	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data, readErr := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if readErr != nil {
			continue
		}

		var summary SessionSummary
		if err := json.Unmarshal(data, &summary); err != nil {
			continue
		}
		if strings.TrimSpace(summary.ID) == "" {
			continue
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})

	return summaries, nil
}

func (s *JSONSessionStore) filePath(id string) string {
	return filepath.Join(s.baseDir, id+".json")
}

func newSession(title string) Session {
	now := time.Now()
	return Session{
		ID:        newID("session"),
		Title:     sanitizeTitle(title),
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []provider.Message{},
	}
}

func sanitizeTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "New Session"
	}
	runes := []rune(title)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return title
}
