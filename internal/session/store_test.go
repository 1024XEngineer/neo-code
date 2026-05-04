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
		Head: SessionHead{
			Provider: "openai",
			Model:    "gpt-5",
			Workdir:  "/repo",
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
		},
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
		Head: SessionHead{
			Provider: "openai",
			Model:    "gpt-5.1",
			Workdir:  "/repo/final",
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
			HasUnknownUsage:  true,
		},
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
	if !loaded.HasUnknownUsage {
		t.Fatalf("expected HasUnknownUsage to round-trip")
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
	store := NewSQLiteStore(baseDir, workspaceRoot)
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
		Messages: []providertypes.Message{
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("after")}},
		},
		Head: SessionHead{
			Provider:  "openai",
			Model:     "gpt-5.2",
			Workdir:   "/repo",
			TaskState: TaskState{Goal: "after compact"},
			Todos: []TodoItem{
				{ID: "todo-1", Content: "after compact"},
			},
			TokenInputTotal:  0,
			TokenOutputTotal: 0,
			HasUnknownUsage:  false,
		},
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
	if loaded.HasUnknownUsage {
		t.Fatalf("expected replace transcript to clear HasUnknownUsage")
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
		Head: SessionHead{
			Todos: []TodoItem{
				{ID: "dup", Content: "a"},
				{ID: "dup", Content: "b"},
			},
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
	store := NewSQLiteStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})

	projectDir := projectDirectory(baseDir, workspaceRoot)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}
	db, err := sql.Open("sqlite", DatabasePath(baseDir, workspaceRoot))
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

func TestSQLiteStoreMigratesSchemaV1ToV2(t *testing.T) {
	ctx := context.Background()
	baseDir, workspaceRoot, store := newMigrationTestStore(t)

	createLegacyV1SessionDB(t, ctx, baseDir, workspaceRoot, false)
	loaded, err := store.LoadSession(ctx, "session_v1")
	if err != nil {
		t.Fatalf("LoadSession() after migration error = %v", err)
	}
	if loaded.ID != "session_v1" || loaded.Title != "Legacy V1" {
		t.Fatalf("unexpected migrated session: %+v", loaded)
	}
	if loaded.HasUnknownUsage {
		t.Fatalf("expected migrated HasUnknownUsage to default false")
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	assertPragmaInt(t, db, "user_version", sqliteSchemaVersion)
	assertSQLiteColumnExists(t, db, "sessions", "has_unknown_usage")
}

func TestSQLiteStoreMigratesSchemaV1ToV2WhenColumnAlreadyExists(t *testing.T) {
	ctx := context.Background()
	baseDir, workspaceRoot, store := newMigrationTestStore(t)

	createLegacyV1SessionDB(t, ctx, baseDir, workspaceRoot, true)
	summaries, err := store.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() after migration error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "session_v1" {
		t.Fatalf("unexpected summaries after migration: %+v", summaries)
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	assertPragmaInt(t, db, "user_version", sqliteSchemaVersion)
	assertSQLiteColumnExists(t, db, "sessions", "has_unknown_usage")
}

func TestSQLiteStorePersistsPlanStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:    "session_plan_roundtrip",
		Title: "Plan Roundtrip",
		Head: SessionHead{
			Provider:                        "openai",
			Model:                           "gpt-5",
			Workdir:                         "/repo",
			AgentMode:                       AgentModePlan,
			LastFullPlanRevision:            2,
			PlanApprovalPendingFullAlign:    true,
			PlanCompletionPendingFullReview: true,
			PlanContextDirty:                true,
			PlanRestorePendingAlign:         true,
			CurrentPlan: &PlanArtifact{
				ID:       "plan-1",
				Revision: 2,
				Status:   PlanStatusDraft,
				Spec: PlanSpec{
					Goal:        "落地 plan/build 模式",
					Steps:       []string{"扩展 session", "扩展 runtime"},
					Constraints: []string{"保持 tools 边界"},
					Verify:      []string{"go test ./internal/..."},
					Todos: []TodoItem{
						{ID: "todo-plan-1", Content: "补 plan 模型"},
					},
				},
				Summary: SummaryView{
					Goal:          "落地 plan/build 模式",
					KeySteps:      []string{"扩展 session", "扩展 runtime"},
					Constraints:   []string{"保持 tools 边界"},
					Verify:        []string{"go test ./internal/..."},
					ActiveTodoIDs: []string{"todo-plan-1"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.AgentMode != AgentModePlan {
		t.Fatalf("expected agent mode plan, got %q", loaded.AgentMode)
	}
	if loaded.CurrentPlan == nil {
		t.Fatal("expected current plan to be persisted")
	}
	if loaded.CurrentPlan.ID != "plan-1" || loaded.CurrentPlan.Revision != 2 {
		t.Fatalf("unexpected loaded current plan: %+v", loaded.CurrentPlan)
	}
	if loaded.CurrentPlan.Summary.Goal != "落地 plan/build 模式" {
		t.Fatalf("unexpected loaded summary: %+v", loaded.CurrentPlan.Summary)
	}
	if !loaded.PlanApprovalPendingFullAlign || !loaded.PlanCompletionPendingFullReview ||
		!loaded.PlanContextDirty || !loaded.PlanRestorePendingAlign {
		t.Fatalf("expected plan alignment flags to round-trip, got %+v", loaded)
	}
}

func TestSQLiteStoreMigratesSchemaV2ToV3(t *testing.T) {
	ctx := context.Background()
	baseDir, workspaceRoot, store := newMigrationTestStore(t)

	createLegacyV2SessionDB(t, ctx, baseDir, workspaceRoot)
	loaded, err := store.LoadSession(ctx, "session_v2")
	if err != nil {
		t.Fatalf("LoadSession() after migration error = %v", err)
	}
	if loaded.AgentMode != AgentModeBuild {
		t.Fatalf("expected migrated AgentMode to default build, got %q", loaded.AgentMode)
	}
	if loaded.CurrentPlan != nil {
		t.Fatalf("expected migrated CurrentPlan to default nil, got %+v", loaded.CurrentPlan)
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	assertPragmaInt(t, db, "user_version", sqliteSchemaVersion)
	assertSQLiteColumnExists(t, db, "sessions", "agent_mode")
	assertSQLiteColumnExists(t, db, "sessions", "current_plan_json")
	assertSQLiteColumnExists(t, db, "sessions", "plan_approval_pending_full_align")
	assertSQLiteColumnExists(t, db, "sessions", "plan_completion_pending_full_review")
	assertSQLiteColumnExists(t, db, "sessions", "plan_context_dirty")
	assertSQLiteColumnExists(t, db, "sessions", "plan_restore_pending_align")
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

func assertSQLiteColumnExists(t *testing.T, db *sql.DB, table string, column string) {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s) error = %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		if name == column {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info: %v", err)
	}
	t.Fatalf("expected column %s.%s to exist", table, column)
}

func newMigrationTestStore(t *testing.T) (string, string, *SQLiteStore) {
	t.Helper()
	baseDir, err := os.MkdirTemp("", "session-base-")
	if err != nil {
		t.Fatalf("MkdirTemp() baseDir error = %v", err)
	}
	workspaceRoot, err := os.MkdirTemp("", "session-workspace-")
	if err != nil {
		t.Fatalf("MkdirTemp() workspaceRoot error = %v", err)
	}
	store := NewSQLiteStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})
	return baseDir, workspaceRoot, store
}

func createLegacyV1SessionDB(
	t *testing.T,
	ctx context.Context,
	baseDir string,
	workspaceRoot string,
	includeUnknownUsageColumn bool,
) {
	t.Helper()
	projectDir := projectDirectory(baseDir, workspaceRoot)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}
	db, err := sql.Open("sqlite", DatabasePath(baseDir, workspaceRoot))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	unknownUsageColumn := ""
	unknownUsageInsertColumn := ""
	unknownUsageInsertValue := ""
	if includeUnknownUsageColumn {
		unknownUsageColumn = ", has_unknown_usage INTEGER NOT NULL DEFAULT 0"
		unknownUsageInsertColumn = ", has_unknown_usage"
		unknownUsageInsertValue = ", 0"
	}
	statements := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			workdir TEXT NOT NULL DEFAULT '',
			task_state_json TEXT NOT NULL,
			todos_json TEXT NOT NULL,
			activated_skills_json TEXT NOT NULL,
			token_input_total INTEGER NOT NULL DEFAULT 0,
			token_output_total INTEGER NOT NULL DEFAULT 0` + unknownUsageColumn + `,
			last_seq INTEGER NOT NULL DEFAULT 0,
			message_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE messages (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			parts_json TEXT NOT NULL,
			tool_calls_json TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			is_error INTEGER NOT NULL DEFAULT 0,
			tool_metadata_json TEXT NOT NULL DEFAULT '',
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY(session_id, seq),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE session_assets (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`INSERT INTO sessions (
			id, title, created_at_ms, updated_at_ms, provider, model, workdir,
			task_state_json, todos_json, activated_skills_json,
			token_input_total, token_output_total` + unknownUsageInsertColumn + `,
			last_seq, message_count
		) VALUES (
			'session_v1', 'Legacy V1', 1000, 1000, 'openai', 'gpt-5', '/repo',
			'{}', '[]', '[]', 11, 7` + unknownUsageInsertValue + `, 0, 0
		)`,
		`PRAGMA user_version=1`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("exec legacy schema statement: %v\n%s", err, statement)
		}
	}
}

func createLegacyV2SessionDB(t *testing.T, ctx context.Context, baseDir string, workspaceRoot string) {
	t.Helper()
	projectDir := projectDirectory(baseDir, workspaceRoot)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}
	db, err := sql.Open("sqlite", DatabasePath(baseDir, workspaceRoot))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	statements := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			workdir TEXT NOT NULL DEFAULT '',
			task_state_json TEXT NOT NULL,
			todos_json TEXT NOT NULL,
			activated_skills_json TEXT NOT NULL,
			token_input_total INTEGER NOT NULL DEFAULT 0,
			token_output_total INTEGER NOT NULL DEFAULT 0,
			has_unknown_usage INTEGER NOT NULL DEFAULT 0,
			last_seq INTEGER NOT NULL DEFAULT 0,
			message_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE messages (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			parts_json TEXT NOT NULL,
			tool_calls_json TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			is_error INTEGER NOT NULL DEFAULT 0,
			tool_metadata_json TEXT NOT NULL DEFAULT '',
			created_at_ms INTEGER NOT NULL,
			PRIMARY KEY(session_id, seq),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE session_assets (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			relative_path TEXT NOT NULL,
			created_at_ms INTEGER NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`INSERT INTO sessions (
			id, title, created_at_ms, updated_at_ms, provider, model, workdir,
			task_state_json, todos_json, activated_skills_json,
			token_input_total, token_output_total, has_unknown_usage,
			last_seq, message_count
		) VALUES (
			'session_v2', 'Legacy V2', 1000, 1000, 'openai', 'gpt-5', '/repo',
			'{}', '[]', '[]', 11, 7, 0, 0, 0
		)`,
		`PRAGMA user_version=2`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("exec legacy schema statement: %v\n%s", err, statement)
		}
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
