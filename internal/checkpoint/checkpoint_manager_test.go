package checkpoint

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

type checkpointStoreFixture struct {
	sessionStore    *session.SQLiteStore
	checkpointStore *SQLiteCheckpointStore
	baseDir         string
	workspaceRoot   string
}

func newCheckpointStoreFixture(t *testing.T) checkpointStoreFixture {
	t.Helper()

	baseDir := t.TempDir()
	workspaceRoot := t.TempDir()

	sessionStore := session.NewSQLiteStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})

	checkpointStore := NewSQLiteCheckpointStore(session.DatabasePath(baseDir, workspaceRoot))
	t.Cleanup(func() {
		_ = checkpointStore.Close()
	})

	return checkpointStoreFixture{
		sessionStore:    sessionStore,
		checkpointStore: checkpointStore,
		baseDir:         baseDir,
		workspaceRoot:   workspaceRoot,
	}
}

func createCheckpointTestSession(t *testing.T, store *session.SQLiteStore, id string, workdir string) session.Session {
	t.Helper()

	created, err := store.CreateSession(context.Background(), session.CreateSessionInput{
		ID:    id,
		Title: "checkpoint test",
		Head: session.SessionHead{
			Provider: "openai",
			Model:    "gpt-test",
			Workdir:  workdir,
			TaskState: session.TaskState{
				Goal:                "before restore",
				VerificationProfile: session.VerificationProfileTaskOnly,
			},
			Todos: []session.TodoItem{
				{ID: "todo-1", Content: "before restore"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleUser,
			Parts: []providertypes.ContentPart{
				providertypes.NewTextPart("before restore"),
			},
		},
		{
			Role: providertypes.RoleAssistant,
			Parts: []providertypes.ContentPart{
				providertypes.NewTextPart("tool planned"),
			},
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: `{"cmd":"pwd"}`},
			},
			ToolMetadata: map[string]string{"source": "test"},
		},
	}
	if err := store.AppendMessages(context.Background(), session.AppendMessagesInput{
		SessionID: created.ID,
		Messages:  messages,
		UpdatedAt: time.Now(),
		Provider:  "openai",
		Model:     "gpt-test",
		Workdir:   workdir,
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	return loaded
}

func checkpointInputFromSession(t *testing.T, loaded session.Session, checkpointID string, reason session.CheckpointReason, createdAt time.Time) CreateCheckpointInput {
	t.Helper()

	headJSON, err := json.Marshal(loaded.HeadSnapshot())
	if err != nil {
		t.Fatalf("Marshal(head) error = %v", err)
	}
	messagesJSON, err := json.Marshal(loaded.Messages)
	if err != nil {
		t.Fatalf("Marshal(messages) error = %v", err)
	}

	return CreateCheckpointInput{
		Record: session.CheckpointRecord{
			CheckpointID:      checkpointID,
			WorkspaceKey:      session.WorkspacePathKey(loaded.Workdir),
			SessionID:         loaded.ID,
			RunID:             "run-" + checkpointID,
			Workdir:           loaded.Workdir,
			CreatedAt:         createdAt,
			Reason:            reason,
			CodeCheckpointRef: RefForPerEditCheckpoint(checkpointID),
			Restorable:        true,
			Status:            session.CheckpointStatusCreating,
		},
		SessionCP: session.SessionCheckpoint{
			ID:           "sc-" + checkpointID,
			SessionID:    loaded.ID,
			HeadJSON:     string(headJSON),
			MessagesJSON: string(messagesJSON),
			CreatedAt:    createdAt,
		},
	}
}

func TestSQLiteCheckpointStoreCreateRestoreAndResume(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_restore", fixture.workspaceRoot)
	checkpointCreatedAt := time.Now().Add(-time.Minute)

	input := checkpointInputFromSession(t, loaded, "cp-restore", session.CheckpointReasonPreWrite, checkpointCreatedAt)
	saved, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateCheckpoint() error = %v", err)
	}
	if saved.SessionCheckpointRef == "" || saved.Status != session.CheckpointStatusAvailable {
		t.Fatalf("CreateCheckpoint() = %#v, want available checkpoint with session ref", saved)
	}

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), loaded.ID, ListCheckpointOpts{
		Limit:          10,
		RestorableOnly: true,
	})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 || records[0].CheckpointID != saved.CheckpointID {
		t.Fatalf("ListCheckpoints() = %#v, want only %q", records, saved.CheckpointID)
	}

	record, sessionCP, err := fixture.checkpointStore.GetCheckpoint(context.Background(), saved.CheckpointID)
	if err != nil {
		t.Fatalf("GetCheckpoint() error = %v", err)
	}
	if record.CheckpointID != saved.CheckpointID || sessionCP == nil || sessionCP.ID != saved.SessionCheckpointRef {
		t.Fatalf("GetCheckpoint() = (%#v, %#v), want saved record and session snapshot", record, sessionCP)
	}

	if err := fixture.sessionStore.UpdateSessionState(context.Background(), session.UpdateSessionStateInput{
		SessionID: loaded.ID,
		UpdatedAt: time.Now(),
		Title:     "mutated",
		Head: session.SessionHead{
			Provider: "openai",
			Model:    "gpt-test",
			Workdir:  loaded.Workdir,
			TaskState: session.TaskState{
				Goal:                "after restore",
				VerificationProfile: session.VerificationProfileTaskOnly,
			},
			Todos: []session.TodoItem{
				{ID: "todo-2", Content: "after restore"},
			},
		},
	}); err != nil {
		t.Fatalf("UpdateSessionState() error = %v", err)
	}
	if err := fixture.sessionStore.AppendMessages(context.Background(), session.AppendMessagesInput{
		SessionID: loaded.ID,
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleAssistant,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("after restore"),
				},
			},
		},
		UpdatedAt: time.Now(),
		Provider:  "openai",
		Model:     "gpt-test",
		Workdir:   loaded.Workdir,
	}); err != nil {
		t.Fatalf("AppendMessages(after) error = %v", err)
	}

	if err := fixture.checkpointStore.RestoreCheckpoint(context.Background(), RestoreCheckpointInput{
		SessionID: loaded.ID,
		Head:      loaded.HeadSnapshot(),
		Messages:  loaded.Messages,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RestoreCheckpoint() error = %v", err)
	}

	restored, err := fixture.sessionStore.LoadSession(context.Background(), loaded.ID)
	if err != nil {
		t.Fatalf("LoadSession(restored) error = %v", err)
	}
	if restored.TaskState.Goal != loaded.TaskState.Goal {
		t.Fatalf("restored goal = %q, want %q", restored.TaskState.Goal, loaded.TaskState.Goal)
	}
	if len(restored.Messages) != len(loaded.Messages) {
		t.Fatalf("restored message count = %d, want %d", len(restored.Messages), len(loaded.Messages))
	}
	if restored.Messages[1].ToolMetadata["source"] != "test" {
		t.Fatalf("restored tool metadata = %#v, want preserved metadata", restored.Messages[1].ToolMetadata)
	}

	if err := fixture.checkpointStore.UpdateCheckpointStatus(context.Background(), saved.CheckpointID, session.CheckpointStatusRestored); err != nil {
		t.Fatalf("UpdateCheckpointStatus() error = %v", err)
	}
	filtered, err := fixture.checkpointStore.ListCheckpoints(context.Background(), loaded.ID, ListCheckpointOpts{
		RestorableOnly: true,
	})
	if err != nil {
		t.Fatalf("ListCheckpoints(filtered) error = %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected no restorable checkpoints after status change, got %#v", filtered)
	}

	firstResume := session.ResumeCheckpoint{
		ID:                 "rc-1",
		WorkspaceKey:       session.WorkspacePathKey(loaded.Workdir),
		RunID:              "run-1",
		SessionID:          loaded.ID,
		Turn:               1,
		Phase:              "plan",
		CompletionState:    "running",
		TranscriptRevision: 3,
		UpdatedAt:          time.Now().Add(-time.Minute),
	}
	secondResume := firstResume
	secondResume.ID = "rc-2"
	secondResume.RunID = "run-2"
	secondResume.Turn = 2
	secondResume.Phase = "execute"
	secondResume.UpdatedAt = time.Now()

	if err := fixture.checkpointStore.SetResumeCheckpoint(context.Background(), firstResume); err != nil {
		t.Fatalf("SetResumeCheckpoint(first) error = %v", err)
	}
	if err := fixture.checkpointStore.SetResumeCheckpoint(context.Background(), secondResume); err != nil {
		t.Fatalf("SetResumeCheckpoint(second) error = %v", err)
	}
	gotResume, err := fixture.checkpointStore.GetLatestResumeCheckpoint(context.Background(), loaded.ID)
	if err != nil {
		t.Fatalf("GetLatestResumeCheckpoint() error = %v", err)
	}
	if gotResume == nil || gotResume.ID != secondResume.ID || gotResume.Turn != secondResume.Turn {
		t.Fatalf("GetLatestResumeCheckpoint() = %#v, want %#v", gotResume, secondResume)
	}
}

func TestSQLiteCheckpointStoreRestoreCheckpointUpdatesStatuses(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_restore_status", fixture.workspaceRoot)

	recordA, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), checkpointInputFromSession(
		t, loaded, "cp-available", session.CheckpointReasonPreWrite, time.Now().Add(-2*time.Minute),
	))
	if err != nil {
		t.Fatalf("CreateCheckpoint(cp-available) error = %v", err)
	}
	recordB, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), checkpointInputFromSession(
		t, loaded, "cp-restored", session.CheckpointReasonEndOfTurn, time.Now().Add(-time.Minute),
	))
	if err != nil {
		t.Fatalf("CreateCheckpoint(cp-restored) error = %v", err)
	}

	if err := fixture.checkpointStore.RestoreCheckpoint(context.Background(), RestoreCheckpointInput{
		SessionID:        loaded.ID,
		Head:             loaded.HeadSnapshot(),
		Messages:         loaded.Messages,
		UpdatedAt:        time.Now(),
		MarkAvailableIDs: []string{recordA.CheckpointID},
		MarkRestoredIDs:  []string{recordB.CheckpointID},
	}); err != nil {
		t.Fatalf("RestoreCheckpoint() error = %v", err)
	}

	availableRecord, _, err := fixture.checkpointStore.GetCheckpoint(context.Background(), recordA.CheckpointID)
	if err != nil {
		t.Fatalf("GetCheckpoint(cp-available) error = %v", err)
	}
	if availableRecord.Status != session.CheckpointStatusAvailable {
		t.Fatalf("available record status = %q, want available", availableRecord.Status)
	}

	restoredRecord, _, err := fixture.checkpointStore.GetCheckpoint(context.Background(), recordB.CheckpointID)
	if err != nil {
		t.Fatalf("GetCheckpoint(cp-restored) error = %v", err)
	}
	if restoredRecord.Status != session.CheckpointStatusRestored {
		t.Fatalf("restored record status = %q, want restored", restoredRecord.Status)
	}
}

func TestSQLiteCheckpointStoreGetCheckpointWithoutSessionSnapshot(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_record_only", fixture.workspaceRoot)
	db, err := fixture.checkpointStore.ensureDB(context.Background())
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}

	if _, err := db.ExecContext(context.Background(), `
INSERT INTO checkpoint_records (
	id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		"cp-record-only",
		session.WorkspacePathKey(loaded.Workdir),
		loaded.ID,
		"run-record-only",
		loaded.Workdir,
		time.Now().UnixMilli(),
		string(session.CheckpointReasonPreWrite),
		"",
		"",
		"",
		0,
		1,
		string(session.CheckpointStatusAvailable),
	); err != nil {
		t.Fatalf("insert checkpoint record error = %v", err)
	}

	record, sessionCP, err := fixture.checkpointStore.GetCheckpoint(context.Background(), "cp-record-only")
	if err != nil {
		t.Fatalf("GetCheckpoint() error = %v", err)
	}
	if record.CheckpointID != "cp-record-only" {
		t.Fatalf("record id = %q, want cp-record-only", record.CheckpointID)
	}
	if sessionCP != nil {
		t.Fatalf("session checkpoint = %#v, want nil", sessionCP)
	}
}

func TestSQLiteCheckpointStorePruneAndRepair(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_prune", fixture.workspaceRoot)

	createdAt := time.Now().Add(-10 * time.Minute)
	for i := 0; i < 4; i++ {
		checkpointID := "cp-auto-" + string(rune('a'+i))
		input := checkpointInputFromSession(t, loaded, checkpointID, session.CheckpointReasonPreWrite, createdAt.Add(time.Duration(i)*time.Minute))
		if _, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), input); err != nil {
			t.Fatalf("CreateCheckpoint(%s) error = %v", checkpointID, err)
		}
	}
	if _, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), checkpointInputFromSession(t, loaded, "cp-manual", session.CheckpointReasonManual, time.Now())); err != nil {
		t.Fatalf("CreateCheckpoint(manual) error = %v", err)
	}
	if _, err := fixture.checkpointStore.CreateCheckpoint(context.Background(), checkpointInputFromSession(t, loaded, "cp-guard", session.CheckpointReasonGuard, time.Now().Add(time.Minute))); err != nil {
		t.Fatalf("CreateCheckpoint(guard) error = %v", err)
	}

	pruned, err := fixture.checkpointStore.PruneExpiredCheckpoints(context.Background(), loaded.ID, 2)
	if err != nil {
		t.Fatalf("PruneExpiredCheckpoints() error = %v", err)
	}
	if pruned != 2 {
		t.Fatalf("PruneExpiredCheckpoints() = %d, want 2", pruned)
	}

	prunedRecord, prunedSessionCP, err := fixture.checkpointStore.GetCheckpoint(context.Background(), "cp-auto-a")
	if err != nil {
		t.Fatalf("GetCheckpoint(pruned) error = %v", err)
	}
	if prunedRecord.Status != session.CheckpointStatusPruned || prunedRecord.Restorable {
		t.Fatalf("pruned record = %#v, want pruned and not restorable", prunedRecord)
	}
	if prunedSessionCP != nil {
		t.Fatalf("expected pruned session snapshot to be deleted, got %#v", prunedSessionCP)
	}

	manualRecord, _, err := fixture.checkpointStore.GetCheckpoint(context.Background(), "cp-manual")
	if err != nil {
		t.Fatalf("GetCheckpoint(manual) error = %v", err)
	}
	if manualRecord.Status != session.CheckpointStatusAvailable || !manualRecord.Restorable {
		t.Fatalf("manual record = %#v, want still available", manualRecord)
	}

	db, err := fixture.checkpointStore.ensureDB(context.Background())
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	withSessionCPID := "cp-creating-with-session"
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO session_checkpoints (id, session_id, head_json, messages_json, created_at_ms)
VALUES (?, ?, ?, ?, ?)
`, "sc-creating", loaded.ID, `{}`, `[]`, time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert session_checkpoint error = %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO checkpoint_records (
	id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		withSessionCPID,
		session.WorkspacePathKey(loaded.Workdir),
		loaded.ID,
		"run-repair",
		loaded.Workdir,
		time.Now().UnixMilli(),
		string(session.CheckpointReasonPreWrite),
		"",
		"sc-creating",
		"",
		0,
		1,
		string(session.CheckpointStatusCreating),
	); err != nil {
		t.Fatalf("insert creating checkpoint with session ref error = %v", err)
	}

	orphanID := "cp-creating-orphan"
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO checkpoint_records (
	id, workspace_key, session_id, run_id, workdir, created_at_ms,
	reason, code_checkpoint_ref, session_checkpoint_ref, resume_checkpoint_ref,
	transcript_revision, restorable, status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		orphanID,
		session.WorkspacePathKey(loaded.Workdir),
		loaded.ID,
		"run-repair",
		loaded.Workdir,
		time.Now().UnixMilli(),
		string(session.CheckpointReasonPreWrite),
		"",
		"",
		"",
		0,
		1,
		string(session.CheckpointStatusCreating),
	); err != nil {
		t.Fatalf("insert orphan checkpoint error = %v", err)
	}

	repaired, err := fixture.checkpointStore.RepairCreatingCheckpoints(context.Background())
	if err != nil {
		t.Fatalf("RepairCreatingCheckpoints() error = %v", err)
	}
	if repaired != 2 {
		t.Fatalf("RepairCreatingCheckpoints() = %d, want 2", repaired)
	}

	repairedRecord, _, err := fixture.checkpointStore.GetCheckpoint(context.Background(), withSessionCPID)
	if err != nil {
		t.Fatalf("GetCheckpoint(repaired) error = %v", err)
	}
	if repairedRecord.Status != session.CheckpointStatusAvailable {
		t.Fatalf("repaired record status = %q, want available", repairedRecord.Status)
	}

	if _, _, err := fixture.checkpointStore.GetCheckpoint(context.Background(), orphanID); err == nil {
		t.Fatalf("expected orphan creating checkpoint to be deleted")
	}
}

func TestSQLiteCheckpointStoreUsesSessionDatabasePath(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	expected := filepath.Clean(session.DatabasePath(fixture.baseDir, fixture.workspaceRoot))
	if filepath.Clean(fixture.checkpointStore.dbPath) != expected {
		t.Fatalf("dbPath = %q, want %q", fixture.checkpointStore.dbPath, expected)
	}
}

func TestSQLiteCheckpointStoreSharedDBAndHelpers(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_shared_db", fixture.workspaceRoot)
	db, err := fixture.checkpointStore.ensureDB(context.Background())
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}

	shared := NewSQLiteCheckpointStoreWithDB(db)
	if shared.ownsDB {
		t.Fatal("shared checkpoint store should not own injected db")
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("Close(shared) error = %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("db should remain open after shared Close(), got %v", err)
	}
	if _, err := shared.ListCheckpoints(context.Background(), loaded.ID, ListCheckpointOpts{}); err != nil {
		t.Fatalf("shared ListCheckpoints() error = %v", err)
	}

	if got := marshalPlanField(nil); got != "" {
		t.Fatalf("marshalPlanField(nil) = %q, want empty", got)
	}
	var nilPlan *session.PlanArtifact
	if got := marshalPlanField(nilPlan); got != "" {
		t.Fatalf("marshalPlanField(nil pointer) = %q, want empty", got)
	}
	if got := marshalPlanField(map[string]any{"step": "verify"}); !strings.Contains(got, `"step":"verify"`) {
		t.Fatalf("marshalPlanField(map) = %q", got)
	}
	if got := marshalPlanField(func() {}); got != "" {
		t.Fatalf("marshalPlanField(unmarshalable) = %q, want empty", got)
	}
	if got := marshalHeadField(func() {}); got != "null" {
		t.Fatalf("marshalHeadField(unmarshalable) = %q, want null", got)
	}
}

func TestSQLiteCheckpointStoreErrorsAndEmptyResults(t *testing.T) {
	t.Parallel()

	fixture := newCheckpointStoreFixture(t)
	loaded := createCheckpointTestSession(t, fixture.sessionStore, "session_empty_resume", fixture.workspaceRoot)
	if err := fixture.checkpointStore.UpdateCheckpointStatus(context.Background(), "missing", session.CheckpointStatusAvailable); err == nil {
		t.Fatal("expected UpdateCheckpointStatus() to fail for missing checkpoint")
	}

	rc, err := fixture.checkpointStore.GetLatestResumeCheckpoint(context.Background(), loaded.ID)
	if err != nil {
		t.Fatalf("GetLatestResumeCheckpoint(missing) error = %v", err)
	}
	if rc != nil {
		t.Fatalf("GetLatestResumeCheckpoint(missing) = %#v, want nil", rc)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.checkpointStore.ensureDB(context.Background()); err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	if _, err := fixture.checkpointStore.CreateCheckpoint(ctx, CreateCheckpointInput{}); err == nil {
		t.Fatal("expected CreateCheckpoint() to honor canceled context")
	}
}

func TestNewSQLiteCheckpointStoreWithNilDBClose(t *testing.T) {
	t.Parallel()

	store := NewSQLiteCheckpointStoreWithDB((*sql.DB)(nil))
	if err := store.Close(); err != nil {
		t.Fatalf("Close(nil db) error = %v", err)
	}
}
