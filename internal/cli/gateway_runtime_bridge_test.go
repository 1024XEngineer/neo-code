package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/gateway"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
)

type runtimeStub struct {
	submitInput     agentruntime.PrepareInput
	submitErr       error
	compactInput    agentruntime.CompactInput
	compactResult   agentruntime.CompactResult
	compactErr      error
	systemToolInput agentruntime.SystemToolInput
	systemToolRes   tools.ToolResult
	systemToolErr   error
	permissionInput agentruntime.PermissionResolutionInput
	permissionErr   error
	activateSession struct {
		sessionID string
		skillID   string
	}
	activateSessionErr error
	deactivateSession  struct {
		sessionID string
		skillID   string
	}
	deactivateSessionErr  error
	sessionSkills         []agentruntime.SessionSkillState
	sessionSkillsErr      error
	listSessionSkillsID   string
	availableSkills       []agentruntime.AvailableSkillState
	availableSkillsErr    error
	listAvailableSkillsID string
	cancelReturn          bool
	eventsCh              chan agentruntime.RuntimeEvent
	sessionList           []agentsession.Summary
	listErr               error
	loadID                string
	loadSession           agentsession.Session
	loadErr               error
	createID              string
	createSession         agentsession.Session
	createErr             error
	listTodosSessionID    string
	listTodosSnapshot     agentruntime.TodoSnapshot
	listTodosErr          error
	getSnapshotSessionID  string
	getSnapshotResult     agentruntime.RuntimeSnapshot
	getSnapshotErr        error
}

const testBridgeSubjectID = bridgeLocalSubjectID

func (s *runtimeStub) Submit(_ context.Context, input agentruntime.PrepareInput) error {
	s.submitInput = input
	return s.submitErr
}

func (s *runtimeStub) PrepareUserInput(context.Context, agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	return agentruntime.UserInput{}, nil
}

func (s *runtimeStub) Run(context.Context, agentruntime.UserInput) error {
	return nil
}

func (s *runtimeStub) Compact(_ context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	s.compactInput = input
	return s.compactResult, s.compactErr
}

func (s *runtimeStub) ExecuteSystemTool(_ context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	s.systemToolInput = input
	return s.systemToolRes, s.systemToolErr
}

func (s *runtimeStub) ResolvePermission(_ context.Context, input agentruntime.PermissionResolutionInput) error {
	s.permissionInput = input
	return s.permissionErr
}

func (s *runtimeStub) CancelActiveRun() bool {
	return s.cancelReturn
}

func (s *runtimeStub) CancelRun(string) bool {
	return s.cancelReturn
}

func (s *runtimeStub) Events() <-chan agentruntime.RuntimeEvent {
	return s.eventsCh
}

func (s *runtimeStub) ListSessions(context.Context) ([]agentsession.Summary, error) {
	return s.sessionList, s.listErr
}

func (s *runtimeStub) LoadSession(_ context.Context, id string) (agentsession.Session, error) {
	s.loadID = id
	return s.loadSession, s.loadErr
}

func (s *runtimeStub) CreateSession(_ context.Context, id string) (agentsession.Session, error) {
	s.createID = id
	return s.createSession, s.createErr
}

func (s *runtimeStub) ActivateSessionSkill(_ context.Context, sessionID string, skillID string) error {
	s.activateSession.sessionID = sessionID
	s.activateSession.skillID = skillID
	return s.activateSessionErr
}

func (s *runtimeStub) DeactivateSessionSkill(_ context.Context, sessionID string, skillID string) error {
	s.deactivateSession.sessionID = sessionID
	s.deactivateSession.skillID = skillID
	return s.deactivateSessionErr
}

func (s *runtimeStub) ListSessionSkills(_ context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	s.listSessionSkillsID = sessionID
	return s.sessionSkills, s.sessionSkillsErr
}

func (s *runtimeStub) ListAvailableSkills(_ context.Context, sessionID string) ([]agentruntime.AvailableSkillState, error) {
	s.listAvailableSkillsID = sessionID
	return s.availableSkills, s.availableSkillsErr
}
func (s *runtimeStub) ListTodos(_ context.Context, sessionID string) (agentruntime.TodoSnapshot, error) {
	s.listTodosSessionID = sessionID
	return s.listTodosSnapshot, s.listTodosErr
}
func (s *runtimeStub) GetRuntimeSnapshot(_ context.Context, sessionID string) (agentruntime.RuntimeSnapshot, error) {
	s.getSnapshotSessionID = sessionID
	return s.getSnapshotResult, s.getSnapshotErr
}
func (s *runtimeStub) DeleteSession(_ context.Context, _ string) error {
	return nil
}
func (s *runtimeStub) UpdateSessionState(_ context.Context, _ agentsession.UpdateSessionStateInput) error {
	return nil
}

type runtimeWithoutCreator struct {
	base *runtimeStub
}

func (r *runtimeWithoutCreator) Submit(ctx context.Context, input agentruntime.PrepareInput) error {
	return r.base.Submit(ctx, input)
}
func (r *runtimeWithoutCreator) PrepareUserInput(ctx context.Context, input agentruntime.PrepareInput) (agentruntime.UserInput, error) {
	return r.base.PrepareUserInput(ctx, input)
}
func (r *runtimeWithoutCreator) Run(ctx context.Context, input agentruntime.UserInput) error {
	return r.base.Run(ctx, input)
}
func (r *runtimeWithoutCreator) Compact(ctx context.Context, input agentruntime.CompactInput) (agentruntime.CompactResult, error) {
	return r.base.Compact(ctx, input)
}
func (r *runtimeWithoutCreator) ExecuteSystemTool(ctx context.Context, input agentruntime.SystemToolInput) (tools.ToolResult, error) {
	return r.base.ExecuteSystemTool(ctx, input)
}
func (r *runtimeWithoutCreator) ResolvePermission(ctx context.Context, input agentruntime.PermissionResolutionInput) error {
	return r.base.ResolvePermission(ctx, input)
}
func (r *runtimeWithoutCreator) CancelActiveRun() bool {
	return r.base.CancelActiveRun()
}
func (r *runtimeWithoutCreator) Events() <-chan agentruntime.RuntimeEvent {
	return r.base.Events()
}
func (r *runtimeWithoutCreator) ListSessions(ctx context.Context) ([]agentsession.Summary, error) {
	return r.base.ListSessions(ctx)
}
func (r *runtimeWithoutCreator) LoadSession(ctx context.Context, id string) (agentsession.Session, error) {
	return r.base.LoadSession(ctx, id)
}
func (r *runtimeWithoutCreator) ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return r.base.ActivateSessionSkill(ctx, sessionID, skillID)
}
func (r *runtimeWithoutCreator) DeactivateSessionSkill(ctx context.Context, sessionID string, skillID string) error {
	return r.base.DeactivateSessionSkill(ctx, sessionID, skillID)
}
func (r *runtimeWithoutCreator) ListSessionSkills(ctx context.Context, sessionID string) ([]agentruntime.SessionSkillState, error) {
	return r.base.ListSessionSkills(ctx, sessionID)
}

func (r *runtimeWithoutCreator) ListAvailableSkills(
	ctx context.Context,
	sessionID string,
) ([]agentruntime.AvailableSkillState, error) {
	return r.base.ListAvailableSkills(ctx, sessionID)
}

type bridgeSessionStoreStub struct {
	deleteFn func(ctx context.Context, id string) error
	updateFn func(ctx context.Context, input agentsession.UpdateSessionStateInput) error
}

func (s *bridgeSessionStoreStub) DeleteSession(ctx context.Context, id string) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, id)
	}
	return nil
}
func (s *bridgeSessionStoreStub) UpdateSessionState(ctx context.Context, input agentsession.UpdateSessionStateInput) error {
	if s.updateFn != nil {
		return s.updateFn(ctx, input)
	}
	return nil
}

var testSessionStore bridgeSessionStore = &bridgeSessionStoreStub{}

func TestNewGatewayRuntimePortBridgeRuntimeUnavailable(t *testing.T) {
	bridge, err := newGatewayRuntimePortBridge(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}
	if bridge != nil {
		t.Fatal("expected nil bridge when runtime is nil")
	}

	var nilBridge *gatewayRuntimePortBridge
	if err := nilBridge.Run(context.Background(), gateway.RunInput{}); err == nil {
		t.Fatal("expected run error for nil bridge")
	}
	if _, err := nilBridge.Compact(context.Background(), gateway.CompactInput{}); err == nil {
		t.Fatal("expected compact error for nil bridge")
	}
	if _, err := nilBridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		ToolName:  "memo_list",
	}); err == nil {
		t.Fatal("expected execute_system_tool error for nil bridge")
	}
	if err := nilBridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected activate_session_skill error for nil bridge")
	}
	if err := nilBridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected deactivate_session_skill error for nil bridge")
	}
	if _, err := nilBridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_session_skills error for nil bridge")
	}
	if _, err := nilBridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected list_available_skills error for nil bridge")
	}
	if err := nilBridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{}); err == nil {
		t.Fatal("expected resolve_permission error for nil bridge")
	}
	if _, err := nilBridge.CancelRun(context.Background(), gateway.CancelInput{SubjectID: testBridgeSubjectID, RunID: "run-1"}); err == nil {
		t.Fatal("expected cancel_run error for nil bridge")
	}
	if nilBridge.Events() != nil {
		t.Fatal("events channel should be nil for nil bridge")
	}
	if _, err := nilBridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error for nil bridge")
	}
	if _, err := nilBridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected load_session error for nil bridge")
	}
	if _, err := nilBridge.CreateSession(context.Background(), gateway.CreateSessionInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected create_session error for nil bridge")
	}
	if err := nilBridge.Close(); err != nil {
		t.Fatalf("close nil bridge: %v", err)
	}
}

func TestGatewayRuntimePortBridgeRuntimeMethods(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		cancelReturn: true,
		compactResult: agentruntime.CompactResult{
			Applied:        true,
			BeforeChars:    200,
			AfterChars:     100,
			SavedRatio:     0.5,
			TriggerMode:    "manual",
			TranscriptID:   "tx-1",
			TranscriptPath: "/tmp/tx-1.md",
		},
		systemToolRes: tools.ToolResult{
			ToolCallID: "call-system-1",
			Name:       "memo_list",
			Content:    "memo ok",
		},
		sessionSkills: []agentruntime.SessionSkillState{
			{
				SkillID: "go-review",
				Descriptor: &skills.Descriptor{
					ID:          "go-review",
					Name:        "Go Review",
					Description: "Review Go code",
					Version:     "v1",
					Source: skills.Source{
						Kind: skills.SourceKindLocal,
					},
					Scope: skills.ScopeSession,
				},
			},
			{
				SkillID: "missing-skill",
				Missing: true,
			},
		},
		availableSkills: []agentruntime.AvailableSkillState{
			{
				Descriptor: skills.Descriptor{
					ID:          "go-review",
					Name:        "Go Review",
					Description: "Review Go code",
					Version:     "v1",
					Source: skills.Source{
						Kind: skills.SourceKindLocal,
					},
					Scope: skills.ScopeSession,
				},
				Active: true,
			},
		},
		sessionList: []agentsession.Summary{
			{
				ID:        "  session-1  ",
				Title:     "  title  ",
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		loadSession: agentsession.Session{
			ID:        "  session-1  ",
			Title:     "  title  ",
			Workdir:   "  /tmp/work  ",
			CreatedAt: now,
			UpdatedAt: now,
			Messages: []providertypes.Message{
				{
					Role: " assistant ",
					Parts: []providertypes.ContentPart{
						{Kind: providertypes.ContentPartText, Text: "  hello  "},
						{Kind: providertypes.ContentPartImage},
					},
					ToolCalls: []providertypes.ToolCall{
						{ID: " tc-1 ", Name: " bash ", Arguments: `{"cmd":"pwd"}`},
					},
					ToolCallID: " call-1 ",
					IsError:    true,
				},
			},
		},
	}

	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer func() {
		if closeErr := bridge.Close(); closeErr != nil {
			t.Fatalf("close bridge: %v", closeErr)
		}
	}()

	runInput := gateway.RunInput{
		SubjectID: testBridgeSubjectID,
		RequestID: " request-1 ",
		SessionID: " session-1 ",
		RunID:     " run-1 ",
		InputText: " base ",
		InputParts: []gateway.InputPart{
			{Type: gateway.InputPartTypeText, Text: " extra "},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: " /tmp/a.png ", MimeType: " image/png "}},
		},
		Workdir: " /tmp/work ",
	}
	if err := bridge.Run(context.Background(), runInput); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stub.submitInput.SessionID != "session-1" {
		t.Fatalf("submit session_id = %q, want %q", stub.submitInput.SessionID, "session-1")
	}
	if stub.submitInput.RunID != "run-1" {
		t.Fatalf("submit run_id = %q, want %q", stub.submitInput.RunID, "run-1")
	}
	if stub.submitInput.Workdir != "/tmp/work" {
		t.Fatalf("submit workdir = %q, want %q", stub.submitInput.Workdir, "/tmp/work")
	}
	if stub.submitInput.Text != "base\nextra" {
		t.Fatalf("submit text = %q, want %q", stub.submitInput.Text, "base\nextra")
	}
	if len(stub.submitInput.Images) != 1 || stub.submitInput.Images[0].Path != "/tmp/a.png" || stub.submitInput.Images[0].MimeType != "image/png" {
		t.Fatalf("submit images = %#v, want single image with trimmed path/mime", stub.submitInput.Images)
	}

	compactResult, err := bridge.Compact(context.Background(), gateway.CompactInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		RunID:     " run-1 ",
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if stub.compactInput.SessionID != "session-1" || stub.compactInput.RunID != "run-1" {
		t.Fatalf("compact input = %#v, want trimmed session/run ids", stub.compactInput)
	}
	if !compactResult.Applied || compactResult.BeforeChars != 200 || compactResult.AfterChars != 100 || compactResult.SavedRatio != 0.5 {
		t.Fatalf("compact result = %#v", compactResult)
	}

	systemToolResult, err := bridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		RunID:     " run-1 ",
		Workdir:   " /tmp/work ",
		ToolName:  " memo_list ",
		Arguments: []byte(`{"limit":10}`),
	})
	if err != nil {
		t.Fatalf("execute_system_tool: %v", err)
	}
	if stub.systemToolInput.SessionID != "session-1" || stub.systemToolInput.RunID != "run-1" {
		t.Fatalf("system tool input ids = %#v, want trimmed session/run ids", stub.systemToolInput)
	}
	if stub.systemToolInput.Workdir != "/tmp/work" || stub.systemToolInput.ToolName != "memo_list" {
		t.Fatalf("system tool input = %#v, want trimmed workdir/tool_name", stub.systemToolInput)
	}
	if string(stub.systemToolInput.Arguments) != `{"limit":10}` {
		t.Fatalf("system tool arguments = %s, want {\"limit\":10}", string(stub.systemToolInput.Arguments))
	}
	if systemToolResult.Content != "memo ok" || systemToolResult.Name != "memo_list" {
		t.Fatalf("system tool result = %#v, want stubbed result", systemToolResult)
	}

	if err := bridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		SkillID:   " go-review ",
	}); err != nil {
		t.Fatalf("activate_session_skill: %v", err)
	}
	if stub.activateSession.sessionID != "session-1" || stub.activateSession.skillID != "go-review" {
		t.Fatalf("activate skill input = %#v, want trimmed session/skill ids", stub.activateSession)
	}

	if err := bridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
		SkillID:   " go-review ",
	}); err != nil {
		t.Fatalf("deactivate_session_skill: %v", err)
	}
	if stub.deactivateSession.sessionID != "session-1" || stub.deactivateSession.skillID != "go-review" {
		t.Fatalf("deactivate skill input = %#v, want trimmed session/skill ids", stub.deactivateSession)
	}

	sessionSkills, err := bridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatalf("list_session_skills: %v", err)
	}
	if stub.listSessionSkillsID != "session-1" {
		t.Fatalf("list session skills session_id = %q, want %q", stub.listSessionSkillsID, "session-1")
	}
	if len(sessionSkills) != 2 || sessionSkills[0].SkillID != "go-review" || sessionSkills[1].SkillID != "missing-skill" {
		t.Fatalf("session skills = %#v, want mapped runtime states", sessionSkills)
	}
	if sessionSkills[0].Descriptor == nil || sessionSkills[0].Descriptor.ID != "go-review" {
		t.Fatalf("session skill descriptor = %#v, want go-review descriptor", sessionSkills[0].Descriptor)
	}
	if !sessionSkills[1].Missing {
		t.Fatalf("missing session skill should keep missing=true, got %#v", sessionSkills[1])
	}

	availableSkills, err := bridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatalf("list_available_skills: %v", err)
	}
	if stub.listAvailableSkillsID != "session-1" {
		t.Fatalf("list available skills session_id = %q, want %q", stub.listAvailableSkillsID, "session-1")
	}
	if len(availableSkills) != 1 || availableSkills[0].Descriptor.ID != "go-review" || !availableSkills[0].Active {
		t.Fatalf("available skills = %#v, want one active go-review skill", availableSkills)
	}

	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{
		SubjectID: testBridgeSubjectID,
		RequestID: " request-1 ",
		Decision:  gateway.PermissionResolutionAllowSession,
	}); err != nil {
		t.Fatalf("resolve_permission: %v", err)
	}
	if stub.permissionInput.RequestID != "request-1" || string(stub.permissionInput.Decision) != "allow_session" {
		t.Fatalf("permission input = %#v, want trimmed request id and allow_session", stub.permissionInput)
	}

	canceled, err := bridge.CancelRun(context.Background(), gateway.CancelInput{
		SubjectID: testBridgeSubjectID,
		RunID:     " run-1 ",
	})
	if err != nil {
		t.Fatalf("cancel_run: %v", err)
	}
	if !canceled {
		t.Fatal("cancel_run should return stub value true")
	}

	sessions, err := bridge.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list_sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" || sessions[0].Title != "title" {
		t.Fatalf("sessions = %#v, want one trimmed session summary", sessions)
	}

	stub.sessionList = nil
	emptySessions, err := bridge.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list empty sessions: %v", err)
	}
	if emptySessions != nil {
		t.Fatalf("empty session list = %#v, want nil", emptySessions)
	}

	session, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatalf("load_session: %v", err)
	}
	if stub.loadID != "session-1" {
		t.Fatalf("load id = %q, want %q", stub.loadID, "session-1")
	}
	if session.ID != "session-1" || session.Title != "title" || session.Workdir != "/tmp/work" {
		t.Fatalf("loaded session = %#v, want trimmed fields", session)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("session messages len = %d, want 1", len(session.Messages))
	}
	if session.Messages[0].Content != "hello\n[image]" {
		t.Fatalf("rendered message content = %q, want %q", session.Messages[0].Content, "hello\n[image]")
	}
	if len(session.Messages[0].ToolCalls) != 1 || session.Messages[0].ToolCalls[0].Name != "bash" {
		t.Fatalf("message tool calls = %#v, want trimmed tool call", session.Messages[0].ToolCalls)
	}
}

func TestGatewayRuntimePortBridgeListSessionTodosAndSnapshot(t *testing.T) {
	t.Run("list todos via runtime todo lister", func(t *testing.T) {
		stub := &runtimeStub{
			listTodosSnapshot: agentruntime.TodoSnapshot{
				Summary: agentruntime.TodoSummary{Total: 1, RequiredTotal: 1, RequiredCompleted: 1},
				Items: []agentruntime.TodoViewItem{
					{ID: "todo-1", Content: "done", Status: "completed", Required: true, Revision: 2},
				},
			},
		}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("newGatewayRuntimePortBridge() error = %v", err)
		}
		t.Cleanup(func() { _ = bridge.Close() })

		snapshot, err := bridge.ListSessionTodos(context.Background(), gateway.ListSessionTodosInput{
			SubjectID: testBridgeSubjectID,
			SessionID: " session-1 ",
		})
		if err != nil {
			t.Fatalf("ListSessionTodos() error = %v", err)
		}
		if stub.listTodosSessionID != "session-1" {
			t.Fatalf("listTodos session = %q, want %q", stub.listTodosSessionID, "session-1")
		}
		if snapshot.Summary.RequiredCompleted != 1 || len(snapshot.Items) != 1 || snapshot.Items[0].ID != "todo-1" {
			t.Fatalf("snapshot = %#v", snapshot)
		}
	})

	t.Run("list todos fallback from session todos", func(t *testing.T) {
		required := true
		stub := &runtimeWithoutCreator{
			base: &runtimeStub{
				loadSession: agentsession.Session{
					Todos: []agentsession.TodoItem{
						{ID: "todo-2", Content: "x", Status: agentsession.TodoStatusPending, Required: &required, Revision: 9},
					},
				},
			},
		}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("newGatewayRuntimePortBridge() error = %v", err)
		}
		t.Cleanup(func() { _ = bridge.Close() })

		snapshot, err := bridge.ListSessionTodos(context.Background(), gateway.ListSessionTodosInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "session-fallback",
		})
		if err != nil {
			t.Fatalf("ListSessionTodos() fallback error = %v", err)
		}
		if len(snapshot.Items) != 1 || snapshot.Summary.RequiredOpen != 1 {
			t.Fatalf("fallback snapshot = %#v", snapshot)
		}
	})

	t.Run("get runtime snapshot", func(t *testing.T) {
		stub := &runtimeStub{
			getSnapshotResult: agentruntime.RuntimeSnapshot{
				RunID:     "run-1",
				SessionID: "session-2",
				Phase:     "acceptance",
				TaskKind:  "workspace_write",
				Decision:  agentruntime.DecisionSnapshot{Status: "continue", StopReason: "unverified_write"},
				SubAgents: agentruntime.SubAgentSnapshot{StartedCount: 1, CompletedCount: 1, FailedCount: 0},
			},
		}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("newGatewayRuntimePortBridge() error = %v", err)
		}
		t.Cleanup(func() { _ = bridge.Close() })

		snapshot, err := bridge.GetRuntimeSnapshot(context.Background(), gateway.GetRuntimeSnapshotInput{
			SubjectID: testBridgeSubjectID,
			SessionID: " session-2 ",
		})
		if err != nil {
			t.Fatalf("GetRuntimeSnapshot() error = %v", err)
		}
		if stub.getSnapshotSessionID != "session-2" {
			t.Fatalf("snapshot session = %q, want %q", stub.getSnapshotSessionID, "session-2")
		}
		if snapshot.RunID != "run-1" || snapshot.Decision["status"] != "continue" {
			t.Fatalf("snapshot = %#v", snapshot)
		}
	})
}

func TestGatewayRuntimePortBridgeLoadSessionNotFoundBranches(t *testing.T) {
	t.Parallel()

	base := &runtimeStub{
		loadErr: agentsession.ErrSessionNotFound,
	}
	bridgeWithoutCreator, err := newGatewayRuntimePortBridge(context.Background(), &runtimeWithoutCreator{base: base}, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge without creator: %v", err)
	}
	t.Cleanup(func() { _ = bridgeWithoutCreator.Close() })

	if _, err := bridgeWithoutCreator.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); !errors.Is(err, gateway.ErrRuntimeResourceNotFound) {
		t.Fatalf("expected ErrRuntimeResourceNotFound, got %v", err)
	}

	stub := &runtimeStub{
		loadErr:   os.ErrNotExist,
		createErr: errors.New("create failed"),
	}
	bridgeWithCreator, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge with creator: %v", err)
	}
	t.Cleanup(func() { _ = bridgeWithCreator.Close() })

	if _, err := bridgeWithCreator.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-2",
	}); err == nil || err.Error() != "create failed" {
		t.Fatalf("expected create failed error, got %v", err)
	}
}

func TestIsRuntimeNotFoundErrorIncludesOSErrNotExist(t *testing.T) {
	t.Parallel()

	if !isRuntimeNotFoundError(os.ErrNotExist) {
		t.Fatalf("os.ErrNotExist should be treated as runtime not found")
	}
	if !isRuntimeNotFoundError(agentsession.ErrSessionNotFound) {
		t.Fatalf("ErrSessionNotFound should be treated as runtime not found")
	}
	if isRuntimeNotFoundError(errors.New("session not found")) {
		t.Fatalf("plain string not-found error should not be treated as runtime not found")
	}
}

func TestGatewayRuntimePortBridgeRuntimeMethodErrors(t *testing.T) {
	stub := &runtimeStub{
		submitErr:            errors.New("submit failed"),
		compactErr:           errors.New("compact failed"),
		systemToolErr:        errors.New("system tool failed"),
		permissionErr:        errors.New("permission failed"),
		activateSessionErr:   errors.New("activate skill failed"),
		deactivateSessionErr: errors.New("deactivate skill failed"),
		sessionSkillsErr:     errors.New("list session skills failed"),
		availableSkillsErr:   errors.New("list available skills failed"),
		listErr:              errors.New("list failed"),
		loadErr:              errors.New("load failed"),
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	if err := bridge.Run(context.Background(), gateway.RunInput{SubjectID: testBridgeSubjectID}); err == nil {
		t.Fatal("expected run error from runtime")
	}
	if _, err := bridge.Compact(context.Background(), gateway.CompactInput{SubjectID: testBridgeSubjectID}); err == nil {
		t.Fatal("expected compact error from runtime")
	}
	if _, err := bridge.ExecuteSystemTool(context.Background(), gateway.ExecuteSystemToolInput{
		SubjectID: testBridgeSubjectID,
		ToolName:  "memo_list",
	}); err == nil {
		t.Fatal("expected execute_system_tool error from runtime")
	}
	if err := bridge.ActivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected activate_session_skill error from runtime")
	}
	if err := bridge.DeactivateSessionSkill(context.Background(), gateway.SessionSkillMutationInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		SkillID:   "go-review",
	}); err == nil {
		t.Fatal("expected deactivate_session_skill error from runtime")
	}
	if _, err := bridge.ListSessionSkills(context.Background(), gateway.ListSessionSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_session_skills error from runtime")
	}
	if _, err := bridge.ListAvailableSkills(context.Background(), gateway.ListAvailableSkillsInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected list_available_skills error from runtime")
	}
	if err := bridge.ResolvePermission(context.Background(), gateway.PermissionResolutionInput{
		SubjectID: testBridgeSubjectID,
	}); err == nil {
		t.Fatal("expected resolve_permission error from runtime")
	}
	if _, err := bridge.ListSessions(context.Background()); err == nil {
		t.Fatal("expected list_sessions error from runtime")
	}
	if _, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	}); err == nil {
		t.Fatal("expected load_session error from runtime")
	}
}

func TestGatewayRuntimePortBridgeLoadSessionUpsertWhenMissing(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		loadErr: agentsession.ErrSessionNotFound,
		createSession: agentsession.Session{
			ID:        "session-new",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	session, err := bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-new ",
	})
	if err != nil {
		t.Fatalf("load_session upsert: %v", err)
	}
	if stub.loadID != "session-new" {
		t.Fatalf("load id = %q, want %q", stub.loadID, "session-new")
	}
	if stub.createID != "session-new" {
		t.Fatalf("create id = %q, want %q", stub.createID, "session-new")
	}
	if session.ID != "session-new" || session.Title != "New Session" || session.Workdir != "/tmp/work" {
		t.Fatalf("upsert session = %#v, want created session snapshot", session)
	}
}

func TestGatewayRuntimePortBridgeLoadSessionNoUpsertOnPlainStringNotFoundError(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		loadErr: errors.New("open sessions/session-new.json: no such file"),
		createSession: agentsession.Session{
			ID:        "session-new",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	_, err = bridge.LoadSession(context.Background(), gateway.LoadSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-new ",
	})
	if err == nil || err.Error() != "open sessions/session-new.json: no such file" {
		t.Fatalf("expected original string error passthrough, got %v", err)
	}
	if stub.createID != "" {
		t.Fatalf("create should not be called for plain string error, got createID=%q", stub.createID)
	}
}

func TestGatewayRuntimePortBridgeCreateSession(t *testing.T) {
	now := time.Now()
	stub := &runtimeStub{
		createSession: agentsession.Session{
			ID:        "session-created",
			Title:     "New Session",
			Workdir:   "/tmp/work",
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, stub)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	sessionID, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: " session-created ",
	})
	if err != nil {
		t.Fatalf("create_session: %v", err)
	}
	if stub.createID != "session-created" {
		t.Fatalf("create id = %q, want %q", stub.createID, "session-created")
	}
	if sessionID != "session-created" {
		t.Fatalf("session_id = %q, want %q", sessionID, "session-created")
	}
}

func TestGatewayRuntimePortBridgeCreateSessionBranches(t *testing.T) {
	t.Run("subject denied", func(t *testing.T) {
		stub := &runtimeStub{}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, stub)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		if _, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
			SubjectID: "external_subject",
		}); !errors.Is(err, gateway.ErrRuntimeAccessDenied) {
			t.Fatalf("expected ErrRuntimeAccessDenied, got %v", err)
		}
	})

	t.Run("runtime no creator", func(t *testing.T) {
		base := &runtimeStub{}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), &runtimeWithoutCreator{base: base}, base)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		if _, err := bridge.CreateSession(context.Background(), gateway.CreateSessionInput{
			SubjectID: testBridgeSubjectID,
		}); err == nil {
			t.Fatal("expected runtime creator unavailable error")
		}
	})
}

func TestGatewayRuntimePortBridgeRunEventBridge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := make(chan agentruntime.RuntimeEvent, 3)
	stub := &runtimeStub{eventsCh: source}
	bridge, err := newGatewayRuntimePortBridge(ctx, stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	source <- agentruntime.RuntimeEvent{
		Type:           agentruntime.EventAgentChunk,
		RunID:          " run-1 ",
		SessionID:      " session-1 ",
		Turn:           3,
		Phase:          " thinking ",
		PayloadVersion: 2,
		Payload:        map[string]any{"k": "v"},
	}
	source <- agentruntime.RuntimeEvent{Type: agentruntime.EventAgentDone, RunID: "run-1", SessionID: "session-1"}
	source <- agentruntime.RuntimeEvent{Type: agentruntime.EventError, RunID: "run-1", SessionID: "session-1"}
	close(source)

	events := make([]gateway.RuntimeEvent, 0, 3)
	for event := range bridge.Events() {
		events = append(events, event)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[0].Type != gateway.RuntimeEventTypeRunProgress {
		t.Fatalf("event[0] type = %q, want %q", events[0].Type, gateway.RuntimeEventTypeRunProgress)
	}
	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("event payload type = %T, want map[string]any", events[0].Payload)
	}
	if payload["runtime_event_type"] != string(agentruntime.EventAgentChunk) {
		t.Fatalf("runtime_event_type = %#v, want %q", payload["runtime_event_type"], agentruntime.EventAgentChunk)
	}
	if payload["phase"] != "thinking" {
		t.Fatalf("payload phase = %#v, want %q", payload["phase"], "thinking")
	}
	if events[1].Type != gateway.RuntimeEventTypeRunDone {
		t.Fatalf("event[1] type = %q, want %q", events[1].Type, gateway.RuntimeEventTypeRunDone)
	}
	if events[2].Type != gateway.RuntimeEventTypeRunError {
		t.Fatalf("event[2] type = %q, want %q", events[2].Type, gateway.RuntimeEventTypeRunError)
	}
}

func TestGatewayRuntimePortBridgeStopsOnCloseAndContextCancel(t *testing.T) {
	source := make(chan agentruntime.RuntimeEvent)
	stub := &runtimeStub{eventsCh: source}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatalf("close bridge: %v", err)
	}
	select {
	case _, ok := <-bridge.Events():
		if ok {
			t.Fatal("events should be closed after bridge close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed events after bridge close")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelBridge, err := newGatewayRuntimePortBridge(cancelCtx, &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent)}, testSessionStore)
	if err != nil {
		t.Fatalf("new cancel bridge: %v", err)
	}
	select {
	case _, ok := <-cancelBridge.Events():
		if ok {
			t.Fatal("events should be closed when context is canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed events after context cancel")
	}

	nilCtxBridge, err := newGatewayRuntimePortBridge(nil, &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent)}, testSessionStore)
	if err != nil {
		t.Fatalf("new nil-ctx bridge: %v", err)
	}
	if err := nilCtxBridge.Close(); err != nil {
		t.Fatalf("close nil-ctx bridge: %v", err)
	}
}

func TestConvertGatewayRunInputAndSessionHelpers(t *testing.T) {
	converted := convertGatewayRunInput(gateway.RunInput{
		RequestID: " req-1 ",
		SessionID: " session-1 ",
		InputText: " base ",
		InputParts: []gateway.InputPart{
			{Type: gateway.InputPartTypeText, Text: "  text  "},
			{Type: gateway.InputPartTypeImage, Media: nil},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: "   "}},
			{Type: gateway.InputPartTypeImage, Media: &gateway.Media{URI: " /tmp/a.png ", MimeType: " image/png "}},
		},
		Workdir: " /tmp/work ",
	})
	if converted.RunID != "req-1" {
		t.Fatalf("run_id = %q, want request id fallback %q", converted.RunID, "req-1")
	}
	if converted.Text != "base\ntext" {
		t.Fatalf("text = %q, want %q", converted.Text, "base\ntext")
	}
	if len(converted.Images) != 1 || converted.Images[0].Path != "/tmp/a.png" {
		t.Fatalf("images = %#v, want one valid image", converted.Images)
	}

	if got := renderSessionMessageContent(nil); got != "" {
		t.Fatalf("render nil parts = %q, want empty", got)
	}
	parts := []providertypes.ContentPart{
		{Kind: providertypes.ContentPartText, Text: "  "},
		{Kind: providertypes.ContentPartText, Text: " line "},
		{Kind: providertypes.ContentPartImage},
	}
	if got := renderSessionMessageContent(parts); got != "line\n[image]" {
		t.Fatalf("rendered parts = %q, want %q", got, "line\n[image]")
	}

	if messages := convertSessionMessages(nil); messages != nil {
		t.Fatalf("convert nil messages = %#v, want nil", messages)
	}
}

func TestGatewayRuntimePortBridgeDeleteSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := &bridgeSessionStoreStub{
			deleteFn: func(_ context.Context, id string) error {
				if id != "session-1" {
					t.Fatalf("id = %q, want %q", id, "session-1")
				}
				return nil
			},
		}

		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, store)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		result, err := bridge.DeleteSession(context.Background(), gateway.DeleteSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "session-1",
		})
		if err != nil {
			t.Fatalf("delete session: %v", err)
		}
		if !result {
			t.Fatal("expected deleted=true")
		}
	})

	t.Run("empty session id returns error", func(t *testing.T) {
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		_, err = bridge.DeleteSession(context.Background(), gateway.DeleteSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "  ",
		})
		if err == nil {
			t.Fatal("expected error for empty session_id")
		}
	})

	t.Run("nil session store returns error", func(t *testing.T) {
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, nil)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		_, err = bridge.DeleteSession(context.Background(), gateway.DeleteSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "session-1",
		})
		if err == nil {
			t.Fatal("expected error for nil session store")
		}
	})
}

func TestGatewayRuntimePortBridgeRenameSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := &bridgeSessionStoreStub{
			updateFn: func(_ context.Context, input agentsession.UpdateSessionStateInput) error {
				if input.SessionID != "session-1" {
					t.Fatalf("id = %q, want %q", input.SessionID, "session-1")
				}
				if input.Title != "New Title" {
					t.Fatalf("title = %q, want %q", input.Title, "New Title")
				}
				return nil
			},
		}

		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, store)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		err = bridge.RenameSession(context.Background(), gateway.RenameSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "session-1",
			Title:     "New Title",
		})
		if err != nil {
			t.Fatalf("rename session: %v", err)
		}
	})

	t.Run("empty title returns error", func(t *testing.T) {
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		err = bridge.RenameSession(context.Background(), gateway.RenameSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "session-1",
			Title:     "  ",
		})
		if err == nil {
			t.Fatal("expected error for empty title")
		}
	})

	t.Run("empty session id returns error", func(t *testing.T) {
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
		if err != nil {
			t.Fatalf("new bridge: %v", err)
		}
		defer bridge.Close()

		err = bridge.RenameSession(context.Background(), gateway.RenameSessionInput{
			SubjectID: testBridgeSubjectID,
			SessionID: "  ",
			Title:     "New Title",
		})
		if err == nil {
			t.Fatal("expected error for empty session_id")
		}
	})
}

// ---- providerSelection stub ----

type providerSelectionStub struct {
	listOptions []configstate.ProviderOption
	listErr     error
	createRes   configstate.Selection
	createErr   error
	selectRes   configstate.Selection
	selectErr   error
	setModelRes configstate.Selection
	setModelErr error
}

func (s *providerSelectionStub) ListProviderOptions(_ context.Context) ([]configstate.ProviderOption, error) {
	return s.listOptions, s.listErr
}
func (s *providerSelectionStub) CreateCustomProvider(_ context.Context, _ configstate.CreateCustomProviderInput) (configstate.Selection, error) {
	return s.createRes, s.createErr
}
func (s *providerSelectionStub) SelectProvider(_ context.Context, _ string) (configstate.Selection, error) {
	return s.selectRes, s.selectErr
}
func (s *providerSelectionStub) SetCurrentModel(_ context.Context, _ string) (configstate.Selection, error) {
	return s.setModelRes, s.setModelErr
}

var _ providerSelectionInterface = (*providerSelectionStub)(nil)

type providerSelectionInterface interface {
	ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error)
	CreateCustomProvider(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
	SelectProvider(ctx context.Context, providerName string) (configstate.Selection, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
}

// ---- configManager stub ----

type configManagerStub struct {
	cfg      config.Config
	loadRes  config.Config
	loadErr  error
	updateFn func(func(*config.Config) error) error
}

func (s *configManagerStub) Get() config.Config {
	return s.cfg.Clone()
}
func (s *configManagerStub) Load(_ context.Context) (config.Config, error) {
	return s.loadRes, s.loadErr
}
func (s *configManagerStub) Update(_ context.Context, mutate func(*config.Config) error) error {
	if s.updateFn != nil {
		return s.updateFn(mutate)
	}
	return mutate(&s.cfg)
}
func (s *configManagerStub) BaseDir() string {
	return ""
}

// ---- DeleteSession ----

func TestGatewayRuntimePortBridgeDeleteSessionStoreError(t *testing.T) {
	store := &bridgeSessionStoreStub{
		deleteFn: func(_ context.Context, _ string) error {
			return errors.New("delete failed")
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, store)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	_, err = bridge.DeleteSession(context.Background(), gateway.DeleteSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "session-1",
	})
	if err == nil || err.Error() != "delete failed" {
		t.Fatalf("expected delete failed error, got %v", err)
	}
}

// ---- RenameSession ----

func TestGatewayRuntimePortBridgeRenameSessionStoreNil(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, nil)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	err = bridge.RenameSession(context.Background(), gateway.RenameSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "session-1",
		Title:     "New Title",
	})
	if err == nil || !strings.Contains(err.Error(), "session store is unavailable") {
		t.Fatalf("expected session store unavailable error, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeRenameSessionUpdateError(t *testing.T) {
	store := &bridgeSessionStoreStub{
		updateFn: func(_ context.Context, _ agentsession.UpdateSessionStateInput) error {
			return errors.New("update failed")
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, store)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	err = bridge.RenameSession(context.Background(), gateway.RenameSessionInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "session-1",
		Title:     "New Title",
	})
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected update failed error, got %v", err)
	}
}

// ---- ListFiles ----

func TestResolveListFilesRootPriorities(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// priority 1: input.Workdir
	bridge1, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, testSessionStore)
	defer bridge1.Close()
	root, err := bridge1.resolveListFilesRoot(context.Background(), gateway.ListFilesInput{Workdir: subDir})
	if err != nil {
		t.Fatalf("resolve with workdir: %v", err)
	}
	if root != subDir {
		t.Fatalf("root = %q, want %q", root, subDir)
	}

	// priority 2: session workdir (store implements bridgeSessionLoader)
	loaderStore := &bridgeSessionStoreWithLoader{
		bridgeSessionStoreStub: bridgeSessionStoreStub{},
		session: agentsession.Session{Workdir: subDir},
	}
	bridge2, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, loaderStore)
	defer bridge2.Close()
	root, err = bridge2.resolveListFilesRoot(context.Background(), gateway.ListFilesInput{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("resolve with session: %v", err)
	}
	if root != subDir {
		t.Fatalf("root = %q, want %q", root, subDir)
	}

	// priority 3: config workdir
	cfgMgr := &configManagerStub{cfg: config.Config{Workdir: subDir}}
	bridge3, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, testSessionStore, cfgMgr, nil)
	defer bridge3.Close()
	root, err = bridge3.resolveListFilesRoot(context.Background(), gateway.ListFilesInput{})
	if err != nil {
		t.Fatalf("resolve with config: %v", err)
	}
	if root != subDir {
		t.Fatalf("root = %q, want %q", root, subDir)
	}

	// priority 4: os.Getwd
	bridge4, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, testSessionStore)
	defer bridge4.Close()
	root, err = bridge4.resolveListFilesRoot(context.Background(), gateway.ListFilesInput{})
	if err != nil {
		t.Fatalf("resolve with getwd: %v", err)
	}
	wd, _ := os.Getwd()
	absWd, _ := filepath.Abs(wd)
	if root != filepath.Clean(absWd) {
		t.Fatalf("root = %q, want %q", root, filepath.Clean(absWd))
	}
}

type bridgeSessionStoreWithLoader struct {
	bridgeSessionStoreStub
	session agentsession.Session
	loadErr error
}

func (s *bridgeSessionStoreWithLoader) LoadSession(_ context.Context, _ string) (agentsession.Session, error) {
	return s.session, s.loadErr
}

func TestResolveListFilesRootSessionNotFound(t *testing.T) {
	loaderStore := &bridgeSessionStoreWithLoader{
		bridgeSessionStoreStub: bridgeSessionStoreStub{},
		loadErr:                agentsession.ErrSessionNotFound,
	}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, loaderStore)
	defer bridge.Close()

	root, err := bridge.resolveListFilesRoot(context.Background(), gateway.ListFilesInput{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("resolve with not-found session should not error: %v", err)
	}
	wd, _ := os.Getwd()
	absWd, _ := filepath.Abs(wd)
	if root != filepath.Clean(absWd) {
		t.Fatalf("root = %q, want %q", root, filepath.Clean(absWd))
	}
}

func TestGatewayRuntimePortBridgeListFilesReadDirFail(t *testing.T) {
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, testSessionStore)
	defer bridge.Close()

	_, err := bridge.ListFiles(context.Background(), gateway.ListFilesInput{
		SubjectID: testBridgeSubjectID,
		Workdir:   t.TempDir(),
		Path:      "nonexistent-dir",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestGatewayRuntimePortBridgeListFilesFiltersAndSorts(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "A.txt"), []byte("a"), 0644)
	_ = os.MkdirAll(filepath.Join(tmpDir, "Zdir"), 0755)
	_ = os.MkdirAll(filepath.Join(tmpDir, "adir"), 0755)

	bridge, _ := newGatewayRuntimePortBridge(context.Background(), &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}, testSessionStore)
	defer bridge.Close()

	entries, err := bridge.ListFiles(context.Background(), gateway.ListFilesInput{
		SubjectID: testBridgeSubjectID,
		Workdir:   tmpDir,
	})
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries count = %d, want 4", len(entries))
	}
	// dirs first, then case-insensitive sort
	if !entries[0].IsDir || entries[0].Name != "adir" {
		t.Fatalf("entries[0] = %+v, want adir dir", entries[0])
	}
	if !entries[1].IsDir || entries[1].Name != "Zdir" {
		t.Fatalf("entries[1] = %+v, want Zdir dir", entries[1])
	}
	if entries[2].Name != "A.txt" {
		t.Fatalf("entries[2] = %+v, want A.txt", entries[2])
	}
	if entries[3].Name != "b.txt" {
		t.Fatalf("entries[3] = %+v, want b.txt", entries[3])
	}
}

// ---- ListModels ----

func TestGatewayRuntimePortBridgeListModelsNilSelection(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	_, err := bridge.ListModels(context.Background(), gateway.ListModelsInput{SubjectID: testBridgeSubjectID})
	if err == nil || !strings.Contains(err.Error(), "provider selection is unavailable") {
		t.Fatalf("expected provider selection unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeListModelsListError(t *testing.T) {
	ps := &providerSelectionStub{listErr: errors.New("list options failed")}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
	defer bridge.Close()

	_, err := bridge.ListModels(context.Background(), gateway.ListModelsInput{SubjectID: testBridgeSubjectID})
	if err == nil || err.Error() != "list options failed" {
		t.Fatalf("expected list options failed, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeListModelsNameFallback(t *testing.T) {
	ps := &providerSelectionStub{
		listOptions: []configstate.ProviderOption{
			{
				ID:   "openai",
				Name: "OpenAI",
				Models: []providertypes.ModelDescriptor{
					{ID: "gpt-4", Name: ""},
					{ID: "  ", Name: "ignored"},
				},
			},
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
	defer bridge.Close()

	models, err := bridge.ListModels(context.Background(), gateway.ListModelsInput{SubjectID: testBridgeSubjectID})
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models count = %d, want 1", len(models))
	}
	if models[0].ID != "gpt-4" || models[0].Name != "gpt-4" {
		t.Fatalf("model = %+v, want id=gpt-4 name=gpt-4", models[0])
	}
}

// ---- SetSessionModel ----

func TestGatewayRuntimePortBridgeSetSessionModelStoreNil(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, nil)
	defer bridge.Close()

	err := bridge.SetSessionModel(context.Background(), gateway.SetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		ModelID:   "gpt-4",
	})
	if err == nil || !strings.Contains(err.Error(), "session store is unavailable") {
		t.Fatalf("expected session store unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeSetSessionModelLoadFail(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{loadErr: errors.New("load failed")}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store)
	defer bridge.Close()

	err := bridge.SetSessionModel(context.Background(), gateway.SetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		ModelID:   "gpt-4",
	})
	if err == nil || err.Error() != "load failed" {
		t.Fatalf("expected load failed, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeSetSessionModelProviderInference(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{
		session: agentsession.Session{ID: "s-1", Provider: "", Model: ""},
	}
	ps := &providerSelectionStub{
		listOptions: []configstate.ProviderOption{
			{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "GPT-4"}}},
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store, nil, ps)
	defer bridge.Close()

	err := bridge.SetSessionModel(context.Background(), gateway.SetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		ModelID:   "gpt-4",
	})
	if err != nil {
		t.Fatalf("set session model: %v", err)
	}
}

func TestGatewayRuntimePortBridgeSetSessionModelNotFound(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{
		session: agentsession.Session{ID: "s-1", Provider: "openai", Model: ""},
	}
	ps := &providerSelectionStub{
		listOptions: []configstate.ProviderOption{
			{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-3", Name: "GPT-3"}}},
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store, nil, ps)
	defer bridge.Close()

	err := bridge.SetSessionModel(context.Background(), gateway.SetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
		ModelID:   "gpt-4",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected model not found, got %v", err)
	}
}

// ---- GetSessionModel ----

func TestGatewayRuntimePortBridgeGetSessionModelStoreNil(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, nil)
	defer bridge.Close()

	_, err := bridge.GetSessionModel(context.Background(), gateway.GetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err == nil || !strings.Contains(err.Error(), "session store is unavailable") {
		t.Fatalf("expected session store unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeGetSessionModelLoadFail(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{loadErr: errors.New("load failed")}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store)
	defer bridge.Close()

	_, err := bridge.GetSessionModel(context.Background(), gateway.GetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err == nil || err.Error() != "load failed" {
		t.Fatalf("expected load failed, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeGetSessionModelConfigFallback(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{
		session: agentsession.Session{ID: "s-1", Provider: "", Model: ""},
	}
	cfgMgr := &configManagerStub{
		cfg: config.Config{SelectedProvider: "openai", CurrentModel: "gpt-4"},
	}
	ps := &providerSelectionStub{
		listOptions: []configstate.ProviderOption{
			{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "GPT-4"}}},
		},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store, cfgMgr, ps)
	defer bridge.Close()

	res, err := bridge.GetSessionModel(context.Background(), gateway.GetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err != nil {
		t.Fatalf("get session model: %v", err)
	}
	if res.ProviderID != "openai" || res.ModelID != "gpt-4" || res.ModelName != "GPT-4" {
		t.Fatalf("result = %+v, want openai/gpt-4/GPT-4", res)
	}
}

func TestGatewayRuntimePortBridgeGetSessionModelDisplayNameNotFound(t *testing.T) {
	store := &bridgeSessionStoreWithLoader{
		session: agentsession.Session{ID: "s-1", Provider: "openai", Model: "gpt-4"},
	}
	ps := &providerSelectionStub{listOptions: []configstate.ProviderOption{}}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, store, nil, ps)
	defer bridge.Close()

	res, err := bridge.GetSessionModel(context.Background(), gateway.GetSessionModelInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err != nil {
		t.Fatalf("get session model: %v", err)
	}
	if res.ModelName != "gpt-4" {
		t.Fatalf("model name = %q, want %q", res.ModelName, "gpt-4")
	}
}

// ---- ListProviders ----

func TestGatewayRuntimePortBridgeListProvidersNilSelection(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	_, err := bridge.ListProviders(context.Background(), gateway.ListProvidersInput{SubjectID: testBridgeSubjectID})
	if err == nil || !strings.Contains(err.Error(), "provider selection is unavailable") {
		t.Fatalf("expected provider selection unavailable, got %v", err)
	}
}

// ---- CreateProvider ----

func TestGatewayRuntimePortBridgeCreateProviderNilSelection(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	_, err := bridge.CreateProvider(context.Background(), gateway.CreateProviderInput{SubjectID: testBridgeSubjectID})
	if err == nil || !strings.Contains(err.Error(), "provider selection is unavailable") {
		t.Fatalf("expected provider selection unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeCreateProviderError(t *testing.T) {
	ps := &providerSelectionStub{createErr: errors.New("create failed")}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
	defer bridge.Close()

	_, err := bridge.CreateProvider(context.Background(), gateway.CreateProviderInput{SubjectID: testBridgeSubjectID})
	if err == nil || err.Error() != "create failed" {
		t.Fatalf("expected create failed, got %v", err)
	}
}

// ---- DeleteProvider ----

func TestGatewayRuntimePortBridgeDeleteProviderNilConfigManager(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	err := bridge.DeleteProvider(context.Background(), gateway.DeleteProviderInput{SubjectID: testBridgeSubjectID, ProviderID: "custom"})
	if err == nil || !strings.Contains(err.Error(), "config manager is unavailable") {
		t.Fatalf("expected config manager unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeDeleteProviderBuiltinRejection(t *testing.T) {
	cfgMgr := &configManagerStub{
		cfg: config.Config{Providers: []config.ProviderConfig{config.OpenAIProvider()}},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, cfgMgr, nil)
	defer bridge.Close()

	err := bridge.DeleteProvider(context.Background(), gateway.DeleteProviderInput{SubjectID: testBridgeSubjectID, ProviderID: "openai"})
	if err == nil || !strings.Contains(err.Error(), "builtin provider") {
		t.Fatalf("expected builtin provider rejection, got %v", err)
	}
}

// ---- SelectProviderModel ----

func TestGatewayRuntimePortBridgeSelectProviderModelNilSelection(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	_, err := bridge.SelectProviderModel(context.Background(), gateway.SelectProviderModelInput{SubjectID: testBridgeSubjectID})
	if err == nil || !strings.Contains(err.Error(), "provider selection is unavailable") {
		t.Fatalf("expected provider selection unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeSelectProviderModelSelectError(t *testing.T) {
	ps := &providerSelectionStub{selectErr: errors.New("select failed")}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
	defer bridge.Close()

	_, err := bridge.SelectProviderModel(context.Background(), gateway.SelectProviderModelInput{SubjectID: testBridgeSubjectID, ProviderID: "openai"})
	if err == nil || err.Error() != "select failed" {
		t.Fatalf("expected select failed, got %v", err)
	}
}

// ---- UpsertMCPServer ----

func TestGatewayRuntimePortBridgeUpsertMCPServerNilConfigManager(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	err := bridge.UpsertMCPServer(context.Background(), gateway.UpsertMCPServerInput{
		SubjectID: testBridgeSubjectID,
		Server:    config.MCPServerConfig{ID: "srv-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "config manager is unavailable") {
		t.Fatalf("expected config manager unavailable, got %v", err)
	}
}

// ---- SetMCPServerEnabled ----

func TestGatewayRuntimePortBridgeSetMCPServerEnabledNilConfigManager(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	err := bridge.SetMCPServerEnabled(context.Background(), gateway.SetMCPServerEnabledInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-1",
		Enabled:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "config manager is unavailable") {
		t.Fatalf("expected config manager unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeSetMCPServerEnabledNotFound(t *testing.T) {
	cfgMgr := &configManagerStub{
		cfg: config.Config{Tools: config.ToolsConfig{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{}}}},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, cfgMgr, nil)
	defer bridge.Close()

	err := bridge.SetMCPServerEnabled(context.Background(), gateway.SetMCPServerEnabledInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-1",
		Enabled:   true,
	})
	if err == nil || !errors.Is(err, gateway.ErrRuntimeResourceNotFound) {
		t.Fatalf("expected ErrRuntimeResourceNotFound, got %v", err)
	}
}

// ---- DeleteMCPServer ----

func TestGatewayRuntimePortBridgeDeleteMCPServerNilConfigManager(t *testing.T) {
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	defer bridge.Close()

	err := bridge.DeleteMCPServer(context.Background(), gateway.DeleteMCPServerInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-1",
	})
	if err == nil || !strings.Contains(err.Error(), "config manager is unavailable") {
		t.Fatalf("expected config manager unavailable, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeDeleteMCPServerNotFound(t *testing.T) {
	cfgMgr := &configManagerStub{
		cfg: config.Config{Tools: config.ToolsConfig{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{}}}},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, cfgMgr, nil)
	defer bridge.Close()

	err := bridge.DeleteMCPServer(context.Background(), gateway.DeleteMCPServerInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-1",
	})
	if err == nil || !errors.Is(err, gateway.ErrRuntimeResourceNotFound) {
		t.Fatalf("expected ErrRuntimeResourceNotFound, got %v", err)
	}
}

// ---- internal helpers ----

func TestResolveProviderModelForSession(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		ps := &providerSelectionStub{
			listOptions: []configstate.ProviderOption{
				{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "GPT-4"}}},
			},
		}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		pid, mid, err := bridge.resolveProviderModelForSession(context.Background(), agentsession.Session{Provider: "openai"}, "openai", "gpt-4")
		if err != nil {
			t.Fatalf("exact match: %v", err)
		}
		if pid != "openai" || mid != "gpt-4" {
			t.Fatalf("pid=%q mid=%q, want openai/gpt-4", pid, mid)
		}
	})

	t.Run("inferred provider", func(t *testing.T) {
		ps := &providerSelectionStub{
			listOptions: []configstate.ProviderOption{
				{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "GPT-4"}}},
			},
		}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		pid, mid, err := bridge.resolveProviderModelForSession(context.Background(), agentsession.Session{}, "", "gpt-4")
		if err != nil {
			t.Fatalf("inferred provider: %v", err)
		}
		if pid != "openai" || mid != "gpt-4" {
			t.Fatalf("pid=%q mid=%q, want openai/gpt-4", pid, mid)
		}
	})

	t.Run("model not found", func(t *testing.T) {
		ps := &providerSelectionStub{listOptions: []configstate.ProviderOption{}}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		_, _, err := bridge.resolveProviderModelForSession(context.Background(), agentsession.Session{Provider: "openai"}, "openai", "gpt-4")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected model not found, got %v", err)
		}
	})
}

func TestModelDisplayName(t *testing.T) {
	t.Run("provider filter", func(t *testing.T) {
		ps := &providerSelectionStub{
			listOptions: []configstate.ProviderOption{
				{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "GPT-4"}}},
				{ID: "gemini", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: "Gemini-GPT-4"}}},
			},
		}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		name := bridge.modelDisplayName(context.Background(), "openai", "gpt-4")
		if name != "GPT-4" {
			t.Fatalf("name = %q, want %q", name, "GPT-4")
		}
	})

	t.Run("name empty fallback", func(t *testing.T) {
		ps := &providerSelectionStub{
			listOptions: []configstate.ProviderOption{
				{ID: "openai", Models: []providertypes.ModelDescriptor{{ID: "gpt-4", Name: ""}}},
			},
		}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		name := bridge.modelDisplayName(context.Background(), "openai", "gpt-4")
		if name != "gpt-4" {
			t.Fatalf("name = %q, want %q", name, "gpt-4")
		}
	})

	t.Run("not found fallback to modelID", func(t *testing.T) {
		ps := &providerSelectionStub{listOptions: []configstate.ProviderOption{}}
		stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
		bridge, _ := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, nil, ps)
		defer bridge.Close()

		name := bridge.modelDisplayName(context.Background(), "openai", "gpt-4")
		if name != "gpt-4" {
			t.Fatalf("name = %q, want %q", name, "gpt-4")
		}
	})
}

func TestGatewayRuntimePortBridgeCancelRunAndSnapshots(t *testing.T) {
	stub := &runtimeStub{
		eventsCh:      make(chan agentruntime.RuntimeEvent, 1),
		cancelReturn:  true,
		listTodosErr:  errors.New("todo failed"),
		getSnapshotErr: errors.New("snapshot failed"),
	}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	ok, err := bridge.CancelRun(context.Background(), gateway.CancelInput{SubjectID: testBridgeSubjectID, RunID: "run-1"})
	if err != nil || !ok {
		t.Fatalf("cancel run = (%v,%v), want (true,nil)", ok, err)
	}
	stub.cancelReturn = false
	ok, err = bridge.CancelRun(context.Background(), gateway.CancelInput{SubjectID: testBridgeSubjectID, RunID: "run-2"})
	if !errors.Is(err, gateway.ErrRuntimeResourceNotFound) || ok {
		t.Fatalf("cancel run = (%v,%v), want (false,ErrRuntimeResourceNotFound)", ok, err)
	}

	_, err = bridge.ListSessionTodos(context.Background(), gateway.ListSessionTodosInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err == nil || err.Error() != "todo failed" {
		t.Fatalf("expected todo failed, got %v", err)
	}
	_, err = bridge.GetRuntimeSnapshot(context.Background(), gateway.GetRuntimeSnapshotInput{
		SubjectID: testBridgeSubjectID,
		SessionID: "s-1",
	})
	if err == nil || err.Error() != "snapshot failed" {
		t.Fatalf("expected snapshot failed, got %v", err)
	}
}

func TestGatewayRuntimePortBridgeProviderAndMCPHappyPaths(t *testing.T) {
	cfgMgr := &configManagerStub{
		cfg: config.Config{
			SelectedProvider: "openai",
			Providers: []config.ProviderConfig{
				{Name: "openai", Driver: "openai", APIKeyEnv: "OPENAI_API_KEY", Source: config.ProviderSourceBuiltin},
			},
			Tools: config.ToolsConfig{MCP: config.MCPConfig{
				Servers: []config.MCPServerConfig{{ID: "srv-1", Enabled: false}},
			}},
		},
	}
	ps := &providerSelectionStub{
		listOptions: []configstate.ProviderOption{{
			ID:   "openai",
			Name: "OpenAI",
			Models: []providertypes.ModelDescriptor{
				{ID: "gpt-4.1", Name: "GPT-4.1"},
			},
		}},
		createRes:   configstate.Selection{ProviderID: "custom-a", ModelID: "m1"},
		selectRes:   configstate.Selection{ProviderID: "openai", ModelID: "gpt-4.1"},
		setModelRes: configstate.Selection{ProviderID: "openai", ModelID: "gpt-4o"},
	}
	stub := &runtimeStub{eventsCh: make(chan agentruntime.RuntimeEvent, 1)}
	bridge, err := newGatewayRuntimePortBridge(context.Background(), stub, testSessionStore, cfgMgr, ps)
	if err != nil {
		t.Fatalf("new bridge: %v", err)
	}
	defer bridge.Close()

	providers, err := bridge.ListProviders(context.Background(), gateway.ListProvidersInput{SubjectID: testBridgeSubjectID})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if len(providers) != 1 || !providers[0].Selected || providers[0].Driver != "openai" {
		t.Fatalf("providers = %+v, want selected openai provider", providers)
	}

	createRes, err := bridge.CreateProvider(context.Background(), gateway.CreateProviderInput{
		SubjectID: testBridgeSubjectID,
		Name:      " custom-a ",
		Models: []providertypes.ModelDescriptor{
			{ID: " m1 ", Name: " ", ContextWindow: 1000, MaxOutputTokens: 200},
		},
	})
	if err != nil || createRes.ProviderID != "custom-a" || createRes.ModelID != "m1" {
		t.Fatalf("create provider = (%+v,%v), want custom-a/m1,nil", createRes, err)
	}

	selectRes, err := bridge.SelectProviderModel(context.Background(), gateway.SelectProviderModelInput{
		SubjectID:  testBridgeSubjectID,
		ProviderID: "openai",
		ModelID:    "gpt-4o",
	})
	if err != nil || selectRes.ModelID != "gpt-4o" {
		t.Fatalf("select provider model = (%+v,%v), want model gpt-4o", selectRes, err)
	}

	servers, err := bridge.ListMCPServers(context.Background(), gateway.ListMCPServersInput{SubjectID: testBridgeSubjectID})
	if err != nil || len(servers) != 1 || servers[0].ID != "srv-1" {
		t.Fatalf("list mcp servers = (%+v,%v), want srv-1", servers, err)
	}
	if err := bridge.UpsertMCPServer(context.Background(), gateway.UpsertMCPServerInput{
		SubjectID: testBridgeSubjectID,
		Server:    config.MCPServerConfig{ID: " srv-2 ", Enabled: true},
	}); err != nil {
		t.Fatalf("upsert mcp server: %v", err)
	}
	if err := bridge.SetMCPServerEnabled(context.Background(), gateway.SetMCPServerEnabledInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-1",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("set mcp enabled: %v", err)
	}
	if err := bridge.DeleteMCPServer(context.Background(), gateway.DeleteMCPServerInput{
		SubjectID: testBridgeSubjectID,
		ID:        "srv-2",
	}); err != nil {
		t.Fatalf("delete mcp server: %v", err)
	}
}

func TestMarshalManualModelsForGateway(t *testing.T) {
	if got := marshalManualModelsForGateway(nil); got != "" {
		t.Fatalf("expected empty string for nil models, got %q", got)
	}
	raw := marshalManualModelsForGateway([]providertypes.ModelDescriptor{
		{ID: " ", Name: "invalid"},
		{ID: " m1 ", Name: " ", ContextWindow: 1000, MaxOutputTokens: 200},
	})
	if raw == "" {
		t.Fatal("expected non-empty json payload")
	}
	var payload []map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("payload len = %d, want 1", len(payload))
	}
	if payload[0]["id"] != "m1" || payload[0]["name"] != "m1" {
		t.Fatalf("payload[0] = %+v, want id/name m1", payload[0])
	}
}
