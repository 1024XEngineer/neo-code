package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

// recordingPort is a RuntimePort fake that tracks which port handled each call.
// It satisfies all RuntimePort methods so we can verify MultiWorkspaceRuntime
// routes by workspaceHash to the correct underlying bundle.
type recordingPort struct {
	id     string
	events chan RuntimeEvent

	runCalls          atomic.Int32
	listSessionsCalls atomic.Int32
	executeSysCalls   atomic.Int32
	cancelCalls       atomic.Int32
	closed            atomic.Int32
	closeOnce         sync.Once

	closeErr error
}

func newRecordingPort(id string) *recordingPort {
	return &recordingPort{
		id:     id,
		events: make(chan RuntimeEvent, 8),
	}
}

func (p *recordingPort) cleanup() error {
	p.closeOnce.Do(func() {
		p.closed.Add(1)
		close(p.events)
	})
	return p.closeErr
}

func (p *recordingPort) Run(_ context.Context, _ RunInput) error {
	p.runCalls.Add(1)
	return nil
}

func (p *recordingPort) Compact(_ context.Context, _ CompactInput) (CompactResult, error) {
	return CompactResult{}, nil
}

func (p *recordingPort) ExecuteSystemTool(_ context.Context, _ ExecuteSystemToolInput) (tools.ToolResult, error) {
	p.executeSysCalls.Add(1)
	return tools.ToolResult{Content: p.id}, nil
}

func (p *recordingPort) ActivateSessionSkill(_ context.Context, _ SessionSkillMutationInput) error {
	return nil
}

func (p *recordingPort) DeactivateSessionSkill(_ context.Context, _ SessionSkillMutationInput) error {
	return nil
}

func (p *recordingPort) ListSessionSkills(_ context.Context, _ ListSessionSkillsInput) ([]SessionSkillState, error) {
	return nil, nil
}

func (p *recordingPort) ListAvailableSkills(_ context.Context, _ ListAvailableSkillsInput) ([]AvailableSkillState, error) {
	return nil, nil
}

func (p *recordingPort) ResolvePermission(_ context.Context, _ PermissionResolutionInput) error {
	return nil
}

func (p *recordingPort) CancelRun(_ context.Context, _ CancelInput) (bool, error) {
	p.cancelCalls.Add(1)
	return true, nil
}

func (p *recordingPort) Events() <-chan RuntimeEvent {
	return p.events
}

func (p *recordingPort) ListSessions(_ context.Context) ([]SessionSummary, error) {
	p.listSessionsCalls.Add(1)
	return []SessionSummary{{ID: p.id}}, nil
}

func (p *recordingPort) LoadSession(_ context.Context, _ LoadSessionInput) (Session, error) {
	return Session{ID: p.id}, nil
}

func (p *recordingPort) ListSessionTodos(_ context.Context, _ ListSessionTodosInput) (TodoSnapshot, error) {
	return TodoSnapshot{}, nil
}

func (p *recordingPort) GetRuntimeSnapshot(_ context.Context, _ GetRuntimeSnapshotInput) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{SessionID: p.id}, nil
}

func (p *recordingPort) CreateSession(_ context.Context, _ CreateSessionInput) (string, error) {
	return p.id, nil
}

func (p *recordingPort) DeleteSession(_ context.Context, _ DeleteSessionInput) (bool, error) {
	return true, nil
}

func (p *recordingPort) RenameSession(_ context.Context, _ RenameSessionInput) error {
	return nil
}

func (p *recordingPort) ListFiles(_ context.Context, _ ListFilesInput) ([]FileEntry, error) {
	return nil, nil
}

func (p *recordingPort) ListModels(_ context.Context, _ ListModelsInput) ([]ModelEntry, error) {
	return nil, nil
}

func (p *recordingPort) SetSessionModel(_ context.Context, _ SetSessionModelInput) error {
	return nil
}

func (p *recordingPort) GetSessionModel(_ context.Context, _ GetSessionModelInput) (SessionModelResult, error) {
	return SessionModelResult{}, nil
}

func (p *recordingPort) ListCheckpoints(_ context.Context, _ ListCheckpointsInput) ([]CheckpointEntry, error) {
	return nil, nil
}

func (p *recordingPort) RestoreCheckpoint(_ context.Context, _ CheckpointRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}

func (p *recordingPort) UndoRestore(_ context.Context, _ UndoRestoreInput) (CheckpointRestoreResult, error) {
	return CheckpointRestoreResult{}, nil
}

func (p *recordingPort) CheckpointDiff(_ context.Context, _ CheckpointDiffInput) (CheckpointDiffResult, error) {
	return CheckpointDiffResult{}, nil
}

// ManagementRuntimePort stubs
func (p *recordingPort) ListProviders(_ context.Context, _ ListProvidersInput) ([]ProviderOption, error) {
	return nil, nil
}
func (p *recordingPort) CreateProvider(_ context.Context, _ CreateProviderInput) (ProviderSelectionResult, error) {
	return ProviderSelectionResult{}, nil
}
func (p *recordingPort) DeleteProvider(_ context.Context, _ DeleteProviderInput) error {
	return nil
}
func (p *recordingPort) SelectProviderModel(_ context.Context, _ SelectProviderModelInput) (ProviderSelectionResult, error) {
	return ProviderSelectionResult{}, nil
}
func (p *recordingPort) ListMCPServers(_ context.Context, _ ListMCPServersInput) ([]MCPServerEntry, error) {
	return nil, nil
}
func (p *recordingPort) UpsertMCPServer(_ context.Context, _ UpsertMCPServerInput) error {
	return nil
}
func (p *recordingPort) SetMCPServerEnabled(_ context.Context, _ SetMCPServerEnabledInput) error {
	return nil
}
func (p *recordingPort) DeleteMCPServer(_ context.Context, _ DeleteMCPServerInput) error {
	return nil
}

// testBuilder captures the workdirs that buildPort was invoked with so tests can
// distinguish lazy builds from cache hits.
type testBuilder struct {
	mu        sync.Mutex
	workdirs  []string
	ports     map[string]*recordingPort
	buildErr  error
	buildErrs map[string]error
}

func newTestBuilder() *testBuilder {
	return &testBuilder{
		ports:     make(map[string]*recordingPort),
		buildErrs: make(map[string]error),
	}
}

func (b *testBuilder) build(_ context.Context, workdir string) (RuntimePort, func() error, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buildErr != nil {
		return nil, nil, b.buildErr
	}
	if err, ok := b.buildErrs[workdir]; ok {
		return nil, nil, err
	}

	b.workdirs = append(b.workdirs, workdir)
	port := newRecordingPort(workdir)
	b.ports[workdir] = port
	return port, port.cleanup, nil
}

func (b *testBuilder) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.workdirs)
}

func (b *testBuilder) portFor(workdir string) *recordingPort {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ports[workdir]
}

// setupIndex registers two workspaces (alpha, beta) under a temp baseDir and
// returns the index along with the registered records.
func setupIndex(t *testing.T) (*agentsession.WorkspaceIndex, agentsession.WorkspaceRecord, agentsession.WorkspaceRecord) {
	t.Helper()
	base := t.TempDir()
	alphaDir := filepath.Join(base, "alpha")
	betaDir := filepath.Join(base, "beta")
	for _, d := range []string{alphaDir, betaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	idx := agentsession.NewWorkspaceIndex(base)
	alpha, err := idx.Register(alphaDir, "alpha")
	if err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	beta, err := idx.Register(betaDir, "beta")
	if err != nil {
		t.Fatalf("register beta: %v", err)
	}
	return idx, alpha, beta
}

func ctxWithHash(t *testing.T, hash string) context.Context {
	t.Helper()
	state := NewConnectionWorkspaceState()
	state.SetWorkspaceHash(hash)
	return WithConnectionWorkspaceState(context.Background(), state)
}

func TestMultiWorkspaceRuntime_DefaultHashRouting(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	// Bare context (no workspace hash): falls back to defaultHash → alpha.
	if _, err := mw.ListSessions(context.Background()); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := builder.callCount(); got != 1 {
		t.Fatalf("buildPort calls = %d, want 1 lazy build for default hash", got)
	}
	port := builder.portFor(alpha.Path)
	if port == nil {
		t.Fatalf("expected port built for path %q", alpha.Path)
	}
	if got := port.listSessionsCalls.Load(); got != 1 {
		t.Fatalf("alpha port listSessions calls = %d, want 1", got)
	}
}

func TestMultiWorkspaceRuntime_NoHashConfigured(t *testing.T) {
	idx := agentsession.NewWorkspaceIndex(t.TempDir())
	builder := newTestBuilder()

	// No defaultHash, no context hash → must error.
	mw := NewMultiWorkspaceRuntime(idx, "", builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	if _, err := mw.ListSessions(context.Background()); err == nil {
		t.Fatalf("expected error when no hash is configured")
	}
	if got := builder.callCount(); got != 0 {
		t.Fatalf("buildPort should not be called when no hash, got %d", got)
	}
}

func TestMultiWorkspaceRuntime_ContextHashOverridesDefault(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	// Context hash points to beta — defaultHash is alpha but should be ignored.
	if _, err := mw.ListSessions(ctxWithHash(t, beta.Hash)); err != nil {
		t.Fatalf("ListSessions on beta: %v", err)
	}
	if builder.portFor(alpha.Path) != nil {
		t.Fatalf("alpha port should not be built when ctx hash is beta")
	}
	betaPort := builder.portFor(beta.Path)
	if betaPort == nil {
		t.Fatalf("beta port should be built")
	}
	if got := betaPort.listSessionsCalls.Load(); got != 1 {
		t.Fatalf("beta listSessions = %d, want 1", got)
	}

	// Subsequent call on alpha builds a separate bundle.
	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("ListSessions on alpha: %v", err)
	}
	if builder.portFor(alpha.Path) == nil {
		t.Fatalf("alpha port should now be built")
	}
	if got := builder.callCount(); got != 2 {
		t.Fatalf("buildPort calls = %d, want 2 (one per workspace)", got)
	}
}

func TestMultiWorkspaceRuntime_CachesBundle(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	for i := 0; i < 4; i++ {
		if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
			t.Fatalf("ListSessions[%d]: %v", i, err)
		}
	}
	if got := builder.callCount(); got != 1 {
		t.Fatalf("buildPort calls = %d, want 1 (cached)", got)
	}
	if got := builder.portFor(alpha.Path).listSessionsCalls.Load(); got != 4 {
		t.Fatalf("listSessions calls = %d, want 4", got)
	}
}

func TestMultiWorkspaceRuntime_UnknownHashErrors(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	_, err := mw.ListSessions(ctxWithHash(t, "deadbeef"))
	if err == nil {
		t.Fatalf("expected error for unknown hash")
	}
	if got := builder.callCount(); got != 0 {
		t.Fatalf("buildPort should not be invoked for unknown hash; got %d", got)
	}
}

func TestMultiWorkspaceRuntime_BuildErrorPropagates(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	builder.buildErrs[alpha.Path] = errors.New("boom")

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	_, err := mw.ListSessions(ctxWithHash(t, alpha.Hash))
	if err == nil {
		t.Fatalf("expected build error to surface")
	}
}

func TestMultiWorkspaceRuntime_PreloadSkipsRebuild(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()

	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	pre := newRecordingPort("preloaded")
	mw.PreloadWorkspaceBundle(alpha.Hash, pre, pre.cleanup)

	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := builder.callCount(); got != 0 {
		t.Fatalf("buildPort should not be called for preloaded bundle; got %d", got)
	}
	if got := pre.listSessionsCalls.Load(); got != 1 {
		t.Fatalf("preloaded port listSessions = %d, want 1", got)
	}
}

func TestMultiWorkspaceRuntime_PreloadIgnoresDuplicate(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	first := newRecordingPort("first")
	second := newRecordingPort("second")
	mw.PreloadWorkspaceBundle(alpha.Hash, first, first.cleanup)
	mw.PreloadWorkspaceBundle(alpha.Hash, second, second.cleanup)

	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := first.listSessionsCalls.Load(); got != 1 {
		t.Fatalf("first port should still be active, calls = %d", got)
	}
	if got := second.listSessionsCalls.Load(); got != 0 {
		t.Fatalf("second preload must be ignored, calls = %d", got)
	}
}

func TestMultiWorkspaceRuntime_SwitchWorkspaceValidates(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	if err := mw.SwitchWorkspace(context.Background(), beta.Hash); err != nil {
		t.Fatalf("SwitchWorkspace: %v", err)
	}
	// SwitchWorkspace pre-loads the target's runtime.
	if builder.portFor(beta.Path) == nil {
		t.Fatalf("beta port should be built after SwitchWorkspace")
	}

	if err := mw.SwitchWorkspace(context.Background(), "missing"); err == nil {
		t.Fatalf("SwitchWorkspace should reject unknown hash")
	}
}

func TestMultiWorkspaceRuntime_CreatePersistsIndex(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	newDir := t.TempDir()
	rec, err := mw.CreateWorkspace(newDir, "Gamma")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if rec.Name != "Gamma" {
		t.Fatalf("name = %q, want Gamma", rec.Name)
	}

	// Index must persist on Save. baseDir of the index is alpha.Path's parent.
	persisted := agentsession.NewWorkspaceIndex(filepath.Dir(alpha.Path))
	if err := persisted.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := persisted.Get(rec.Hash); !ok {
		t.Fatalf("CreateWorkspace did not persist index entry %s", rec.Hash)
	}
}

func TestMultiWorkspaceRuntime_RenameAndDeletePersist(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	if err := mw.RenameWorkspace(beta.Hash, "Beta-Renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Trigger lazy build of beta then delete it.
	if _, err := mw.ListSessions(ctxWithHash(t, beta.Hash)); err != nil {
		t.Fatalf("ListSessions beta: %v", err)
	}
	betaPort := builder.portFor(beta.Path)
	if betaPort == nil {
		t.Fatalf("beta port not built")
	}

	if err := mw.DeleteWorkspace(beta.Hash, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Bundle cleanup should have been invoked.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if betaPort.closed.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := betaPort.closed.Load(); got != 1 {
		t.Fatalf("beta cleanup should be invoked, got closed=%d", got)
	}

	// Persisted index reflects rename + delete.
	persisted := agentsession.NewWorkspaceIndex(filepath.Dir(alpha.Path))
	if err := persisted.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := persisted.Get(beta.Hash); ok {
		t.Fatalf("beta should be removed from persisted index")
	}
	if _, ok := persisted.Get(alpha.Hash); !ok {
		t.Fatalf("alpha should remain in persisted index")
	}
}

func TestMultiWorkspaceRuntime_DeleteUnknownErrors(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	if err := mw.DeleteWorkspace("missing", false); err == nil {
		t.Fatalf("expected error for unknown delete")
	}
}

func TestMultiWorkspaceRuntime_CloseCleansAllBundles(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)

	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("alpha lazy build: %v", err)
	}
	if _, err := mw.ListSessions(ctxWithHash(t, beta.Hash)); err != nil {
		t.Fatalf("beta lazy build: %v", err)
	}

	if err := mw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, ws := range []agentsession.WorkspaceRecord{alpha, beta} {
		port := builder.portFor(ws.Path)
		if port == nil {
			t.Fatalf("port not built for %s", ws.Path)
		}
		if got := port.closed.Load(); got != 1 {
			t.Fatalf("port %s closed=%d, want 1", ws.Path, got)
		}
	}
}

func TestMultiWorkspaceRuntime_CloseReturnsFirstError(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)

	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("lazy build: %v", err)
	}
	port := builder.portFor(alpha.Path)
	port.closeErr = errors.New("disk full")

	err := mw.Close()
	if err == nil {
		t.Fatalf("expected close error to surface")
	}
}

func TestMultiWorkspaceRuntime_EventsForwarded(t *testing.T) {
	idx, alpha, _ := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	// Trigger lazy build so the forwarder goroutine attaches.
	if _, err := mw.ListSessions(ctxWithHash(t, alpha.Hash)); err != nil {
		t.Fatalf("lazy build: %v", err)
	}
	port := builder.portFor(alpha.Path)

	go func() {
		port.events <- RuntimeEvent{Type: "session.update", SessionID: "s-1"}
	}()

	select {
	case ev := <-mw.Events():
		if ev.SessionID != "s-1" {
			t.Fatalf("expected session_id s-1, got %q", ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("event not forwarded within 2s")
	}
}

func TestMultiWorkspaceRuntime_RoutingMatrix(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	alphaCtx := ctxWithHash(t, alpha.Hash)
	betaCtx := ctxWithHash(t, beta.Hash)

	// Run() routes by ctx.
	if err := mw.Run(alphaCtx, RunInput{}); err != nil {
		t.Fatalf("Run alpha: %v", err)
	}
	if err := mw.Run(betaCtx, RunInput{}); err != nil {
		t.Fatalf("Run beta: %v", err)
	}
	// CancelRun
	if _, err := mw.CancelRun(betaCtx, CancelInput{}); err != nil {
		t.Fatalf("CancelRun beta: %v", err)
	}
	// ExecuteSystemTool
	if _, err := mw.ExecuteSystemTool(alphaCtx, ExecuteSystemToolInput{}); err != nil {
		t.Fatalf("ExecuteSystemTool alpha: %v", err)
	}

	alphaPort := builder.portFor(alpha.Path)
	betaPort := builder.portFor(beta.Path)
	if alphaPort == nil || betaPort == nil {
		t.Fatalf("ports not built; alpha=%v beta=%v", alphaPort, betaPort)
	}
	if got := alphaPort.runCalls.Load(); got != 1 {
		t.Fatalf("alpha Run calls = %d, want 1", got)
	}
	if got := betaPort.runCalls.Load(); got != 1 {
		t.Fatalf("beta Run calls = %d, want 1", got)
	}
	if got := betaPort.cancelCalls.Load(); got != 1 {
		t.Fatalf("beta cancel calls = %d, want 1", got)
	}
	if got := alphaPort.executeSysCalls.Load(); got != 1 {
		t.Fatalf("alpha ExecuteSystemTool calls = %d, want 1", got)
	}
}

func TestMultiWorkspaceRuntime_ListWorkspacesMatchesIndex(t *testing.T) {
	idx, alpha, beta := setupIndex(t)
	builder := newTestBuilder()
	mw := NewMultiWorkspaceRuntime(idx, alpha.Hash, builder.build)
	t.Cleanup(func() { _ = mw.Close() })

	got := mw.ListWorkspaces()
	if len(got) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(got))
	}
	have := map[string]bool{}
	for _, r := range got {
		have[r.Hash] = true
	}
	if !have[alpha.Hash] || !have[beta.Hash] {
		t.Fatalf("missing workspace; got %v", have)
	}
}

// guard against future drift: MultiWorkspaceRuntime must implement RuntimePort and ManagementRuntimePort.
var _ RuntimePort = (*MultiWorkspaceRuntime)(nil)
var _ ManagementRuntimePort = (*MultiWorkspaceRuntime)(nil)

// guard helper: ensure recordingPort builds correctly under sync access.
func TestRecordingPort_Concurrent(t *testing.T) {
	p := newRecordingPort("c")
	wg := sync.WaitGroup{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.ListSessions(context.Background())
		}()
	}
	wg.Wait()
	if got := p.listSessionsCalls.Load(); got != 16 {
		t.Fatalf("expected 16 calls, got %d", got)
	}
	if err := p.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
