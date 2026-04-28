package runtime

import (
	"context"
	"fmt"
	"os"
	"testing"

	agentsession "neo-code/internal/session"
)

type createSessionUpsertStore struct {
	*memoryStore
	missingErr error
}

func (s *createSessionUpsertStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	s.memoryStore.mu.Lock()
	_, exists := s.memoryStore.sessions[id]
	s.memoryStore.mu.Unlock()
	if !exists {
		return agentsession.Session{}, s.missingErr
	}
	return s.memoryStore.LoadSession(ctx, id)
}

func TestServiceCreateSessionUpsertWhenMissing(t *testing.T) {
	t.Parallel()

	store := &createSessionUpsertStore{
		memoryStore: newMemoryStore(),
		missingErr:  fmt.Errorf("load session row: %w", agentsession.ErrSessionNotFound),
	}
	service := &Service{
		configManager: newRuntimeConfigManager(t),
		sessionStore:  store,
	}

	created, err := service.CreateSession(context.Background(), "session-upsert")
	if err != nil {
		t.Fatalf("CreateSession() upsert error = %v", err)
	}
	if created.ID != "session-upsert" {
		t.Fatalf("created session id = %q, want %q", created.ID, "session-upsert")
	}
	if created.Title != "New Session" {
		t.Fatalf("created session title = %q, want %q", created.Title, "New Session")
	}
	if created.TaskState.VerificationProfile != agentsession.VerificationProfileTaskOnly {
		t.Fatalf("created verification profile = %q, want %q", created.TaskState.VerificationProfile, agentsession.VerificationProfileTaskOnly)
	}

	savesAfterCreate := store.memoryStore.saves
	loaded, err := service.CreateSession(context.Background(), "session-upsert")
	if err != nil {
		t.Fatalf("CreateSession() load existing error = %v", err)
	}
	if loaded.ID != "session-upsert" {
		t.Fatalf("loaded session id = %q, want %q", loaded.ID, "session-upsert")
	}
	if store.memoryStore.saves != savesAfterCreate {
		t.Fatalf("unexpected additional create, saves=%d want %d", store.memoryStore.saves, savesAfterCreate)
	}
}

func TestServiceCreateSessionReturnsOriginalErrorWhenMissingErrorIsNotSentinel(t *testing.T) {
	t.Parallel()

	store := &createSessionUpsertStore{
		memoryStore: newMemoryStore(),
		missingErr:  fmt.Errorf("dependency not found"),
	}
	service := &Service{
		configManager: newRuntimeConfigManager(t),
		sessionStore:  store,
	}

	_, err := service.CreateSession(context.Background(), "session-upsert")
	if err == nil {
		t.Fatalf("CreateSession() expected error when missing error is not sentinel")
	}
	if err.Error() != "dependency not found" {
		t.Fatalf("CreateSession() error = %v, want dependency not found", err)
	}
	if store.memoryStore.saves != 0 {
		t.Fatalf("CreateSession() should not create on non-sentinel error, saves=%d", store.memoryStore.saves)
	}
}

type createSessionDuplicateStore struct {
	*createSessionUpsertStore
	createErr error
	loadHits  int
	loaded    agentsession.Session
	loadErr   error
}

func (s *createSessionDuplicateStore) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	s.loadHits++
	if s.loadHits == 1 {
		return agentsession.Session{}, s.missingErr
	}
	if s.loadErr != nil {
		return agentsession.Session{}, s.loadErr
	}
	return s.loaded, nil
}

func (s *createSessionDuplicateStore) CreateSession(ctx context.Context, input agentsession.CreateSessionInput) (agentsession.Session, error) {
	if err := ctx.Err(); err != nil {
		return agentsession.Session{}, err
	}
	if s.createErr != nil {
		return agentsession.Session{}, s.createErr
	}
	return s.memoryStore.CreateSession(ctx, input)
}

func TestServiceCreateSessionBranches(t *testing.T) {
	t.Parallel()

	store := &createSessionUpsertStore{
		memoryStore: newMemoryStore(),
		missingErr:  fmt.Errorf("load session row: %w", agentsession.ErrSessionNotFound),
	}
	service := &Service{
		configManager: newRuntimeConfigManager(t),
		sessionStore:  store,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.CreateSession(ctx, "session-canceled"); err == nil {
		t.Fatalf("CreateSession() should reject canceled context")
	}
	created, err := service.CreateSession(context.Background(), "   ")
	if err != nil {
		t.Fatalf("CreateSession() should auto-generate id for empty input, got error: %v", err)
	}
	if created.ID == "" || len(created.ID) <= len("session_") || created.ID[:len("session_")] != "session_" {
		t.Fatalf("CreateSession() generated id = %q, want prefix %q", created.ID, "session_")
	}
}

func TestServiceCreateSessionReturnsWorkdirResolutionError(t *testing.T) {
	t.Parallel()

	service := &Service{
		sessionStore: newMemoryStore(),
		// 不注入 configManager 会使默认 workdir 为空，触发 resolveWorkdirForSession 错误路径。
	}
	if _, err := service.CreateSession(context.Background(), "session-workdir"); err == nil {
		t.Fatalf("CreateSession() should fail when default workdir cannot be resolved")
	}
}

func TestServiceCreateSessionDuplicateCreateFallsBackToLoad(t *testing.T) {
	t.Parallel()

	store := &createSessionDuplicateStore{
		createSessionUpsertStore: &createSessionUpsertStore{
			memoryStore: newMemoryStore(),
			missingErr:  fmt.Errorf("load session row: %w", agentsession.ErrSessionNotFound),
		},
		createErr: fmt.Errorf("sqlite: %w", agentsession.ErrSessionAlreadyExists),
		loaded:    agentsession.Session{ID: "session-dup", Title: "loaded"},
	}
	service := &Service{
		configManager: newRuntimeConfigManager(t),
		sessionStore:  store,
	}

	loaded, err := service.CreateSession(context.Background(), "session-dup")
	if err != nil {
		t.Fatalf("CreateSession() duplicate fallback error = %v", err)
	}
	if loaded.ID != "session-dup" || loaded.Title != "loaded" {
		t.Fatalf("CreateSession() loaded session = %#v", loaded)
	}
}

func TestCreateSessionErrorPredicates(t *testing.T) {
	t.Parallel()

	if isRuntimeSessionNotFoundError(nil) {
		t.Fatalf("isRuntimeSessionNotFoundError(nil) should be false")
	}
	if !isRuntimeSessionNotFoundError(fmt.Errorf("wrapped: %w", agentsession.ErrSessionNotFound)) {
		t.Fatalf("wrapped ErrSessionNotFound should be detected")
	}

	if isRuntimeSessionAlreadyExistsError(nil) {
		t.Fatalf("isRuntimeSessionAlreadyExistsError(nil) should be false")
	}
	if !isRuntimeSessionAlreadyExistsError(fmt.Errorf("wrapped: %w", agentsession.ErrSessionAlreadyExists)) {
		t.Fatalf("wrapped ErrSessionAlreadyExists should be detected")
	}
	if !isRuntimeSessionAlreadyExistsError(fmt.Errorf("wrapped: %w", os.ErrExist)) {
		t.Fatalf("wrapped os.ErrExist should be detected")
	}
	for _, text := range []string{"already exists", "UNIQUE CONSTRAINT", "duplicate key"} {
		if isRuntimeSessionAlreadyExistsError(fmt.Errorf("%s", text)) {
			t.Fatalf("plain text %q should not be treated as already exists", text)
		}
	}
}
