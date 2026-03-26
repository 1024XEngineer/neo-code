package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neocode/internal/provider"
)

func TestSessionStorePersistsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	store, err := NewSessionStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	session, err := store.Create("Persisted Session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.Append(session.ID, provider.Message{
		Role:    provider.RoleUser,
		Content: "hello persistence",
	}); err != nil {
		t.Fatalf("append message: %v", err)
	}

	reloaded, err := NewSessionStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}

	got, ok := reloaded.Get(session.ID)
	if !ok {
		t.Fatalf("expected persisted session to exist")
	}
	if got.Title != "Persisted Session" {
		t.Fatalf("unexpected title: %q", got.Title)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hello persistence" {
		t.Fatalf("unexpected persisted messages: %#v", got.Messages)
	}
}

func TestSessionStoreRejectsLegacyArrayFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	now := time.Now().UTC().Truncate(time.Second)

	payload := `[
  {
    "id": "session-legacy",
    "title": "Legacy Session",
    "messages": [
      {
        "role": "user",
        "content": "hello from legacy store"
      }
    ],
    "createdAt": "` + now.Format(time.RFC3339) + `",
    "updatedAt": "` + now.Format(time.RFC3339) + `"
  }
]`

	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}

	_, err := NewSessionStore(path)
	if err == nil {
		t.Fatalf("expected legacy array format to be rejected")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "legacy array session store format is not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
