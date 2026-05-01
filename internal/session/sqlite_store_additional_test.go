package session

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if err := store.UpdateSessionTitle(ctx, UpdateSessionTitleInput{SessionID: "cancel_ctx", Title: "x"}); err == nil {
		t.Fatalf("expected UpdateSessionTitle canceled error")
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
	if err := store.UpdateSessionTitle(ctx, UpdateSessionTitleInput{SessionID: "bad/id", Title: "x"}); err == nil {
		t.Fatalf("expected UpdateSessionTitle invalid id error")
	}
	if err := store.ReplaceTranscript(ctx, ReplaceTranscriptInput{SessionID: "bad/id"}); err == nil {
		t.Fatalf("expected ReplaceTranscript invalid id error")
	}
}

func TestSQLiteStoreUpdateSessionTitlePersistsSanitizedTitle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	created, err := store.CreateSession(context.Background(), CreateSessionInput{
		ID:    "title_update_case",
		Title: "New Session",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	updatedAt := created.UpdatedAt.Add(time.Minute)
	if err := store.UpdateSessionTitle(context.Background(), UpdateSessionTitleInput{
		SessionID: created.ID,
		UpdatedAt: updatedAt,
		Title:     " line1 \n\t line2 ",
	}); err != nil {
		t.Fatalf("UpdateSessionTitle() error = %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "line1 line2" {
		t.Fatalf("expected sanitized single-line title, got %q", loaded.Title)
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
		Head: SessionHead{
			Todos: []TodoItem{{ID: "todo-1", Content: "a"}},
		},
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

func TestNormalizeCreateSessionInputNormalizesMultilineTitle(t *testing.T) {
	t.Parallel()

	session, err := normalizeCreateSessionInput(CreateSessionInput{
		Title: "  first line\n\tsecond line   third  ",
	})
	if err != nil {
		t.Fatalf("normalizeCreateSessionInput() error = %v", err)
	}
	if session.Title != "first line second line third" {
		t.Fatalf("normalized title = %q, want single-line collapsed whitespace", session.Title)
	}
}

func TestSQLiteStoreCreateSessionPropagatesEnsureStorageDirsError(t *testing.T) {
	t.Parallel()

	invalidBase := createNonDirectoryPath(t)
	store := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join(invalidBase, "db.sqlite"),
	}
	_, err := store.CreateSession(context.Background(), CreateSessionInput{ID: "s1", Title: "title"})
	if err == nil {
		t.Fatalf("expected CreateSession() to fail when db dir cannot be created")
	}
}

func TestSQLiteStoreEnsureStorageDirsErrorBranches(t *testing.T) {
	t.Parallel()

	invalidDBDirBase := createNonDirectoryPath(t)
	dbDirErrStore := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join(invalidDBDirBase, "db.sqlite"),
	}
	if err := dbDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create db dir") {
		t.Fatalf("expected create db dir error, got %v", err)
	}

	invalidProjectDirBase := createNonDirectoryPath(t)
	projectDirErrStore := &SQLiteStore{
		projectDir: filepath.Join(invalidProjectDirBase, "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join(t.TempDir(), "db.sqlite"),
	}
	if err := projectDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create project dir") {
		t.Fatalf("expected create project dir error, got %v", err)
	}

	invalidAssetsDirBase := createNonDirectoryPath(t)
	assetsDirErrStore := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(invalidAssetsDirBase, "assets"),
		dbPath:     filepath.Join(t.TempDir(), "db.sqlite"),
	}
	if err := assetsDirErrStore.ensureStorageDirs(); err == nil || !strings.Contains(err.Error(), "create assets dir") {
		t.Fatalf("expected create assets dir error, got %v", err)
	}
}

func TestSQLiteStoreInitializePropagatesStorageDirError(t *testing.T) {
	t.Parallel()

	invalidBase := createNonDirectoryPath(t)
	store := &SQLiteStore{
		projectDir: filepath.Join(t.TempDir(), "project"),
		assetsDir:  filepath.Join(t.TempDir(), "assets"),
		dbPath:     filepath.Join(invalidBase, "db.sqlite"),
	}
	if err := store.initialize(context.Background()); err == nil {
		t.Fatalf("expected initialize() to fail when storage dirs are invalid")
	}
}

func createNonDirectoryPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "non-dir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
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

func TestSQLiteStoreCleanupExpiredSessionsNoopBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "cleanup_noop",
		Title:     "cleanup noop",
		CreatedAt: time.Now().UTC().Add(-time.Hour),
		UpdatedAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	removed, err := store.CleanupExpiredSessions(ctx, 0)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions(0) error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("CleanupExpiredSessions(0) removed = %d, want 0", removed)
	}

	removed, err = store.CleanupExpiredSessions(ctx, DefaultSessionMaxAge)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions(DefaultSessionMaxAge) error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("CleanupExpiredSessions(DefaultSessionMaxAge) removed = %d, want 0", removed)
	}

	if _, err := store.LoadSession(ctx, session.ID); err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
}

func TestSQLiteStoreCleanupExpiredSessionsRemovesExpiredSessionsAndAssets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	expiredAt := time.Now().UTC().Add(-DefaultSessionMaxAge - time.Hour).Truncate(time.Millisecond)
	freshAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

	expired, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "cleanup_expired",
		Title:     "expired",
		CreatedAt: expiredAt,
		UpdatedAt: expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateSession(expired) error = %v", err)
	}
	fresh, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "cleanup_fresh",
		Title:     "fresh",
		CreatedAt: freshAt,
		UpdatedAt: freshAt,
	})
	if err != nil {
		t.Fatalf("CreateSession(fresh) error = %v", err)
	}

	expiredAssetDir := filepath.Join(store.assetsDir, expired.ID)
	freshAssetDir := filepath.Join(store.assetsDir, fresh.ID)
	for _, dir := range []string{expiredAssetDir, freshAssetDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("asset"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", dir, err)
		}
	}

	removed, err := store.CleanupExpiredSessions(ctx, DefaultSessionMaxAge)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("CleanupExpiredSessions() removed = %d, want 1", removed)
	}

	if _, err := store.LoadSession(ctx, expired.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected expired session to be removed, got %v", err)
	}
	if _, err := store.LoadSession(ctx, fresh.ID); err != nil {
		t.Fatalf("expected fresh session to remain, got %v", err)
	}
	if _, err := os.Stat(expiredAssetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected expired asset dir to be removed, got %v", err)
	}
	if _, err := os.Stat(freshAssetDir); err != nil {
		t.Fatalf("expected fresh asset dir to remain, got %v", err)
	}
}

func TestSQLiteStoreCleanupExpiredSessionsSkipsInvalidAssetPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	expiredAt := time.Now().UTC().Add(-DefaultSessionMaxAge - time.Hour).Truncate(time.Millisecond)
	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "cleanup_escape_seed",
		Title:     "escape seed",
		CreatedAt: expiredAt,
		UpdatedAt: expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	maliciousID := filepath.Join("..", "escaped-cleanup-target")
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET id = ? WHERE id = ?`, maliciousID, session.ID); err != nil {
		t.Fatalf("UPDATE session id error = %v", err)
	}

	victimDir := filepath.Join(store.assetsDir, maliciousID)
	if err := os.MkdirAll(victimDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(victim) error = %v", err)
	}
	victimFile := filepath.Join(victimDir, "note.txt")
	if err := os.WriteFile(victimFile, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("WriteFile(victim) error = %v", err)
	}

	removed, err := store.CleanupExpiredSessions(ctx, DefaultSessionMaxAge)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("CleanupExpiredSessions() removed = %d, want 1", removed)
	}
	if _, err := os.Stat(victimFile); err != nil {
		t.Fatalf("expected invalid-path victim file to remain, got %v", err)
	}
}

func TestSQLiteStoreInitializeTightensExistingDirectoryPermissions(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("directory mode assertions are not reliable on Windows")
	}

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewSQLiteStore(baseDir, workspaceRoot)
	t.Cleanup(func() { _ = store.Close() })

	for _, dir := range []string{store.projectDir, store.assetsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatalf("Chmod(%q, 0755) error = %v", dir, err)
		}
	}

	if _, err := store.ensureDB(context.Background()); err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}

	for _, dir := range []string{store.projectDir, store.assetsDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode = %o, want 700", dir, got)
		}
	}
}

func TestSQLiteStoreAppendMessagesCapsBatchAndSessionCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "append_cap", Title: "append cap"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("seed-0")}},
			{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("seed-1")}},
		},
	}); err != nil {
		t.Fatalf("AppendMessages(seed) error = %v", err)
	}

	batch := make([]providertypes.Message, 0, MaxSessionMessages+2)
	for i := 0; i < MaxSessionMessages+2; i++ {
		batch = append(batch, providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(buildIndexedSuffix(i))},
		})
	}
	if err := store.AppendMessages(ctx, AppendMessagesInput{
		SessionID: session.ID,
		Messages:  batch,
	}); err != nil {
		t.Fatalf("AppendMessages(large batch) error = %v", err)
	}

	loaded, err := store.LoadSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != MaxSessionMessages {
		t.Fatalf("len(Messages) = %d, want %d", len(loaded.Messages), MaxSessionMessages)
	}
	if got := renderSessionMessageParts(loaded.Messages[0]); got != buildIndexedSuffix(2) {
		t.Fatalf("first kept message = %q, want %q", got, buildIndexedSuffix(2))
	}
	if got := renderSessionMessageParts(loaded.Messages[len(loaded.Messages)-1]); got != buildIndexedSuffix(MaxSessionMessages+1) {
		t.Fatalf("last kept message = %q, want %q", got, buildIndexedSuffix(MaxSessionMessages+1))
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	var headerCount int
	if err := db.QueryRowContext(ctx, `SELECT message_count FROM sessions WHERE id = ?`, session.ID).Scan(&headerCount); err != nil {
		t.Fatalf("query message_count error = %v", err)
	}
	if headerCount != MaxSessionMessages {
		t.Fatalf("message_count = %d, want %d", headerCount, MaxSessionMessages)
	}

	var rowCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ?`, session.ID).Scan(&rowCount); err != nil {
		t.Fatalf("query row count error = %v", err)
	}
	if rowCount != MaxSessionMessages {
		t.Fatalf("message rows = %d, want %d", rowCount, MaxSessionMessages)
	}
}

func TestDeleteSessionsByIDSetWithBatchSizeDeletesAllBatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	expiredAt := time.Now().UTC().Add(-DefaultSessionMaxAge - time.Hour).Truncate(time.Millisecond)
	sessionIDs := []string{"cleanup_batch_01", "cleanup_batch_02", "cleanup_batch_03", "cleanup_batch_04", "cleanup_batch_05"}
	for _, id := range sessionIDs {
		if _, err := store.CreateSession(ctx, CreateSessionInput{
			ID:        id,
			Title:     id,
			CreatedAt: expiredAt,
			UpdatedAt: expiredAt,
		}); err != nil {
			t.Fatalf("CreateSession(%q) error = %v", id, err)
		}
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer rollbackTx(tx)

	affected, err := deleteSessionsByIDSetWithBatchSize(ctx, tx, sessionIDs, 2)
	if err != nil {
		t.Fatalf("deleteSessionsByIDSetWithBatchSize() error = %v", err)
	}
	if affected != len(sessionIDs) {
		t.Fatalf("affected = %d, want %d", affected, len(sessionIDs))
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	for _, id := range sessionIDs {
		if _, err := store.LoadSession(ctx, id); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %q to be deleted, got %v", id, err)
		}
	}
}

func TestDeleteSessionsByIDSetHelpersCoverEmptyAndDefaultBatchSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	expiredAt := time.Now().UTC().Add(-DefaultSessionMaxAge - time.Hour).Truncate(time.Millisecond)
	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        "cleanup_batch_default",
		Title:     "cleanup batch default",
		CreatedAt: expiredAt,
		UpdatedAt: expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer rollbackTx(tx)

	affected, err := deleteSessionsByIDSetWithBatchSize(ctx, tx, nil, 2)
	if err != nil {
		t.Fatalf("deleteSessionsByIDSetWithBatchSize(nil) error = %v", err)
	}
	if affected != 0 {
		t.Fatalf("deleteSessionsByIDSetWithBatchSize(nil) affected = %d, want 0", affected)
	}

	affected, err = deleteSessionsByIDSet(ctx, tx, []string{session.ID})
	if err != nil {
		t.Fatalf("deleteSessionsByIDSet() error = %v", err)
	}
	if affected != 1 {
		t.Fatalf("deleteSessionsByIDSet() affected = %d, want 1", affected)
	}
}

func TestTrimMessagesToSessionLimitBranches(t *testing.T) {
	t.Parallel()

	within := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("a")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("b")}},
	}
	if got := trimMessagesToSessionLimit(within); len(got) != len(within) {
		t.Fatalf("trimMessagesToSessionLimit(within) len = %d, want %d", len(got), len(within))
	}

	overflow := make([]providertypes.Message, 0, MaxSessionMessages+1)
	for i := 0; i < MaxSessionMessages+1; i++ {
		overflow = append(overflow, providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart(buildIndexedSuffix(i))},
		})
	}
	got := trimMessagesToSessionLimit(overflow)
	if len(got) != MaxSessionMessages {
		t.Fatalf("trimMessagesToSessionLimit(overflow) len = %d, want %d", len(got), MaxSessionMessages)
	}
	if renderSessionMessageParts(got[0]) != buildIndexedSuffix(1) {
		t.Fatalf("first kept trimmed message = %q, want %q", renderSessionMessageParts(got[0]), buildIndexedSuffix(1))
	}
}

func TestRemoveSessionAssetsDirBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "remove_assets_ok", Title: "remove assets"}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	assetDir := filepath.Join(store.assetsDir, "remove_assets_ok")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(assetDir) error = %v", err)
	}
	if err := store.removeSessionAssetsDir("remove_assets_ok"); err != nil {
		t.Fatalf("removeSessionAssetsDir(valid) error = %v", err)
	}
	if _, err := os.Stat(assetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected asset dir removed, got %v", err)
	}

	if err := store.removeSessionAssetsDir("../bad-id"); err == nil {
		t.Fatalf("expected invalid session id error")
	}
}

func TestCleanupExpiredSessionAssetsStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	sessionIDs := []string{"cleanup_ctx_01", "cleanup_ctx_02"}
	for _, id := range sessionIDs {
		assetDir := filepath.Join(store.assetsDir, id)
		if err := os.MkdirAll(assetDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", assetDir, err)
		}
		if err := os.WriteFile(filepath.Join(assetDir, "note.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", assetDir, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.cleanupExpiredSessionAssets(ctx, sessionIDs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanupExpiredSessionAssets() error = %v, want context.Canceled", err)
	}
	for _, id := range sessionIDs {
		if _, err := os.Stat(filepath.Join(store.assetsDir, id)); err != nil {
			t.Fatalf("expected asset dir %q to remain after canceled cleanup, got %v", id, err)
		}
	}
}

func TestBuildSessionFromRowDefaultsMissingExecutorToAgent(t *testing.T) {
	t.Parallel()

	nowMS := toUnixMillis(time.Now().UTC())
	row := sqliteSessionRow{
		ID:            "session_legacy_executor",
		Title:         "legacy",
		CreatedAtMS:   nowMS,
		UpdatedAtMS:   nowMS,
		TaskStateJSON: "{}",
		ActivatedJSON: "[]",
		TodosJSON:     `[{"id":"todo-1","content":"legacy subagent","status":"in_progress","owner_type":"subagent","revision":1}]`,
	}

	session, err := buildSessionFromRow(row, nil)
	if err != nil {
		t.Fatalf("expected missing executor to default, got session=%+v err=%v", session, err)
	}
	if len(session.Todos) != 1 {
		t.Fatalf("todos len = %d, want 1", len(session.Todos))
	}
	if session.Todos[0].Executor != TodoExecutorAgent {
		t.Fatalf("legacy todo executor = %q, want %q", session.Todos[0].Executor, TodoExecutorAgent)
	}
	if session.TodoVersion != CurrentTodoVersion {
		t.Fatalf("todo_version = %d, want %d", session.TodoVersion, CurrentTodoVersion)
	}
}

func TestBuildSessionFromRowDefaultsRetryTodoMissingExecutorToAgent(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	nowMS := toUnixMillis(now)
	nextRetry := now.Add(2 * time.Minute).Format(time.RFC3339Nano)
	row := sqliteSessionRow{
		ID:            "session_legacy_executor_retry",
		Title:         "legacy-retry",
		CreatedAtMS:   nowMS,
		UpdatedAtMS:   nowMS,
		TaskStateJSON: "{}",
		ActivatedJSON: "[]",
		TodosJSON: `[
{"id":"todo-1","content":"legacy subagent retry","status":"blocked","owner_type":"","retry_count":1,"next_retry_at":"` + nextRetry + `","revision":1}
]`,
	}

	session, err := buildSessionFromRow(row, nil)
	if err != nil {
		t.Fatalf("expected retry todo missing executor to default, got session=%+v err=%v", session, err)
	}
	if len(session.Todos) != 1 {
		t.Fatalf("todos len = %d, want 1", len(session.Todos))
	}
	if session.Todos[0].Executor != TodoExecutorAgent {
		t.Fatalf("legacy retry todo executor = %q, want %q", session.Todos[0].Executor, TodoExecutorAgent)
	}
}

func TestLoadSessionDoesNotPersistLegacyExecutorInference(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}

	now := time.Now().UTC()
	_, err = db.ExecContext(ctx, `
INSERT INTO sessions (
	id, title, provider, model, created_at_ms, updated_at_ms, workdir,
	task_state_json, todos_json, activated_skills_json,
	token_input_total, token_output_total, last_seq, message_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		"session_legacy_executor_persist",
		"legacy",
		"",
		"",
		toUnixMillis(now),
		toUnixMillis(now),
		"",
		"{}",
		`[{"id":"todo-1","content":"legacy subagent","status":"pending","owner_type":"subagent","revision":1}]`,
		"[]",
		0,
		0,
		0,
		0,
	)
	if err != nil {
		t.Fatalf("insert legacy session row: %v", err)
	}

	session, err := store.LoadSession(ctx, "session_legacy_executor_persist")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(session.Todos) != 1 {
		t.Fatalf("todos len = %d, want 1", len(session.Todos))
	}
	if session.Todos[0].Executor != TodoExecutorAgent {
		t.Fatalf("loaded executor = %q, want %q", session.Todos[0].Executor, TodoExecutorAgent)
	}

	var persistedTodosJSON string
	if err := db.QueryRowContext(ctx, `SELECT todos_json FROM sessions WHERE id = ?`, session.ID).Scan(&persistedTodosJSON); err != nil {
		t.Fatalf("query persisted todos_json: %v", err)
	}
	if strings.Contains(persistedTodosJSON, `"executor":`) {
		t.Fatalf("persisted todos_json = %q, want legacy payload unchanged during load", persistedTodosJSON)
	}
}
