package gateway

import (
	"context"

	"neo-code/internal/tools"
)

// runtimePortCompileStub 用于编译期验证 RuntimePort 契约完整性。
type runtimePortCompileStub struct{}

func (s *runtimePortCompileStub) Run(_ context.Context, _ RunInput) error {
	return nil
}

func (s *runtimePortCompileStub) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (s *runtimePortCompileStub) ExecuteSystemTool(
	_ context.Context,
	_ ExecuteSystemToolInput,
) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (s *runtimePortCompileStub) ActivateSessionSkill(_ context.Context, _ SessionSkillMutationInput) error {
	return nil
}

func (s *runtimePortCompileStub) DeactivateSessionSkill(_ context.Context, _ SessionSkillMutationInput) error {
	return nil
}

func (s *runtimePortCompileStub) ListSessionSkills(
	_ context.Context,
	_ ListSessionSkillsInput,
) ([]SessionSkillState, error) {
	return nil, nil
}

func (s *runtimePortCompileStub) ListAvailableSkills(
	_ context.Context,
	_ ListAvailableSkillsInput,
) ([]AvailableSkillState, error) {
	return nil, nil
}

func (s *runtimePortCompileStub) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (s *runtimePortCompileStub) CancelRun(_ context.Context, _ CancelInput) (bool, error) {
	return false, nil
}

func (s *runtimePortCompileStub) Events() <-chan RuntimeEvent {
	return nil
}

func (s *runtimePortCompileStub) ListSessions(_ context.Context) ([]SessionSummary, error) {
	return nil, nil
}

func (s *runtimePortCompileStub) LoadSession(_ context.Context, _ LoadSessionInput) (Session, error) {
	return Session{}, nil
}
func (s *runtimePortCompileStub) DeleteSession(_ context.Context, _ DeleteSessionInput) (bool, error) {
	return false, nil
}
func (s *runtimePortCompileStub) RenameSession(_ context.Context, _ RenameSessionInput) error {
	return nil
}
func (s *runtimePortCompileStub) ListFiles(_ context.Context, _ ListFilesInput) ([]FileEntry, error) {
	return nil, nil
}
func (s *runtimePortCompileStub) ListModels(_ context.Context, _ ListModelsInput) ([]ModelEntry, error) {
	return nil, nil
}
func (s *runtimePortCompileStub) SetSessionModel(_ context.Context, _ SetSessionModelInput) error {
	return nil
}
func (s *runtimePortCompileStub) GetSessionModel(_ context.Context, _ GetSessionModelInput) (SessionModelResult, error) {
	return SessionModelResult{}, nil
}
func (s *runtimePortCompileStub) ListProviders(_ context.Context, _ ListProvidersInput) ([]ProviderOption, error) {
	return nil, nil
}
func (s *runtimePortCompileStub) CreateProvider(_ context.Context, _ CreateProviderInput) (ProviderSelectionResult, error) {
	return ProviderSelectionResult{}, nil
}
func (s *runtimePortCompileStub) DeleteProvider(_ context.Context, _ DeleteProviderInput) error {
	return nil
}
func (s *runtimePortCompileStub) SelectProviderModel(
	_ context.Context,
	_ SelectProviderModelInput,
) (ProviderSelectionResult, error) {
	return ProviderSelectionResult{}, nil
}
func (s *runtimePortCompileStub) ListMCPServers(_ context.Context, _ ListMCPServersInput) ([]MCPServerEntry, error) {
	return nil, nil
}
func (s *runtimePortCompileStub) UpsertMCPServer(_ context.Context, _ UpsertMCPServerInput) error {
	return nil
}
func (s *runtimePortCompileStub) SetMCPServerEnabled(_ context.Context, _ SetMCPServerEnabledInput) error {
	return nil
}
func (s *runtimePortCompileStub) DeleteMCPServer(_ context.Context, _ DeleteMCPServerInput) error {
	return nil
}

func (s *runtimePortCompileStub) ListSessionTodos(_ context.Context, _ ListSessionTodosInput) (TodoSnapshot, error) {
	return TodoSnapshot{}, nil
}

func (s *runtimePortCompileStub) GetRuntimeSnapshot(
	_ context.Context,
	_ GetRuntimeSnapshotInput,
) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{}, nil
}

func (s *runtimePortCompileStub) CreateSession(_ context.Context, _ CreateSessionInput) (string, error) {
	return "", nil
}

func (s *runtimePortCompileStub) ListCheckpoints(_ context.Context, _ ListCheckpointsInput) ([]CheckpointEntry, error) {
	return nil, nil
}

func (s *runtimePortCompileStub) RestoreCheckpoint(_ context.Context, _ CheckpointRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}

func (s *runtimePortCompileStub) UndoRestore(_ context.Context, _ UndoRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}

func (s *runtimePortCompileStub) CheckpointDiff(_ context.Context, _ CheckpointDiffInput) (CheckpointDiffResult, error) {
	return CheckpointDiffResult{}, nil
}

var _ RuntimePort = (*runtimePortCompileStub)(nil)
var _ TransportAdapter = (*Server)(nil)
var _ TransportAdapter = (*NetworkServer)(nil)
