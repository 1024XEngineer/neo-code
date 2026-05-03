package runtime

import (
	"context"
	"encoding/json"
	"os"
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
}

func (s *checkpointStoreSpy) CreateCheckpoint(context.Context, checkpoint.CreateCheckpointInput) (agentsession.CheckpointRecord, error) {
	return agentsession.CheckpointRecord{}, nil
}

func (s *checkpointStoreSpy) ListCheckpoints(_ context.Context, sessionID string, opts checkpoint.ListCheckpointOpts) ([]agentsession.CheckpointRecord, error) {
	s.listSessionID = sessionID
	s.listOpts = opts
	return s.listRecords, s.listErr
}

func (s *checkpointStoreSpy) GetCheckpoint(context.Context, string) (agentsession.CheckpointRecord, *agentsession.SessionCheckpoint, error) {
	return agentsession.CheckpointRecord{}, nil, nil
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
	shadowRepo      *checkpoint.ShadowRepo
	workdir         string
	projectDir      string
	session         agentsession.Session
}

func newRuntimeCheckpointFixture(t *testing.T, withShadow bool) runtimeCheckpointFixture {
	t.Helper()

	baseDir := t.TempDir()
	workdir := t.TempDir()
	projectDir := t.TempDir()

	sessionStore := agentsession.NewSQLiteStore(baseDir, workdir)
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})

	checkpointStore := checkpoint.NewSQLiteCheckpointStore(agentsession.DatabasePath(baseDir, workdir))
	t.Cleanup(func() {
		_ = checkpointStore.Close()
	})

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

	var shadowRepo *checkpoint.ShadowRepo
	if withShadow {
		shadowRepo = checkpoint.NewShadowRepo(projectDir, workdir)
		if err := shadowRepo.Init(context.Background()); err != nil {
			t.Fatalf("Init shadow repo error = %v", err)
		}
	}

	return runtimeCheckpointFixture{
		service: &Service{
			sessionStore:    sessionStore,
			checkpointStore: checkpointStore,
			shadowRepo:      shadowRepo,
			events:          make(chan RuntimeEvent, 32),
		},
		sessionStore:    sessionStore,
		checkpointStore: checkpointStore,
		shadowRepo:      shadowRepo,
		workdir:         workdir,
		projectDir:      projectDir,
		session:         loaded,
	}
}

func createStoredCheckpointFromSession(
	t *testing.T,
	cpStore *checkpoint.SQLiteCheckpointStore,
	shadowRepo *checkpoint.ShadowRepo,
	loaded agentsession.Session,
	checkpointID string,
) agentsession.CheckpointRecord {
	t.Helper()

	headJSON, err := json.Marshal(loaded.HeadSnapshot())
	if err != nil {
		t.Fatalf("Marshal(head) error = %v", err)
	}
	messagesJSON, err := json.Marshal(loaded.Messages)
	if err != nil {
		t.Fatalf("Marshal(messages) error = %v", err)
	}

	ref := checkpoint.RefForCheckpoint(loaded.ID, checkpointID)
	if _, err := shadowRepo.Snapshot(context.Background(), ref, checkpointID); err != nil {
		t.Fatalf("Snapshot(%s) error = %v", checkpointID, err)
	}

	record, err := cpStore.CreateCheckpoint(context.Background(), checkpoint.CreateCheckpointInput{
		Record: agentsession.CheckpointRecord{
			CheckpointID:      checkpointID,
			WorkspaceKey:      agentsession.WorkspacePathKey(loaded.Workdir),
			SessionID:         loaded.ID,
			RunID:             "run-" + checkpointID,
			Workdir:           loaded.Workdir,
			CreatedAt:         time.Now().Add(-time.Minute),
			Reason:            agentsession.CheckpointReasonPreWrite,
			CodeCheckpointRef: ref,
			Restorable:        true,
			Status:            agentsession.CheckpointStatusCreating,
		},
		SessionCP: agentsession.SessionCheckpoint{
			ID:           "sc-" + checkpointID,
			SessionID:    loaded.ID,
			HeadJSON:     string(headJSON),
			MessagesJSON: string(messagesJSON),
			CreatedAt:    time.Now().Add(-time.Minute),
		},
	})
	if err != nil {
		t.Fatalf("CreateCheckpoint(%s) error = %v", checkpointID, err)
	}
	return record
}

func TestCreatePerTurnCheckpointVariants(t *testing.T) {
	t.Run("full checkpoint when code changed", func(t *testing.T) {
		fixture := newRuntimeCheckpointFixture(t, true)
		target := fixture.workdir + "/main.go"
		if err := os.WriteFile(target, []byte("package main\nconst value = 1\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := fixture.shadowRepo.Snapshot(context.Background(), "refs/heads/base", "baseline"); err != nil {
			t.Fatalf("Snapshot(baseline) error = %v", err)
		}
		if err := os.WriteFile(target, []byte("package main\nconst value = 2\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(modified) error = %v", err)
		}

		state := newRunState("run-full", fixture.session)
		if err := fixture.service.createPerTurnCheckpoint(context.Background(), &state); err != nil {
			t.Fatalf("createPerTurnCheckpoint() error = %v", err)
		}

		records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
		if err != nil {
			t.Fatalf("ListCheckpoints() error = %v", err)
		}
		if len(records) != 1 || records[0].Reason != agentsession.CheckpointReasonPreWrite || records[0].CodeCheckpointRef == "" {
			t.Fatalf("records = %#v, want one full checkpoint", records)
		}
	})

	t.Run("degraded checkpoint when repo unavailable", func(t *testing.T) {
		fixture := newRuntimeCheckpointFixture(t, false)
		state := newRunState("run-degraded", fixture.session)
		if err := fixture.service.createPerTurnCheckpoint(context.Background(), &state); err != nil {
			t.Fatalf("createPerTurnCheckpoint() error = %v", err)
		}

		records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
		if err != nil {
			t.Fatalf("ListCheckpoints() error = %v", err)
		}
		if len(records) != 1 || records[0].Reason != agentsession.CheckpointReasonPreWriteDegraded || records[0].CodeCheckpointRef != "" {
			t.Fatalf("records = %#v, want one degraded checkpoint", records)
		}
	})

	t.Run("degraded checkpoint when no code changes", func(t *testing.T) {
		fixture := newRuntimeCheckpointFixture(t, true)
		target := fixture.workdir + "/main.go"
		if err := os.WriteFile(target, []byte("package main\nconst value = 1\n"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if _, err := fixture.shadowRepo.Snapshot(context.Background(), "refs/heads/base", "baseline"); err != nil {
			t.Fatalf("Snapshot(baseline) error = %v", err)
		}

		state := newRunState("run-noop", fixture.session)
		if err := fixture.service.createPerTurnCheckpoint(context.Background(), &state); err != nil {
			t.Fatalf("createPerTurnCheckpoint() error = %v", err)
		}

		records, err := fixture.checkpointStore.ListCheckpoints(context.Background(), fixture.session.ID, checkpoint.ListCheckpointOpts{})
		if err != nil {
			t.Fatalf("ListCheckpoints() error = %v", err)
		}
		if len(records) != 1 || records[0].Reason != agentsession.CheckpointReasonPreWriteDegraded || records[0].CodeCheckpointRef != "" {
			t.Fatalf("records = %#v, want session-only checkpoint for no-op turn", records)
		}
	})
}

func TestCreateCompactCheckpointAndResumeCheckpoint(t *testing.T) {
	t.Parallel()

	fixture := newRuntimeCheckpointFixture(t, true)
	if err := os.WriteFile(fixture.workdir+"/compact.txt", []byte("compact"), 0o644); err != nil {
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
		repo := checkpoint.NewShadowRepo(t.TempDir(), t.TempDir())

		service.SetCheckpointDependencies(store, repo)
		if service.checkpointStore != store || service.shadowRepo != repo {
			t.Fatalf("service checkpoint dependencies not set correctly")
		}
	})

	t.Run("update runtime session after restore is no-op", func(t *testing.T) {
		service := &Service{}
		service.updateRuntimeSessionAfterRestore("session-1", agentsession.SessionHead{}, nil)
	})
}

func TestRestoreCheckpointAndUndoRestore(t *testing.T) {
	t.Parallel()

	fixture := newRuntimeCheckpointFixture(t, true)
	target := fixture.workdir + "/restore.txt"
	if err := os.WriteFile(target, []byte("version one"), 0o644); err != nil {
		t.Fatalf("WriteFile(version one) error = %v", err)
	}

	originalSession, err := fixture.sessionStore.LoadSession(context.Background(), fixture.session.ID)
	if err != nil {
		t.Fatalf("LoadSession(original) error = %v", err)
	}
	record := createStoredCheckpointFromSession(t, fixture.checkpointStore, fixture.shadowRepo, originalSession, "cp-restore")

	if err := os.WriteFile(target, []byte("version two"), 0o644); err != nil {
		t.Fatalf("WriteFile(version two) error = %v", err)
	}
	if err := fixture.sessionStore.UpdateSessionState(context.Background(), agentsession.UpdateSessionStateInput{
		SessionID: originalSession.ID,
		UpdatedAt: time.Now(),
		Title:     "mutated",
		Head: agentsession.SessionHead{
			Provider: "openai",
			Model:    "gpt-test",
			Workdir:  fixture.workdir,
			TaskState: agentsession.TaskState{
				Goal:                "mutated goal",
				VerificationProfile: agentsession.VerificationProfileTaskOnly,
			},
		},
	}); err != nil {
		t.Fatalf("UpdateSessionState() error = %v", err)
	}
	if err := fixture.sessionStore.AppendMessages(context.Background(), agentsession.AppendMessagesInput{
		SessionID: originalSession.ID,
		Messages: []providertypes.Message{
			{
				Role: providertypes.RoleAssistant,
				Parts: []providertypes.ContentPart{
					providertypes.NewTextPart("after snapshot"),
				},
			},
		},
		UpdatedAt: time.Now(),
		Provider:  "openai",
		Model:     "gpt-test",
		Workdir:   fixture.workdir,
	}); err != nil {
		t.Fatalf("AppendMessages(mutated) error = %v", err)
	}

	conflictResult, err := fixture.service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
		SessionID:    originalSession.ID,
		CheckpointID: record.CheckpointID,
	})
	if err == nil || conflictResult.Conflict == nil || !conflictResult.Conflict.HasConflict {
		t.Fatalf("RestoreCheckpoint(conflict) = (%#v, %v), want conflict error", conflictResult, err)
	}

	restoreResult, err := fixture.service.RestoreCheckpoint(context.Background(), GatewayRestoreInput{
		SessionID:    originalSession.ID,
		CheckpointID: record.CheckpointID,
		Force:        true,
	})
	if err != nil {
		t.Fatalf("RestoreCheckpoint(force) error = %v", err)
	}
	if restoreResult.CheckpointID != record.CheckpointID {
		t.Fatalf("RestoreCheckpoint(force) = %#v", restoreResult)
	}

	restoredContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(restored) error = %v", err)
	}
	if string(restoredContent) != "version one" {
		t.Fatalf("restored content = %q, want version one", string(restoredContent))
	}

	restoredSession, err := fixture.sessionStore.LoadSession(context.Background(), originalSession.ID)
	if err != nil {
		t.Fatalf("LoadSession(restored) error = %v", err)
	}
	if restoredSession.TaskState.Goal != originalSession.TaskState.Goal || len(restoredSession.Messages) != len(originalSession.Messages) {
		t.Fatalf("restored session = %#v, want original goal/messages", restoredSession)
	}

	undoResult, err := fixture.service.UndoRestoreCheckpoint(context.Background(), originalSession.ID)
	if err != nil {
		t.Fatalf("UndoRestoreCheckpoint() error = %v", err)
	}
	if undoResult.SessionID != originalSession.ID {
		t.Fatalf("UndoRestoreCheckpoint() = %#v", undoResult)
	}

	undoneContent, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(undone) error = %v", err)
	}
	if string(undoneContent) != "version two" {
		t.Fatalf("undone content = %q, want version two", string(undoneContent))
	}

	undoneSession, err := fixture.sessionStore.LoadSession(context.Background(), originalSession.ID)
	if err != nil {
		t.Fatalf("LoadSession(undone) error = %v", err)
	}
	if undoneSession.TaskState.Goal != "mutated goal" || len(undoneSession.Messages) != len(originalSession.Messages)+1 {
		t.Fatalf("undone session = %#v, want mutated session restored", undoneSession)
	}
}
