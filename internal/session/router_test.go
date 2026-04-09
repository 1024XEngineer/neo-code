package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestScopedStoreRouterStoreForWorkspaceCachesPerRoot(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	defaultRoot := filepath.Join(t.TempDir(), "workspace-default")
	otherRoot := filepath.Join(t.TempDir(), "workspace-other")

	router := NewScopedStoreRouter(baseDir, defaultRoot)
	defaultStore := router.StoreForWorkspace("")
	if defaultStore == nil {
		t.Fatalf("expected default workspace store")
	}
	if got := router.StoreForWorkspace(defaultRoot); got != defaultStore {
		t.Fatalf("expected default workspace store to be reused")
	}

	otherStore := router.StoreForWorkspace(otherRoot)
	if otherStore == nil {
		t.Fatalf("expected other workspace store")
	}
	if otherStore == defaultStore {
		t.Fatalf("expected distinct store instances for different workspace roots")
	}
	if got := router.StoreForWorkspace(otherRoot); got != otherStore {
		t.Fatalf("expected other workspace store to be reused")
	}
}

func TestScopedStoreRouterSaveLoadAndListUseDefaultWorkspace(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	defaultRoot := filepath.Join(t.TempDir(), "workspace-default")
	router := NewScopedStoreRouter(baseDir, defaultRoot)

	session := &Session{
		ID:        "session-default",
		Title:     "Default Workspace",
		CreatedAt: time.Now().Add(-time.Minute),
		UpdatedAt: time.Now(),
		Workdir:   defaultRoot,
	}
	if err := router.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := router.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ID != session.ID || loaded.Title != session.Title {
		t.Fatalf("expected saved session to be loaded back, got %+v", loaded)
	}

	summaries, err := router.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != session.ID {
		t.Fatalf("expected default workspace summary, got %+v", summaries)
	}
}

func TestScopedStoreRouterNilReceiverBehaviors(t *testing.T) {
	t.Parallel()

	var router *ScopedStoreRouter

	if store := router.StoreForWorkspace("workspace"); store != nil {
		t.Fatalf("expected nil receiver to return nil store, got %#v", store)
	}
	if err := router.Save(context.Background(), &Session{ID: "session"}); err != nil {
		t.Fatalf("expected nil receiver Save() to be a no-op, got %v", err)
	}
	loaded, err := router.Load(context.Background(), "session")
	if err != nil {
		t.Fatalf("expected nil receiver Load() to return nil error, got %v", err)
	}
	if loaded.ID != "" || loaded.Title != "" || len(loaded.Messages) != 0 {
		t.Fatalf("expected zero session from nil receiver Load(), got %+v", loaded)
	}
	summaries, err := router.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("expected nil receiver ListSummaries() to return nil error, got %v", err)
	}
	if summaries != nil {
		t.Fatalf("expected nil summaries from nil receiver, got %+v", summaries)
	}
}
