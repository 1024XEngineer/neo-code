package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestJSONStoreSaveLoadAndListSummaries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace root: %v", err)
	}
	store := NewJSONStore(baseDir, workspaceRoot)

	older := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "session-old",
		Title:         "Old Session",
		CreatedAt:     time.Now().Add(-2 * time.Hour),
		UpdatedAt:     time.Now().Add(-1 * time.Hour),
		Messages: []providertypes.Message{
			{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			{Role: "assistant", Parts: []providertypes.ContentPart{providertypes.NewTextPart("world")}},
		},
	}
	newer := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "session-new",
		Title:         "New Session",
		CreatedAt:     time.Now().Add(-30 * time.Minute),
		UpdatedAt:     time.Now(),
		Workdir:       t.TempDir(),
		Messages: []providertypes.Message{
			{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("new")}},
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
	if len(loaded.Messages) != 2 || renderPartsForTest(loaded.Messages[1].Parts) != "world" {
		t.Fatalf("unexpected loaded messages: %+v", loaded.Messages)
	}

	rawPath := sessionFilePathForTest(baseDir, workspaceRoot, newer.ID)
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read saved session: %v", err)
	}
	if !strings.Contains(string(raw), "\"workdir\"") {
		t.Fatalf("expected persisted session file to include workdir, got:\n%s", string(raw))
	}

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "invalid"), "{invalid")
	if err := os.MkdirAll(filepath.Join(sessionDirectory(baseDir, workspaceRoot), "directory"), 0o755); err != nil {
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

func TestJSONStoreScopesSessionsByWorkspaceRoot(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceA := filepath.Join(t.TempDir(), "中文项目A")
	workspaceB := filepath.Join(t.TempDir(), "中文项目B")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("mkdir workspaceA: %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("mkdir workspaceB: %v", err)
	}

	storeA := NewJSONStore(baseDir, workspaceA)
	storeB := NewJSONStore(baseDir, workspaceB)

	sessionA := &Session{SchemaVersion: CurrentSchemaVersion, ID: "session-a", Title: "A", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	sessionB := &Session{SchemaVersion: CurrentSchemaVersion, ID: "session-b", Title: "B", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := storeA.Save(context.Background(), sessionA); err != nil {
		t.Fatalf("save sessionA: %v", err)
	}
	if err := storeB.Save(context.Background(), sessionB); err != nil {
		t.Fatalf("save sessionB: %v", err)
	}

	summariesA, err := storeA.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() for storeA error: %v", err)
	}
	if len(summariesA) != 1 || summariesA[0].ID != sessionA.ID {
		t.Fatalf("expected storeA to only list sessionA, got %+v", summariesA)
	}

	summariesB, err := storeB.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() for storeB error: %v", err)
	}
	if len(summariesB) != 1 || summariesB[0].ID != sessionB.ID {
		t.Fatalf("expected storeB to only list sessionB, got %+v", summariesB)
	}

	if _, err := storeA.Load(context.Background(), sessionB.ID); err == nil {
		t.Fatalf("expected storeA to fail loading session from another workspace bucket")
	}
}

func TestHashWorkspaceRootNormalizesChinesePathVariants(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "中文项目")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	normalized := NormalizeWorkspaceRoot(base)
	slashVariant := strings.ReplaceAll(normalized, `\`, `/`)
	if got, want := HashWorkspaceRoot(normalized), HashWorkspaceRoot(slashVariant); got != want {
		t.Fatalf("expected slash variants to hash equally, got %q and %q", got, want)
	}

	upperVariant := strings.ToUpper(normalized)
	lowerVariant := strings.ToLower(normalized)
	gotCaseUpper := HashWorkspaceRoot(upperVariant)
	gotCaseLower := HashWorkspaceRoot(lowerVariant)
	if goruntime.GOOS == "windows" {
		if gotCaseUpper != gotCaseLower {
			t.Fatalf("expected case variants to hash equally on windows, got %q and %q", gotCaseUpper, gotCaseLower)
		}
	} else {
		if gotCaseUpper == gotCaseLower {
			t.Fatalf("expected case variants to hash differently on case-sensitive platforms, got %q", gotCaseUpper)
		}
	}
}

func TestWorkspaceHelpersHandleEmptyAndRelativePath(t *testing.T) {
	t.Parallel()

	if got := WorkspacePathKey("   "); got != "" {
		t.Fatalf("expected empty workspace key, got %q", got)
	}
	if got := NormalizeWorkspaceRoot("   "); got != "" {
		t.Fatalf("expected empty normalized workspace root, got %q", got)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relative := "."
	normalized := NormalizeWorkspaceRoot(relative)
	if normalized != filepath.Clean(workingDir) {
		t.Fatalf("expected relative path to normalize to %q, got %q", filepath.Clean(workingDir), normalized)
	}

	if got, want := HashWorkspaceRoot(""), HashWorkspaceRoot("   "); got != want {
		t.Fatalf("expected empty workspace root variants to share fallback hash, got %q want %q", got, want)
	}
}

func TestJSONStoreErrors(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir, t.TempDir())

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
	if _, err := store.Load(context.Background(), "   "); err == nil || !strings.Contains(err.Error(), "session id is empty") {
		t.Fatalf("expected empty session id load error, got %v", err)
	}
}

func TestJSONStoreCorruptedSessionBehaviors(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	valid := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "valid-session",
		Title:         "Valid Session",
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Messages:      []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
	}
	if err := store.Save(context.Background(), valid); err != nil {
		t.Fatalf("Save valid session: %v", err)
	}

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "broken"), "{broken")

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

	store := NewJSONStore(baseFile, t.TempDir())
	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "session-x",
		Title:         "Broken Save",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "create sessions dir") {
		t.Fatalf("expected invalid base dir error, got %v", err)
	}
}

func TestJSONStoreSaveReplaceFailureWhenTargetIsNonEmptyDirectory(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)
	targetDir := sessionFilePathForTest(baseDir, workspaceRoot, "blocked")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write child file: %v", err)
	}

	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "blocked",
		Title:         "Blocked",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "replace file") {
		t.Fatalf("expected replace failure, got %v", err)
	}
}

func TestJSONStoreSaveOverwritesExistingSessionFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)
	session := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "overwrite",
		Title:         "First",
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now().Add(-time.Minute),
	}
	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save initial session: %v", err)
	}

	session.Title = "Second"
	session.UpdatedAt = time.Now()
	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save updated session: %v", err)
	}

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load updated session: %v", err)
	}
	if loaded.Title != "Second" {
		t.Fatalf("expected overwritten session title %q, got %q", "Second", loaded.Title)
	}
}

func TestJSONStoreSaveWriteTempFailure(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("chmod write permission behavior is platform-specific on windows")
	}

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)
	sessionDir := filepath.Join(sessionDirectory(baseDir, workspaceRoot), "temp-blocked")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.Chmod(sessionDir, 0o555); err != nil {
		t.Fatalf("chmod session dir readonly: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sessionDir, 0o755)
	})

	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "temp-blocked",
		Title:         "Temp Blocked",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "create temp file") {
		t.Fatalf("expected temp write failure, got %v", err)
	}
}

func TestEnsurePathWithinBaseRejectsEscapedPath(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	escaped := filepath.Join(baseDir, "..", "escaped", "session.json")

	if err := ensurePathWithinBase(baseDir, escaped); err == nil || !strings.Contains(err.Error(), "escapes base dir") {
		t.Fatalf("expected escaped path rejection, got %v", err)
	}
}

func TestJSONStoreLoadMissingFileReturnsError(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	if _, err := store.Load(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing file load to fail")
	}
}

func TestJSONStoreLoadRejectsInvalidSessionID(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	if _, err := store.Load(context.Background(), "bad/id"); err == nil || !strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("expected invalid session id error, got %v", err)
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
	if session.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", CurrentSchemaVersion, session.SchemaVersion)
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
	if session.TaskState.Established() {
		t.Fatalf("expected empty task state, got %+v", session.TaskState)
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

	tooLong := strings.Repeat("测", 45)
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

	store := NewStore(t.TempDir(), t.TempDir())
	if store == nil {
		t.Fatalf("expected non-nil store")
	}
}

func TestJSONStoreListSummariesReadDirFailure(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	sessionsPath := sessionDirectory(baseDir, workspaceRoot)
	mustWriteSessionFile(t, sessionsPath, "not-a-dir")

	_, err := store.ListSummaries(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create sessions dir") {
		t.Fatalf("expected create sessions dir error, got %v", err)
	}
}

func TestJSONStoreListSummariesContextCanceledDuringIteration(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	store := NewJSONStore(baseDir, t.TempDir())

	for i := 0; i < 10; i++ {
		s := &Session{
			SchemaVersion: CurrentSchemaVersion,
			ID:            "session-iter-" + strings.Repeat("x", i+1),
			Title:         "iter",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
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
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "decode-bad"), "{not-json")

	_, err := store.Load(context.Background(), "decode-bad")
	if err == nil || !strings.Contains(err.Error(), "decode session decode-bad") {
		t.Fatalf("expected decode session error, got %v", err)
	}
}

func TestJSONStoreLoadRejectsMissingSchemaVersion(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(
		t,
		sessionFilePathForTest(baseDir, workspaceRoot, "missing-schema"),
		`{"id":"missing-schema","title":"x","task_state":{"goal":"","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[],"last_updated_at":"0001-01-01T00:00:00Z"},"messages":[]}`,
	)

	_, err := store.Load(context.Background(), "missing-schema")
	if err == nil || !strings.Contains(err.Error(), "missing required field schema_version") {
		t.Fatalf("expected missing schema_version rejection, got %v", err)
	}
}

func TestJSONStoreLoadRejectsMissingTaskState(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(
		t,
		sessionFilePathForTest(baseDir, workspaceRoot, "missing-task-state"),
		`{"schema_version":2,"id":"missing-task-state","title":"x","messages":[]}`,
	)

	_, err := store.Load(context.Background(), "missing-task-state")
	if err == nil || !strings.Contains(err.Error(), "missing required field task_state") {
		t.Fatalf("expected missing task_state rejection, got %v", err)
	}
}

func TestJSONStoreListSummariesSkipsUnreadableAndMalformedEntries(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	valid := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "valid-summary",
		Title:         "Valid",
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
	}
	if err := store.Save(context.Background(), valid); err != nil {
		t.Fatalf("save valid session: %v", err)
	}

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "malformed"), "{malformed")
	mustWriteSessionFile(
		t,
		sessionFilePathForTest(baseDir, workspaceRoot, "empty-id"),
		`{"schema_version":2,"id":"   ","title":"x","created_at":"2026-04-13T00:00:00Z","updated_at":"2026-04-13T00:00:00Z","task_state":{"goal":"","progress":[],"open_items":[],"next_step":"","blockers":[],"key_artifacts":[],"decisions":[],"user_constraints":[],"last_updated_at":"2026-04-13T00:00:00Z"}}`,
	)
	mustWriteSessionFile(
		t,
		sessionFilePathForTest(baseDir, workspaceRoot, "missing-task-state-summary"),
		`{"schema_version":2,"id":"missing-task-state-summary","title":"x","created_at":"2026-04-13T00:00:00Z","updated_at":"2026-04-13T00:00:00Z"}`,
	)

	summaries, err := store.ListSummaries(context.Background())
	if err != nil {
		t.Fatalf("ListSummaries() error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != valid.ID {
		t.Fatalf("expected only valid summary, got %+v", summaries)
	}
}

func TestJSONStoreSaveRejectsInvalidSchemaAndID(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())

	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion + 1,
		ID:            "session-invalid-schema",
		Title:         "Invalid Schema",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected schema version error, got %v", err)
	}

	err = store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "bad/session",
		Title:         "Invalid ID",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("expected invalid id error, got %v", err)
	}
}

func TestJSONStoreSaveCreateSessionDirFailure(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)
	sessionID := "session-dir-failed"

	sessionDirPath := filepath.Join(sessionDirectory(baseDir, workspaceRoot), sessionID)
	if err := os.MkdirAll(filepath.Dir(sessionDirPath), 0o755); err != nil {
		t.Fatalf("mkdir session parent: %v", err)
	}
	if err := os.WriteFile(sessionDirPath, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("write session dir blocker: %v", err)
	}

	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            sessionID,
		Title:         "Blocked SessionDir",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "create session dir") {
		t.Fatalf("expected create session dir error, got %v", err)
	}
}

func TestDecodeStoredSessionAndSummarySchemaValidation(t *testing.T) {
	t.Parallel()

	_, err := decodeStoredSession([]byte(`{
  "schema_version": 999,
  "id": "decode-invalid-schema",
  "title": "x",
  "created_at": "2026-04-13T08:00:00Z",
  "updated_at": "2026-04-13T08:00:00Z",
  "task_state": {
    "goal": "",
    "progress": [],
    "open_items": [],
    "next_step": "",
    "blockers": [],
    "key_artifacts": [],
    "decisions": [],
    "user_constraints": [],
    "last_updated_at": "2026-04-13T08:00:00Z"
  },
  "messages": []
}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected decodeStoredSession schema error, got %v", err)
	}

	_, err = decodeStoredSummary([]byte(`{
  "schema_version": 999,
  "id": "summary-invalid-schema",
  "title": "Summary",
  "created_at": "2026-04-13T08:00:00Z",
  "updated_at": "2026-04-13T08:00:00Z",
  "task_state": {
    "goal": "",
    "progress": [],
    "open_items": [],
    "next_step": "",
    "blockers": [],
    "key_artifacts": [],
    "decisions": [],
    "user_constraints": [],
    "last_updated_at": "2026-04-13T08:00:00Z"
  }
}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("expected decodeStoredSummary schema error, got %v", err)
	}
}

func TestJSONStoreSavePersistsProviderModelAndMessages(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	session := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "persist-full-fields",
		Title:         "Persist Fields",
		Provider:      "openai",
		Model:         "gpt-4.1",
		Workdir:       "/tmp/persist-workdir",
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}},
			{
				Role:  providertypes.RoleAssistant,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("calling tool")},
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "webfetch", Arguments: `{"url":"https://example.com"}`},
				},
			},
			{
				Role:       providertypes.RoleTool,
				ToolCallID: "call-1",
				Parts:      []providertypes.ContentPart{providertypes.NewTextPart("ok")},
				ToolMetadata: map[string]string{
					"tool_name":   "webfetch",
					"http_status": "200",
				},
			},
		},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	rawPath := sessionFilePathForTest(baseDir, workspaceRoot, session.ID)
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

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load saved session: %v", err)
	}
	if loaded.Messages[2].ToolMetadata["tool_name"] != "webfetch" || loaded.Messages[2].ToolMetadata["http_status"] != "200" {
		t.Fatalf("expected tool metadata round-trip, got %+v", loaded.Messages[2].ToolMetadata)
	}
}

func TestJSONStoreSaveRoundTripsMetadataOnlyToolMessage(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	session := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "metadata-only-tool-message",
		Title:         "Metadata Only Tool Message",
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now(),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("inspect")}},
			{
				Role: providertypes.RoleAssistant,
				ToolCalls: []providertypes.ToolCall{
					{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
				},
			},
			{
				Role:       providertypes.RoleTool,
				ToolCallID: "call-1",
				Parts:      []providertypes.ContentPart{providertypes.NewTextPart("")},
				ToolMetadata: map[string]string{
					"tool_name": "filesystem_read_file",
					"path":      "README.md",
				},
			},
		},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load saved session: %v", err)
	}
	if renderPartsForTest(loaded.Messages[2].Parts) != "" {
		t.Fatalf("expected empty content to round-trip, got %q", renderPartsForTest(loaded.Messages[2].Parts))
	}
	if loaded.Messages[2].ToolMetadata["tool_name"] != "filesystem_read_file" ||
		loaded.Messages[2].ToolMetadata["path"] != "README.md" {
		t.Fatalf("expected metadata-only tool message round-trip, got %+v", loaded.Messages[2].ToolMetadata)
	}
}

func TestDecodeStoredSummaryUsesLightweightMetadataPath(t *testing.T) {
	t.Parallel()

	summary, err := decodeStoredSummary([]byte(`{
  "schema_version": 2,
  "id": "summary-only",
  "title": "Summary Only",
  "created_at": "2026-04-13T08:00:00Z",
  "updated_at": "2026-04-13T09:00:00Z",
  "task_state": {
    "goal": "persist task state",
    "progress": [],
    "open_items": [],
    "next_step": "",
    "blockers": [],
    "key_artifacts": [],
    "decisions": [],
    "user_constraints": [],
    "last_updated_at": "2026-04-13T09:00:00Z"
  }
}`))
	if err != nil {
		t.Fatalf("decodeStoredSummary() error: %v", err)
	}

	if summary.ID != "summary-only" {
		t.Fatalf("expected summary id %q, got %q", "summary-only", summary.ID)
	}
	if summary.Title != "Summary Only" {
		t.Fatalf("expected summary title %q, got %q", "Summary Only", summary.Title)
	}
	if summary.CreatedAt.IsZero() || summary.UpdatedAt.IsZero() {
		t.Fatalf("expected non-zero timestamps, got created=%v updated=%v", summary.CreatedAt, summary.UpdatedAt)
	}
}

func TestJSONStoreSaveClampsOversizedTaskState(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	progress := make([]string, 0, taskStateMaxListItems+8)
	for i := 0; i < taskStateMaxListItems+8; i++ {
		progress = append(progress, strings.Repeat("p", taskStateMaxListItemChars-4)+buildIndexedSuffix(i))
	}
	session := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "task-state-clamp-save",
		Title:         "Clamp Save",
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		TaskState: TaskState{
			Goal:      strings.Repeat("g", taskStateMaxFieldChars+50),
			NextStep:  strings.Repeat("n", taskStateMaxFieldChars+50),
			Progress:  progress,
			OpenItems: progress,
		},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if len([]rune(session.TaskState.Goal)) != taskStateMaxFieldChars {
		t.Fatalf("expected goal to be clamped to %d runes, got %d", taskStateMaxFieldChars, len([]rune(session.TaskState.Goal)))
	}
	if len(session.TaskState.Progress) != taskStateMaxListItems {
		t.Fatalf("expected progress list clamped to %d, got %d", taskStateMaxListItems, len(session.TaskState.Progress))
	}
	if len([]rune(session.TaskState.Progress[0])) != taskStateMaxListItemChars {
		t.Fatalf(
			"expected progress item clamped to %d runes, got %d",
			taskStateMaxListItemChars,
			len([]rune(session.TaskState.Progress[0])),
		)
	}
}

func TestJSONStoreLoadClampsOversizedTaskState(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	payload := strings.Join([]string{
		`{`,
		`  "schema_version": 2,`,
		`  "id": "task-state-clamp-load",`,
		`  "title": "Clamp Load",`,
		`  "created_at": "2026-04-13T08:00:00Z",`,
		`  "updated_at": "2026-04-13T09:00:00Z",`,
		`  "task_state": {`,
		`    "goal": "` + strings.Repeat("g", taskStateMaxFieldChars+30) + `",`,
		`    "progress": [` + buildQuotedRepeatedWithIndex("p", taskStateMaxListItemChars+30, taskStateMaxListItems+3) + `],`,
		`    "open_items": [],`,
		`    "next_step": "",`,
		`    "blockers": [],`,
		`    "key_artifacts": [],`,
		`    "decisions": [],`,
		`    "user_constraints": [],`,
		`    "last_updated_at": "2026-04-13T09:00:00Z"`,
		`  },`,
		`  "messages": []`,
		`}`,
	}, "\n")
	mustWriteSessionFile(
		t,
		sessionFilePathForTest(baseDir, workspaceRoot, "task-state-clamp-load"),
		payload,
	)

	loaded, err := store.Load(context.Background(), "task-state-clamp-load")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len([]rune(loaded.TaskState.Goal)) != taskStateMaxFieldChars {
		t.Fatalf("expected loaded goal to be clamped to %d runes, got %d", taskStateMaxFieldChars, len([]rune(loaded.TaskState.Goal)))
	}
	if len(loaded.TaskState.Progress) != taskStateMaxListItems {
		t.Fatalf("expected loaded progress list clamped to %d, got %d", taskStateMaxListItems, len(loaded.TaskState.Progress))
	}
}

func TestJSONStoreSaveLoadRoundTripTodos(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	createdAt := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(5 * time.Minute)
	session := &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "todos-round-trip",
		Title:         "Todos Round Trip",
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		TaskState:     TaskState{},
		Todos: []TodoItem{
			{
				ID:           "todo-1",
				Content:      "  design session todo model  ",
				Status:       TodoStatusPending,
				Dependencies: []string{"todo-2", "todo-2", " "},
				CreatedAt:    createdAt,
				UpdatedAt:    updatedAt,
			},
			{
				ID: "todo-2", Content: "persist todos in session",
				Status:    TodoStatusInProgress,
				Priority:  2,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
		},
		Messages: []providertypes.Message{{Role: "user", Parts: []providertypes.ContentPart{providertypes.NewTextPart("hello")}}},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("save session with todos: %v", err)
	}
	if got := session.Todos[0].Dependencies; len(got) != 1 || got[0] != "todo-2" {
		t.Fatalf("expected dependencies normalized in-memory, got %+v", got)
	}
	if got := session.Todos[0].Content; got != "design session todo model" {
		t.Fatalf("expected content normalized, got %q", got)
	}

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load session with todos: %v", err)
	}
	if len(loaded.Todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(loaded.Todos))
	}
	if loaded.Todos[0].ID != "todo-1" || loaded.Todos[1].ID != "todo-2" {
		t.Fatalf("unexpected todo ids: %+v", loaded.Todos)
	}
	if loaded.Todos[1].Priority != 2 {
		t.Fatalf("expected priority 2, got %d", loaded.Todos[1].Priority)
	}
}

func TestJSONStoreLoadAllowsMissingTodosField(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "no-todos"), strings.Join([]string{
		`{`,
		`  "schema_version": 2,`,
		`  "id": "no-todos",`,
		`  "title": "No Todos",`,
		`  "created_at": "2026-04-14T10:00:00Z",`,
		`  "updated_at": "2026-04-14T10:05:00Z",`,
		`  "task_state": {`,
		`    "goal": "",`,
		`    "progress": [],`,
		`    "open_items": [],`,
		`    "next_step": "",`,
		`    "blockers": [],`,
		`    "key_artifacts": [],`,
		`    "decisions": [],`,
		`    "user_constraints": [],`,
		`    "last_updated_at": "2026-04-14T10:05:00Z"`,
		`  },`,
		`  "messages": []`,
		`}`,
	}, "\n"))

	loaded, err := store.Load(context.Background(), "no-todos")
	if err != nil {
		t.Fatalf("load session without todos field: %v", err)
	}
	if len(loaded.Todos) != 0 {
		t.Fatalf("expected no todos, got %+v", loaded.Todos)
	}
}

func TestJSONStoreLoadBackfillsTodoVersionWhenMissing(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "todos-no-version"), strings.Join([]string{
		`{`,
		`  "schema_version": 2,`,
		`  "id": "todos-no-version",`,
		`  "title": "Todos No Version",`,
		`  "created_at": "2026-04-14T10:00:00Z",`,
		`  "updated_at": "2026-04-14T10:05:00Z",`,
		`  "task_state": {`,
		`    "goal": "",`,
		`    "progress": [],`,
		`    "open_items": [],`,
		`    "next_step": "",`,
		`    "blockers": [],`,
		`    "key_artifacts": [],`,
		`    "decisions": [],`,
		`    "user_constraints": [],`,
		`    "last_updated_at": "2026-04-14T10:05:00Z"`,
		`  },`,
		`  "todos": [`,
		`    {`,
		`      "id": "todo-1",`,
		`      "content": "todo item",`,
		`      "status": "pending",`,
		`      "created_at": "2026-04-14T10:00:00Z",`,
		`      "updated_at": "2026-04-14T10:05:00Z"`,
		`    }`,
		`  ],`,
		`  "messages": []`,
		`}`,
	}, "\n"))

	loaded, err := store.Load(context.Background(), "todos-no-version")
	if err != nil {
		t.Fatalf("load session with todos and missing todo_version: %v", err)
	}
	if loaded.TodoVersion != CurrentTodoVersion {
		t.Fatalf("expected todo version %d, got %d", CurrentTodoVersion, loaded.TodoVersion)
	}
}

func TestJSONStoreSaveRejectsInvalidTodos(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	err := store.Save(context.Background(), &Session{
		SchemaVersion: CurrentSchemaVersion,
		ID:            "invalid-todos",
		Title:         "Invalid Todos",
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		TaskState:     TaskState{},
		Todos: []TodoItem{
			{ID: "todo-1", Content: "first", Dependencies: []string{"missing"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown dependency "missing"`) {
		t.Fatalf("expected invalid dependency error, got %v", err)
	}
}

func TestDecodeStoredSummaryRejectsMissingSchemaVersion(t *testing.T) {
	t.Parallel()

	_, err := decodeStoredSummary([]byte(`{"id":"summary-no-schema","task_state":{}}`))
	if err == nil || !strings.Contains(err.Error(), "missing required field schema_version") {
		t.Fatalf("expected missing schema_version error, got %v", err)
	}
}

func TestCreateTempFileAndAtomicReplaceFailureBranches(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	missingDir := filepath.Join(baseDir, "missing", "dir")
	if _, _, err := createTempFile(missingDir, "tmp-*.tmp", "create temp file"); err == nil ||
		!strings.Contains(err.Error(), "create temp file") {
		t.Fatalf("expected create temp file error for missing dir, got %v", err)
	}

	source := filepath.Join(baseDir, "source.tmp")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write source temp: %v", err)
	}
	blockerDir := filepath.Join(baseDir, "target")
	if err := os.MkdirAll(filepath.Join(blockerDir, "child"), 0o755); err != nil {
		t.Fatalf("mkdir blocker dir: %v", err)
	}
	if err := replaceFileWithTemp(source, blockerDir, "file"); err == nil || !strings.Contains(err.Error(), "replace file") {
		t.Fatalf("expected replace failure for non-empty target dir, got %v", err)
	}
}

func TestReplaceFileWithTempCommitFailure(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	target := filepath.Join(baseDir, "target.txt")
	if err := replaceFileWithTemp(filepath.Join(baseDir, "missing.tmp"), target, "file"); err == nil ||
		!strings.Contains(err.Error(), "commit file") {
		t.Fatalf("expected commit failure when temp file missing, got %v", err)
	}
}

func TestWriteFileAtomicallyCreateTempFailure(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "missing", "nested", "session.json")
	err := writeFileAtomically(target, "session-*.tmp", []byte("data"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "create temp file") {
		t.Fatalf("expected create temp file error, got %v", err)
	}
}

func TestJSONStoreRejectsInvalidBasePathDuringAssetAndSessionResolve(t *testing.T) {
	t.Parallel()

	store := &JSONStore{baseDir: string([]byte{'b', 'a', 'd', 0})}
	if _, err := store.Load(context.Background(), "session-ok"); err == nil {
		t.Fatalf("expected load error for invalid base path")
	}

	if _, err := store.SaveAsset(context.Background(), "session-ok", strings.NewReader("img"), "image/png"); err == nil {
		t.Fatalf("expected save asset error for invalid base path")
	}

	if _, err := store.Stat(context.Background(), "session-ok", "asset-ok"); err == nil {
		t.Fatalf("expected stat error for invalid base path")
	}
}

func TestJSONStoreLoadRejectsInvalidTodos(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()
	store := NewJSONStore(baseDir, workspaceRoot)

	mustWriteSessionFile(t, sessionFilePathForTest(baseDir, workspaceRoot, "invalid-todos-load"), strings.Join([]string{
		`{`,
		`  "schema_version": 2,`,
		`  "id": "invalid-todos-load",`,
		`  "title": "Invalid Todos Load",`,
		`  "created_at": "2026-04-14T10:00:00Z",`,
		`  "updated_at": "2026-04-14T10:05:00Z",`,
		`  "task_state": {`,
		`    "goal": "",`,
		`    "progress": [],`,
		`    "open_items": [],`,
		`    "next_step": "",`,
		`    "blockers": [],`,
		`    "key_artifacts": [],`,
		`    "decisions": [],`,
		`    "user_constraints": [],`,
		`    "last_updated_at": "2026-04-14T10:05:00Z"`,
		`  },`,
		`  "todos": [`,
		`    {`,
		`      "id": "todo-1",`,
		`      "content": "broken todo",`,
		`      "status": "paused",`,
		`      "created_at": "2026-04-14T10:00:00Z",`,
		`      "updated_at": "2026-04-14T10:05:00Z"`,
		`    }`,
		`  ],`,
		`  "messages": []`,
		`}`,
	}, "\n"))

	_, err := store.Load(context.Background(), "invalid-todos-load")
	if err == nil || !strings.Contains(err.Error(), `invalid todo status "paused"`) {
		t.Fatalf("expected invalid todo status load error, got %v", err)
	}
}

func buildQuotedRepeatedWithIndex(ch string, itemLen int, count int) string {
	items := make([]string, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, `"`+strings.Repeat(ch, itemLen-4)+buildIndexedSuffix(i)+`"`)
	}
	return strings.Join(items, ",")
}

func buildIndexedSuffix(index int) string {
	chars := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	hi := chars[(index/len(chars))%len(chars)]
	lo := chars[index%len(chars)]
	return string([]rune{hi, lo, 'x', 'x'})
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

func sessionFilePathForTest(baseDir string, workspaceRoot string, sessionID string) string {
	return filepath.Join(sessionDirectory(baseDir, workspaceRoot), sessionID, sessionFileName)
}
