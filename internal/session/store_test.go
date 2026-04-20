package session

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestSQLiteStoreLifecycleRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	createdAt := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Millisecond)
	updatedAt := createdAt.Add(time.Minute)

	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "session_roundtrip",
		Title:     "  Session Roundtrip  ",
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Provider:  "openai",
		Model:     "gpt-5",
		Workdir:   "/repo",
		TaskState: TaskState{
			Goal:     "ship sqlite migration",
			Progress: []string{"draft plan"},
		},
		ActivatedSkills: []SkillActivation{{SkillID: "go_review"}, {SkillID: "go-review"}},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "implement store"},
		},
		TokenInputTotal:  11,
		TokenOutputTotal: 7,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ID != "session_roundtrip" || session.Title != "Session Roundtrip" {
		t.Fatalf("unexpected created session: %+v", session)
	}

	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")},
			},
			{
				Role: providertypes.RoleAssistant,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("world"),
				},
				ToolCalls: []providertypes.ToolCall{{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`}},
			},
		},
		UpdatedAt:        updatedAt.Add(time.Minute),
		Provider:         "openai",
		Model:            "gpt-5.1",
		Workdir:          "/repo/subdir",
		TokenInputDelta:  3,
		TokenOutputDelta: 5,
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	if err := store.UpdateSessionState(ctx, UpdateSessionStateInput{
		SessionID: session.ID,
		Title:     "SQLite Ready",
		UpdatedAt: updatedAt.Add(2 * time.Minute),
		Provider:  "openai",
		Model:     "gpt-5.1",
		Workdir:   "/repo/final",
		TaskState: TaskState{
			Goal:            "ship sqlite migration",
			Progress:        []string{"draft plan", "replace store"},
			UserConstraints: []string{"no legacy compatibility"},
		},
		ActivatedSkills: []SkillActivation{{SkillID: "go-review"}},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "implement store", Status: TodoStatusInProgress},
		},
		TokenInputTotal:  99,
		TokenOutputTotal: 42,
	}); err != nil {
		t.Fatalf("UpdateSessionState() error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "SQLite Ready" || loaded.Workdir != "/repo/final" {
		t.Fatalf("unexpected loaded header: %+v", loaded)
	}
	if loaded.Provider != "openai" || loaded.Model != "gpt-5.1" {
		t.Fatalf("unexpected provider/model: %+v", loaded)
	}
	if loaded.TokenInputTotal != 99 || loaded.TokenOutputTotal != 42 {
		t.Fatalf("unexpected token totals: in=%d out=%d", loaded.TokenInputTotal, loaded.TokenOutputTotal)
	}
	if got := loaded.ActiveSkillIDs(); len(got) != 1 || got[0] != "go-review" {
		t.Fatalf("unexpected active skills: %+v", got)
	}
	if len(loaded.Todos) != 1 || loaded.Todos[0].Status != TodoStatusInProgress {
		t.Fatalf("unexpected todos: %+v", loaded.Todos)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Messages))
	}
	if renderSessionMessageParts(loaded.Messages[0]) != "hello" || renderSessionMessageParts(loaded.Messages[1]) != "world" {
		t.Fatalf("unexpected messages: %+v", loaded.Messages)
	}
	if len(loaded.Messages[1].ToolCalls) != 1 || loaded.Messages[1].ToolCalls[0].ID != "call-1" {
		t.Fatalf("unexpected tool calls: %+v", loaded.Messages[1].ToolCalls)
	}
}

func TestSQLiteStoreCreateSessionDuplicateReturnsSentinel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	input := CreateSessionInput{ID: "dup_session", Title: "dup"}
	if _, err := store.CreateSession(ctx, input); err != nil {
		t.Fatalf("first CreateSession() error = %v", err)
	}

	_, err := store.CreateSession(ctx, input)
	if err == nil {
		t.Fatalf("expected duplicate CreateSession() to fail")
	}
	if !errors.Is(err, ErrSessionAlreadyExists) {
		t.Fatalf("expected ErrSessionAlreadyExists, got %v", err)
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected os.ErrExist chain, got %v", err)
	}
}

func TestSQLiteStoreListSummariesSortedAndLegacyJSONIgnored(t *testing.T) {
	ctx := context.Background()
	baseDir, err := os.MkdirTemp("", "session-base-")
	if err != nil {
		t.Fatalf("MkdirTemp() baseDir error = %v", err)
	}
	workspaceRoot, err := os.MkdirTemp("", "session-workspace-")
	if err != nil {
		t.Fatalf("MkdirTemp() workspaceRoot error = %v", err)
	}
	store := NewStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})

	legacyPath := filepath.Join(projectDirectory(baseDir, workspaceRoot), "sessions", "legacy", "session.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy path: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"id":"legacy"}`), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	firstTime := time.Now().Add(-2 * time.Hour).UTC()
	secondTime := firstTime.Add(time.Hour)
	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "s1", Title: "Older", CreatedAt: firstTime, UpdatedAt: firstTime}); err != nil {
		t.Fatalf("CreateSession(s1) error = %v", err)
	}
	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "s2", Title: "Newer", CreatedAt: secondTime, UpdatedAt: secondTime}); err != nil {
		t.Fatalf("CreateSession(s2) error = %v", err)
	}

	summaries, err := store.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].ID != "s2" || summaries[1].ID != "s1" {
		t.Fatalf("unexpected summary order: %+v", summaries)
	}
}

func TestSQLiteStoreReplaceTranscriptAndPragmas(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "replace_me", Title: "replace me"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("before-response")}},
		},
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{
		SessionID: session.ID,
		UpdatedAt: time.Now().UTC(),
		Provider:  "openai",
		Model:     "gpt-5.2",
		Workdir:   "/repo",
		TaskState: TaskState{Goal: "after compact"},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "after compact"},
		},
		Messages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after")}},
		},
		TokenInputTotal:  0,
		TokenOutputTotal: 0,
	}); err != nil {
		t.Fatalf("ReplaceTranscript() error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "replace me" {
		t.Fatalf("expected title to be preserved after replace, got %q", loaded.Title)
	}
	if len(loaded.Messages) != 1 || renderSessionMessageParts(loaded.Messages[0]) != "after" {
		t.Fatalf("unexpected messages after replace: %+v", loaded.Messages)
	}
	if loaded.TaskState.Goal != "after compact" {
		t.Fatalf("unexpected task state after replace: %+v", loaded.TaskState)
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	assertPragmaString(t, db, "journal_mode", "wal")
	assertPragmaInt(t, db, "foreign_keys", 1)
	assertPragmaInt(t, db, "busy_timeout", 5000)
	assertPragmaInt(t, db, "user_version", sqliteSchemaVersion)
}

func TestSQLiteStoreAppendMessagesRollbackOnTriggerFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "rollback_me", Title: "rollback"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TRIGGER fail_second_insert
BEFORE INSERT ON messages
WHEN NEW.seq = 2
BEGIN
	SELECT RAISE(ABORT, 'boom');
END
`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	err = store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("one")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("two")}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("AppendMessages() err = %v, want trigger failure", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 0 {
		t.Fatalf("expected rollback to leave zero messages, got %+v", loaded.Messages)
	}
}

func TestSQLiteStoreErrors(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "bad/id", Title: "x"}); err == nil {
		t.Fatalf("expected invalid create session id error")
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{SessionID: "missing"}); err == nil {
		t.Fatalf("expected append empty messages error")
	}
	if err := store.UpdateSessionState(ctx, UpdateSessionStateInput{SessionID: "missing", Title: "x"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected update missing session to return os.ErrNotExist, got %v", err)
	} else if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected update missing session to return ErrSessionNotFound, got %v", err)
	}
	if _, err := store.LoadSession(ctx, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected load missing session to return os.ErrNotExist, got %v", err)
	} else if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected load missing session to return ErrSessionNotFound, got %v", err)
	}
}

func TestSQLiteStoreEnsureDBCanRetryAfterInitFailure(t *testing.T) {
	store := newTestStore(t)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.ensureDB(canceledCtx); err == nil {
		t.Fatalf("expected ensureDB() with canceled context to fail")
	}
	db, err := store.ensureDB(context.Background())
	if err != nil {
		t.Fatalf("ensureDB() retry with healthy context error = %v", err)
	}
	if db == nil {
		t.Fatalf("expected ensureDB() retry to return non-nil db")
	}
}

func TestSQLiteStoreLoadSessionRejectsCorruptHeaderAndMessageData(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "corrupt_header", Title: "header"})
	if err != nil {
		t.Fatalf("CreateSession(corrupt_header) error = %v", err)
	}
	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET task_state_json = '{' WHERE id = ?`, session.ID); err != nil {
		t.Fatalf("corrupt task_state_json: %v", err)
	}
	if _, err := store.LoadSession(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "decode task_state") {
		t.Fatalf("expected task_state decode error, got %v", err)
	}

	session, err = store.CreateSession(ctx, CreateSessionInput{ID: "corrupt_message", Title: "message"})
	if err != nil {
		t.Fatalf("CreateSession(corrupt_message) error = %v", err)
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ok")}},
		},
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE messages SET parts_json = '{' WHERE session_id = ?`, session.ID); err != nil {
		t.Fatalf("corrupt parts_json: %v", err)
	}
	if _, err := store.LoadSession(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "decode message parts") {
		t.Fatalf("expected message parts decode error, got %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE messages SET parts_json = '[]', tool_calls_json = '{' WHERE session_id = ?`, session.ID); err != nil {
		t.Fatalf("corrupt tool_calls_json: %v", err)
	}
	if _, err := store.LoadSession(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "decode tool calls") {
		t.Fatalf("expected tool calls decode error, got %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE messages SET tool_calls_json = '[]', tool_metadata_json = '{' WHERE session_id = ?`, session.ID); err != nil {
		t.Fatalf("corrupt tool_metadata_json: %v", err)
	}
	if _, err := store.LoadSession(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "decode tool metadata") {
		t.Fatalf("expected tool metadata decode error, got %v", err)
	}
}

func TestSQLiteStoreAppendReplaceAndSchemaErrors(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: "missing_session",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}},
		},
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected append missing session to return os.ErrNotExist, got %v", err)
	} else if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected append missing session to return ErrSessionNotFound, got %v", err)
	}

	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "invalid_message", Title: "invalid"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	invalidPart := providertypes.ContentPart{Kind: "unknown"}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages:  []providertypes.Message{{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{invalidPart}}},
	}); err == nil {
		t.Fatalf("expected invalid message parts error")
	}
	if err := store.UpdateSessionState(ctx, UpdateSessionStateInput{
		SessionID: session.ID,
		Title:     "x",
		Todos: []TodoItem{
			{ID: "dup", Content: "a"},
			{ID: "dup", Content: "b"},
		},
	}); err == nil {
		t.Fatalf("expected invalid todos error")
	}
	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{
		SessionID: session.ID,
		Messages:  []providertypes.Message{{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{invalidPart}}},
	}); err == nil {
		t.Fatalf("expected replace transcript invalid message error")
	}
	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{
		SessionID: "missing_session",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("ok")}},
		},
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected replace transcript missing session to return os.ErrNotExist, got %v", err)
	} else if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected replace transcript missing session to return ErrSessionNotFound, got %v", err)
	}
}

func TestSQLiteStoreInitializationRejectsUnsupportedSchemaVersion(t *testing.T) {
	ctx := context.Background()
	baseDir, err := os.MkdirTemp("", "session-base-")
	if err != nil {
		t.Fatalf("MkdirTemp() baseDir error = %v", err)
	}
	workspaceRoot, err := os.MkdirTemp("", "session-workspace-")
	if err != nil {
		t.Fatalf("MkdirTemp() workspaceRoot error = %v", err)
	}
	store := NewStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})

	projectDir := projectDirectory(baseDir, workspaceRoot)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}
	db, err := sql.Open("sqlite", databasePath(baseDir, workspaceRoot))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA user_version=999`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	if _, err := store.ListSummaries(ctx); err == nil || !strings.Contains(err.Error(), "unsupported sqlite schema version") {
		t.Fatalf("expected unsupported schema version error, got %v", err)
	}
}

func assertPragmaString(t *testing.T, db *sql.DB, name string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`PRAGMA ` + name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s scan error = %v", name, err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %q, want %q", name, got, want)
	}
}

func assertPragmaInt(t *testing.T, db *sql.DB, name string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`PRAGMA ` + name).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s scan error = %v", name, err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %d, want %d", name, got, want)
	}
}

func renderSessionMessageParts(message providertypes.Message) string {
	if len(message.Parts) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, part := range message.Parts {
		builder.WriteString(part.Text)
	}
	return builder.String()
}
