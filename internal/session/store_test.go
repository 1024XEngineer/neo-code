package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestJSONStoreSaveLoadAndListSummaries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	older := &Session{
		ID:        "session-old",
		Title:     "Old Session",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		UpdatedAt: time.Now().Add(-1 * time.Hour),
		Messages: []providertypes.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	newer := &Session{
		ID:        "session-new",
		Title:     "New Session",
		CreatedAt: time.Now().Add(-30 * time.Minute),
		UpdatedAt: time.Now(),
		Workdir:   t.TempDir(),
		Messages: []providertypes.Message{
			{Role: "user", Content: "new"},
		},
	}

	if err := store.Save(context.Background(), older); err != nil {
		t.Fatalf("Save older session: %v", err)
	}
	if err := store.Save(context.Background(), newer); err != nil {
		t.Fatalf("Save newer session: %v", err)
	}

	loaded, err := store.Load(context.Background(), older.ID)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Title != older.Title {
		t.Fatalf("expected title %q, got %q", older.Title, loaded.Title)
	}
	if loaded.Workdir != older.Workdir {
		t.Fatalf("expected persisted workdir %q, got %q", older.Workdir, loaded.Workdir)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "world" {
		t.Fatalf("unexpected loaded messages: %+v", loaded.Messages)
	}

	rawPath := filepath.Join(baseDir, sessionsDirName, newer.ID+".json")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read saved session: %v", err)
	}
	if !strings.Contains(string(raw), "\"workdir\"") {
		t.Fatalf("expected persisted session file to include workdir, got:\n%s", string(raw))
	}

	mustWriteSessionFile(t, filepath.Join(baseDir, sessionsDirName, "invalid.json"), "{invalid")
	if err := os.MkdirAll(filepath.Join(baseDir, sessionsDirName, "directory"), 0o755); err != nil {
		t.Fatalf("mkdir stray directory: %v", err)
	}

	summaries, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].ID != newer.ID || summaries[1].ID != older.ID {
		t.Fatalf("expected summaries sorted by UpdatedAt desc, got %+v", summaries)
	}
}

func TestJSONStoreErrors(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.Save(cancelledCtx, &Session{ID: "x"}); err == nil {
		t.Fatalf("expected cancelled save to fail")
	}
	if err := store.Save(context.Background(), nil); err == nil {
		t.Fatalf("expected nil session save to fail")
	}
	if _, err := store.Load(cancelledCtx, "missing"); err == nil {
		t.Fatalf("expected cancelled load to fail")
	}
	if _, err := store.ListSummaries(cancelledCtx); err == nil {
		t.Fatalf("expected cancelled list to fail")
	}
}

func TestJSONStoreCorruptedSessionBehaviors(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	valid := &Session{
		ID:        "valid-session",
		Title:     "Valid Session",
		CreatedAt: time.Now().Add(-time.Minute),
		UpdatedAt: time.Now(),
		Messages:  []providertypes.Message{{Role: "user", Content: "hello"}},
	}
	if err := store.Save(context.Background(), valid); err != nil {
		t.Fatalf("Save valid session: %v", err)
	}

	mustWriteSessionFile(t, filepath.Join(baseDir, sessionsDirName, "broken.json"), "{broken")

	_, err := store.Load(context.Background(), "broken")
	if err == nil || !strings.Contains(err.Error(), "decode session broken") {
		t.Fatalf("expected corrupted session decode error, got %v", err)
	}

	summaries, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != valid.ID {
		t.Fatalf("expected corrupted session file to be skipped, got %+v", summaries)
	}
}

func TestJSONStoreSaveInvalidBaseDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	baseFile := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(baseFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}

	store := NewJSONStore(baseFile)
	err := store.Save(context.Background(), &Session{
		ID:        "session-x",
		Title:     "Broken Save",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "create sessions dir") {
		t.Fatalf("expected invalid base dir error, got %v", err)
	}
}

func TestNewUsesDefaultWorkdirAndEmptyMessages(t *testing.T) {
	t.Parallel()

	session := New("hello title")

	if session.ID == "" {
		t.Fatalf("expected non-empty id")
	}
	if !strings.HasPrefix(session.ID, "session_") {
		t.Fatalf("expected id with session_ prefix, got %q", session.ID)
	}
	if session.Title != "hello title" {
		t.Fatalf("expected title %q, got %q", "hello title", session.Title)
	}
	if session.Workdir != "" {
		t.Fatalf("expected empty workdir, got %q", session.Workdir)
	}
	if len(session.Messages) != 0 {
		t.Fatalf("expected empty messages, got %+v", session.Messages)
	}
	if session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("expected non-zero timestamps, got created=%v updated=%v", session.CreatedAt, session.UpdatedAt)
	}
	if session.UpdatedAt.Before(session.CreatedAt) {
		t.Fatalf("expected UpdatedAt >= CreatedAt, got created=%v updated=%v", session.CreatedAt, session.UpdatedAt)
	}
}

func TestNewWithWorkdirTrimAndTitleSanitize(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("中", 45) // rune 长度 > 40
	workdir := "   /tmp/workdir   "

	session := NewWithWorkdir(tooLong, workdir)

	if session.Workdir != "/tmp/workdir" {
		t.Fatalf("expected trimmed workdir %q, got %q", "/tmp/workdir", session.Workdir)
	}
	if got := len([]rune(session.Title)); got != 40 {
		t.Fatalf("expected title rune length 40, got %d (title=%q)", got, session.Title)
	}
}

func TestNewWithWorkdirFallsBackDefaultTitle(t *testing.T) {
	t.Parallel()

	session := NewWithWorkdir("   \n\t  ", "")

	if session.Title != "New Session" {
		t.Fatalf("expected default title %q, got %q", "New Session", session.Title)
	}
}

func TestNewStoreReturnsJSONStore(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	if store == nil {
		t.Fatalf("expected non-nil store")
	}
}

func TestJSONStoreListSummariesReadDirFailure(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	// 把 sessions 目录位置占成普通文件，触发 ReadDir 失败路径。
	sessionsPath := filepath.Join(baseDir, sessionsDirName)
	if err := os.WriteFile(sessionsPath, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write %s: %v", sessionsPath, err)
	}

	_, err := store.ListSummaries(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create sessions dir") {
		t.Fatalf("expected create sessions dir error, got %v", err)
	}
}

func TestJSONStoreListSummariesContextCanceledDuringIteration(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	for i := 0; i < 10; i++ {
		s := &Session{
			ID:        "session-iter-" + strings.Repeat("x", i+1),
			Title:     "iter",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := store.Save(context.Background(), s); err != nil {
			t.Fatalf("save session %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.ListSummaries(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestJSONStoreLoadDecodeErrorWithNonJSONPayload(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	mustWriteSessionFile(t, filepath.Join(baseDir, sessionsDirName, "decode-bad.json"), "{not-json")

	_, err := store.Load(context.Background(), "decode-bad")
	if err == nil || !strings.Contains(err.Error(), "decode session decode-bad") {
		t.Fatalf("expected decode session error, got %v", err)
	}
}

func TestJSONStoreListSummariesSkipsUnreadableAndMalformedEntries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	valid := &Session{
		ID:        "valid-summary",
		Title:     "Valid",
		CreatedAt: time.Now().Add(-time.Minute),
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), valid); err != nil {
		t.Fatalf("save valid session: %v", err)
	}

	mustWriteSessionFile(t, filepath.Join(baseDir, sessionsDirName, "malformed.json"), "{malformed")
	mustWriteSessionFile(t, filepath.Join(baseDir, sessionsDirName, "empty-id.json"), `{"id":"   ","title":"x"}`)

	summaries, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != valid.ID {
		t.Fatalf("expected only valid summary, got %+v", summaries)
	}
}

func TestJSONStoreSavePersistsProviderModelAndMessages(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir)

	session := &Session{
		ID:        "persist-full-fields",
		Title:     "Persist Fields",
		Provider:  "openai",
		Model:     "gpt-4.1",
		Workdir:   "/tmp/persist-workdir",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Content: "hello"},
			{
				Role:    providertypes.RoleAssistant,
				Content: "calling tool",
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "webfetch", Arguments: `{"url":"https://example.com"}`},
				},
			},
			{Role: providertypes.RoleTool, ToolCallID: "call-1", Content: "ok"},
		},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	rawPath := filepath.Join(baseDir, sessionsDirName, session.ID+".json")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode raw json: %v", err)
	}

	if decoded["provider"] != "openai" {
		t.Fatalf("expected provider persisted, got %+v", decoded["provider"])
	}
	if decoded["model"] != "gpt-4.1" {
		t.Fatalf("expected model persisted, got %+v", decoded["model"])
	}
	if _, ok := decoded["messages"]; !ok {
		t.Fatalf("expected messages field persisted, got %+v", decoded)
	}
	if decoded["workdir"] != session.Workdir {
		t.Fatalf("expected workdir persisted as %q, got %+v", session.Workdir, decoded["workdir"])
	}
}

func mustWriteSessionFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
