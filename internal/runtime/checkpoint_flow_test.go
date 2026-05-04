package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/checkpoint"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

type checkpointStoreSpy struct {
	lastResume    agentsession.ResumeCheckpoint
	listRecords   []agentsession.CheckpointRecord
	listSessionID string
	listOpts      checkpoint.ListCheckpointOpts
	listErr       error
	getRecord     agentsession.CheckpointRecord
	getSessionCP  *agentsession.SessionCheckpoint
	getErr        error
}

func (s *checkpointStoreSpy) CreateCheckpoint(_ context.Context, in checkpoint.CreateCheckpointInput) (agentsession.CheckpointRecord, error) {
	return in.Record, nil
}

func (s *checkpointStoreSpy) ListCheckpoints(_ context.Context, sessionID string, opts checkpoint.ListCheckpointOpts) ([]agentsession.CheckpointRecord, error) {
	s.listSessionID = sessionID
	s.listOpts = opts
	return s.listRecords, s.listErr
}

func (s *checkpointStoreSpy) GetCheckpoint(context.Context, string) (agentsession.CheckpointRecord, *agentsession.SessionCheckpoint, error) {
	return s.getRecord, s.getSessionCP, s.getErr
}

func (s *checkpointStoreSpy) UpdateCheckpointStatus(context.Context, string, agentsession.CheckpointStatus) error {
	return nil
}

func (s *checkpointStoreSpy) GetLatestResumeCheckpoint(context.Context, string) (*agentsession.ResumeCheckpoint, error) {
	return nil, nil
}

func (s *checkpointStoreSpy) RestoreCheckpoint(context.Context, checkpoint.RestoreCheckpointInput) error {
	return nil
}

func (s *checkpointStoreSpy) SetResumeCheckpoint(_ context.Context, rc agentsession.ResumeCheckpoint) error {
	s.lastResume = rc
	return nil
}

func (s *checkpointStoreSpy) PruneExpiredCheckpoints(context.Context, string, int) (int, error) {
	return 0, nil
}

func (s *checkpointStoreSpy) RepairCreatingCheckpoints(context.Context) (int, error) {
	return 0, nil
}

type runtimeCheckpointFixture struct {
	service         *Service
	sessionStore    *agentsession.SQLiteStore
	checkpointStore *checkpoint.SQLiteCheckpointStore
	perEditStore    *checkpoint.PerEditSnapshotStore
	workdir         string
	projectDir      string
	session         agentsession.Session
}

func newRuntimeCheckpointFixture(t *testing.T) runtimeCheckpointFixture {
	t.Helper()

	baseDir := t.TempDir()
	workdir := t.TempDir()
	projectDir := t.TempDir()

	sessionStore := agentsession.NewSQLiteStore(baseDir, workdir)
	t.Cleanup(func() { _ = sessionStore.Close() })

	checkpointStore := checkpoint.NewSQLiteCheckpointStore(agentsession.DatabasePath(baseDir, workdir))
	t.Cleanup(func() { _ = checkpointStore.Close() })

	perEditStore := checkpoint.NewPerEditSnapshotStore(projectDir, workdir)

	created, err := sessionStore.CreateSession(context.Background(), agentsession.CreateSessionInput{
		ID:    "runtime-checkpoint-session",
		Title: "runtime checkpoint",
		Head: agentsession.SessionHead{
			Provider: "openai",
			Model:    "gpt-test",
			Workdir:  workdir,
			TaskState: agentsession.TaskState{
				Goal:                "initial goal",
				VerificationProfile: agentsession.VerificationProfileTaskOnly,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := sessionStore.AppendMessages(context.Background(), agentsession.AppendMessagesInput{
		SessionID: created.ID,
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleUser,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("before restore"),
				},
			},
		},
		UpdatedAt: time.Now(),
		Provider:  "openai",
		Model:     "gpt-test",
		Workdir:   workdir,
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	loaded, err := sessionStore.LoadSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}

	return runtimeCheckpointFixture{
		service: &Service{
			sessionStore:    sessionStore,
			checkpointStore: checkpointStore,
			perEditStore:    perEditStore,
			events:          make(chan RuntimeEvent, 32),
		},
		sessionStore:    sessionStore,
		checkpointStore: checkpointStore,
		perEditStore:    perEditStore,
		workdir:         workdir,
		projectDir:      projectDir,
		session:         loaded,
	}
}

// captureFile is a test helper that drops a file at workdir-relative path and asks
// the per-edit store to capture its current content as a pending pre-write version.
func (f runtimeCheckpointFixture) captureFile(t *testing.T, relPath string, content []byte) string {
	t.Helper()
	abs := filepath.Join(f.workdir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := f.perEditStore.CapturePreWrite(abs); err != nil {
		t.Fatalf("CapturePreWrite() error = %v", err)
	}
	return abs
}

func TestCreateStartOfTurnCheckpoint_PendingWrite(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	fixture.captureFile(t, "main.go", []byte("package main\nconst v = 1\n"))

	state := newRunState("run-pending", fixture.session)
	if err := fixture.service.createStartOfTurnCheckpoint(context.Background(), &state); err != nil {
		t.Fatalf("createStartOfTurnCheckpoint() error = %v", err)
	}

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records count = %d, want 1: %#v", len(records), records)
	}
	if records[0].Reason != agentsession.CheckpointReasonPreWrite {
		t.Fatalf("reason = %s, want pre_write", records[0].Reason)
	}
	if !checkpoint.IsPerEditRef(records[0].CodeCheckpointRef) {
		t.Fatalf("code ref = %q, want peredit ref", records[0].CodeCheckpointRef)
	}
}

func TestCreateStartOfTurnCheckpoint_NoPending_SessionOnly(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)

	state := newRunState("run-empty", fixture.session)
	if err := fixture.service.createStartOfTurnCheckpoint(context.Background(), &state); err != nil {
		t.Fatalf("createStartOfTurnCheckpoint() error = %v", err)
	}

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one session-only checkpoint", records)
	}
	if records[0].Reason != agentsession.CheckpointReasonPreWrite {
		t.Fatalf("reason = %s, want pre_write", records[0].Reason)
	}
	if records[0].CodeCheckpointRef != "" {
		t.Fatalf("code ref = %q, want empty (session-only)", records[0].CodeCheckpointRef)
	}
}

func TestCreateEndOfTurnCheckpoint_NoWriteSkipped(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	fixture.captureFile(t, "main.go", []byte("package main\n"))

	state := newRunState("run-no-write", fixture.session)
	fixture.service.createEndOfTurnCheckpoint(context.Background(), &state, false)

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want no checkpoint when hasWorkspaceWrite=false", records)
	}
}

func TestCreateEndOfTurnCheckpoint_PerEditSkipsEmpty(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)

	state := newRunState("run-empty", fixture.session)
	fixture.service.createEndOfTurnCheckpoint(context.Background(), &state, true)

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want no checkpoint when no pending writes captured", records)
	}
}

func TestCreateEndOfTurnCheckpoint_WithPending(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	fixture.captureFile(t, "lib.go", []byte("package lib\n"))

	state := newRunState("run-eot", fixture.session)
	fixture.service.createEndOfTurnCheckpoint(context.Background(), &state, true)

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want 1 end-of-turn checkpoint", records)
	}
	if records[0].Reason != agentsession.CheckpointReasonEndOfTurn {
		t.Fatalf("reason = %s, want end_of_turn", records[0].Reason)
	}
	if !checkpoint.IsPerEditRef(records[0].CodeCheckpointRef) {
		t.Fatalf("code ref = %q, want peredit ref", records[0].CodeCheckpointRef)
	}
}

func TestCreateCompactCheckpoint(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.workdir, "compact.txt"), []byte("compact"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	fixture.service.createCompactCheckpoint(context.Background(), "run-compact", fixture.session)

	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 || records[0].Reason != agentsession.CheckpointReasonCompact {
		t.Fatalf("records = %#v, want compact checkpoint", records)
	}
}

func TestUpdateResumeCheckpoint(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	state := newRunState("run-resume", fixture.session)
	state.turn = 3
	spy := &checkpointStoreSpy{}
	service := &Service{checkpointStore: spy}
	service.updateResumeCheckpoint(context.Background(), &state, "verify", "running")

	if spy.lastResume.SessionID != fixture.session.ID || spy.lastResume.RunID != "run-resume" || spy.lastResume.Turn != 3 || spy.lastResume.Phase != "verify" {
		t.Fatalf("SetResumeCheckpoint() captured %#v", spy.lastResume)
	}
}

func TestRuntimeCheckpointFacadeMethods(t *testing.T) {
	t.Run("list checkpoints delegates to store", func(t *testing.T) {
		spy := &checkpointStoreSpy{
			listRecords: []agentsession.CheckpointRecord{{CheckpointID: "cp-1"}},
		}
		service := &Service{checkpointStore: spy}

		records, err := service.ListCheckpoints(context.Background(), "session-1", checkpoint.ListCheckpointOpts{
			Limit:          5,
			RestorableOnly: true,
		})
		if err != nil {
			t.Fatalf("ListCheckpoints() error = %v", err)
		}
		if spy.listSessionID != "session-1" || spy.listOpts.Limit != 5 || !spy.listOpts.RestorableOnly {
			t.Fatalf("spy captured session=%q opts=%#v", spy.listSessionID, spy.listOpts)
		}
		if len(records) != 1 || records[0].CheckpointID != "cp-1" {
			t.Fatalf("records = %#v", records)
		}
	})

	t.Run("list checkpoints reports unavailable store", func(t *testing.T) {
		service := &Service{}
		if _, err := service.ListCheckpoints(context.Background(), "session-1", checkpoint.ListCheckpointOpts{}); err == nil {
			t.Fatal("expected error when checkpoint store is unavailable")
		}
	})

	t.Run("set checkpoint dependencies stores references", func(t *testing.T) {
		service := &Service{}
		store := &checkpointStoreSpy{}
		perEdit := checkpoint.NewPerEditSnapshotStore(t.TempDir(), t.TempDir())

		service.SetCheckpointDependencies(store, perEdit)
		if service.checkpointStore != store || service.perEditStore != perEdit {
			t.Fatalf("service checkpoint dependencies not set correctly")
		}
	})

	t.Run("update runtime session after restore invalidates cache", func(t *testing.T) {
		service := &Service{
			runtimeSnapshots: map[string]RuntimeSnapshot{
				"session-1": {SessionID: "session-1", Phase: "execute"},
			},
		}
		service.updateRuntimeSessionAfterRestore("session-1", agentsession.SessionHead{}, nil)
		if _, ok := service.runtimeSnapshots["session-1"]; ok {
			t.Fatal("expected cached snapshot to be deleted after restore")
		}
	})
}

func TestRestoreCheckpoint_RecoversCapturedFile(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	target := filepath.Join(fixture.workdir, "restore.txt")
	if err := os.WriteFile(target, []byte("version one"), 0o644); err != nil {
		t.Fatalf("WriteFile(version one) error = %v", err)
	}
	if _, err := fixture.perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite() error = %v", err)
	}

	state := newRunState("run-restore", fixture.session)
	if err := fixture.service.createStartOfTurnCheckpoint(context.Background(), &state); err != nil {
		t.Fatalf("createStartOfTurnCheckpoint() error = %v", err)
	}
	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want 1", records)
	}
	cpRecord := records[0]

	// mark checkpoint available so RestoreCheckpoint accepts it
	if err := fixture.checkpointStore.UpdateCheckpointStatus(context.Background(), cpRecord.CheckpointID, agentsession.CheckpointStatusAvailable); err != nil {
		t.Fatalf("UpdateCheckpointStatus() error = %v", err)
	}

	// agent rewrites the file (capture v2 mid-flight)
	if _, err := fixture.perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite(v2) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("version two"), 0o644); err != nil {
		t.Fatalf("WriteFile(version two) error = %v", err)
	}

	if _, err := fixture.service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
		SessionID:    fixture.session.ID,
		CheckpointID: cpRecord.CheckpointID,
	}); err != nil {
		t.Fatalf("RestoreCheckpoint() error = %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "version one" {
		t.Fatalf("restored content = %q, want %q", string(got), "version one")
	}
}

func TestUndoRestoreCheckpoint_RestoresGuardState(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	target := filepath.Join(fixture.workdir, "undo.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatalf("WriteFile(before) error = %v", err)
	}
	if _, err := fixture.perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite(before) error = %v", err)
	}

	state := newRunState("run-undo", fixture.session)
	if err := fixture.service.createStartOfTurnCheckpoint(context.Background(), &state); err != nil {
		t.Fatalf("createStartOfTurnCheckpoint() error = %v", err)
	}
	records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
	if err != nil {
		t.Fatalf("ListCheckpoints() error = %v", err)
	}
	cpRecord := records[0]
	if err := fixture.checkpointStore.UpdateCheckpointStatus(context.Background(), cpRecord.CheckpointID, agentsession.CheckpointStatusAvailable); err != nil {
		t.Fatalf("UpdateCheckpointStatus() error = %v", err)
	}

	if _, err := fixture.perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite(guard) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("after"), 0o644); err != nil {
		t.Fatalf("WriteFile(after) error = %v", err)
	}

	if _, err := fixture.service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
		SessionID:    fixture.session.ID,
		CheckpointID: cpRecord.CheckpointID,
	}); err != nil {
		t.Fatalf("RestoreCheckpoint() error = %v", err)
	}
	if got := string(mustReadRuntimeFile(t, target)); got != "before" {
		t.Fatalf("restored content = %q, want before", got)
	}

	if _, err := fixture.service.UndoRestoreCheckpoint(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("UndoRestoreCheckpoint() error = %v", err)
	}
	if got := string(mustReadRuntimeFile(t, target)); got != "before" {
		t.Fatalf("undo content = %q, want before", got)
	}

	seenUndo := false
	for {
		select {
		case evt := <-fixture.service.events:
			if evt.Type == EventCheckpointUndoRestore {
				payload, ok := evt.Payload.(CheckpointUndoRestorePayload)
				if !ok {
					t.Fatalf("undo payload type = %T", evt.Payload)
				}
				if payload.SessionID != fixture.session.ID {
					t.Fatalf("undo payload session = %q, want %q", payload.SessionID, fixture.session.ID)
				}
				seenUndo = true
			}
		default:
			if !seenUndo {
				t.Fatal("expected checkpoint undo event")
			}
			return
		}
	}
}

func TestCheckpointDiff_ReturnsPatchAndClassifiedFiles(t *testing.T) {
	fixture := newRuntimeCheckpointFixture(t)
	ctx := context.Background()

	alpha := filepath.Join(fixture.workdir, "alpha.txt")
	if err := os.WriteFile(alpha, []byte("zero\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alpha) error = %v", err)
	}

	if _, err := fixture.perEditStore.CapturePreWrite(alpha); err != nil {
		t.Fatalf("CapturePreWrite(alpha cp1) error = %v", err)
	}
	if err := os.WriteFile(alpha, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alpha one) error = %v", err)
	}
	if _, err := fixture.perEditStore.Finalize("cp1"); err != nil {
		t.Fatalf("Finalize(cp1) error = %v", err)
	}
	fixture.perEditStore.Reset()

	if _, err := fixture.perEditStore.CapturePreWrite(alpha); err != nil {
		t.Fatalf("CapturePreWrite(alpha cp2) error = %v", err)
	}
	if err := os.WriteFile(alpha, []byte("two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(alpha two) error = %v", err)
	}
	if _, err := fixture.perEditStore.Finalize("cp2"); err != nil {
		t.Fatalf("Finalize(cp2) error = %v", err)
	}
	fixture.perEditStore.Reset()

	for _, cpID := range []string{"cp1", "cp2"} {
		if _, err := fixture.checkpointStore.CreateCheckpoint(ctx, checkpoint.CreateCheckpointInput{
			Record: agentsession.CheckpointRecord{
				CheckpointID:      cpID,
				WorkspaceKey:      agentsession.WorkspacePathKey(fixture.workdir),
				SessionID:         fixture.session.ID,
				RunID:             "run-diff",
				Workdir:           fixture.workdir,
				CreatedAt:         time.Now().UTC(),
				Reason:            agentsession.CheckpointReasonEndOfTurn,
				CodeCheckpointRef: checkpoint.RefForPerEditCheckpoint(cpID),
				Restorable:        true,
				Status:            agentsession.CheckpointStatusAvailable,
			},
			SessionCP: agentsession.SessionCheckpoint{
				ID:           agentsession.NewID("sc"),
				SessionID:    fixture.session.ID,
				HeadJSON:     `{}`,
				MessagesJSON: `[]`,
				CreatedAt:    time.Now().UTC(),
			},
		}); err != nil {
			t.Fatalf("CreateCheckpoint(%s) error = %v", cpID, err)
		}
		time.Sleep(time.Millisecond)
	}

	result, err := fixture.service.CheckpointDiff(ctx, CheckpointDiffInput{
		SessionID:    fixture.session.ID,
		CheckpointID: "cp2",
	})
	if err != nil {
		t.Fatalf("CheckpointDiff() error = %v", err)
	}
	if result.CheckpointID != "cp2" || result.PrevCheckpointID != "cp1" {
		t.Fatalf("unexpected checkpoint ids: %#v", result)
	}
	if !strings.Contains(result.Patch, "alpha.txt") {
		t.Fatalf("patch should mention changed files, got:\n%s", result.Patch)
	}
	if len(result.Files.Modified) != 1 || result.Files.Modified[0] != "alpha.txt" {
		t.Fatalf("modified files = %#v, want alpha.txt", result.Files.Modified)
	}
	if len(result.Files.Added) != 0 {
		t.Fatalf("added files = %#v, want empty", result.Files.Added)
	}
	if len(result.Files.Deleted) != 0 {
		t.Fatalf("deleted files = %#v, want empty", result.Files.Deleted)
	}
}

func TestRestoreCheckpointRejectsInvalidRequestAndMismatchedSession(t *testing.T) {
	service := &Service{}
	if _, err := service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{}); err == nil {
		t.Fatal("expected error when checkpoint store is unavailable")
	}

	service = &Service{
		checkpointStore: &checkpointStoreSpy{},
		perEditStore:    checkpoint.NewPerEditSnapshotStore(t.TempDir(), t.TempDir()),
	}
	if _, err := service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{}); err == nil {
		t.Fatal("expected validation error for empty identifiers")
	}

	service.checkpointStore = &checkpointStoreSpy{
		getRecord: agentsession.CheckpointRecord{
			CheckpointID: "cp-1",
			SessionID:    "other-session",
			Status:       agentsession.CheckpointStatusAvailable,
			Restorable:   true,
		},
		getSessionCP: &agentsession.SessionCheckpoint{
			HeadJSON:     `{}`,
			MessagesJSON: `[]`,
		},
	}
	if _, err := service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
		SessionID:    "session-1",
		CheckpointID: "cp-1",
	}); err == nil || !strings.Contains(err.Error(), "session mismatch") {
		t.Fatalf("RestoreCheckpoint() error = %v, want session mismatch", err)
	}

	for _, tc := range []struct {
		name       string
		record     agentsession.CheckpointRecord
		sessionCP  *agentsession.SessionCheckpoint
		wantSubstr string
	}{
		{
			name: "status must be available",
			record: agentsession.CheckpointRecord{
				CheckpointID: "cp-status",
				SessionID:    "session-1",
				Status:       agentsession.CheckpointStatusRestored,
				Restorable:   true,
			},
			sessionCP:  &agentsession.SessionCheckpoint{HeadJSON: `{}`, MessagesJSON: `[]`},
			wantSubstr: "status is restored",
		},
		{
			name: "checkpoint must be restorable",
			record: agentsession.CheckpointRecord{
				CheckpointID: "cp-restorable",
				SessionID:    "session-1",
				Status:       agentsession.CheckpointStatusAvailable,
				Restorable:   false,
			},
			sessionCP:  &agentsession.SessionCheckpoint{HeadJSON: `{}`, MessagesJSON: `[]`},
			wantSubstr: "not restorable",
		},
		{
			name: "session checkpoint data is required",
			record: agentsession.CheckpointRecord{
				CheckpointID: "cp-session-data",
				SessionID:    "session-1",
				Status:       agentsession.CheckpointStatusAvailable,
				Restorable:   true,
			},
			sessionCP:  nil,
			wantSubstr: "no session checkpoint data",
		},
		{
			name: "head json must be valid",
			record: agentsession.CheckpointRecord{
				CheckpointID: "cp-head-json",
				SessionID:    "session-1",
				Status:       agentsession.CheckpointStatusAvailable,
				Restorable:   true,
			},
			sessionCP:  &agentsession.SessionCheckpoint{HeadJSON: `{invalid`, MessagesJSON: `[]`},
			wantSubstr: "unmarshal head",
		},
		{
			name: "messages json must be valid",
			record: agentsession.CheckpointRecord{
				CheckpointID: "cp-messages-json",
				SessionID:    "session-1",
				Status:       agentsession.CheckpointStatusAvailable,
				Restorable:   true,
			},
			sessionCP:  &agentsession.SessionCheckpoint{HeadJSON: `{}`, MessagesJSON: `{invalid`},
			wantSubstr: "unmarshal messages",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newRuntimeCheckpointFixture(t)
			tc.record.SessionID = fixture.session.ID
			spy := &checkpointStoreSpy{
				getRecord:    tc.record,
				getSessionCP: tc.sessionCP,
			}
			service := &Service{
				sessionStore:    fixture.sessionStore,
				checkpointStore: spy,
				perEditStore:    checkpoint.NewPerEditSnapshotStore(t.TempDir(), fixture.workdir),
				events:          make(chan RuntimeEvent, 8),
			}
			_, err := service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
				SessionID:    fixture.session.ID,
				CheckpointID: tc.record.CheckpointID,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("RestoreCheckpoint() error = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestCheckpointDiffSelectsLatestCodeCheckpointAndRejectsSessionOnlyTarget(t *testing.T) {
	now := time.Now().UTC()
	workdir := t.TempDir()
	projectDir := t.TempDir()
	perEditStore := checkpoint.NewPerEditSnapshotStore(projectDir, workdir)
	target := filepath.Join(workdir, "tracked.txt")
	if err := os.WriteFile(target, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(cp1 base) error = %v", err)
	}
	if _, err := perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite(cp1) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(cp1 next) error = %v", err)
	}
	if _, err := perEditStore.Finalize("cp-1"); err != nil {
		t.Fatalf("Finalize(cp-1) error = %v", err)
	}
	perEditStore.Reset()

	if _, err := perEditStore.CapturePreWrite(target); err != nil {
		t.Fatalf("CapturePreWrite(cp2) error = %v", err)
	}
	if err := os.WriteFile(target, []byte("three\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(cp2 next) error = %v", err)
	}
	if _, err := perEditStore.Finalize("cp-2"); err != nil {
		t.Fatalf("Finalize(cp-2) error = %v", err)
	}
	perEditStore.Reset()

	spy := &checkpointStoreSpy{
		listRecords: []agentsession.CheckpointRecord{
			{
				CheckpointID:      "session-only",
				SessionID:         "session-1",
				CreatedAt:         now.Add(2 * time.Second),
				CodeCheckpointRef: "",
			},
			{
				CheckpointID:      "cp-2",
				SessionID:         "session-1",
				CreatedAt:         now.Add(time.Second),
				CodeCheckpointRef: checkpoint.RefForPerEditCheckpoint("cp-2"),
			},
			{
				CheckpointID:      "cp-1",
				SessionID:         "session-1",
				CreatedAt:         now,
				CodeCheckpointRef: checkpoint.RefForPerEditCheckpoint("cp-1"),
			},
		},
	}
	service := &Service{
		checkpointStore: spy,
		perEditStore:    perEditStore,
	}

	result, err := service.CheckpointDiff(context.Background(), CheckpointDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CheckpointDiff() error = %v", err)
	}
	if result.CheckpointID != "cp-2" || result.PrevCheckpointID != "cp-1" {
		t.Fatalf("CheckpointDiff() = %#v, want latest code checkpoint pair", result)
	}

	if _, err := service.CheckpointDiff(context.Background(), CheckpointDiffInput{
		SessionID:    "session-1",
		CheckpointID: "session-only",
	}); err == nil || !strings.Contains(err.Error(), "not found or has no code snapshot") {
		t.Fatalf("CheckpointDiff() error = %v, want session-only target rejection", err)
	}
}

func TestCheckpointDiffRejectsMissingStateAndReturnsEmptyWhenNoPreviousSnapshot(t *testing.T) {
	service := &Service{}
	if _, err := service.CheckpointDiff(context.Background(), CheckpointDiffInput{}); err == nil {
		t.Fatal("expected store availability error")
	}

	service = &Service{
		checkpointStore: &checkpointStoreSpy{},
		perEditStore:    checkpoint.NewPerEditSnapshotStore(t.TempDir(), t.TempDir()),
	}
	if _, err := service.CheckpointDiff(context.Background(), CheckpointDiffInput{}); err == nil || !strings.Contains(err.Error(), "session_id required") {
		t.Fatalf("CheckpointDiff() error = %v, want session_id validation", err)
	}

	spy := &checkpointStoreSpy{
		listRecords: []agentsession.CheckpointRecord{
			{
				CheckpointID:      "cp-only",
				SessionID:         "session-1",
				CreatedAt:         time.Now().UTC(),
				CodeCheckpointRef: checkpoint.RefForPerEditCheckpoint("cp-only"),
			},
		},
	}
	service = &Service{
		checkpointStore: spy,
		perEditStore:    checkpoint.NewPerEditSnapshotStore(t.TempDir(), t.TempDir()),
	}
	result, err := service.CheckpointDiff(context.Background(), CheckpointDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CheckpointDiff() error = %v", err)
	}
	if result.CheckpointID != "cp-only" || result.PrevCheckpointID != "" || result.Patch != "" {
		t.Fatalf("CheckpointDiff() = %#v, want latest checkpoint without previous diff", result)
	}
}

func mustReadRuntimeFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return data
}
