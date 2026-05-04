package gateway

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"neo-code/internal/gateway/auth"
	"neo-code/internal/gateway/handlers"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"
)

type bootstrapRuntimeStub struct {
	runFn                func(ctx context.Context, input RunInput) error
	createSessionFn      func(ctx context.Context, input CreateSessionInput) (string, error)
	compactFn            func(ctx context.Context, input CompactInput) (CompactResult, error)
	executeSystemToolFn  func(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error)
	activateSkillFn      func(ctx context.Context, input SessionSkillMutationInput) error
	deactivateSkillFn    func(ctx context.Context, input SessionSkillMutationInput) error
	listSessionSkillsFn  func(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error)
	listAvailableFn      func(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error)
	resolvePermissionFn  func(ctx context.Context, input PermissionResolutionInput) error
	cancelRunFn          func(ctx context.Context, input CancelInput) (bool, error)
	events               <-chan RuntimeEvent
	listSessionsFn       func(ctx context.Context) ([]SessionSummary, error)
	loadSessionFn        func(ctx context.Context, input LoadSessionInput) (Session, error)
	listSessionTodosFn   func(ctx context.Context, input ListSessionTodosInput) (TodoSnapshot, error)
	getRuntimeSnapshotFn func(ctx context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error)
	deleteSessionFn      func(ctx context.Context, input DeleteSessionInput) (bool, error)
	renameSessionFn      func(ctx context.Context, input RenameSessionInput) error
	listFilesFn          func(ctx context.Context, input ListFilesInput) ([]FileEntry, error)
	listModelsFn         func(ctx context.Context, input ListModelsInput) ([]ModelEntry, error)
	setSessionModelFn    func(ctx context.Context, input SetSessionModelInput) error
	getSessionModelFn    func(ctx context.Context, input GetSessionModelInput) (SessionModelResult, error)
	listProvidersFn      func(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error)
	createProviderFn     func(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error)
	deleteProviderFn     func(ctx context.Context, input DeleteProviderInput) error
	selectProviderFn     func(ctx context.Context, input SelectProviderModelInput) (ProviderSelectionResult, error)
	listMCPServersFn     func(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error)
	upsertMCPServerFn    func(ctx context.Context, input UpsertMCPServerInput) error
	setMCPEnabledFn      func(ctx context.Context, input SetMCPServerEnabledInput) error
	deleteMCPServerFn    func(ctx context.Context, input DeleteMCPServerInput) error
	listCheckpointsFn    func(ctx context.Context, input ListCheckpointsInput) ([]CheckpointEntry, error)
	restoreCheckpointFn  func(ctx context.Context, input CheckpointRestoreInput) (CheckpointRestoreResult, error)
	undoRestoreFn        func(ctx context.Context, input UndoRestoreInput) (CheckpointRestoreResult, error)
	checkpointDiffFn     func(ctx context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error)
}

func (s *bootstrapRuntimeStub) Run(ctx context.Context, input RunInput) error {
	if s != nil && s.runFn != nil {
		return s.runFn(ctx, input)
	}
	return nil
}

func (s *bootstrapRuntimeStub) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	if s != nil && s.compactFn != nil {
		return s.compactFn(ctx, input)
	}
	return CompactResult{}, nil
}

func (s *bootstrapRuntimeStub) ExecuteSystemTool(
	ctx context.Context,
	input ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	if s != nil && s.executeSystemToolFn != nil {
		return s.executeSystemToolFn(ctx, input)
	}
	return tools.ToolResult{}, nil
}

func (s *bootstrapRuntimeStub) ActivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	if s != nil && s.activateSkillFn != nil {
		return s.activateSkillFn(ctx, input)
	}
	return nil
}

func (s *bootstrapRuntimeStub) DeactivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	if s != nil && s.deactivateSkillFn != nil {
		return s.deactivateSkillFn(ctx, input)
	}
	return nil
}

func (s *bootstrapRuntimeStub) ListSessionSkills(
	ctx context.Context,
	input ListSessionSkillsInput,
) ([]SessionSkillState, error) {
	if s != nil && s.listSessionSkillsFn != nil {
		return s.listSessionSkillsFn(ctx, input)
	}
	return nil, nil
}

func (s *bootstrapRuntimeStub) ListAvailableSkills(
	ctx context.Context,
	input ListAvailableSkillsInput,
) ([]AvailableSkillState, error) {
	if s != nil && s.listAvailableFn != nil {
		return s.listAvailableFn(ctx, input)
	}
	return nil, nil
}

func (s *bootstrapRuntimeStub) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	if s != nil && s.resolvePermissionFn != nil {
		return s.resolvePermissionFn(ctx, input)
	}
	return nil
}

func (s *bootstrapRuntimeStub) CancelRun(ctx context.Context, input CancelInput) (bool, error) {
	if s != nil && s.cancelRunFn != nil {
		return s.cancelRunFn(ctx, input)
	}
	return false, nil
}

func (s *bootstrapRuntimeStub) Events() <-chan RuntimeEvent {
	if s == nil {
		return nil
	}
	return s.events
}

func (s *bootstrapRuntimeStub) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	if s != nil && s.listSessionsFn != nil {
		return s.listSessionsFn(ctx)
	}
	return nil, nil
}

func (s *bootstrapRuntimeStub) LoadSession(ctx context.Context, input LoadSessionInput) (Session, error) {
	if s != nil && s.loadSessionFn != nil {
		return s.loadSessionFn(ctx, input)
	}
	return Session{}, nil
}

func (s *bootstrapRuntimeStub) ListSessionTodos(ctx context.Context, input ListSessionTodosInput) (TodoSnapshot, error) {
	if s != nil && s.listSessionTodosFn != nil {
		return s.listSessionTodosFn(ctx, input)
	}
	return TodoSnapshot{}, nil
}

func (s *bootstrapRuntimeStub) GetRuntimeSnapshot(ctx context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error) {
	if s != nil && s.getRuntimeSnapshotFn != nil {
		return s.getRuntimeSnapshotFn(ctx, input)
	}
	return RuntimeSnapshot{}, nil
}

func (s *bootstrapRuntimeStub) DeleteSession(ctx context.Context, input DeleteSessionInput) (bool, error) {
	if s != nil && s.deleteSessionFn != nil {
		return s.deleteSessionFn(ctx, input)
	}
	return false, nil
}
func (s *bootstrapRuntimeStub) RenameSession(ctx context.Context, input RenameSessionInput) error {
	if s != nil && s.renameSessionFn != nil {
		return s.renameSessionFn(ctx, input)
	}
	return nil
}
func (s *bootstrapRuntimeStub) ListFiles(ctx context.Context, input ListFilesInput) ([]FileEntry, error) {
	if s != nil && s.listFilesFn != nil {
		return s.listFilesFn(ctx, input)
	}
	return nil, nil
}
func (s *bootstrapRuntimeStub) ListModels(ctx context.Context, input ListModelsInput) ([]ModelEntry, error) {
	if s != nil && s.listModelsFn != nil {
		return s.listModelsFn(ctx, input)
	}
	return nil, nil
}
func (s *bootstrapRuntimeStub) SetSessionModel(ctx context.Context, input SetSessionModelInput) error {
	if s != nil && s.setSessionModelFn != nil {
		return s.setSessionModelFn(ctx, input)
	}
	return nil
}
func (s *bootstrapRuntimeStub) GetSessionModel(ctx context.Context, input GetSessionModelInput) (SessionModelResult, error) {
	if s != nil && s.getSessionModelFn != nil {
		return s.getSessionModelFn(ctx, input)
	}
	return SessionModelResult{}, nil
}
func (s *bootstrapRuntimeStub) ListProviders(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error) {
	if s != nil && s.listProvidersFn != nil {
		return s.listProvidersFn(ctx, input)
	}
	return nil, nil
}
func (s *bootstrapRuntimeStub) CreateProvider(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error) {
	if s != nil && s.createProviderFn != nil {
		return s.createProviderFn(ctx, input)
	}
	return ProviderSelectionResult{}, nil
}
func (s *bootstrapRuntimeStub) DeleteProvider(ctx context.Context, input DeleteProviderInput) error {
	if s != nil && s.deleteProviderFn != nil {
		return s.deleteProviderFn(ctx, input)
	}
	return nil
}
func (s *bootstrapRuntimeStub) SelectProviderModel(
	ctx context.Context,
	input SelectProviderModelInput,
) (ProviderSelectionResult, error) {
	if s != nil && s.selectProviderFn != nil {
		return s.selectProviderFn(ctx, input)
	}
	return ProviderSelectionResult{}, nil
}
func (s *bootstrapRuntimeStub) ListMCPServers(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error) {
	if s != nil && s.listMCPServersFn != nil {
		return s.listMCPServersFn(ctx, input)
	}
	return nil, nil
}
func (s *bootstrapRuntimeStub) UpsertMCPServer(ctx context.Context, input UpsertMCPServerInput) error {
	if s != nil && s.upsertMCPServerFn != nil {
		return s.upsertMCPServerFn(ctx, input)
	}
	return nil
}
func (s *bootstrapRuntimeStub) SetMCPServerEnabled(ctx context.Context, input SetMCPServerEnabledInput) error {
	if s != nil && s.setMCPEnabledFn != nil {
		return s.setMCPEnabledFn(ctx, input)
	}
	return nil
}
func (s *bootstrapRuntimeStub) DeleteMCPServer(ctx context.Context, input DeleteMCPServerInput) error {
	if s != nil && s.deleteMCPServerFn != nil {
		return s.deleteMCPServerFn(ctx, input)
	}
	return nil
}

func (s *bootstrapRuntimeStub) CreateSession(ctx context.Context, input CreateSessionInput) (string, error) {
	if s != nil && s.createSessionFn != nil {
		return s.createSessionFn(ctx, input)
	}
	return strings.TrimSpace(input.SessionID), nil
}

func (s *bootstrapRuntimeStub) ListCheckpoints(ctx context.Context, input ListCheckpointsInput) ([]CheckpointEntry, error) {
	if s != nil && s.listCheckpointsFn != nil {
		return s.listCheckpointsFn(ctx, input)
	}
	return nil, nil
}

func (s *bootstrapRuntimeStub) RestoreCheckpoint(ctx context.Context, input CheckpointRestoreInput) (CheckpointRestoreResult, error) {
	if s != nil && s.restoreCheckpointFn != nil {
		return s.restoreCheckpointFn(ctx, input)
	}
	return CheckpointRestoreResult{}, nil
}

func (s *bootstrapRuntimeStub) UndoRestore(ctx context.Context, input UndoRestoreInput) (CheckpointRestoreResult, error) {
	if s != nil && s.undoRestoreFn != nil {
		return s.undoRestoreFn(ctx, input)
	}
	return CheckpointRestoreResult{}, nil
}

func (s *bootstrapRuntimeStub) CheckpointDiff(ctx context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error) {
	if s != nil && s.checkpointDiffFn != nil {
		return s.checkpointDiffFn(ctx, input)
	}
	return CheckpointDiffResult{}, nil
}

func TestDispatchRequestFramePing(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionPing,
		RequestID: "req-ping",
	}, nil)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionPing {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionPing)
	}
}

func TestDecodeExecuteSystemToolPayloadBranches(t *testing.T) {
	t.Parallel()

	params, frameErr := decodeExecuteSystemToolPayload(map[string]any{
		"tool_name": "memo_list",
		"arguments": map[string]any{"title": "a"},
	})
	if frameErr != nil {
		t.Fatalf("decodeExecuteSystemToolPayload(map) err = %v", frameErr)
	}
	if string(params.Arguments) == "" || !bytes.Contains(params.Arguments, []byte(`"title"`)) {
		t.Fatalf("arguments = %s, want marshaled json", string(params.Arguments))
	}

	_, frameErr = decodeExecuteSystemToolPayload((*protocol.ExecuteSystemToolParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil pointer payload should be invalid_action, got %v", frameErr)
	}

	_, frameErr = decodeExecuteSystemToolPayload(invalidJSONMarshaler{})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("marshal failure should be invalid_action, got %v", frameErr)
	}
}

func TestNormalizeExecuteSystemToolParamsBranches(t *testing.T) {
	t.Parallel()

	normalized, frameErr := normalizeExecuteSystemToolParams(protocol.ExecuteSystemToolParams{
		ToolName:  "memo_list",
		Arguments: []byte("null"),
	})
	if frameErr != nil {
		t.Fatalf("normalize null arguments err = %v", frameErr)
	}
	if string(normalized.Arguments) != "{}" {
		t.Fatalf("normalized args = %s, want {}", string(normalized.Arguments))
	}

	_, frameErr = normalizeExecuteSystemToolParams(protocol.ExecuteSystemToolParams{
		ToolName:  "memo_list",
		Arguments: []byte("{"),
	})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("invalid json arguments should be invalid_action, got %v", frameErr)
	}
}

func TestDecodeSessionSkillAndSnapshotPayloadBranches(t *testing.T) {
	t.Parallel()

	params, frameErr := decodeActivateSessionSkillPayload(protocol.ActivateSessionSkillParams{
		SessionID: " s-1 ",
		SkillID:   " skill-1 ",
	})
	if frameErr != nil || params.SessionID != "s-1" || params.SkillID != "skill-1" {
		t.Fatalf("decodeActivateSessionSkillPayload(struct) = %#v, err=%v", params, frameErr)
	}
	params, frameErr = decodeActivateSessionSkillPayload(&protocol.ActivateSessionSkillParams{
		SessionID: "s-2",
		SkillID:   "skill-2",
	})
	if frameErr != nil || params.SessionID != "s-2" || params.SkillID != "skill-2" {
		t.Fatalf("decodeActivateSessionSkillPayload(ptr) = %#v, err=%v", params, frameErr)
	}
	params, frameErr = decodeActivateSessionSkillPayload(map[string]any{"session_id": "s-3", "skill_id": "skill-3"})
	if frameErr != nil || params.SessionID != "s-3" || params.SkillID != "skill-3" {
		t.Fatalf("decodeActivateSessionSkillPayload(map) = %#v, err=%v", params, frameErr)
	}
	_, frameErr = decodeActivateSessionSkillPayload((*protocol.ActivateSessionSkillParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil activate payload should be invalid_action, got %v", frameErr)
	}
	_, frameErr = decodeActivateSessionSkillPayload(invalidJSONMarshaler{})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("marshal error activate payload should be invalid_action, got %v", frameErr)
	}

	_, frameErr = decodeDeactivateSessionSkillPayload(protocol.DeactivateSessionSkillParams{
		SessionID: "",
		SkillID:   "skill",
	})
	if frameErr == nil || frameErr.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("missing session_id should be missing_required_field, got %v", frameErr)
	}
	_, frameErr = decodeDeactivateSessionSkillPayload((*protocol.DeactivateSessionSkillParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil deactivate payload should be invalid_action, got %v", frameErr)
	}
	_, frameErr = decodeDeactivateSessionSkillPayload(invalidJSONMarshaler{})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("marshal error deactivate payload should be invalid_action, got %v", frameErr)
	}

	_, frameErr = decodeListSessionSkillsPayload((*protocol.ListSessionSkillsParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil list_session_skills payload should be invalid_action, got %v", frameErr)
	}
	listSkills, frameErr := decodeListSessionSkillsPayload(protocol.ListSessionSkillsParams{SessionID: " s-1 "})
	if frameErr != nil || listSkills.SessionID != "s-1" {
		t.Fatalf("decodeListSessionSkillsPayload(struct) = %#v, err=%v", listSkills, frameErr)
	}
	listSkills, frameErr = decodeListSessionSkillsPayload(map[string]any{"session_id": " s-2 "})
	if frameErr != nil || listSkills.SessionID != "s-2" {
		t.Fatalf("decodeListSessionSkillsPayload(map) = %#v, err=%v", listSkills, frameErr)
	}
	_, frameErr = decodeListSessionSkillsPayload(invalidJSONMarshaler{})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("marshal error list_session_skills payload should be invalid_action, got %v", frameErr)
	}

	_, frameErr = decodeListAvailableSkillsPayload((*protocol.ListAvailableSkillsParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil list_available_skills payload should be invalid_action, got %v", frameErr)
	}
	listAvailable, frameErr := decodeListAvailableSkillsPayload(protocol.ListAvailableSkillsParams{SessionID: " s-3 "})
	if frameErr != nil || listAvailable.SessionID != "s-3" {
		t.Fatalf("decodeListAvailableSkillsPayload(struct) = %#v, err=%v", listAvailable, frameErr)
	}
	listAvailable, frameErr = decodeListAvailableSkillsPayload(map[string]any{"session_id": " s-4 "})
	if frameErr != nil || listAvailable.SessionID != "s-4" {
		t.Fatalf("decodeListAvailableSkillsPayload(map) = %#v, err=%v", listAvailable, frameErr)
	}
	_, frameErr = decodeListAvailableSkillsPayload(invalidJSONMarshaler{})
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("marshal error list_available_skills payload should be invalid_action, got %v", frameErr)
	}

	listTodos, frameErr := decodeListSessionTodosPayload(protocol.ListSessionTodosParams{SessionID: " s-5 "})
	if frameErr != nil || listTodos.SessionID != "s-5" {
		t.Fatalf("decodeListSessionTodosPayload(struct) = %#v, err=%v", listTodos, frameErr)
	}
	listTodos, frameErr = decodeListSessionTodosPayload(map[string]any{"session_id": " s-6 "})
	if frameErr != nil || listTodos.SessionID != "s-6" {
		t.Fatalf("decodeListSessionTodosPayload(map) = %#v, err=%v", listTodos, frameErr)
	}
	_, frameErr = decodeListSessionTodosPayload((*protocol.ListSessionTodosParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil session_todos_list payload should be invalid_action, got %v", frameErr)
	}

	getSnapshot, frameErr := decodeGetRuntimeSnapshotPayload(protocol.GetRuntimeSnapshotParams{SessionID: " s-7 "})
	if frameErr != nil || getSnapshot.SessionID != "s-7" {
		t.Fatalf("decodeGetRuntimeSnapshotPayload(struct) = %#v, err=%v", getSnapshot, frameErr)
	}
	getSnapshot, frameErr = decodeGetRuntimeSnapshotPayload(map[string]any{"session_id": " s-8 "})
	if frameErr != nil || getSnapshot.SessionID != "s-8" {
		t.Fatalf("decodeGetRuntimeSnapshotPayload(map) = %#v, err=%v", getSnapshot, frameErr)
	}
	_, frameErr = decodeGetRuntimeSnapshotPayload((*protocol.GetRuntimeSnapshotParams)(nil))
	if frameErr == nil || frameErr.Code != string(ErrorCodeInvalidAction) {
		t.Fatalf("nil runtime_snapshot_get payload should be invalid_action, got %v", frameErr)
	}
}

func TestCheckpointFrameHandlers(t *testing.T) {
	t.Run("list checkpoints success", func(t *testing.T) {
		runtime := &bootstrapRuntimeStub{
			listCheckpointsFn: func(_ context.Context, input ListCheckpointsInput) ([]CheckpointEntry, error) {
				if input.SubjectID != "subject-1" || input.SessionID != "session-1" ||
					input.Limit != 3 || !input.RestorableOnly {
					t.Fatalf("input = %#v", input)
				}
				return []CheckpointEntry{{CheckpointID: "cp-1", SessionID: "session-1"}}, nil
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)

		response := handleListCheckpointsFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListCheckpoints,
			RequestID: "req-checkpoint-list",
			SessionID: " session-1 ",
			Payload: ListCheckpointsInput{
				Limit:          3,
				RestorableOnly: true,
			},
		}, runtime)

		if response.Type != FrameTypeAck || response.Action != FrameActionListCheckpoints {
			t.Fatalf("response = %#v", response)
		}
		entries, ok := response.Payload.([]CheckpointEntry)
		if !ok || len(entries) != 1 || entries[0].CheckpointID != "cp-1" {
			t.Fatalf("payload = %#v", response.Payload)
		}
	})

	t.Run("restore checkpoint success", func(t *testing.T) {
		runtime := &bootstrapRuntimeStub{
			restoreCheckpointFn: func(_ context.Context, input CheckpointRestoreInput) (CheckpointRestoreResult, error) {
				if input.SubjectID != "subject-1" || input.SessionID != "session-1" || input.CheckpointID != "cp-1" || !input.Force {
					t.Fatalf("input = %#v", input)
				}
				return CheckpointRestoreResult{CheckpointID: input.CheckpointID, SessionID: input.SessionID}, nil
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)

		response := handleRestoreCheckpointFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRestoreCheckpoint,
			RequestID: "req-checkpoint-restore",
			SessionID: " session-1 ",
			Payload: map[string]any{
				"checkpoint_id": " cp-1 ",
				"force":         true,
			},
		}, runtime)

		if response.Type != FrameTypeAck || response.Action != FrameActionRestoreCheckpoint || response.SessionID != "session-1" {
			t.Fatalf("response = %#v", response)
		}
		result, ok := response.Payload.(CheckpointRestoreResult)
		if !ok || result.CheckpointID != "cp-1" {
			t.Fatalf("payload = %#v", response.Payload)
		}
	})

	t.Run("undo restore success", func(t *testing.T) {
		runtime := &bootstrapRuntimeStub{
			undoRestoreFn: func(_ context.Context, input UndoRestoreInput) (CheckpointRestoreResult, error) {
				if input.SubjectID != "subject-1" || input.SessionID != "session-1" {
					t.Fatalf("input = %#v", input)
				}
				return CheckpointRestoreResult{CheckpointID: "cp-guard", SessionID: input.SessionID}, nil
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)

		response := handleUndoRestoreFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUndoRestore,
			RequestID: "req-checkpoint-undo",
			SessionID: " session-1 ",
		}, runtime)

		if response.Type != FrameTypeAck || response.Action != FrameActionUndoRestore || response.SessionID != "session-1" {
			t.Fatalf("response = %#v", response)
		}
		result, ok := response.Payload.(CheckpointRestoreResult)
		if !ok || result.CheckpointID != "cp-guard" {
			t.Fatalf("payload = %#v", response.Payload)
		}
	})

	t.Run("checkpoint diff success", func(t *testing.T) {
		runtime := &bootstrapRuntimeStub{
			checkpointDiffFn: func(_ context.Context, input CheckpointDiffInput) (CheckpointDiffResult, error) {
				if input.SubjectID != "subject-1" || input.SessionID != "session-1" || input.CheckpointID != "cp-1" {
					t.Fatalf("input = %#v", input)
				}
				return CheckpointDiffResult{
					CheckpointID:     input.CheckpointID,
					PrevCheckpointID: "cp-0",
					Files: FileDiffs{
						Modified: []string{"README.md"},
					},
					Patch: "diff --git a/README.md b/README.md",
				}, nil
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)

		response := handleCheckpointDiffFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCheckpointDiff,
			RequestID: "req-checkpoint-diff",
			SessionID: " session-1 ",
			Payload: map[string]any{
				"checkpoint_id": " cp-1 ",
			},
		}, runtime)

		if response.Type != FrameTypeAck || response.Action != FrameActionCheckpointDiff || response.SessionID != "session-1" {
			t.Fatalf("response = %#v", response)
		}
		result, ok := response.Payload.(CheckpointDiffResult)
		if !ok || result.CheckpointID != "cp-1" || result.PrevCheckpointID != "cp-0" ||
			len(result.Files.Modified) != 1 || result.Files.Modified[0] != "README.md" ||
			result.Patch != "diff --git a/README.md b/README.md" {
			t.Fatalf("payload = %#v", response.Payload)
		}
	})
}

func TestDecodeCheckpointRestorePayloadBranches(t *testing.T) {
	t.Parallel()

	params := decodeCheckpointRestorePayload(map[string]any{
		"session_id":    " session-1 ",
		"checkpoint_id": " cp-1 ",
		"force":         true,
	})
	if params.SessionID != "session-1" || params.CheckpointID != "cp-1" || !params.Force {
		t.Fatalf("decode map payload = %#v", params)
	}

	params = decodeCheckpointRestorePayload(CheckpointRestoreInput{
		SessionID:    "session-2",
		CheckpointID: "cp-2",
		Force:        true,
	})
	if params.SessionID != "session-2" || params.CheckpointID != "cp-2" || !params.Force {
		t.Fatalf("decode struct payload = %#v", params)
	}

	params = decodeCheckpointRestorePayload(invalidJSONMarshaler{})
	if params != (CheckpointRestoreInput{}) {
		t.Fatalf("marshal failure should return zero input, got %#v", params)
	}
}

func TestDecodeCheckpointDiffPayloadBranches(t *testing.T) {
	t.Parallel()

	params := decodeCheckpointDiffPayload(map[string]any{
		"session_id":    " session-1 ",
		"checkpoint_id": " cp-1 ",
	})
	if params.SessionID != "session-1" || params.CheckpointID != "cp-1" {
		t.Fatalf("decode map payload = %#v", params)
	}

	params = decodeCheckpointDiffPayload(CheckpointDiffInput{
		SessionID:    "session-2",
		CheckpointID: "cp-2",
	})
	if params.SessionID != "session-2" || params.CheckpointID != "cp-2" {
		t.Fatalf("decode struct payload = %#v", params)
	}

	params = decodeCheckpointDiffPayload(invalidJSONMarshaler{})
	if params != (CheckpointDiffInput{}) {
		t.Fatalf("marshal failure should return zero input, got %#v", params)
	}
}

func TestDispatchRequestFrameWakeOpenURLReviewSuccess(t *testing.T) {
	createInputs := make(chan CreateSessionInput, 1)
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
			createInputs <- input
			return "session-review-created", nil
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-review",
		Payload: map[string]any{
			"action":  "review",
			"workdir": "/workspace/repo",
			"params": map[string]string{
				"path": "README.md",
			},
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionWakeOpenURL {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionWakeOpenURL)
	}
	if response.SessionID != "session-review-created" {
		t.Fatalf("response session_id = %q, want %q", response.SessionID, "session-review-created")
	}

	select {
	case createInput := <-createInputs:
		if createInput.SubjectID != defaultLocalSubjectID {
			t.Fatalf("create session subject_id = %q, want %q", createInput.SubjectID, defaultLocalSubjectID)
		}
		if createInput.SessionID != "" {
			t.Fatalf("create session input session_id = %q, want empty", createInput.SessionID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected wake.review to create session")
	}
}

func TestDispatchRequestFrameWakeOpenURLReviewAllowsSessionIDWithoutWorkdir(t *testing.T) {
	var createCalled bool
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, _ CreateSessionInput) (string, error) {
			createCalled = true
			return "", nil
		},
		listSessionsFn: func(_ context.Context) ([]SessionSummary, error) {
			return []SessionSummary{{ID: "session-review-keep"}}, nil
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-review-session",
		Payload: map[string]any{
			"action":     "review",
			"session_id": "session-review-keep",
			"params": map[string]string{
				"path": "README.md",
			},
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.SessionID != "session-review-keep" {
		t.Fatalf("response session_id = %q, want %q", response.SessionID, "session-review-keep")
	}
	if createCalled {
		t.Fatal("expected wake.review with session_id not to create session")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunWithSessionIDResumesWithoutCreate(t *testing.T) {
	var createCalled bool
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, _ CreateSessionInput) (string, error) {
			createCalled = true
			return "", nil
		},
		listSessionsFn: func(_ context.Context) ([]SessionSummary, error) {
			return []SessionSummary{{ID: "session-run-resume"}}, nil
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-session",
		Payload: map[string]any{
			"action":     "run",
			"session_id": "session-run-resume",
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.SessionID != "session-run-resume" {
		t.Fatalf("response session_id = %q, want %q", response.SessionID, "session-run-resume")
	}
	if createCalled {
		t.Fatal("expected wake.run with session_id not to create session")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunSessionNotFound(t *testing.T) {
	stub := &bootstrapRuntimeStub{
		listSessionsFn: func(_ context.Context) ([]SessionSummary, error) {
			return []SessionSummary{{ID: "session-other"}}, nil
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-session-missing",
		Payload: map[string]any{
			"action":     "run",
			"session_id": "session-missing",
		},
	}, stub)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeResourceNotFound.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeResourceNotFound.String())
	}
}

func TestDispatchRequestFrameWakeOpenURLInvalidAction(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "open",
			"params": map[string]string{
				"path": "README.md",
			},
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeInvalidAction.String())
	}
}

func TestDispatchRequestFrameWakeOpenURLMissingPath(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "review",
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeMissingRequiredField.String())
	}
}

func TestDispatchRequestFrameWakeOpenURLReviewMissingWorkdirAndSessionID(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionWakeOpenURL,
		Payload: map[string]any{
			"action": "review",
			"params": map[string]string{
				"path": "README.md",
			},
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeMissingRequiredField.String())
	}
	if response.Error.Message != "missing required field: workdir or session_id for review" {
		t.Fatalf("error message = %q, want %q", response.Error.Message, "missing required field: workdir or session_id for review")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunSuccess(t *testing.T) {
	createInputs := make(chan CreateSessionInput, 1)
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
			createInputs <- input
			return "session-created-by-runtime", nil
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run",
		Payload: map[string]any{
			"action": "run",
			"params": map[string]string{
				"prompt": "写一个简单的HTTP服务器",
			},
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionWakeOpenURL {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionWakeOpenURL)
	}
	if response.SessionID != "session-created-by-runtime" {
		t.Fatalf("response session_id = %q, want %q", response.SessionID, "session-created-by-runtime")
	}

	select {
	case createInput := <-createInputs:
		if createInput.SubjectID != defaultLocalSubjectID {
			t.Fatalf("create session subject_id = %q, want %q", createInput.SubjectID, defaultLocalSubjectID)
		}
		if createInput.SessionID != "" {
			t.Fatalf("create session input session_id = %q, want empty", createInput.SessionID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected wake.run to create session")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunCanceledParentStillCreatesSession(t *testing.T) {
	createInputs := make(chan CreateSessionInput, 1)
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
			createInputs <- input
			return "session-detached-run", nil
		},
	}

	parentCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	response := dispatchRequestFrame(parentCtx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-canceled-parent",
		Payload: map[string]any{
			"action": "run",
			"params": map[string]string{
				"prompt": "echo hello",
			},
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}

	select {
	case createInput := <-createInputs:
		if createInput.SubjectID != defaultLocalSubjectID {
			t.Fatalf("create session subject_id = %q, want %q", createInput.SubjectID, defaultLocalSubjectID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected wake.run to create session")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunAllowsIPCUnauthenticatedFallback(t *testing.T) {
	createInputs := make(chan CreateSessionInput, 1)
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, input CreateSessionInput) (string, error) {
			createInputs <- input
			return "session-ipc-fallback", nil
		},
	}

	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithTokenAuthenticator(ctx, stubTokenAuthenticator{token: "token-1"})

	response := dispatchRequestFrame(ctx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-ipc-fallback",
		Payload: map[string]any{
			"action": "run",
			"params": map[string]string{
				"prompt": "hello",
			},
		},
	}, stub)

	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.SessionID != "session-ipc-fallback" {
		t.Fatalf("response session_id = %q, want %q", response.SessionID, "session-ipc-fallback")
	}

	select {
	case createInput := <-createInputs:
		if createInput.SubjectID != defaultLocalSubjectID {
			t.Fatalf("create session subject_id = %q, want %q", createInput.SubjectID, defaultLocalSubjectID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected wake.run to create session in ipc fallback path")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunCreateSessionFailed(t *testing.T) {
	stub := &bootstrapRuntimeStub{
		createSessionFn: func(_ context.Context, _ CreateSessionInput) (string, error) {
			return "", errors.New("create failed")
		},
	}

	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-create-failed",
		Payload: map[string]any{
			"action": "run",
			"params": map[string]string{
				"prompt": "hello",
			},
		},
	}, stub)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeInternalError.String())
	}
	if response.Error.Message != "failed to create wake session" {
		t.Fatalf("error message = %q, want %q", response.Error.Message, "failed to create wake session")
	}
}

func TestDispatchRequestFrameWakeOpenURLRunRequiresRuntimePort(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionWakeOpenURL,
		RequestID: "req-wake-run-no-runtime",
		Payload: map[string]any{
			"action": "run",
			"params": map[string]string{
				"prompt": "hello",
			},
		},
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeInternalError.String())
	}
}

func TestDispatchRequestFrameUnsupportedAction(t *testing.T) {
	response := dispatchRequestFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameAction("unknown_action"),
	}, nil)

	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeUnsupportedAction.String() {
		t.Fatalf("error = %#v, want code %q", response.Error, ErrorCodeUnsupportedAction.String())
	}
}

func TestDispatchRequestFrameBindStream(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connectionID := NewConnectionID()
	connectionCtx := WithConnectionID(ctx, connectionID)
	connectionCtx = WithStreamRelay(connectionCtx, relay)

	if err := relay.RegisterConnection(ConnectionRegistration{
		ConnectionID: connectionID,
		Channel:      StreamChannelIPC,
		Context:      connectionCtx,
		Cancel:       cancel,
		Write: func(message RelayMessage) error {
			_ = message
			return nil
		},
		Close: func() {},
	}); err != nil {
		t.Fatalf("register connection: %v", err)
	}
	defer relay.dropConnection(connectionID)

	response := dispatchRequestFrame(connectionCtx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionBindStream,
		RequestID: "bind-1",
		Payload: protocol.BindStreamParams{
			SessionID: "session-1",
			RunID:     "run-1",
			Channel:   "ipc",
		},
	}, nil)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionBindStream {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionBindStream)
	}
	if response.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", response.SessionID, "session-1")
	}
}

func TestHandleBindStreamFrameErrors(t *testing.T) {
	t.Run("missing relay context", func(t *testing.T) {
		response := handleBindStreamFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionBindStream,
			Payload: protocol.BindStreamParams{
				SessionID: "session-1",
			},
		})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("channel mismatch", func(t *testing.T) {
		relay := NewStreamRelay(StreamRelayOptions{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		connectionID := NewConnectionID()
		connectionCtx := WithConnectionID(ctx, connectionID)
		connectionCtx = WithStreamRelay(connectionCtx, relay)
		if err := relay.RegisterConnection(ConnectionRegistration{
			ConnectionID: connectionID,
			Channel:      StreamChannelWS,
			Context:      connectionCtx,
			Cancel:       cancel,
			Write: func(message RelayMessage) error {
				_ = message
				return nil
			},
			Close: func() {},
		}); err != nil {
			t.Fatalf("register connection: %v", err)
		}
		defer relay.dropConnection(connectionID)

		response := handleBindStreamFrame(connectionCtx, MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionBindStream,
			Payload: protocol.BindStreamParams{
				SessionID: "session-1",
				Channel:   "ipc",
			},
		})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidAction.String())
		}
	})
}

func TestDecodeWakeIntentAdditionalBranches(t *testing.T) {
	t.Run("nil payload", func(t *testing.T) {
		_, err := decodeWakeIntent(nil)
		if err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("pointer payload", func(t *testing.T) {
		intent, err := decodeWakeIntent(&protocol.WakeIntent{
			Action: "review",
			Params: map[string]string{"path": "README.md"},
		})
		if err != nil {
			t.Fatalf("decode wake intent: %v", err)
		}
		if intent.Action != "review" {
			t.Fatalf("action = %q, want %q", intent.Action, "review")
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeWakeIntent(map[string]any{"bad": make(chan int)})
		if err == nil {
			t.Fatal("expected marshal error")
		}
	})
}

func TestToFrameError(t *testing.T) {
	stable := toFrameError(&handlers.WakeError{
		Code:    ErrorCodeInvalidAction.String(),
		Message: "invalid",
	})
	if stable.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("stable code = %q, want %q", stable.Code, ErrorCodeInvalidAction.String())
	}

	fallback := toFrameError(&handlers.WakeError{
		Code:    "custom",
		Message: "custom error",
	})
	if fallback.Code != ErrorCodeInternalError.String() {
		t.Fatalf("fallback code = %q, want %q", fallback.Code, ErrorCodeInternalError.String())
	}
}

func TestDecodeAuthenticatePayloadBranches(t *testing.T) {
	t.Run("struct with whitespace token", func(t *testing.T) {
		params, err := decodeAuthenticatePayload(protocol.AuthenticateParams{Token: " token-1 "})
		if err != nil {
			t.Fatalf("decode authenticate struct: %v", err)
		}
		if params.Token != "token-1" {
			t.Fatalf("token = %q, want %q", params.Token, "token-1")
		}
	})

	t.Run("pointer with empty token", func(t *testing.T) {
		params, err := decodeAuthenticatePayload(&protocol.AuthenticateParams{Token: " "})
		if err != nil {
			t.Fatalf("empty token should be allowed, got error: %v", err)
		}
		if params.Token != "" {
			t.Fatalf("token = %q, want empty after trim", params.Token)
		}
	})

	t.Run("map missing token", func(t *testing.T) {
		params, err := decodeAuthenticatePayload(map[string]any{"id": "x"})
		if err != nil {
			t.Fatalf("missing token should be allowed, got error: %v", err)
		}
		if params.Token != "" {
			t.Fatalf("token = %q, want empty", params.Token)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeAuthenticatePayload(struct {
			Token chan int `json:"token"`
		}{Token: make(chan int)})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})
}

func TestHandleAuthenticateFrameBranches(t *testing.T) {
	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionAuthenticate,
		RequestID: "auth-1",
		Payload: protocol.AuthenticateParams{
			Token: "token-1",
		},
	}

	t.Run("missing authenticator uses default local subject", func(t *testing.T) {
		response := handleAuthenticateFrame(context.Background(), frame)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		payload, ok := response.Payload.(map[string]string)
		if !ok {
			t.Fatalf("expected map[string]string payload, got %T", response.Payload)
		}
		if payload["subject_id"] != auth.DefaultLocalSubjectID {
			t.Fatalf("subject_id = %q, want %q", payload["subject_id"], auth.DefaultLocalSubjectID)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "other"})
		response := handleAuthenticateFrame(ctx, frame)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("empty token with authenticator rejects early", func(t *testing.T) {
		emptyFrame := MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionAuthenticate,
			RequestID: "req-empty-token",
			Payload:   protocol.AuthenticateParams{Token: ""},
		}
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "valid"})
		response := handleAuthenticateFrame(ctx, emptyFrame)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("success marks auth state", func(t *testing.T) {
		authState := NewConnectionAuthState()
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		ctx = WithConnectionAuthState(ctx, authState)

		response := handleAuthenticateFrame(ctx, frame)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		if response.Action != FrameActionAuthenticate {
			t.Fatalf("response action = %q, want %q", response.Action, FrameActionAuthenticate)
		}
		if !authState.IsAuthenticated() {
			t.Fatal("expected auth state to be marked authenticated")
		}
	})
}

func TestHandleRunFrameGeneratesFallbackRunIDAndTimeout(t *testing.T) {
	const requestID = "req-run-fallback-1"
	stub := &bootstrapRuntimeStub{
		runFn: func(ctx context.Context, input RunInput) error {
			if input.RunID != requestID {
				t.Fatalf("runtime input run_id = %q, want %q", input.RunID, requestID)
			}
			deadline, hasDeadline := ctx.Deadline()
			if !hasDeadline {
				t.Fatal("runtime context should include timeout deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 {
				t.Fatalf("runtime deadline should be in future, remaining=%v", remaining)
			}
			if remaining > defaultRuntimeOperationTimeout+time.Second {
				t.Fatalf("runtime deadline too long, remaining=%v", remaining)
			}
			return nil
		},
	}

	response := handleRunFrame(context.Background(), MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRun,
		RequestID: requestID,
		InputText: "hello",
	}, stub)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.RunID != requestID {
		t.Fatalf("response run_id = %q, want %q", response.RunID, requestID)
	}
}

func TestHandleRunFrameBranches(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleRunFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRun,
			RequestID: "req-run-unavailable",
			InputText: "hello",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("requires authenticated subject when authenticator exists", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleRunFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRun,
			RequestID: "req-run-unauthorized",
			InputText: "hello",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime canceled error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			runFn: func(_ context.Context, _ RunInput) error {
				return context.Canceled
			},
		}
		response := handleRunFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRun,
			RequestID: "req-run-canceled",
			InputText: "hello",
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
	})
}

func TestRuntimeCallFailedFrameSanitizesErrorAndMapsCode(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithGatewayLogger(context.Background(), log.New(&buf, "", 0))
	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRun,
		RequestID: "req-safe-1",
		SessionID: "session-safe-1",
		RunID:     "run-safe-1",
	}

	internalErr := runtimeCallFailedFrame(ctx, frame, errors.New("db password leaked"), "run")
	if internalErr.Error == nil {
		t.Fatal("internal error response should include frame error payload")
	}
	if internalErr.Error.Code != ErrorCodeInternalError.String() {
		t.Fatalf("error code = %q, want %q", internalErr.Error.Code, ErrorCodeInternalError.String())
	}
	if internalErr.Error.Message != "run failed" {
		t.Fatalf("error message = %q, want %q", internalErr.Error.Message, "run failed")
	}
	if strings.Contains(internalErr.Error.Message, "password") {
		t.Fatalf("error message leaked internal details: %q", internalErr.Error.Message)
	}
	if !strings.Contains(buf.String(), "db password leaked") {
		t.Fatalf("server log should contain internal error details, got %q", buf.String())
	}

	timeoutErr := runtimeCallFailedFrame(context.Background(), frame, context.DeadlineExceeded, "run")
	if timeoutErr.Error == nil || timeoutErr.Error.Code != ErrorCodeTimeout.String() {
		t.Fatalf("timeout error payload = %#v, want timeout", timeoutErr.Error)
	}
	if timeoutErr.Error.Message != "run timed out" {
		t.Fatalf("timeout message = %q, want %q", timeoutErr.Error.Message, "run timed out")
	}

	canceledErr := runtimeCallFailedFrame(context.Background(), frame, context.Canceled, "run")
	if canceledErr.Error == nil || canceledErr.Error.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("canceled error payload = %#v, want invalid_action", canceledErr.Error)
	}
	if canceledErr.Error.Message != "run canceled" {
		t.Fatalf("canceled message = %q, want %q", canceledErr.Error.Message, "run canceled")
	}
}

func TestNormalizeRunID(t *testing.T) {
	if got := normalizeRunID("run-explicit", "req-1"); got != "run-explicit" {
		t.Fatalf("explicit run_id = %q, want %q", got, "run-explicit")
	}
	if got := normalizeRunID("", "req-2"); got != "req-2" {
		t.Fatalf("fallback request_id = %q, want %q", got, "req-2")
	}
	if got := normalizeRunID("", ""); !strings.HasPrefix(got, "run_") {
		t.Fatalf("generated run_id = %q, want prefix %q", got, "run_")
	}
}

func TestWithRuntimeOperationTimeoutFromNilContext(t *testing.T) {
	ctx, cancel := withRuntimeOperationTimeout(nil)
	defer cancel()
	if ctx == nil {
		t.Fatal("timeout wrapper should return non-nil context")
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Fatal("timeout wrapper should set deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("timeout deadline should be in future, remaining=%v", remaining)
	}
}

func TestHandleCompactFrameBranches(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleCompactFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCompact,
			RequestID: "compact-unavailable",
			SessionID: "session-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			compactFn: func(ctx context.Context, input CompactInput) (CompactResult, error) {
				if input.SessionID != "session-compact" {
					t.Fatalf("compact session_id = %q, want %q", input.SessionID, "session-compact")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("compact should use timeout context")
				}
				return CompactResult{Applied: true, BeforeChars: 100, AfterChars: 50}, nil
			},
		}
		response := handleCompactFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCompact,
			RequestID: "compact-ok",
			SessionID: "session-compact",
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		if response.Action != FrameActionCompact {
			t.Fatalf("response action = %q, want %q", response.Action, FrameActionCompact)
		}
	})

	t.Run("runtime timeout", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			compactFn: func(_ context.Context, _ CompactInput) (CompactResult, error) {
				return CompactResult{}, context.DeadlineExceeded
			},
		}
		response := handleCompactFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCompact,
			RequestID: "compact-timeout",
			SessionID: "session-compact",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeTimeout.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeTimeout.String())
		}
	})
}

func TestHandleExecuteSystemToolFrameBranches(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionExecuteSystemTool,
			Payload: protocol.ExecuteSystemToolParams{
				ToolName: "memo_list",
			},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionExecuteSystemTool,
			RequestID: "exec-invalid-1",
			Payload: map[string]any{
				"tool_name": " ",
			},
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeMissingRequiredField.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeMissingRequiredField.String())
		}
	})

	t.Run("reject non-memo system tool", func(t *testing.T) {
		called := false
		stub := &bootstrapRuntimeStub{
			executeSystemToolFn: func(_ context.Context, _ ExecuteSystemToolInput) (tools.ToolResult, error) {
				called = true
				return tools.ToolResult{}, nil
			},
		}
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionExecuteSystemTool,
			RequestID: "exec-invalid-tool-1",
			Payload: protocol.ExecuteSystemToolParams{
				ToolName:  "bash",
				Arguments: []byte("{}"),
			},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidAction.String())
		}
		if called {
			t.Fatal("runtime executeSystemTool should not be called for non-whitelisted tools")
		}
	})

	t.Run("success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			executeSystemToolFn: func(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("execute_system_tool should use timeout context")
				}
				if input.ToolName != "memo_list" {
					t.Fatalf("tool name = %q, want %q", input.ToolName, "memo_list")
				}
				if string(input.Arguments) != "{}" {
					t.Fatalf("arguments = %s, want {}", string(input.Arguments))
				}
				if input.Workdir != "/repo" {
					t.Fatalf("workdir = %q, want %q", input.Workdir, "/repo")
				}
				return tools.ToolResult{
					Name:    "memo_list",
					Content: "ok",
				}, nil
			},
		}
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionExecuteSystemTool,
			RequestID: "exec-ok-1",
			Workdir:   "/repo",
			Payload: protocol.ExecuteSystemToolParams{
				ToolName:  "memo_list",
				Arguments: []byte("null"),
			},
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		if response.Action != FrameActionExecuteSystemTool {
			t.Fatalf("response action = %q, want %q", response.Action, FrameActionExecuteSystemTool)
		}
	})

	t.Run("success diagnose", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			executeSystemToolFn: func(_ context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error) {
				if input.ToolName != tools.ToolNameDiagnose {
					t.Fatalf("tool name = %q, want %q", input.ToolName, tools.ToolNameDiagnose)
				}
				if string(input.Arguments) != `{"error_log":"fatal","os_env":{"os":"linux"}}` {
					t.Fatalf("arguments = %s", string(input.Arguments))
				}
				return tools.ToolResult{
					Name:    tools.ToolNameDiagnose,
					Content: "ok",
				}, nil
			},
		}
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionExecuteSystemTool,
			RequestID: "exec-diagnose-ok-1",
			Payload: protocol.ExecuteSystemToolParams{
				ToolName:  tools.ToolNameDiagnose,
				Arguments: []byte(`{"error_log":"fatal","os_env":{"os":"linux"}}`),
			},
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			executeSystemToolFn: func(_ context.Context, _ ExecuteSystemToolInput) (tools.ToolResult, error) {
				return tools.ToolResult{}, context.DeadlineExceeded
			},
		}
		response := handleExecuteSystemToolFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionExecuteSystemTool,
			RequestID: "exec-timeout-1",
			Payload: protocol.ExecuteSystemToolParams{
				ToolName:  "memo_list",
				Arguments: []byte("{}"),
			},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeTimeout.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeTimeout.String())
		}
	})
}

func TestHandleCancelListLoadResolveBranches(t *testing.T) {
	t.Run("cancel runtime unavailable", func(t *testing.T) {
		response := handleCancelFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCancel,
			RequestID: "cancel-unavailable",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("cancel success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			cancelRunFn: func(_ context.Context, input CancelInput) (bool, error) {
				if input.RunID != "run-cancel-1" {
					t.Fatalf("cancel run_id = %q, want %q", input.RunID, "run-cancel-1")
				}
				return true, nil
			},
		}
		response := handleCancelFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCancel,
			RequestID: "cancel-1",
			Payload: protocol.CancelParams{
				RunID: "run-cancel-1",
			},
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		payload, ok := response.Payload.(map[string]any)
		if !ok {
			t.Fatalf("cancel payload type = %T, want map[string]any", response.Payload)
		}
		if canceled, _ := payload["canceled"].(bool); !canceled {
			t.Fatalf("cancel payload canceled = %v, want true", payload["canceled"])
		}
	})

	t.Run("list sessions runtime unavailable", func(t *testing.T) {
		response := handleListSessionsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListSessions,
			RequestID: "list-unavailable",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("list sessions success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listSessionsFn: func(ctx context.Context) ([]SessionSummary, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("list sessions should use timeout context")
				}
				return []SessionSummary{{ID: "s-1"}}, nil
			},
		}
		response := handleListSessionsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListSessions,
			RequestID: "list-1",
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
	})

	t.Run("list sessions runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listSessionsFn: func(_ context.Context) ([]SessionSummary, error) {
				return nil, errors.New("list failed internals")
			},
		}
		response := handleListSessionsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListSessions,
			RequestID: "list-failed",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
		if response.Error.Message != "list_sessions failed" {
			t.Fatalf("response message = %q, want %q", response.Error.Message, "list_sessions failed")
		}
	})

	t.Run("load session runtime unavailable", func(t *testing.T) {
		response := handleLoadSessionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionLoadSession,
			RequestID: "load-unavailable",
			SessionID: "session-load",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("load session success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			loadSessionFn: func(ctx context.Context, input LoadSessionInput) (Session, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("load session should use timeout context")
				}
				if input.SessionID != "session-load" {
					t.Fatalf("load session id = %q, want %q", input.SessionID, "session-load")
				}
				return Session{ID: input.SessionID}, nil
			},
		}
		response := handleLoadSessionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionLoadSession,
			RequestID: "load-1",
			SessionID: "session-load",
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
	})

	t.Run("load session runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			loadSessionFn: func(_ context.Context, _ LoadSessionInput) (Session, error) {
				return Session{}, errors.New("load failed internals")
			},
		}
		response := handleLoadSessionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionLoadSession,
			RequestID: "load-failed",
			SessionID: "session-load",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
		if response.Error.Message != "load_session failed" {
			t.Fatalf("response message = %q, want %q", response.Error.Message, "load_session failed")
		}
	})

	t.Run("resolve permission runtime unavailable", func(t *testing.T) {
		response := handleResolvePermissionFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionResolvePermission,
			Payload: map[string]any{
				"request_id": "perm-1",
				"decision":   string(PermissionResolutionReject),
			},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("resolve permission invalid payload", func(t *testing.T) {
		response := handleResolvePermissionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionResolvePermission,
			RequestID: "resolve-invalid-payload",
			Payload:   "bad",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidAction.String())
		}
	})

	t.Run("resolve permission invalid decision", func(t *testing.T) {
		response := handleResolvePermissionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionResolvePermission,
			RequestID: "resolve-invalid-decision",
			Payload: map[string]any{
				"request_id": "perm-1",
				"decision":   "allow_forever",
			},
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidAction.String())
		}
	})

	t.Run("resolve permission success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			resolvePermissionFn: func(ctx context.Context, input PermissionResolutionInput) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("resolve permission should use timeout context")
				}
				if input.RequestID != "perm-1" {
					t.Fatalf("permission request_id = %q, want %q", input.RequestID, "perm-1")
				}
				return nil
			},
		}
		response := handleResolvePermissionFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionResolvePermission,
			Payload: map[string]any{
				"request_id": "perm-1",
				"decision":   string(PermissionResolutionReject),
			},
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
	})

	t.Run("resolve permission runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			resolvePermissionFn: func(_ context.Context, _ PermissionResolutionInput) error {
				return errors.New("resolve failed internals")
			},
		}
		response := handleResolvePermissionFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionResolvePermission,
			Payload: map[string]any{
				"request_id": "perm-2",
				"decision":   string(PermissionResolutionReject),
			},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
		if response.Error.Message != "resolve_permission failed" {
			t.Fatalf("response message = %q, want %q", response.Error.Message, "resolve_permission failed")
		}
	})
}

func TestHandleSessionSkillFramesBranches(t *testing.T) {
	t.Run("activate session skill runtime unavailable", func(t *testing.T) {
		response := handleActivateSessionSkillFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionActivateSessionSkill,
			Payload: protocol.ActivateSessionSkillParams{
				SessionID: "session-1",
				SkillID:   "go-review",
			},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("activate and deactivate session skill success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			activateSkillFn: func(ctx context.Context, input SessionSkillMutationInput) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("activate session skill should use timeout context")
				}
				if input.SessionID != "session-skills" || input.SkillID != "go-review" {
					t.Fatalf("activate input = %#v, want session-skills/go-review", input)
				}
				return nil
			},
			deactivateSkillFn: func(ctx context.Context, input SessionSkillMutationInput) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("deactivate session skill should use timeout context")
				}
				if input.SessionID != "session-skills" || input.SkillID != "go-review" {
					t.Fatalf("deactivate input = %#v, want session-skills/go-review", input)
				}
				return nil
			},
		}

		activate := handleActivateSessionSkillFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionActivateSessionSkill,
			RequestID: "activate-1",
			Payload: protocol.ActivateSessionSkillParams{
				SessionID: " session-skills ",
				SkillID:   " go-review ",
			},
		}, stub)
		if activate.Type != FrameTypeAck {
			t.Fatalf("activate response type = %q, want %q", activate.Type, FrameTypeAck)
		}
		if activate.Action != FrameActionActivateSessionSkill {
			t.Fatalf("activate response action = %q, want %q", activate.Action, FrameActionActivateSessionSkill)
		}

		deactivate := handleDeactivateSessionSkillFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeactivateSessionSkill,
			RequestID: "deactivate-1",
			Payload: protocol.DeactivateSessionSkillParams{
				SessionID: " session-skills ",
				SkillID:   " go-review ",
			},
		}, stub)
		if deactivate.Type != FrameTypeAck {
			t.Fatalf("deactivate response type = %q, want %q", deactivate.Type, FrameTypeAck)
		}
		if deactivate.Action != FrameActionDeactivateSessionSkill {
			t.Fatalf("deactivate response action = %q, want %q", deactivate.Action, FrameActionDeactivateSessionSkill)
		}
	})

	t.Run("activate session skill invalid payload", func(t *testing.T) {
		response := handleActivateSessionSkillFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionActivateSessionSkill,
			RequestID: "activate-invalid",
			Payload:   "invalid",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidAction.String())
		}
	})

	t.Run("list session skills success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listSessionSkillsFn: func(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("list session skills should use timeout context")
				}
				if input.SessionID != "session-skills" {
					t.Fatalf("list session skills session_id = %q, want %q", input.SessionID, "session-skills")
				}
				return []SessionSkillState{{SkillID: "go-review"}}, nil
			},
		}

		response := handleListSessionSkillsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListSessionSkills,
			RequestID: "list-session-skills-1",
			Payload: protocol.ListSessionSkillsParams{
				SessionID: " session-skills ",
			},
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		if response.Action != FrameActionListSessionSkills {
			t.Fatalf("response action = %q, want %q", response.Action, FrameActionListSessionSkills)
		}
	})

	t.Run("list available skills success", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listAvailableFn: func(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("list available skills should use timeout context")
				}
				if input.SessionID != "" {
					t.Fatalf("list available skills session_id = %q, want empty", input.SessionID)
				}
				return []AvailableSkillState{
					{
						Descriptor: SkillDescriptor{ID: "go-review"},
						Active:     false,
					},
				}, nil
			},
		}

		response := handleListAvailableSkillsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListAvailableSkills,
			RequestID: "list-available-skills-1",
		}, stub)
		if response.Type != FrameTypeAck {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
		}
		if response.Action != FrameActionListAvailableSkills {
			t.Fatalf("response action = %q, want %q", response.Action, FrameActionListAvailableSkills)
		}
	})
}

func TestRuntimeCallFailedFrameNilErrorFallback(t *testing.T) {
	response := runtimeCallFailedFrame(context.Background(), MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionRun,
	}, nil, "")
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
		t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
	}
	if response.Error.Message != "runtime operation failed" {
		t.Fatalf("response message = %q, want %q", response.Error.Message, "runtime operation failed")
	}
}

func TestHandleCompactFrame_DenyCrossSubjectSession(t *testing.T) {
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject_intruder")

	ctx := WithConnectionAuthState(context.Background(), authState)
	stub := &bootstrapRuntimeStub{
		compactFn: func(_ context.Context, input CompactInput) (CompactResult, error) {
			if input.SubjectID != "subject_owner" {
				return CompactResult{}, ErrRuntimeAccessDenied
			}
			return CompactResult{Applied: true}, nil
		},
	}

	response := handleCompactFrame(ctx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionCompact,
		RequestID: "compact-deny-1",
		SessionID: "session-1",
	}, stub)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeAccessDenied.String() {
		t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeAccessDenied.String())
	}
}

func TestHandleResolvePermissionFrame_DenyCrossSubjectRequestID(t *testing.T) {
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject_intruder")

	ctx := WithConnectionAuthState(context.Background(), authState)
	stub := &bootstrapRuntimeStub{
		resolvePermissionFn: func(_ context.Context, input PermissionResolutionInput) error {
			if input.SubjectID != "subject_owner" {
				return ErrRuntimeAccessDenied
			}
			return nil
		},
	}

	response := handleResolvePermissionFrame(ctx, MessageFrame{
		Type:   FrameTypeRequest,
		Action: FrameActionResolvePermission,
		Payload: map[string]any{
			"request_id": "perm-1",
			"decision":   string(PermissionResolutionReject),
		},
	}, stub)
	if response.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
	}
	if response.Error == nil || response.Error.Code != ErrorCodeAccessDenied.String() {
		t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeAccessDenied.String())
	}
}

func TestHandleCancelFrame_CancelByRunIDOnly(t *testing.T) {
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("local_admin")

	ctx := WithConnectionAuthState(context.Background(), authState)
	stub := &bootstrapRuntimeStub{
		cancelRunFn: func(_ context.Context, input CancelInput) (bool, error) {
			if input.RunID != "run-target-1" {
				t.Fatalf("cancel run_id = %q, want %q", input.RunID, "run-target-1")
			}
			if input.SessionID != "session-ignore" {
				t.Fatalf("cancel session_id = %q, want %q", input.SessionID, "session-ignore")
			}
			return true, nil
		},
	}

	response := handleCancelFrame(ctx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionCancel,
		RequestID: "cancel-precision-1",
		SessionID: "session-ignore",
		Payload: protocol.CancelParams{
			RunID: "run-target-1",
		},
	}, stub)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	payload, ok := response.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", response.Payload)
	}
	if runID, _ := payload["run_id"].(string); runID != "run-target-1" {
		t.Fatalf("payload run_id = %q, want %q", runID, "run-target-1")
	}

	missingRunID := handleCancelFrame(ctx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionCancel,
		RequestID: "cancel-missing-run",
		SessionID: "session-ignore",
	}, stub)
	if missingRunID.Type != FrameTypeError {
		t.Fatalf("response type = %q, want %q", missingRunID.Type, FrameTypeError)
	}
	if missingRunID.Error == nil || missingRunID.Error.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("response error = %#v, want %q", missingRunID.Error, ErrorCodeMissingRequiredField.String())
	}
}

func TestGatewayRun_ClientDisconnectCancelsRuntime(t *testing.T) {
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("local_admin")

	runStarted := make(chan struct{}, 1)
	runCanceled := make(chan error, 1)
	stub := &bootstrapRuntimeStub{
		runFn: func(ctx context.Context, _ RunInput) error {
			select {
			case runStarted <- struct{}{}:
			default:
			}
			<-ctx.Done()
			select {
			case runCanceled <- ctx.Err():
			default:
			}
			return ctx.Err()
		},
	}

	connectionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectionCtx = WithConnectionAuthState(connectionCtx, authState)

	response := handleRunFrame(connectionCtx, MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRun,
		RequestID: "run-disconnect-1",
		SessionID: "session-disconnect-1",
		InputText: "hello",
	}, stub)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}

	select {
	case <-runStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime run did not start")
	}

	cancel()

	select {
	case err := <-runCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run cancellation err = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime run was not canceled on disconnect")
	}
}

type invalidJSONMarshaler struct{}

func (invalidJSONMarshaler) MarshalJSON() ([]byte, error) {
	return []byte("{"), nil
}

func TestRequireAuthenticatedSubjectIDBranches(t *testing.T) {
	t.Run("subject from auth state", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-from-state")
		ctx := WithConnectionAuthState(context.Background(), authState)

		subjectID, frameErr := requireAuthenticatedSubjectID(ctx)
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if subjectID != "subject-from-state" {
			t.Fatalf("subject_id = %q, want %q", subjectID, "subject-from-state")
		}
	})

	t.Run("no authenticator fallback local subject", func(t *testing.T) {
		subjectID, frameErr := requireAuthenticatedSubjectID(context.Background())
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if subjectID != auth.DefaultLocalSubjectID {
			t.Fatalf("subject_id = %q, want %q", subjectID, auth.DefaultLocalSubjectID)
		}
	})

	t.Run("missing request token", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		_, frameErr := requireAuthenticatedSubjectID(ctx)
		if frameErr == nil || frameErr.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("frame error = %#v, want %q", frameErr, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid request token", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		ctx = WithRequestToken(ctx, "bad-token")
		_, frameErr := requireAuthenticatedSubjectID(ctx)
		if frameErr == nil || frameErr.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("frame error = %#v, want %q", frameErr, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("valid token marks auth state", func(t *testing.T) {
		authState := NewConnectionAuthState()
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		ctx = WithRequestToken(ctx, "token-1")
		ctx = WithConnectionAuthState(ctx, authState)

		subjectID, frameErr := requireAuthenticatedSubjectID(ctx)
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if subjectID != "local_admin" {
			t.Fatalf("subject_id = %q, want %q", subjectID, "local_admin")
		}
		if !authState.IsAuthenticated() || authState.SubjectID() != "local_admin" {
			t.Fatalf("auth state = authenticated:%v subject:%q", authState.IsAuthenticated(), authState.SubjectID())
		}
	})
}

func TestResolveWakeRunSubjectIDBranches(t *testing.T) {
	t.Run("ipc unauthorized fallback to local admin", func(t *testing.T) {
		ctx := WithRequestSource(context.Background(), RequestSourceIPC)
		ctx = WithTokenAuthenticator(ctx, stubTokenAuthenticator{token: "token-1"})

		subjectID, frameErr := resolveWakeRunSubjectID(ctx)
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if subjectID != defaultLocalSubjectID {
			t.Fatalf("subject_id = %q, want %q", subjectID, defaultLocalSubjectID)
		}
	})

	t.Run("http unauthorized keeps error", func(t *testing.T) {
		ctx := WithRequestSource(context.Background(), RequestSourceHTTP)
		ctx = WithTokenAuthenticator(ctx, stubTokenAuthenticator{token: "token-1"})

		_, frameErr := resolveWakeRunSubjectID(ctx)
		if frameErr == nil || frameErr.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("frame error = %#v, want %q", frameErr, ErrorCodeUnauthorized.String())
		}
	})
}

func TestDeriveRuntimeExecutionContextBranches(t *testing.T) {
	if got := deriveRuntimeExecutionContext(nil); got == nil {
		t.Fatal("nil input should return non-nil context")
	}

	httpCtx, cancelHTTP := context.WithCancel(WithRequestSource(context.Background(), RequestSourceHTTP))
	derivedHTTP := deriveRuntimeExecutionContext(httpCtx)
	cancelHTTP()
	if derivedHTTP.Err() != nil {
		t.Fatalf("http derived context should not be canceled with parent, got %v", derivedHTTP.Err())
	}

	otherCtx := WithRequestSource(context.Background(), RequestSourceIPC)
	if got := deriveRuntimeExecutionContext(otherCtx); got != otherCtx {
		t.Fatal("non-http context should be returned as-is")
	}
}

func TestDetachWakeRunContextBranches(t *testing.T) {
	if got := detachWakeRunContext(nil); got == nil {
		t.Fatal("nil input should return non-nil context")
	}

	parentCtx, cancelParent := context.WithCancel(context.Background())
	detached := detachWakeRunContext(parentCtx)
	cancelParent()
	if detached.Err() != nil {
		t.Fatalf("detached context should ignore parent cancel, got %v", detached.Err())
	}
}

func TestDecodeCancelInputBranches(t *testing.T) {
	t.Run("frame run id fallback", func(t *testing.T) {
		params, frameErr := decodeCancelInput(MessageFrame{RunID: "run-1"})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.RunID != "run-1" {
			t.Fatalf("run_id = %q, want %q", params.RunID, "run-1")
		}
	})

	t.Run("struct payload", func(t *testing.T) {
		params, frameErr := decodeCancelInput(MessageFrame{
			Payload: protocol.CancelParams{SessionID: "s-1", RunID: "r-1"},
		})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.SessionID != "s-1" || params.RunID != "r-1" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("pointer payload", func(t *testing.T) {
		params, frameErr := decodeCancelInput(MessageFrame{
			Payload: &protocol.CancelParams{SessionID: "s-2", RunID: "r-2"},
		})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.SessionID != "s-2" || params.RunID != "r-2" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("map payload", func(t *testing.T) {
		params, frameErr := decodeCancelInput(MessageFrame{
			Payload: map[string]any{"session_id": "s-3", "run_id": "r-3"},
		})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.SessionID != "s-3" || params.RunID != "r-3" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, frameErr := decodeCancelInput(MessageFrame{
			Payload: struct {
				Bad chan int `json:"bad"`
			}{Bad: make(chan int)},
		})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidAction.String())
		}
	})

	t.Run("unmarshal error from invalid json marshaler", func(t *testing.T) {
		_, frameErr := decodeCancelInput(MessageFrame{Payload: invalidJSONMarshaler{}})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidAction.String())
		}
	})
}

func TestDecodeAuthenticatePayloadAdditionalBranches(t *testing.T) {
	t.Run("struct with whitespace-only token", func(t *testing.T) {
		params, frameErr := decodeAuthenticatePayload(protocol.AuthenticateParams{Token: " "})
		if frameErr != nil {
			t.Fatalf("whitespace-only token should be allowed, got error: %v", frameErr)
		}
		if params.Token != "" {
			t.Fatalf("token = %q, want empty after trim", params.Token)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		_, frameErr := decodeAuthenticatePayload((*protocol.AuthenticateParams)(nil))
		if frameErr == nil || frameErr.Code != ErrorCodeMissingRequiredField.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeMissingRequiredField.String())
		}
	})

	t.Run("map success", func(t *testing.T) {
		params, frameErr := decodeAuthenticatePayload(map[string]any{"token": " token-ok "})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.Token != "token-ok" {
			t.Fatalf("token = %q, want %q", params.Token, "token-ok")
		}
	})

	t.Run("invalid marshaled json", func(t *testing.T) {
		_, frameErr := decodeAuthenticatePayload(invalidJSONMarshaler{})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("default branch missing token", func(t *testing.T) {
		_, frameErr := decodeAuthenticatePayload(map[string]int{"token": 1})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidFrame.String())
		}
	})
}

func TestDecodeBindStreamAndWakeBranches(t *testing.T) {
	t.Run("bind stream struct success", func(t *testing.T) {
		params, frameErr := decodeBindStreamParams(protocol.BindStreamParams{
			SessionID: "session-1",
			RunID:     "run-1",
			Channel:   "ipc",
		})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.SessionID != "session-1" || params.RunID != "run-1" || params.Channel != StreamChannelIPC {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("bind stream pointer success", func(t *testing.T) {
		params, frameErr := decodeBindStreamParams(&protocol.BindStreamParams{
			SessionID: "session-2",
			Channel:   "ws",
		})
		if frameErr != nil {
			t.Fatalf("unexpected frame error: %#v", frameErr)
		}
		if params.SessionID != "session-2" || params.Channel != StreamChannelWS {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("bind stream pointer nil", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams((*protocol.BindStreamParams)(nil))
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("bind stream invalid marshaled json", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams(invalidJSONMarshaler{})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("bind stream missing session", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams(map[string]any{"channel": "all"})
		if frameErr == nil || frameErr.Code != ErrorCodeMissingRequiredField.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeMissingRequiredField.String())
		}
	})

	t.Run("bind stream invalid channel", func(t *testing.T) {
		_, frameErr := decodeBindStreamParams(map[string]any{"session_id": "s", "channel": "tcp"})
		if frameErr == nil || frameErr.Code != ErrorCodeInvalidAction.String() {
			t.Fatalf("frameErr = %#v, want %q", frameErr, ErrorCodeInvalidAction.String())
		}
	})

	t.Run("wake nil pointer", func(t *testing.T) {
		_, err := decodeWakeIntent((*protocol.WakeIntent)(nil))
		if err == nil {
			t.Fatal("expected wake intent decode error")
		}
	})

	t.Run("wake direct struct", func(t *testing.T) {
		intent, err := decodeWakeIntent(protocol.WakeIntent{
			Action:    "REVIEW",
			SessionID: " session-1 ",
		})
		if err != nil {
			t.Fatalf("unexpected decode error: %v", err)
		}
		if intent.Action != "review" || intent.SessionID != "session-1" {
			t.Fatalf("intent = %#v", intent)
		}
	})

	t.Run("wake invalid marshaled json", func(t *testing.T) {
		_, err := decodeWakeIntent(invalidJSONMarshaler{})
		if err == nil {
			t.Fatal("expected wake intent decode error")
		}
	})
}

func TestToFrameErrorNilBranch(t *testing.T) {
	frameErr := toFrameError(nil)
	if frameErr.Code != ErrorCodeInternalError.String() {
		t.Fatalf("frame error code = %q, want %q", frameErr.Code, ErrorCodeInternalError.String())
	}
}

func TestHandleDeleteSessionFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		deleteSessionFn: func(_ context.Context, input DeleteSessionInput) (bool, error) {
			if input.SessionID != "session-1" {
				t.Fatalf("session_id = %q, want %q", input.SessionID, "session-1")
			}
			return true, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionDeleteSession,
		RequestID: "req-del-1",
		SessionID: "session-1",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	if response.Action != FrameActionDeleteSession {
		t.Fatalf("response action = %q, want %q", response.Action, FrameActionDeleteSession)
	}
}

func TestHandleDeleteSessionFrameEmptySessionID(t *testing.T) {
	runtime := &bootstrapRuntimeStub{}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionDeleteSession,
		RequestID: "req-del-2",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	// handler 层不做 session_id 校验（由 validateRequestFrame 在 RPC 分发层做），空 session_id 走默认 stub 返回 deleted=false
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
	payload, ok := response.Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", response.Payload)
	}
	if payload["deleted"] != false {
		t.Fatalf("deleted = %v, want false", payload["deleted"])
	}
}

func TestHandleRenameSessionFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		renameSessionFn: func(_ context.Context, input RenameSessionInput) (err error) {
			if input.SessionID != "session-1" {
				t.Fatalf("session_id = %q, want %q", input.SessionID, "session-1")
			}
			if input.Title != "New Title" {
				t.Fatalf("title = %q, want %q", input.Title, "New Title")
			}
			return nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRenameSession,
		RequestID: "req-rename-1",
		SessionID: "session-1",
		Payload:   protocol.RenameSessionParams{SessionID: "session-1", Title: "New Title"},
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleRenameSessionFrameNilPayload(t *testing.T) {
	runtime := &bootstrapRuntimeStub{}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionRenameSession,
		RequestID: "req-rename-2",
		SessionID: "session-1",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	// handler 层不做 payload 校验（由 validateRequestFrame 在 RPC 分发层做），nil payload 走默认 stub 返回成功
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleListFilesFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		listFilesFn: func(_ context.Context, input ListFilesInput) ([]FileEntry, error) {
			return []FileEntry{{Name: "main.go", Path: "main.go", IsDir: false, Size: 100}}, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionListFiles,
		RequestID: "req-ls-1",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleListModelsFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		listModelsFn: func(_ context.Context, input ListModelsInput) ([]ModelEntry, error) {
			return []ModelEntry{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}}, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionListModels,
		RequestID: "req-models-1",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleListSessionTodosFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		listSessionTodosFn: func(_ context.Context, input ListSessionTodosInput) (TodoSnapshot, error) {
			if input.SubjectID != "subject-1" || input.SessionID != "session-1" {
				t.Fatalf("input = %#v", input)
			}
			return TodoSnapshot{
				Summary: TodoSummary{Total: 1, RequiredTotal: 1, RequiredCompleted: 1},
				Items: []TodoViewItem{
					{ID: "t-1", Content: "done", Status: "completed", Required: true, Revision: 3},
				},
			}, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)
	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionListSessionTodos,
		RequestID: "req-todo-1",
		Payload:   protocol.ListSessionTodosParams{SessionID: " session-1 "},
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck || response.Action != FrameActionListSessionTodos {
		t.Fatalf("response = %#v", response)
	}
	if response.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", response.SessionID, "session-1")
	}
}

func TestHandleGetRuntimeSnapshotFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		getRuntimeSnapshotFn: func(_ context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error) {
			if input.SubjectID != "subject-1" || input.SessionID != "session-2" {
				t.Fatalf("input = %#v", input)
			}
			return RuntimeSnapshot{
				RunID:     "run-1",
				SessionID: "session-2",
				Phase:     "acceptance",
				TaskKind:  "workspace_write",
			}, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)
	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionGetRuntimeSnapshot,
		RequestID: "req-snapshot-1",
		Payload:   protocol.GetRuntimeSnapshotParams{SessionID: " session-2 "},
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck || response.Action != FrameActionGetRuntimeSnapshot {
		t.Fatalf("response = %#v", response)
	}
	if response.SessionID != "session-2" {
		t.Fatalf("session_id = %q, want %q", response.SessionID, "session-2")
	}
}

func TestDecodeRuntimeSnapshotAndTodoPayloadBranches(t *testing.T) {
	if got := buildWakeReviewPrompt("  README.md "); got != "请审查文件 README.md" {
		t.Fatalf("buildWakeReviewPrompt() = %q", got)
	}
	if params := normalizeListSessionTodosParams(" s-1 "); params.SessionID != "s-1" {
		t.Fatalf("normalizeListSessionTodosParams() = %#v", params)
	}
	if params := normalizeGetRuntimeSnapshotParams(" s-2 "); params.SessionID != "s-2" {
		t.Fatalf("normalizeGetRuntimeSnapshotParams() = %#v", params)
	}

	todoParams, todoErr := decodeListSessionTodosPayload(map[string]any{"session_id": " s-3 "})
	if todoErr != nil || todoParams.SessionID != "s-3" {
		t.Fatalf("decodeListSessionTodosPayload map = %#v, err=%v", todoParams, todoErr)
	}
	snapshotParams, snapshotErr := decodeGetRuntimeSnapshotPayload(map[string]any{"session_id": " s-4 "})
	if snapshotErr != nil || snapshotParams.SessionID != "s-4" {
		t.Fatalf("decodeGetRuntimeSnapshotPayload map = %#v, err=%v", snapshotParams, snapshotErr)
	}

	_, todoErr = decodeListSessionTodosPayload(invalidJSONMarshaler{})
	if todoErr == nil || todoErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("expected invalid action for todo payload, got %#v", todoErr)
	}
	_, snapshotErr = decodeGetRuntimeSnapshotPayload(invalidJSONMarshaler{})
	if snapshotErr == nil || snapshotErr.Code != ErrorCodeInvalidAction.String() {
		t.Fatalf("expected invalid action for runtime snapshot payload, got %#v", snapshotErr)
	}
}

func TestHandleSetSessionModelFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		setSessionModelFn: func(_ context.Context, input SetSessionModelInput) error {
			if input.SessionID != "session-1" || input.ModelID != "gpt-4" {
				t.Fatalf("input = %#v", input)
			}
			return nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionSetSessionModel,
		RequestID: "req-setmodel-1",
		SessionID: "session-1",
		Payload:   protocol.SetSessionModelParams{SessionID: "session-1", ModelID: "gpt-4"},
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleGetSessionModelFrameSuccess(t *testing.T) {
	runtime := &bootstrapRuntimeStub{
		getSessionModelFn: func(_ context.Context, input GetSessionModelInput) (SessionModelResult, error) {
			return SessionModelResult{ModelID: "gpt-4", ModelName: "GPT-4", Provider: "openai"}, nil
		},
	}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionGetSessionModel,
		RequestID: "req-getmodel-1",
		SessionID: "session-1",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleGetSessionModelFrameEmptySessionID(t *testing.T) {
	runtime := &bootstrapRuntimeStub{}
	authState := NewConnectionAuthState()
	authState.MarkAuthenticated("subject-1")
	ctx := WithRequestSource(context.Background(), RequestSourceIPC)
	ctx = WithConnectionAuthState(ctx, authState)

	frame := MessageFrame{
		Type:      FrameTypeRequest,
		Action:    FrameActionGetSessionModel,
		RequestID: "req-getmodel-2",
	}
	response := dispatchRequestFrame(ctx, frame, runtime)
	// handler 层不做 session_id 校验（由 validateRequestFrame 在 RPC 分发层做），空 session_id 走默认 stub
	if response.Type != FrameTypeAck {
		t.Fatalf("response type = %q, want %q", response.Type, FrameTypeAck)
	}
}

func TestHandleDeleteSessionFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleDeleteSessionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteSession,
			RequestID: "req-del-err-1",
			SessionID: "session-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleDeleteSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteSession,
			RequestID: "req-del-err-2",
			SessionID: "session-1",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime access denied", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			deleteSessionFn: func(_ context.Context, _ DeleteSessionInput) (bool, error) {
				return false, ErrRuntimeAccessDenied
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteSession,
			RequestID: "req-del-err-3",
			SessionID: "session-1",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeAccessDenied.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeAccessDenied.String())
		}
	})

	t.Run("runtime resource not found", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			deleteSessionFn: func(_ context.Context, _ DeleteSessionInput) (bool, error) {
				return false, ErrRuntimeResourceNotFound
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteSession,
			RequestID: "req-del-err-4",
			SessionID: "session-1",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeResourceNotFound.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeResourceNotFound.String())
		}
	})
}

func TestHandleRenameSessionFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleRenameSessionFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRenameSession,
			RequestID: "req-rename-err-1",
			Payload:   protocol.RenameSessionParams{SessionID: "s-1", Title: "T"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleRenameSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRenameSession,
			RequestID: "req-rename-err-2",
			Payload:   protocol.RenameSessionParams{SessionID: "s-1", Title: "T"},
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleRenameSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRenameSession,
			RequestID: "req-rename-err-3",
			Payload:   "bad",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			renameSessionFn: func(_ context.Context, _ RenameSessionInput) error {
				return errors.New("rename failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleRenameSessionFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionRenameSession,
			RequestID: "req-rename-err-4",
			Payload:   protocol.RenameSessionParams{SessionID: "s-1", Title: "T"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleListFilesFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleListFilesFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListFiles,
			RequestID: "req-ls-err-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleListFilesFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListFiles,
			RequestID: "req-ls-err-2",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListFilesFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListFiles,
			RequestID: "req-ls-err-3",
			Payload:   "bad",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listFilesFn: func(_ context.Context, _ ListFilesInput) ([]FileEntry, error) {
				return nil, errors.New("list failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListFilesFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListFiles,
			RequestID: "req-ls-err-4",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleListModelsFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleListModelsFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListModels,
			RequestID: "req-models-err-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleListModelsFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListModels,
			RequestID: "req-models-err-2",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			listModelsFn: func(_ context.Context, _ ListModelsInput) ([]ModelEntry, error) {
				return nil, errors.New("list failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListModelsFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListModels,
			RequestID: "req-models-err-3",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleSetSessionModelFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleSetSessionModelFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetSessionModel,
			RequestID: "req-setmodel-err-1",
			Payload:   protocol.SetSessionModelParams{SessionID: "s-1", ModelID: "m-1"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleSetSessionModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetSessionModel,
			RequestID: "req-setmodel-err-2",
			Payload:   protocol.SetSessionModelParams{SessionID: "s-1", ModelID: "m-1"},
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSetSessionModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetSessionModel,
			RequestID: "req-setmodel-err-3",
			Payload:   "bad",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			setSessionModelFn: func(_ context.Context, _ SetSessionModelInput) error {
				return errors.New("set failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSetSessionModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetSessionModel,
			RequestID: "req-setmodel-err-4",
			Payload:   protocol.SetSessionModelParams{SessionID: "s-1", ModelID: "m-1"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleGetSessionModelFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleGetSessionModelFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionGetSessionModel,
			RequestID: "req-getmodel-err-1",
			SessionID: "session-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleGetSessionModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionGetSessionModel,
			RequestID: "req-getmodel-err-2",
			SessionID: "session-1",
		}, &bootstrapRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &bootstrapRuntimeStub{
			getSessionModelFn: func(_ context.Context, _ GetSessionModelInput) (SessionModelResult, error) {
				return SessionModelResult{}, errors.New("get failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleGetSessionModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionGetSessionModel,
			RequestID: "req-getmodel-err-3",
			SessionID: "session-1",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleListProvidersFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleListProvidersFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListProviders,
			RequestID: "req-providers-err-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListProvidersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListProviders,
			RequestID: "req-providers-err-2",
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
		if response.Error.Message != "management runtime port is unavailable" {
			t.Fatalf("error message = %q, want %q", response.Error.Message, "management runtime port is unavailable")
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleListProvidersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListProviders,
			RequestID: "req-providers-err-3",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			listProvidersFn: func(_ context.Context, _ ListProvidersInput) ([]ProviderOption, error) {
				return nil, errors.New("list failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListProvidersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListProviders,
			RequestID: "req-providers-err-4",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleCreateCustomProviderFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleCreateCustomProviderFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateCustomProvider,
			RequestID: "req-create-prov-err-1",
			Payload:   protocol.CreateCustomProviderParams{Name: "p", Driver: "d", APIKeyEnv: "e"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleCreateCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateCustomProvider,
			RequestID: "req-create-prov-err-2",
			Payload:   protocol.CreateCustomProviderParams{Name: "p", Driver: "d", APIKeyEnv: "e"},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleCreateCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateCustomProvider,
			RequestID: "req-create-prov-err-3",
			Payload:   protocol.CreateCustomProviderParams{Name: "p", Driver: "d", APIKeyEnv: "e"},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleCreateCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateCustomProvider,
			RequestID: "req-create-prov-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			createProviderFn: func(_ context.Context, _ CreateProviderInput) (ProviderSelectionResult, error) {
				return ProviderSelectionResult{}, errors.New("create failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleCreateCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionCreateCustomProvider,
			RequestID: "req-create-prov-err-5",
			Payload:   protocol.CreateCustomProviderParams{Name: "p", Driver: "d", APIKeyEnv: "e"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleDeleteCustomProviderFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleDeleteCustomProviderFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteCustomProvider,
			RequestID: "req-del-prov-err-1",
			Payload:   protocol.DeleteCustomProviderParams{ProviderID: "p-1"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteCustomProvider,
			RequestID: "req-del-prov-err-2",
			Payload:   protocol.DeleteCustomProviderParams{ProviderID: "p-1"},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleDeleteCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteCustomProvider,
			RequestID: "req-del-prov-err-3",
			Payload:   protocol.DeleteCustomProviderParams{ProviderID: "p-1"},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteCustomProvider,
			RequestID: "req-del-prov-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			deleteProviderFn: func(_ context.Context, _ DeleteProviderInput) error {
				return errors.New("delete failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteCustomProviderFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteCustomProvider,
			RequestID: "req-del-prov-err-5",
			Payload:   protocol.DeleteCustomProviderParams{ProviderID: "p-1"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleSelectProviderModelFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleSelectProviderModelFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSelectProviderModel,
			RequestID: "req-select-err-1",
			Payload:   protocol.SelectProviderModelParams{ProviderID: "p-1"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSelectProviderModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSelectProviderModel,
			RequestID: "req-select-err-2",
			Payload:   protocol.SelectProviderModelParams{ProviderID: "p-1"},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleSelectProviderModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSelectProviderModel,
			RequestID: "req-select-err-3",
			Payload:   protocol.SelectProviderModelParams{ProviderID: "p-1"},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSelectProviderModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSelectProviderModel,
			RequestID: "req-select-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			selectProviderFn: func(_ context.Context, _ SelectProviderModelInput) (ProviderSelectionResult, error) {
				return ProviderSelectionResult{}, errors.New("select failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSelectProviderModelFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSelectProviderModel,
			RequestID: "req-select-err-5",
			Payload:   protocol.SelectProviderModelParams{ProviderID: "p-1"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleListMCPServersFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleListMCPServersFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListMCPServers,
			RequestID: "req-mcp-list-err-1",
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListMCPServersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListMCPServers,
			RequestID: "req-mcp-list-err-2",
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleListMCPServersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListMCPServers,
			RequestID: "req-mcp-list-err-3",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			listMCPServersFn: func(_ context.Context, _ ListMCPServersInput) ([]MCPServerEntry, error) {
				return nil, errors.New("list failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleListMCPServersFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionListMCPServers,
			RequestID: "req-mcp-list-err-4",
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleUpsertMCPServerFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleUpsertMCPServerFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUpsertMCPServer,
			RequestID: "req-mcp-upsert-err-1",
			Payload:   protocol.UpsertMCPServerParams{Server: protocol.MCPServerParams{ID: "mcp-1"}},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleUpsertMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUpsertMCPServer,
			RequestID: "req-mcp-upsert-err-2",
			Payload:   protocol.UpsertMCPServerParams{Server: protocol.MCPServerParams{ID: "mcp-1"}},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleUpsertMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUpsertMCPServer,
			RequestID: "req-mcp-upsert-err-3",
			Payload:   protocol.UpsertMCPServerParams{Server: protocol.MCPServerParams{ID: "mcp-1"}},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleUpsertMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUpsertMCPServer,
			RequestID: "req-mcp-upsert-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			upsertMCPServerFn: func(_ context.Context, _ UpsertMCPServerInput) error {
				return errors.New("upsert failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleUpsertMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionUpsertMCPServer,
			RequestID: "req-mcp-upsert-err-5",
			Payload:   protocol.UpsertMCPServerParams{Server: protocol.MCPServerParams{ID: "mcp-1"}},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleSetMCPServerEnabledFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleSetMCPServerEnabledFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetMCPServerEnabled,
			RequestID: "req-mcp-set-err-1",
			Payload:   protocol.SetMCPServerEnabledParams{ID: "mcp-1", Enabled: true},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSetMCPServerEnabledFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetMCPServerEnabled,
			RequestID: "req-mcp-set-err-2",
			Payload:   protocol.SetMCPServerEnabledParams{ID: "mcp-1", Enabled: true},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleSetMCPServerEnabledFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetMCPServerEnabled,
			RequestID: "req-mcp-set-err-3",
			Payload:   protocol.SetMCPServerEnabledParams{ID: "mcp-1", Enabled: true},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSetMCPServerEnabledFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetMCPServerEnabled,
			RequestID: "req-mcp-set-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			setMCPEnabledFn: func(_ context.Context, _ SetMCPServerEnabledInput) error {
				return errors.New("set failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleSetMCPServerEnabledFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionSetMCPServerEnabled,
			RequestID: "req-mcp-set-err-5",
			Payload:   protocol.SetMCPServerEnabledParams{ID: "mcp-1", Enabled: true},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestHandleDeleteMCPServerFrameErrors(t *testing.T) {
	t.Run("runtime unavailable", func(t *testing.T) {
		response := handleDeleteMCPServerFrame(context.Background(), MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteMCPServer,
			RequestID: "req-mcp-del-err-1",
			Payload:   protocol.DeleteMCPServerParams{ID: "mcp-1"},
		}, nil)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("management port unavailable", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteMCPServer,
			RequestID: "req-mcp-del-err-2",
			Payload:   protocol.DeleteMCPServerParams{ID: "mcp-1"},
		}, runtimeOnlyStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "token-1"})
		response := handleDeleteMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteMCPServer,
			RequestID: "req-mcp-del-err-3",
			Payload:   protocol.DeleteMCPServerParams{ID: "mcp-1"},
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteMCPServer,
			RequestID: "req-mcp-del-err-4",
			Payload:   "bad",
		}, &managementRuntimeStub{})
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})

	t.Run("runtime error", func(t *testing.T) {
		stub := &managementRuntimeStub{
			deleteMCPServerFn: func(_ context.Context, _ DeleteMCPServerInput) error {
				return errors.New("delete failed")
			},
		}
		authState := NewConnectionAuthState()
		authState.MarkAuthenticated("subject-1")
		ctx := WithConnectionAuthState(context.Background(), authState)
		response := handleDeleteMCPServerFrame(ctx, MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionDeleteMCPServer,
			RequestID: "req-mcp-del-err-5",
			Payload:   protocol.DeleteMCPServerParams{ID: "mcp-1"},
		}, stub)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInternalError.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInternalError.String())
		}
	})
}

func TestDecodeRenameSessionPayloadBranches(t *testing.T) {
	t.Run("struct success", func(t *testing.T) {
		params, err := decodeRenameSessionPayload(protocol.RenameSessionParams{SessionID: " s-1 ", Title: " T "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.Title != "T" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		_, err := decodeRenameSessionPayload((*protocol.RenameSessionParams)(nil))
		if err == nil || err.Code != ErrorCodeMissingRequiredField.String() {
			t.Fatalf("expected missing required field error, got %#v", err)
		}
	})

	t.Run("map success", func(t *testing.T) {
		params, err := decodeRenameSessionPayload(map[string]any{"session_id": " s-1 ", "title": " T "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.Title != "T" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeRenameSessionPayload(struct {
			Bad chan int `json:"bad"`
		}{Bad: make(chan int)})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		_, err := decodeRenameSessionPayload(invalidJSONMarshaler{})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})
}

func TestDecodeListFilesPayloadBranches(t *testing.T) {
	t.Run("struct success", func(t *testing.T) {
		params, err := decodeListFilesPayload(protocol.ListFilesParams{SessionID: " s-1 ", Workdir: " /w ", Path: " /p "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.Workdir != "/w" || params.Path != "/p" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("nil pointer returns empty", func(t *testing.T) {
		params, err := decodeListFilesPayload((*protocol.ListFilesParams)(nil))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "" || params.Workdir != "" || params.Path != "" {
			t.Fatalf("expected empty params, got %#v", params)
		}
	})

	t.Run("map success", func(t *testing.T) {
		params, err := decodeListFilesPayload(map[string]any{"session_id": " s-1 ", "workdir": " /w ", "path": " /p "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.Workdir != "/w" || params.Path != "/p" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeListFilesPayload(struct {
			Bad chan int `json:"bad"`
		}{Bad: make(chan int)})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		_, err := decodeListFilesPayload(invalidJSONMarshaler{})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})
}

func TestDecodeSetSessionModelPayloadBranches(t *testing.T) {
	t.Run("struct success", func(t *testing.T) {
		params, err := decodeSetSessionModelPayload(protocol.SetSessionModelParams{SessionID: " s-1 ", ProviderID: " p-1 ", ModelID: " m-1 "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.ProviderID != "p-1" || params.ModelID != "m-1" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		_, err := decodeSetSessionModelPayload((*protocol.SetSessionModelParams)(nil))
		if err == nil || err.Code != ErrorCodeMissingRequiredField.String() {
			t.Fatalf("expected missing required field error, got %#v", err)
		}
	})

	t.Run("map success", func(t *testing.T) {
		params, err := decodeSetSessionModelPayload(map[string]any{"session_id": " s-1 ", "provider_id": " p-1 ", "model_id": " m-1 "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if params.SessionID != "s-1" || params.ProviderID != "p-1" || params.ModelID != "m-1" {
			t.Fatalf("params = %#v", params)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		_, err := decodeSetSessionModelPayload(struct {
			Bad chan int `json:"bad"`
		}{Bad: make(chan int)})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		_, err := decodeSetSessionModelPayload(invalidJSONMarshaler{})
		if err == nil || err.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("expected invalid frame error, got %#v", err)
		}
	})
}

func TestHandleAuthenticateFrameAdditionalBranches(t *testing.T) {
	t.Run("empty token rejects when authenticator exists", func(t *testing.T) {
		frame := MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionAuthenticate,
			RequestID: "auth-empty-1",
			Payload:   protocol.AuthenticateParams{Token: "   "},
		}
		ctx := WithTokenAuthenticator(context.Background(), stubTokenAuthenticator{token: "valid"})
		response := handleAuthenticateFrame(ctx, frame)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("valid token but empty subjectID rejects", func(t *testing.T) {
		frame := MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionAuthenticate,
			RequestID: "auth-empty-subject-1",
			Payload:   protocol.AuthenticateParams{Token: "token-1"},
		}
		ctx := WithTokenAuthenticator(context.Background(), emptySubjectAuthenticator{})
		response := handleAuthenticateFrame(ctx, frame)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeUnauthorized.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeUnauthorized.String())
		}
	})

	t.Run("decode error", func(t *testing.T) {
		frame := MessageFrame{
			Type:      FrameTypeRequest,
			Action:    FrameActionAuthenticate,
			RequestID: "auth-decode-err-1",
			Payload:   "bad",
		}
		response := handleAuthenticateFrame(context.Background(), frame)
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeInvalidFrame.String() {
			t.Fatalf("response error = %#v, want %q", response.Error, ErrorCodeInvalidFrame.String())
		}
	})
}

type emptySubjectAuthenticator struct{}

func (emptySubjectAuthenticator) ValidateToken(token string) bool              { return true }
func (emptySubjectAuthenticator) ResolveSubjectID(token string) (string, bool) { return "", true }

func TestRuntimeCallFailedFrameErrorCodes(t *testing.T) {
	t.Run("access denied", func(t *testing.T) {
		response := runtimeCallFailedFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionDeleteSession,
		}, ErrRuntimeAccessDenied, "delete_session")
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeAccessDenied.String() {
			t.Fatalf("error code = %q, want %q", response.Error.Code, ErrorCodeAccessDenied.String())
		}
		if response.Error.Message != "delete_session access denied" {
			t.Fatalf("error message = %q, want %q", response.Error.Message, "delete_session access denied")
		}
	})

	t.Run("resource not found", func(t *testing.T) {
		response := runtimeCallFailedFrame(context.Background(), MessageFrame{
			Type:   FrameTypeRequest,
			Action: FrameActionLoadSession,
		}, ErrRuntimeResourceNotFound, "load_session")
		if response.Type != FrameTypeError {
			t.Fatalf("response type = %q, want %q", response.Type, FrameTypeError)
		}
		if response.Error == nil || response.Error.Code != ErrorCodeResourceNotFound.String() {
			t.Fatalf("error code = %q, want %q", response.Error.Code, ErrorCodeResourceNotFound.String())
		}
		if response.Error.Message != "load_session target not found" {
			t.Fatalf("error message = %q, want %q", response.Error.Message, "load_session target not found")
		}
	})
}

// runtimeOnlyStub implements RuntimePort but NOT ManagementRuntimePort.
type runtimeOnlyStub struct{}

func (runtimeOnlyStub) Run(ctx context.Context, input RunInput) error { return nil }
func (runtimeOnlyStub) Compact(ctx context.Context, input CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}
func (runtimeOnlyStub) ExecuteSystemTool(ctx context.Context, input ExecuteSystemToolInput) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (runtimeOnlyStub) ActivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	return nil
}
func (runtimeOnlyStub) DeactivateSessionSkill(ctx context.Context, input SessionSkillMutationInput) error {
	return nil
}
func (runtimeOnlyStub) ListSessionSkills(ctx context.Context, input ListSessionSkillsInput) ([]SessionSkillState, error) {
	return nil, nil
}
func (runtimeOnlyStub) ListAvailableSkills(ctx context.Context, input ListAvailableSkillsInput) ([]AvailableSkillState, error) {
	return nil, nil
}
func (runtimeOnlyStub) ResolvePermission(ctx context.Context, input PermissionResolutionInput) error {
	return nil
}
func (runtimeOnlyStub) CancelRun(ctx context.Context, input CancelInput) (bool, error) {
	return false, nil
}
func (runtimeOnlyStub) Events() <-chan RuntimeEvent                                { return nil }
func (runtimeOnlyStub) ListSessions(ctx context.Context) ([]SessionSummary, error) { return nil, nil }
func (runtimeOnlyStub) LoadSession(ctx context.Context, input LoadSessionInput) (Session, error) {
	return Session{}, nil
}
func (runtimeOnlyStub) ListSessionTodos(ctx context.Context, input ListSessionTodosInput) (TodoSnapshot, error) {
	return TodoSnapshot{}, nil
}
func (runtimeOnlyStub) GetRuntimeSnapshot(ctx context.Context, input GetRuntimeSnapshotInput) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{}, nil
}
func (runtimeOnlyStub) CreateSession(ctx context.Context, input CreateSessionInput) (string, error) {
	return "", nil
}
func (runtimeOnlyStub) DeleteSession(ctx context.Context, input DeleteSessionInput) (bool, error) {
	return false, nil
}
func (runtimeOnlyStub) RenameSession(ctx context.Context, input RenameSessionInput) error { return nil }
func (runtimeOnlyStub) ListFiles(ctx context.Context, input ListFilesInput) ([]FileEntry, error) {
	return nil, nil
}
func (runtimeOnlyStub) ListModels(ctx context.Context, input ListModelsInput) ([]ModelEntry, error) {
	return nil, nil
}
func (runtimeOnlyStub) SetSessionModel(ctx context.Context, input SetSessionModelInput) error {
	return nil
}
func (runtimeOnlyStub) GetSessionModel(ctx context.Context, input GetSessionModelInput) (SessionModelResult, error) {
	return SessionModelResult{}, nil
}
func (runtimeOnlyStub) ListCheckpoints(_ context.Context, _ ListCheckpointsInput) ([]CheckpointEntry, error) {
	return nil, nil
}
func (runtimeOnlyStub) RestoreCheckpoint(_ context.Context, _ CheckpointRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}
func (runtimeOnlyStub) UndoRestore(_ context.Context, _ UndoRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}
func (runtimeOnlyStub) CheckpointDiff(_ context.Context, _ CheckpointDiffInput) (CheckpointDiffResult, error) {
	return CheckpointDiffResult{}, nil
}

type managementRuntimeStub struct {
	bootstrapRuntimeStub
	listProvidersFn   func(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error)
	createProviderFn  func(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error)
	deleteProviderFn  func(ctx context.Context, input DeleteProviderInput) error
	selectProviderFn  func(ctx context.Context, input SelectProviderModelInput) (ProviderSelectionResult, error)
	listMCPServersFn  func(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error)
	upsertMCPServerFn func(ctx context.Context, input UpsertMCPServerInput) error
	setMCPEnabledFn   func(ctx context.Context, input SetMCPServerEnabledInput) error
	deleteMCPServerFn func(ctx context.Context, input DeleteMCPServerInput) error
}

func (s *managementRuntimeStub) ListProviders(ctx context.Context, input ListProvidersInput) ([]ProviderOption, error) {
	if s != nil && s.listProvidersFn != nil {
		return s.listProvidersFn(ctx, input)
	}
	return nil, nil
}

func (s *managementRuntimeStub) CreateProvider(ctx context.Context, input CreateProviderInput) (ProviderSelectionResult, error) {
	if s != nil && s.createProviderFn != nil {
		return s.createProviderFn(ctx, input)
	}
	return ProviderSelectionResult{}, nil
}

func (s *managementRuntimeStub) DeleteProvider(ctx context.Context, input DeleteProviderInput) error {
	if s != nil && s.deleteProviderFn != nil {
		return s.deleteProviderFn(ctx, input)
	}
	return nil
}

func (s *managementRuntimeStub) SelectProviderModel(ctx context.Context, input SelectProviderModelInput) (ProviderSelectionResult, error) {
	if s != nil && s.selectProviderFn != nil {
		return s.selectProviderFn(ctx, input)
	}
	return ProviderSelectionResult{}, nil
}

func (s *managementRuntimeStub) ListMCPServers(ctx context.Context, input ListMCPServersInput) ([]MCPServerEntry, error) {
	if s != nil && s.listMCPServersFn != nil {
		return s.listMCPServersFn(ctx, input)
	}
	return nil, nil
}

func (s *managementRuntimeStub) UpsertMCPServer(ctx context.Context, input UpsertMCPServerInput) error {
	if s != nil && s.upsertMCPServerFn != nil {
		return s.upsertMCPServerFn(ctx, input)
	}
	return nil
}

func (s *managementRuntimeStub) SetMCPServerEnabled(ctx context.Context, input SetMCPServerEnabledInput) error {
	if s != nil && s.setMCPEnabledFn != nil {
		return s.setMCPEnabledFn(ctx, input)
	}
	return nil
}

func (s *managementRuntimeStub) DeleteMCPServer(ctx context.Context, input DeleteMCPServerInput) error {
	if s != nil && s.deleteMCPServerFn != nil {
		return s.deleteMCPServerFn(ctx, input)
	}
	return nil
}
