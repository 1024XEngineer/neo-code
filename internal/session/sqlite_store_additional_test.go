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

func TestSQLiteStoreMethodsRespectCanceledContext(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "cancel_ctx", Title: "cancel"}); err == nil {
		t.Fatalf("expected CreateSession canceled error")
	}
	if _, err := store.LoadSession(ctx, "cancel_ctx"); err == nil {
		t.Fatalf("expected LoadSession canceled error")
	}
	if _, err := store.ListSummaries(ctx); err == nil {
		t.Fatalf("expected ListSummaries canceled error")
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: "cancel_ctx",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}},
		},
	}); err == nil {
		t.Fatalf("expected AppendMessages canceled error")
	}
	if err := store.UpdateSessionState(ctx, UpdateSessionStateInput{SessionID: "cancel_ctx", Title: "x"}); err == nil {
		t.Fatalf("expected UpdateSessionState canceled error")
	}
	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{SessionID: "cancel_ctx"}); err == nil {
		t.Fatalf("expected ReplaceTranscript canceled error")
	}
}

func TestSQLiteStoreMethodsRejectInvalidSessionID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.LoadSession(ctx, "bad/id"); err == nil {
		t.Fatalf("expected LoadSession invalid id error")
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: "bad/id",
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}},
		},
	}); err == nil {
		t.Fatalf("expected AppendMessages invalid id error")
	}
	if err := store.UpdateSessionState(ctx, UpdateSessionStateInput{SessionID: "bad/id", Title: "x"}); err == nil {
		t.Fatalf("expected UpdateSessionState invalid id error")
	}
	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{SessionID: "bad/id"}); err == nil {
		t.Fatalf("expected ReplaceTranscript invalid id error")
	}
}

func TestSQLiteHelperBranches(t *testing.T) {
	if got, err := normalizeMessages(nil); err != nil || got != nil {
		t.Fatalf("normalizeMessages(nil) = (%v, %v), want (nil, nil)", got, err)
	}
	if !fromUnixMillis(0).IsZero() {
		t.Fatalf("fromUnixMillis(0) should return zero time")
	}
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatalf("boolToInt conversion mismatch")
	}

	withoutMetadata := cloneMessage(providertypes.Message{Role: providertypes.RoleAssistant})
	if withoutMetadata.ToolMetadata != nil {
		t.Fatalf("expected nil metadata clone for empty metadata input")
	}

	original := Session{
		ID: "clone_test",
		TaskState: TaskState{
			Goal: "goal",
		},
		ActivatedSkills: []SkillActivation{{SkillID: "go-review"}},
		Todos:           []TodoItem{{ID: "todo-1", Content: "a"}},
		Messages: []providertypes.Message{
			{
				Role:         providertypes.RoleAssistant,
				Parts:        []providertypes.ContentPart{providertypes.NewTextPart("x")},
				ToolMetadata: map[string]string{"k": "v"},
			},
		},
	}
	cloned := cloneSessionValue(original)
	cloned.TaskState.Goal = "updated"
	cloned.ActivatedSkills[0].SkillID = "other"
	cloned.Todos[0].Content = "b"
	cloned.Messages[0].ToolMetadata["k"] = "changed"
	if original.TaskState.Goal != "goal" {
		t.Fatalf("cloneSessionValue should deep-clone task state")
	}
	if original.ActivatedSkills[0].SkillID != "go-review" {
		t.Fatalf("cloneSessionValue should deep-clone activated skills")
	}
	if original.Todos[0].Content != "a" {
		t.Fatalf("cloneSessionValue should deep-clone todos")
	}
	if original.Messages[0].ToolMetadata["k"] != "v" {
		t.Fatalf("cloneSessionValue should deep-clone message metadata")
	}

	if got := mustJSONString(nil); got != "[]" {
		t.Fatalf("mustJSONString(nil) = %q, want []", got)
	}
	var nilMap map[string]string
	if got := mustJSONString(nilMap); got != "{}" {
		t.Fatalf("mustJSONString(nil map) = %q, want {}", got)
	}
	var nilCalls []providertypes.ToolCall
	if got := mustJSONString(nilCalls); got != "[]" {
		t.Fatalf("mustJSONString(nil tool calls) = %q, want []", got)
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("mustJSONString should panic for unsupported value")
		}
	}()
	_ = mustJSONString(func() {})
}

type fakeResult struct {
	rows int64
	err  error
}

func (f fakeResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (f fakeResult) RowsAffected() (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.rows, nil
}

func TestExpectRowsAffectedBranches(t *testing.T) {
	if err := expectRowsAffected(fakeResult{err: errors.New("boom")}, "s1"); err == nil || !strings.Contains(err.Error(), "inspect rows affected") {
		t.Fatalf("expected rows affected inspect error, got %v", err)
	}
	if err := expectRowsAffected(fakeResult{rows: 0}, "s1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist when rows=0, got %v", err)
	} else if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound when rows=0, got %v", err)
	}
	if err := expectRowsAffected(fakeResult{rows: 1}, "s1"); err != nil {
		t.Fatalf("expected rows=1 to pass, got %v", err)
	}
}

func TestStorageHelpersAdditionalErrorBranches(t *testing.T) {
	baseDir := t.TempDir()
	if err := ensurePathWithinBase(filepath.Join(baseDir, "missing"), filepath.Join(baseDir, "target")); err == nil {
		t.Fatalf("expected ensurePathWithinBase to fail with missing base dir")
	}

	missingParentTarget := filepath.Join(baseDir, "missing-parent", "child.txt")
	if _, err := resolvePathForContainment(missingParentTarget); err == nil || !strings.Contains(err.Error(), "eval parent symlinks") {
		t.Fatalf("expected parent symlink resolution error, got %v", err)
	}

	tempFile, tempPath, err := createTempFile(baseDir, "replace-*.tmp", "create temp")
	if err != nil {
		t.Fatalf("createTempFile() error = %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("tempFile.Close() error = %v", err)
	}
	// Using an empty path for target will cause os.Remove to fail.
	err = replaceFileWithTemp(tempPath, "", "invalid")
	if err == nil {
		t.Fatalf("expected replaceFileWithTemp remove-target error for empty path")
	}
}

func TestInitializeSQLiteSchemaOnClosedDB(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "closed.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}
	if err := initializeSQLiteSchema(context.Background(), db); err == nil {
		t.Fatalf("expected initializeSQLiteSchema on closed db to fail")
	}
}

func TestNormalizeCreateSessionInputDefaultsGeneratedID(t *testing.T) {
	t.Parallel()

	session, err := normalizeCreateSessionInput(CreateSessionInput{
		Title: "  test  ",
		Todos: []TodoItem{{ID: "todo-1", Content: "a"}},
	})
	if err != nil {
		t.Fatalf("normalizeCreateSessionInput() error = %v", err)
	}
	if session.ID == "" || !strings.HasPrefix(session.ID, "session_") {
		t.Fatalf("expected generated session id, got %q", session.ID)
	}
	if session.CreatedAt.IsZero() || session.UpdatedAt.IsZero() {
		t.Fatalf("expected default timestamps to be populated")
	}
	if session.TodoVersion != CurrentTodoVersion {
		t.Fatalf("expected todo version %d, got %d", CurrentTodoVersion, session.TodoVersion)
	}
}

func TestSQLiteStoreCreateSessionPropagatesEnsureStorageDirsError(t *testing.T) {
	t.Parallel()

	store := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join("/dev/null", "db.sqlite"),
	}
	_, err := store.CreateSession(context.Background(), CreateSessionInput{ID: "s1", Title: "title"})
	if err == nil {
		t.Fatalf("expected CreateSession() to fail when db dir cannot be created")
	}
}

func TestSQLiteStoreEnsureStorageDirsErrorBranches(t *testing.T) {
	t.Parallel()

	dbDirErrStore := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join("/dev/null", "db.sqlite"),
	}
	if err := dbDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create db dir") {
		t.Fatalf("expected create db dir error, got %v", err)
	}

	projectDirErrStore := &SQLiteStore{
		projectDir: filepath.Join("/dev/null", "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join(t.TempDir(), "db.sqlite"),
	}
	if err := projectDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create project dir") {
		t.Fatalf("expected create project dir error, got %v", err)
	}

	assetsDirErrStore := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join("/dev/null", "assets"),
		dbPath:     filepath.Join(t.TempDir(), "db.sqlite"),
	}
	if err := assetsDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create assets dir") {
		t.Fatalf("expected create assets dir error, got %v", err)
	}
}

func TestSQLiteStoreInitializePropagatesStorageDirError(t *testing.T) {
	t.Parallel()

	store := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join("/dev/null", "db.sqlite"),
	}
	if err := store.initialize(context.Background()); err == nil {
		t.Fatalf("expected initialize() to fail when storage dirs are invalid")
	}
}

func TestWrapSessionNotFoundWithNilCause(t *testing.T) {
	t.Parallel()

	err := wrapSessionNotFound(nil)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestResolveUpdatedAtReturnsProvidedValue(t *testing.T) {
	t.Parallel()

	provided := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if got := resolveUpdatedAt(provided); !got.Equal(provided) {
		t.Fatalf("resolveUpdatedAt should keep non-zero value, got %v want %v", got, provided)
	}
}

func TestSQLiteStoreCreateSessionEnsuresDBParentDir(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "project")
	dbPath := filepath.Join(baseDir, "db-parent", "nested", "session.db")

	store := &SQLiteStore{
		projectDir: projectDir,
		assetsDir:  filepath.Join(projectDir, assetsDirName),
		dbPath:     dbPath,
		limits:     providertypes.DefaultSessionAssetLimits(),
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	created, err := store.CreateSession(context.Background(), CreateSessionInput{
		ID:    "session_parent_dir",
		Title: "parent dir",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if created.ID != "session_parent_dir" {
		t.Fatalf("created session id = %q, want %q", created.ID, "session_parent_dir")
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("expected db parent dir to exist, stat error = %v", err)
	}
}
